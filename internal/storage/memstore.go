package storage

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/syst3mctl/crashctl/internal/domain"
)

// MemStore is an in-memory implementation of Store backed by maps and a
// sync.RWMutex. It is intended exclusively for unit tests — never production.
type MemStore struct {
	mu sync.RWMutex

	events     map[string]*domain.Event      // key: eventID
	groups     map[string]*domain.ErrorGroup // key: groupID
	fingerprints map[string]string           // key: "projectID:fingerprint" → groupID
	crashes    map[string]*domain.PodCrash   // key: crashID
	projects   map[string]*domain.Project    // key: projectID
	dsnIndex   map[string]string             // key: dsnKey → projectID
}

// NewMemStore returns an empty, ready-to-use MemStore.
func NewMemStore() *MemStore {
	return &MemStore{
		events:       make(map[string]*domain.Event),
		groups:       make(map[string]*domain.ErrorGroup),
		fingerprints: make(map[string]string),
		crashes:      make(map[string]*domain.PodCrash),
		projects:     make(map[string]*domain.Project),
		dsnIndex:     make(map[string]string),
	}
}

// --- helpers ----------------------------------------------------------------

func copyEvent(e *domain.Event) *domain.Event {
	if e == nil {
		return nil
	}
	cp := *e
	if e.StackTrace != nil {
		cp.StackTrace = make([]domain.Frame, len(e.StackTrace))
		for i, f := range e.StackTrace {
			cf := f
			if f.Source != nil {
				cf.Source = make([]domain.SourceLine, len(f.Source))
				copy(cf.Source, f.Source)
			}
			cp.StackTrace[i] = cf
		}
	}
	if e.ErrorChain != nil {
		cp.ErrorChain = make([]domain.ChainedError, len(e.ErrorChain))
		copy(cp.ErrorChain, e.ErrorChain)
	}
	if e.Tags != nil {
		cp.Tags = make(map[string]string, len(e.Tags))
		for k, v := range e.Tags {
			cp.Tags[k] = v
		}
	}
	if e.Context != nil {
		cp.Context = make(map[string]any, len(e.Context))
		for k, v := range e.Context {
			cp.Context[k] = v
		}
	}
	return &cp
}

func copyGroup(g *domain.ErrorGroup) *domain.ErrorGroup {
	if g == nil {
		return nil
	}
	cp := *g
	return &cp
}

func copyCrash(c *domain.PodCrash) *domain.PodCrash {
	if c == nil {
		return nil
	}
	cp := *c
	if c.LinkedGroup != nil {
		id := *c.LinkedGroup
		cp.LinkedGroup = &id
	}
	return &cp
}

func copyProject(p *domain.Project) *domain.Project {
	if p == nil {
		return nil
	}
	cp := *p
	return &cp
}

func fingerprintKey(projectID, fingerprint string) string {
	return projectID + ":" + fingerprint
}

// --- Event operations -------------------------------------------------------

func (m *MemStore) SaveEvent(ctx context.Context, event *domain.Event) error {
	if event == nil {
		return fmt.Errorf("save event: event is nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events[event.ID.String()] = copyEvent(event)
	return nil
}

func (m *MemStore) GetEvent(ctx context.Context, projectID string, id ulid.ULID) (*domain.Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.events[id.String()]
	if !ok || e.ProjectID != projectID {
		return nil, fmt.Errorf("get event %s: %w", id, ErrNotFound)
	}
	return copyEvent(e), nil
}

func (m *MemStore) ListEvents(ctx context.Context, opts ListEventsOpts) ([]*domain.Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var matched []*domain.Event
	for _, e := range m.events {
		if opts.ProjectID != "" && e.ProjectID != opts.ProjectID {
			continue
		}
		if opts.Level != nil && e.Level != *opts.Level {
			continue
		}
		matched = append(matched, e)
	}

	// Sort by timestamp descending (newest first).
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].Timestamp.After(matched[j].Timestamp)
	})

	matched = applyPage(matched, opts.Offset, opts.Limit)

	out := make([]*domain.Event, len(matched))
	for i, e := range matched {
		out[i] = copyEvent(e)
	}
	return out, nil
}

// --- ErrorGroup operations --------------------------------------------------

