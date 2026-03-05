package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	badger "github.com/dgraph-io/badger/v4"
	"github.com/dgraph-io/badger/v4/options"
	"github.com/oklog/ulid/v2"

	"github.com/syst3mctl/crashctl/internal/domain"
)

// Key-schema prefixes. Colons are used as delimiters; K8s namespaces and
// project IDs never contain colons so the schema is unambiguous.
const (
	pfxEvent        = "e:"  // e:{projectID}:{ts20}:{eventID}
	pfxEventLookup  = "ei:" // ei:{projectID}:{eventID} → ts20
	pfxGroup        = "g:"  // g:{projectID}:{groupID}
	pfxFingerprint  = "f:"  // f:{projectID}:{fingerprint} → groupID
	pfxCrash        = "c:"  // c:{namespace}:{ts20}:{crashID}
	pfxCrashLookup  = "ci:" // ci:{crashID} → namespace:ts20
	pfxProject      = "p:"  // p:{projectID}
	pfxDSN          = "d:"  // d:{dsnKey} → projectID
)

// BadgerStore is a production-grade Store backed by BadgerDB v4.
// It is safe for concurrent use.
type BadgerStore struct {
	db     *badger.DB
	stopGC chan struct{}
}

// Open opens (or creates) a BadgerDB database at path and starts the
// background GC goroutine. Caller must call Close when done.
func Open(path string) (*BadgerStore, error) {
	opts := badger.DefaultOptions(path)
	opts.Compression = options.ZSTD
	opts.Logger = nil // slog is used at the application level
	opts.SyncWrites = false

	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("open badger at %s: %w", path, err)
	}

	s := &BadgerStore{db: db, stopGC: make(chan struct{})}
	go s.startGC()
	return s, nil
}

// Close stops the GC goroutine and closes the underlying database.
func (s *BadgerStore) Close() error {
	close(s.stopGC)
	return s.db.Close()
}

// startGC runs BadgerDB value-log GC every 5 minutes.
func (s *BadgerStore) startGC() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := s.db.RunValueLogGC(0.5); err != nil && err != badger.ErrNoRewrite {
				slog.Warn("badger GC", "err", err)
			}
		case <-s.stopGC:
			return
		}
	}
}

// ---------------------------------------------------------------------------
// Key builders
// ---------------------------------------------------------------------------

// ts20 formats a Unix nanosecond timestamp as a zero-padded 20-digit string
// so that lexicographic order equals chronological order.
func ts20(t time.Time) string {
	return fmt.Sprintf("%020d", t.UnixNano())
}

func eventKey(projectID string, t time.Time, id ulid.ULID) []byte {
	return []byte(pfxEvent + projectID + ":" + ts20(t) + ":" + id.String())
}

func eventLookupKey(projectID string, id ulid.ULID) []byte {
	return []byte(pfxEventLookup + projectID + ":" + id.String())
}

func groupKey(projectID string, id ulid.ULID) []byte {
	return []byte(pfxGroup + projectID + ":" + id.String())
}

func fpKey(projectID, fingerprint string) []byte {
	return []byte(pfxFingerprint + projectID + ":" + fingerprint)
}

func crashKey(namespace string, t time.Time, id ulid.ULID) []byte {
	return []byte(pfxCrash + namespace + ":" + ts20(t) + ":" + id.String())
}

func crashLookupKey(id ulid.ULID) []byte {
	return []byte(pfxCrashLookup + id.String())
}

func projectKey(id string) []byte {
	return []byte(pfxProject + id)
}

func dsnIndexKey(dsnKey string) []byte {
	return []byte(pfxDSN + dsnKey)
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// reverseIter executes fn for every key that starts with prefix, iterating
// from newest to oldest (reverse lexicographic order).
func reverseIter(txn *badger.Txn, prefix string, fn func(item *badger.Item) error) error {
	pfx := []byte(prefix)
	opts := badger.DefaultIteratorOptions
	opts.Reverse = true
	it := txn.NewIterator(opts)
	defer it.Close()

	// Seek to just past the last possible key for this prefix.
	seekKey := append([]byte(prefix), 0xFF)
	for it.Seek(seekKey); it.ValidForPrefix(pfx); it.Next() {
		if err := fn(it.Item()); err != nil {
			return err
		}
	}
	return nil
}

// getValue reads and JSON-decodes the value of item into dst.
func getValue(item *badger.Item, dst any) error {
	return item.Value(func(val []byte) error {
		return json.Unmarshal(val, dst)
	})
}

// marshal JSON-encodes v, wrapping any error with context.
func marshal(v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal %T: %w", v, err)
	}
	return data, nil
}