func (m *MemStore) SaveGroup(ctx context.Context, group *domain.ErrorGroup) error {
	if group == nil {
		return fmt.Errorf("save group: group is nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.groups[group.ID.String()] = copyGroup(group)
	m.fingerprints[fingerprintKey(group.ProjectID, group.Fingerprint)] = group.ID.String()
	return nil
}

func (m *MemStore) GetGroup(ctx context.Context, projectID string, id ulid.ULID) (*domain.ErrorGroup, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	g, ok := m.groups[id.String()]
	if !ok || g.ProjectID != projectID {
		return nil, fmt.Errorf("get group %s: %w", id, ErrNotFound)
	}
	return copyGroup(g), nil
}

func (m *MemStore) GetGroupByFingerprint(ctx context.Context, projectID, fingerprint string) (*domain.ErrorGroup, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	gid, ok := m.fingerprints[fingerprintKey(projectID, fingerprint)]
	if !ok {
		return nil, fmt.Errorf("get group by fingerprint %q: %w", fingerprint, ErrNotFound)
	}
	g, ok := m.groups[gid]
	if !ok {
		return nil, fmt.Errorf("get group by fingerprint %q: %w", fingerprint, ErrNotFound)
	}
	return copyGroup(g), nil
}

func (m *MemStore) ListGroups(ctx context.Context, opts ListGroupsOpts) ([]*domain.ErrorGroup, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var matched []*domain.ErrorGroup
	for _, g := range m.groups {
		if opts.ProjectID != "" && g.ProjectID != opts.ProjectID {
			continue
		}
		if opts.Status != nil && g.Status != *opts.Status {
			continue
		}
		matched = append(matched, g)
	}

	switch opts.SortBy {
	case GroupSortFirstSeen:
		sort.Slice(matched, func(i, j int) bool {
			return matched[i].FirstSeen.After(matched[j].FirstSeen)
		})
	case GroupSortCount:
		sort.Slice(matched, func(i, j int) bool {
			return matched[i].Count > matched[j].Count
		})
	default: // GroupSortLastSeen or empty
		sort.Slice(matched, func(i, j int) bool {
			return matched[i].LastSeen.After(matched[j].LastSeen)
		})
	}

	matched = applyPage(matched, opts.Offset, opts.Limit)

	out := make([]*domain.ErrorGroup, len(matched))
	for i, g := range matched {
		out[i] = copyGroup(g)
	}
	return out, nil
}

func (m *MemStore) IncrementGroupCount(ctx context.Context, projectID string, id ulid.ULID, lastSeen time.Time, lastEventID ulid.ULID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.groups[id.String()]
	if !ok || g.ProjectID != projectID {
		return fmt.Errorf("increment group count %s: %w", id, ErrNotFound)
	}
	g.Count++
	g.LastSeen = lastSeen
	g.LastEvent = lastEventID
	return nil
}

func (m *MemStore) UpdateGroupStatus(ctx context.Context, projectID string, id ulid.ULID, status domain.GroupStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.groups[id.String()]
	if !ok || g.ProjectID != projectID {
		return fmt.Errorf("update group status %s: %w", id, ErrNotFound)
	}
	g.Status = status
	return nil
}

// --- PodCrash operations ----------------------------------------------------

func (m *MemStore) SavePodCrash(ctx context.Context, crash *domain.PodCrash) error {
	if crash == nil {
		return fmt.Errorf("save pod crash: crash is nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.crashes[crash.ID.String()] = copyCrash(crash)
	return nil
}

func (m *MemStore) GetPodCrash(ctx context.Context, id ulid.ULID) (*domain.PodCrash, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.crashes[id.String()]
	if !ok {
		return nil, fmt.Errorf("get pod crash %s: %w", id, ErrNotFound)
	}
	return copyCrash(c), nil
}

func (m *MemStore) ListPodCrashes(ctx context.Context, opts ListCrashesOpts) ([]*domain.PodCrash, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var matched []*domain.PodCrash
	for _, c := range m.crashes {
		if opts.Namespace != "" && c.Namespace != opts.Namespace {
			continue
		}
		matched = append(matched, c)
	}

	// Sort by timestamp descending (newest first).
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].Timestamp.After(matched[j].Timestamp)
	})

	matched = applyPage(matched, opts.Offset, opts.Limit)

	out := make([]*domain.PodCrash, len(matched))
	for i, c := range matched {
		out[i] = copyCrash(c)
	}
	return out, nil
}

// --- Project operations -----------------------------------------------------

func (m *MemStore) SaveProject(ctx context.Context, project *domain.Project) error {
	if project == nil {
		return fmt.Errorf("save project: project is nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.projects[project.ID] = copyProject(project)
	m.dsnIndex[project.DSNKey] = project.ID
	return nil
}

func (m *MemStore) GetProject(ctx context.Context, id string) (*domain.Project, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.projects[id]
	if !ok {
		return nil, fmt.Errorf("get project %s: %w", id, ErrNotFound)
	}
	return copyProject(p), nil
}

func (m *MemStore) GetProjectByDSNKey(ctx context.Context, dsnKey string) (*domain.Project, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	pid, ok := m.dsnIndex[dsnKey]
	if !ok {
		return nil, fmt.Errorf("get project by dsn key: %w", ErrNotFound)
	}
	p, ok := m.projects[pid]
	if !ok {
		return nil, fmt.Errorf("get project by dsn key: %w", ErrNotFound)
	}
	return copyProject(p), nil
}

// --- Maintenance ------------------------------------------------------------

func (m *MemStore) DeleteEventsOlderThan(ctx context.Context, cutoff time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	deleted := 0
	for id, e := range m.events {
		if e.Timestamp.Before(cutoff) {
			delete(m.events, id)
			deleted++
		}
	}
	return deleted, nil
}