// ---------------------------------------------------------------------------
// Event operations
// ---------------------------------------------------------------------------

// SaveEvent persists the event under its time-ordered primary key and writes a
// secondary lookup entry so GetEvent can resolve it by ID alone.
func (s *BadgerStore) SaveEvent(ctx context.Context, event *domain.Event) error {
	data, err := marshal(event)
	if err != nil {
		return fmt.Errorf("save event %s: %w", event.ID, err)
	}
	return s.db.Update(func(txn *badger.Txn) error {
		if err := txn.Set(eventKey(event.ProjectID, event.Timestamp, event.ID), data); err != nil {
			return fmt.Errorf("save event %s primary: %w", event.ID, err)
		}
		if err := txn.Set(eventLookupKey(event.ProjectID, event.ID), []byte(ts20(event.Timestamp))); err != nil {
			return fmt.Errorf("save event %s lookup: %w", event.ID, err)
		}
		return nil
	})
}

// GetEvent retrieves a single event by project and ID.
func (s *BadgerStore) GetEvent(ctx context.Context, projectID string, id ulid.ULID) (*domain.Event, error) {
	var event domain.Event
	err := s.db.View(func(txn *badger.Txn) error {
		// Resolve timestamp from secondary index.
		lkItem, err := txn.Get(eventLookupKey(projectID, id))
		if err == badger.ErrKeyNotFound {
			return fmt.Errorf("get event %s: %w", id, ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("get event %s lookup: %w", id, err)
		}
		ts20val, err := lkItem.ValueCopy(nil)
		if err != nil {
			return fmt.Errorf("get event %s lookup value: %w", id, err)
		}
		var tsNano int64
		if _, err = fmt.Sscanf(string(ts20val), "%d", &tsNano); err != nil {
			return fmt.Errorf("get event %s parse ts: %w", id, err)
		}

		// Retrieve the primary record.
		t := time.Unix(0, tsNano)
		item, err := txn.Get(eventKey(projectID, t, id))
		if err == badger.ErrKeyNotFound {
			return fmt.Errorf("get event %s: %w", id, ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("get event %s: %w", id, err)
		}
		return getValue(item, &event)
	})
	if err != nil {
		return nil, err
	}
	return &event, nil
}

// ListEvents returns events newest-first, optionally filtered and paginated.
func (s *BadgerStore) ListEvents(ctx context.Context, opts ListEventsOpts) ([]*domain.Event, error) {
	prefix := pfxEvent
	if opts.ProjectID != "" {
		prefix = pfxEvent + opts.ProjectID + ":"
	}

	var results []*domain.Event
	err := s.db.View(func(txn *badger.Txn) error {
		return reverseIter(txn, prefix, func(item *badger.Item) error {
			var e domain.Event
			if err := getValue(item, &e); err != nil {
				return fmt.Errorf("list events decode: %w", err)
			}
			if opts.Level != nil && e.Level != *opts.Level {
				return nil
			}
			results = append(results, &e)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	// Reverse iteration already yields newest-first for a single-project prefix.
	// If no project filter, keys are ordered by projectID then time, so sort
	// globally by timestamp descending.
	if opts.ProjectID == "" {
		sort.Slice(results, func(i, j int) bool {
			return results[i].Timestamp.After(results[j].Timestamp)
		})
	}
	return applyPage(results, opts.Offset, opts.Limit), nil
}

// ---------------------------------------------------------------------------
// ErrorGroup operations
// ---------------------------------------------------------------------------

// SaveGroup persists an ErrorGroup and updates the fingerprint index.
func (s *BadgerStore) SaveGroup(ctx context.Context, group *domain.ErrorGroup) error {
	data, err := marshal(group)
	if err != nil {
		return fmt.Errorf("save group %s: %w", group.ID, err)
	}
	return s.db.Update(func(txn *badger.Txn) error {
		if err := txn.Set(groupKey(group.ProjectID, group.ID), data); err != nil {
			return fmt.Errorf("save group %s primary: %w", group.ID, err)
		}
		if err := txn.Set(fpKey(group.ProjectID, group.Fingerprint), []byte(group.ID.String())); err != nil {
			return fmt.Errorf("save group %s fingerprint: %w", group.ID, err)
		}
		return nil
	})
}

// GetGroup retrieves an ErrorGroup by project and ID.
func (s *BadgerStore) GetGroup(ctx context.Context, projectID string, id ulid.ULID) (*domain.ErrorGroup, error) {
	var g domain.ErrorGroup
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(groupKey(projectID, id))
		if err == badger.ErrKeyNotFound {
			return fmt.Errorf("get group %s: %w", id, ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("get group %s: %w", id, err)
		}
		return getValue(item, &g)
	})
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// GetGroupByFingerprint resolves an ErrorGroup by project and fingerprint hash.
func (s *BadgerStore) GetGroupByFingerprint(ctx context.Context, projectID, fingerprint string) (*domain.ErrorGroup, error) {
	var g domain.ErrorGroup
	err := s.db.View(func(txn *badger.Txn) error {
		// Resolve group ID from fingerprint index.
		fpItem, err := txn.Get(fpKey(projectID, fingerprint))
		if err == badger.ErrKeyNotFound {
			return fmt.Errorf("get group by fingerprint %q: %w", fingerprint, ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("get group by fingerprint %q: %w", fingerprint, err)
		}
		gidBytes, err := fpItem.ValueCopy(nil)
		if err != nil {
			return fmt.Errorf("get group by fingerprint %q value: %w", fingerprint, err)
		}
		gid, err := ulid.ParseStrict(string(gidBytes))
		if err != nil {
			return fmt.Errorf("get group by fingerprint %q parse id: %w", fingerprint, err)
		}

		item, err := txn.Get(groupKey(projectID, gid))
		if err == badger.ErrKeyNotFound {
			return fmt.Errorf("get group by fingerprint %q: %w", fingerprint, ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("get group by fingerprint %q: %w", fingerprint, err)
		}
		return getValue(item, &g)
	})
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// ListGroups returns groups for a project, sorted and paginated.
// Because sorting is by arbitrary fields (count, first/last seen) BadgerDB
// cannot serve the order from key layout alone — results are loaded into
// memory and sorted there.
func (s *BadgerStore) ListGroups(ctx context.Context, opts ListGroupsOpts) ([]*domain.ErrorGroup, error) {
	prefix := pfxGroup
	if opts.ProjectID != "" {
		prefix = pfxGroup + opts.ProjectID + ":"
	}

	var results []*domain.ErrorGroup
	err := s.db.View(func(txn *badger.Txn) error {
		return reverseIter(txn, prefix, func(item *badger.Item) error {
			var g domain.ErrorGroup
			if err := getValue(item, &g); err != nil {
				return fmt.Errorf("list groups decode: %w", err)
			}
			if opts.Status != nil && g.Status != *opts.Status {
				return nil
			}
			results = append(results, &g)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}

	switch opts.SortBy {
	case GroupSortFirstSeen:
		sort.Slice(results, func(i, j int) bool {
			return results[i].FirstSeen.After(results[j].FirstSeen)
		})
	case GroupSortCount:
		sort.Slice(results, func(i, j int) bool {
			return results[i].Count > results[j].Count
		})
	default: // GroupSortLastSeen or empty
		sort.Slice(results, func(i, j int) bool {
			return results[i].LastSeen.After(results[j].LastSeen)
		})
	}
	return applyPage(results, opts.Offset, opts.Limit), nil
}

// IncrementGroupCount atomically increments the event count for a group and
// updates its LastSeen timestamp and LastEvent reference.
func (s *BadgerStore) IncrementGroupCount(ctx context.Context, projectID string, id ulid.ULID, lastSeen time.Time, lastEventID ulid.ULID) error {
	key := groupKey(projectID, id)
	return s.db.Update(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err == badger.ErrKeyNotFound {
			return fmt.Errorf("increment group count %s: %w", id, ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("increment group count %s: get: %w", id, err)
		}

		var g domain.ErrorGroup
		if err := getValue(item, &g); err != nil {
			return fmt.Errorf("increment group count %s: decode: %w", id, err)
		}

		g.Count++
		g.LastSeen = lastSeen
		g.LastEvent = lastEventID

		data, err := marshal(&g)
		if err != nil {
			return fmt.Errorf("increment group count %s: %w", id, err)
		}
		return txn.Set(key, data)
	})
}

// UpdateGroupStatus changes the lifecycle status of an ErrorGroup.
func (s *BadgerStore) UpdateGroupStatus(ctx context.Context, projectID string, id ulid.ULID, status domain.GroupStatus) error {
	key := groupKey(projectID, id)
	return s.db.Update(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err == badger.ErrKeyNotFound {
			return fmt.Errorf("update group status %s: %w", id, ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("update group status %s: get: %w", id, err)
		}

		var g domain.ErrorGroup
		if err := getValue(item, &g); err != nil {
			return fmt.Errorf("update group status %s: decode: %w", id, err)
		}
		g.Status = status

		data, err := marshal(&g)
		if err != nil {
			return fmt.Errorf("update group status %s: %w", id, err)
		}
		return txn.Set(key, data)
	})
}

// ---------------------------------------------------------------------------
// PodCrash operations
// ---------------------------------------------------------------------------

// SavePodCrash persists a PodCrash under its time-ordered primary key and
// writes a secondary lookup entry for GetPodCrash.
func (s *BadgerStore) SavePodCrash(ctx context.Context, crash *domain.PodCrash) error {
	data, err := marshal(crash)
	if err != nil {
		return fmt.Errorf("save pod crash %s: %w", crash.ID, err)
	}
	lookup := crash.Namespace + ":" + ts20(crash.Timestamp)
	return s.db.Update(func(txn *badger.Txn) error {
		if err := txn.Set(crashKey(crash.Namespace, crash.Timestamp, crash.ID), data); err != nil {
			return fmt.Errorf("save pod crash %s primary: %w", crash.ID, err)
		}
		if err := txn.Set(crashLookupKey(crash.ID), []byte(lookup)); err != nil {
			return fmt.Errorf("save pod crash %s lookup: %w", crash.ID, err)
		}
		return nil
	})
}

// GetPodCrash retrieves a PodCrash by ID.
func (s *BadgerStore) GetPodCrash(ctx context.Context, id ulid.ULID) (*domain.PodCrash, error) {
	var crash domain.PodCrash
	err := s.db.View(func(txn *badger.Txn) error {
		lkItem, err := txn.Get(crashLookupKey(id))
		if err == badger.ErrKeyNotFound {
			return fmt.Errorf("get pod crash %s: %w", id, ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("get pod crash %s lookup: %w", id, err)
		}
		val, err := lkItem.ValueCopy(nil)
		if err != nil {
			return fmt.Errorf("get pod crash %s lookup value: %w", id, err)
		}

		// val = "{namespace}:{ts20}"
		sep := strings.LastIndex(string(val), ":")
		if sep < 0 {
			return fmt.Errorf("get pod crash %s: malformed lookup value", id)
		}
		namespace := string(val[:sep])
		ts20val := string(val[sep+1:])
		var tsNano int64
		if _, err = fmt.Sscanf(ts20val, "%d", &tsNano); err != nil {
			return fmt.Errorf("get pod crash %s parse ts: %w", id, err)
		}
		t := time.Unix(0, tsNano)

		item, err := txn.Get(crashKey(namespace, t, id))
		if err == badger.ErrKeyNotFound {
			return fmt.Errorf("get pod crash %s: %w", id, ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("get pod crash %s: %w", id, err)
		}
		return getValue(item, &crash)
	})
	if err != nil {
		return nil, err
	}
	return &crash, nil
}

// ListPodCrashes returns crashes newest-first, optionally filtered by
// namespace and paginated.
func (s *BadgerStore) ListPodCrashes(ctx context.Context, opts ListCrashesOpts) ([]*domain.PodCrash, error) {
	prefix := pfxCrash
	if opts.Namespace != "" {
		prefix = pfxCrash + opts.Namespace + ":"
	}

	var results []*domain.PodCrash
	err := s.db.View(func(txn *badger.Txn) error {
		return reverseIter(txn, prefix, func(item *badger.Item) error {
			var c domain.PodCrash
			if err := getValue(item, &c); err != nil {
				return fmt.Errorf("list pod crashes decode: %w", err)
			}
			results = append(results, &c)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	// When scanning across all namespaces, keys are ordered per-namespace so
	// we must sort globally by timestamp.
	if opts.Namespace == "" {
		sort.Slice(results, func(i, j int) bool {
			return results[i].Timestamp.After(results[j].Timestamp)
		})
	}
	return applyPage(results, opts.Offset, opts.Limit), nil
}

// ---------------------------------------------------------------------------
// Project operations
// ---------------------------------------------------------------------------

// SaveProject persists a Project and updates the DSN key index.
func (s *BadgerStore) SaveProject(ctx context.Context, project *domain.Project) error {
	data, err := marshal(project)
	if err != nil {
		return fmt.Errorf("save project %s: %w", project.ID, err)
	}
	return s.db.Update(func(txn *badger.Txn) error {
		if err := txn.Set(projectKey(project.ID), data); err != nil {
			return fmt.Errorf("save project %s primary: %w", project.ID, err)
		}
		if err := txn.Set(dsnIndexKey(project.DSNKey), []byte(project.ID)); err != nil {
			return fmt.Errorf("save project %s dsn index: %w", project.ID, err)
		}
		return nil
	})
}

// GetProject retrieves a Project by its ID.
func (s *BadgerStore) GetProject(ctx context.Context, id string) (*domain.Project, error) {
	var p domain.Project
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(projectKey(id))
		if err == badger.ErrKeyNotFound {
			return fmt.Errorf("get project %s: %w", id, ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("get project %s: %w", id, err)
		}
		return getValue(item, &p)
	})
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// GetProjectByDSNKey looks up a Project by its DSN key.
func (s *BadgerStore) GetProjectByDSNKey(ctx context.Context, dsnKey string) (*domain.Project, error) {
	var p domain.Project
	err := s.db.View(func(txn *badger.Txn) error {
		idxItem, err := txn.Get(dsnIndexKey(dsnKey))
		if err == badger.ErrKeyNotFound {
			return fmt.Errorf("get project by dsn key: %w", ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("get project by dsn key: %w", err)
		}
		idBytes, err := idxItem.ValueCopy(nil)
		if err != nil {
			return fmt.Errorf("get project by dsn key value: %w", err)
		}

		item, err := txn.Get(projectKey(string(idBytes)))
		if err == badger.ErrKeyNotFound {
			return fmt.Errorf("get project by dsn key: %w", ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("get project by dsn key: %w", err)
		}
		return getValue(item, &p)
	})
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ---------------------------------------------------------------------------
// Maintenance
// ---------------------------------------------------------------------------

// DeleteEventsOlderThan removes all events (and their lookup entries) with a
// Timestamp before cutoff. Key timestamps are parsed directly without JSON
// decoding for efficiency. Returns the number of deleted records.
func (s *BadgerStore) DeleteEventsOlderThan(ctx context.Context, cutoff time.Time) (int, error) {
	type toDelete struct {
		primary []byte
		lookup  []byte
	}
	cutoffNano := cutoff.UnixNano()

	// Collect keys to delete in a read transaction first.
	var stale []toDelete
	if err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false // keys only for efficiency
		it := txn.NewIterator(opts)
		defer it.Close()

		pfx := []byte(pfxEvent)
		for it.Seek(pfx); it.ValidForPrefix(pfx); it.Next() {
			key := it.Item().KeyCopy(nil)
			projectID, tsNano, eventIDStr, ok := parseEventKey(key)
			if !ok || tsNano >= cutoffNano {
				continue
			}
			id, err := ulid.ParseStrict(eventIDStr)
			if err != nil {
				continue
			}
			stale = append(stale, toDelete{
				primary: key,
				lookup:  eventLookupKey(projectID, id),
			})
		}
		return nil
	}); err != nil {
		return 0, fmt.Errorf("delete events older than: scan: %w", err)
	}

	if len(stale) == 0 {
		return 0, nil
	}

	// Delete in batches via WriteBatch for efficiency.
	wb := s.db.NewWriteBatch()
	defer wb.Cancel()
	for _, d := range stale {
		if err := wb.Delete(d.primary); err != nil {
			return 0, fmt.Errorf("delete events older than: delete primary: %w", err)
		}
		if err := wb.Delete(d.lookup); err != nil {
			return 0, fmt.Errorf("delete events older than: delete lookup: %w", err)
		}
	}
	if err := wb.Flush(); err != nil {
		return 0, fmt.Errorf("delete events older than: flush: %w", err)
	}
	return len(stale), nil
}

// parseEventKey extracts the projectID, timestamp (nanoseconds), and event ID
// string from a key of the form "e:{projectID}:{ts20}:{eventID}".
// The ts20 segment is always exactly 20 characters.
func parseEventKey(key []byte) (projectID string, tsNano int64, eventID string, ok bool) {
	// Strip "e:" prefix.
	s := string(key)
	if len(s) < 3 || s[:2] != pfxEvent {
		return
	}
	s = s[2:]

	// projectID ends at the first ':'.
	sep1 := strings.Index(s, ":")
	if sep1 < 0 {
		return
	}
	projectID = s[:sep1]
	s = s[sep1+1:]

	// ts20 is the next 20 bytes, followed by ':'.
	if len(s) < 22 { // 20 digits + ':' + at least 1 char for eventID
		return
	}
	if _, err := fmt.Sscanf(s[:20], "%d", &tsNano); err != nil {
		return
	}
	if s[20] != ':' {
		return
	}
	eventID = s[21:]
	ok = true
	return
}

// applyPage applies offset and limit pagination to a slice.
// limit=0 means no upper bound.
func applyPage[T any](s []T, offset, limit int) []T {
	if offset >= len(s) {
		return nil
	}
	s = s[offset:]
	if limit > 0 && limit < len(s) {
		s = s[:limit]
	}
	return s
}
