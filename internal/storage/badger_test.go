package storage

import (
	"errors"
	"testing"
	"time"

	"github.com/syst3mctl/crashctl/internal/domain"
)

// package-level level/status shorthands used across Badger tests.
var (
	lvlError   = domain.LevelError
	lvlInfo    = domain.LevelInfo
	stOpen     = domain.GroupStatusOpen
	stResolved = domain.GroupStatusResolved
	stIgnored  = domain.GroupStatusIgnored
)

// openTestStore opens a BadgerStore in a temp directory that is
// automatically cleaned up when the test ends.
func openTestStore(t *testing.T) *BadgerStore {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return s
}

// ---------------------------------------------------------------------------
// Event tests
// ---------------------------------------------------------------------------

func TestBadger_SaveAndGetEvent(t *testing.T) {
	s := openTestStore(t)
	e := makeEvent(t, "proj1", domain.LevelError, time.Now())
	if err := s.SaveEvent(ctx, e); err != nil {
		t.Fatalf("SaveEvent: %v", err)
	}
	got, err := s.GetEvent(ctx, "proj1", e.ID)
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if got.ID != e.ID {
		t.Errorf("got ID %v, want %v", got.ID, e.ID)
	}
	if got.ProjectID != e.ProjectID {
		t.Errorf("got ProjectID %q, want %q", got.ProjectID, e.ProjectID)
	}
}

func TestBadger_GetEvent_NotFound(t *testing.T) {
	s := openTestStore(t)

	t.Run("missing id", func(t *testing.T) {
		_, err := s.GetEvent(ctx, "proj1", newULID(t))
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("wrong project", func(t *testing.T) {
		e := makeEvent(t, "proj1", domain.LevelError, time.Now())
		_ = s.SaveEvent(ctx, e)
		_, err := s.GetEvent(ctx, "other-project", e.ID)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound for wrong project, got %v", err)
		}
	})
}

func TestBadger_Event_RoundTrip(t *testing.T) {
	s := openTestStore(t)
	e := makeEvent(t, "p", domain.LevelError, time.Now().Truncate(time.Millisecond))
	e.Message = "database connection refused"
	e.Service = "payments-api"
	e.Tags = map[string]string{"region": "us-east-1"}
	_ = s.SaveEvent(ctx, e)

	got, err := s.GetEvent(ctx, "p", e.ID)
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if got.Message != e.Message {
		t.Errorf("Message: got %q, want %q", got.Message, e.Message)
	}
	if got.Service != e.Service {
		t.Errorf("Service: got %q, want %q", got.Service, e.Service)
	}
	if got.Tags["region"] != "us-east-1" {
		t.Errorf("Tags[region]: got %q", got.Tags["region"])
	}
}

func TestBadger_ListEvents_FilterByProject(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	_ = s.SaveEvent(ctx, makeEvent(t, "proj1", domain.LevelError, now))
	_ = s.SaveEvent(ctx, makeEvent(t, "proj2", domain.LevelError, now))
	_ = s.SaveEvent(ctx, makeEvent(t, "proj1", domain.LevelInfo, now.Add(time.Second)))

	got, err := s.ListEvents(ctx, ListEventsOpts{ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d events, want 2", len(got))
	}
}

func TestBadger_ListEvents_FilterByLevel(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	_ = s.SaveEvent(ctx, makeEvent(t, "p", domain.LevelError, now))
	_ = s.SaveEvent(ctx, makeEvent(t, "p", domain.LevelInfo, now))
	_ = s.SaveEvent(ctx, makeEvent(t, "p", domain.LevelError, now.Add(time.Second)))

	got, err := s.ListEvents(ctx, ListEventsOpts{ProjectID: "p", Level: &lvlError})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d events, want 2", len(got))
	}
	for _, e := range got {
		if e.Level != domain.LevelError {
			t.Errorf("unexpected level %v", e.Level)
		}
	}
}

func TestBadger_ListEvents_OrderedNewestFirst(t *testing.T) {
	s := openTestStore(t)
	base := time.Now()
	for i := 0; i < 5; i++ {
		_ = s.SaveEvent(ctx, makeEvent(t, "p", domain.LevelInfo, base.Add(time.Duration(i)*time.Second)))
	}

	got, _ := s.ListEvents(ctx, ListEventsOpts{ProjectID: "p"})
	for i := 1; i < len(got); i++ {
		if got[i].Timestamp.After(got[i-1].Timestamp) {
			t.Errorf("not in descending order at index %d", i)
		}
	}
}

func TestBadger_ListEvents_LimitAndOffset(t *testing.T) {
	s := openTestStore(t)
	base := time.Now()
	for i := 0; i < 5; i++ {
		_ = s.SaveEvent(ctx, makeEvent(t, "p", domain.LevelInfo, base.Add(time.Duration(i)*time.Second)))
	}

	tests := []struct{ limit, offset, want int }{
		{2, 0, 2},
		{2, 3, 2},
		{10, 0, 5},
		{0, 0, 5},
		{2, 10, 0},
	}
	for _, tc := range tests {
		got, _ := s.ListEvents(ctx, ListEventsOpts{ProjectID: "p", Limit: tc.limit, Offset: tc.offset})
		if len(got) != tc.want {
			t.Errorf("limit=%d offset=%d: got %d, want %d", tc.limit, tc.offset, len(got), tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// ErrorGroup tests
// ---------------------------------------------------------------------------

func TestBadger_SaveAndGetGroup(t *testing.T) {
	s := openTestStore(t)
	g := makeGroup(t, "proj1", "fp1", domain.GroupStatusOpen, time.Now(), 1)
	_ = s.SaveGroup(ctx, g)

	got, err := s.GetGroup(ctx, "proj1", g.ID)
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	if got.Fingerprint != "fp1" {
		t.Errorf("got fingerprint %q, want %q", got.Fingerprint, "fp1")
	}
}

func TestBadger_GetGroup_NotFound(t *testing.T) {
	s := openTestStore(t)

	t.Run("missing id", func(t *testing.T) {
		_, err := s.GetGroup(ctx, "proj1", newULID(t))
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("wrong project", func(t *testing.T) {
		g := makeGroup(t, "proj1", "fp1", domain.GroupStatusOpen, time.Now(), 1)
		_ = s.SaveGroup(ctx, g)
		_, err := s.GetGroup(ctx, "other", g.ID)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound for wrong project, got %v", err)
		}
	})
}

func TestBadger_GetGroupByFingerprint(t *testing.T) {
	s := openTestStore(t)
	g := makeGroup(t, "proj1", "sha256abc", domain.GroupStatusOpen, time.Now(), 1)
	_ = s.SaveGroup(ctx, g)

	got, err := s.GetGroupByFingerprint(ctx, "proj1", "sha256abc")
	if err != nil {
		t.Fatalf("GetGroupByFingerprint: %v", err)
	}
	if got.ID != g.ID {
		t.Errorf("got group %v, want %v", got.ID, g.ID)
	}
}

func TestBadger_GetGroupByFingerprint_NotFound(t *testing.T) {
	s := openTestStore(t)

	t.Run("no groups", func(t *testing.T) {
		_, err := s.GetGroupByFingerprint(ctx, "proj1", "nope")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("fingerprint in different project", func(t *testing.T) {
		g := makeGroup(t, "proj1", "fp1", domain.GroupStatusOpen, time.Now(), 1)
		_ = s.SaveGroup(ctx, g)
		_, err := s.GetGroupByFingerprint(ctx, "proj2", "fp1")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound for different project, got %v", err)
		}
	})
}

func TestBadger_ListGroups_FilterByStatus(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	_ = s.SaveGroup(ctx, makeGroup(t, "p", "fp1", domain.GroupStatusOpen, now, 1))
	_ = s.SaveGroup(ctx, makeGroup(t, "p", "fp2", domain.GroupStatusResolved, now, 1))
	_ = s.SaveGroup(ctx, makeGroup(t, "p", "fp3", domain.GroupStatusOpen, now, 1))

	got, _ := s.ListGroups(ctx, ListGroupsOpts{ProjectID: "p", Status: &stOpen})
	if len(got) != 2 {
		t.Errorf("got %d groups, want 2", len(got))
	}
	for _, g := range got {
		if g.Status != domain.GroupStatusOpen {
			t.Errorf("unexpected status %v", g.Status)
		}
	}
}

func TestBadger_ListGroups_SortByCount(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	_ = s.SaveGroup(ctx, makeGroup(t, "p", "fp1", domain.GroupStatusOpen, now, 10))
	_ = s.SaveGroup(ctx, makeGroup(t, "p", "fp2", domain.GroupStatusOpen, now, 50))
	_ = s.SaveGroup(ctx, makeGroup(t, "p", "fp3", domain.GroupStatusOpen, now, 5))

	got, _ := s.ListGroups(ctx, ListGroupsOpts{ProjectID: "p", SortBy: GroupSortCount})
	if len(got) != 3 {
		t.Fatalf("got %d groups, want 3", len(got))
	}
	if got[0].Count != 50 || got[1].Count != 10 || got[2].Count != 5 {
		t.Errorf("wrong sort order: %v %v %v", got[0].Count, got[1].Count, got[2].Count)
	}
}

func TestBadger_ListGroups_SortByLastSeen(t *testing.T) {
	s := openTestStore(t)
	base := time.Now()
	_ = s.SaveGroup(ctx, makeGroup(t, "p", "fp1", domain.GroupStatusOpen, base.Add(1*time.Hour), 1))
	_ = s.SaveGroup(ctx, makeGroup(t, "p", "fp2", domain.GroupStatusOpen, base.Add(3*time.Hour), 1))
	_ = s.SaveGroup(ctx, makeGroup(t, "p", "fp3", domain.GroupStatusOpen, base.Add(2*time.Hour), 1))

	got, _ := s.ListGroups(ctx, ListGroupsOpts{ProjectID: "p", SortBy: GroupSortLastSeen})
	if len(got) != 3 {
		t.Fatalf("got %d groups, want 3", len(got))
	}
	if !got[0].LastSeen.After(got[1].LastSeen) || !got[1].LastSeen.After(got[2].LastSeen) {
		t.Error("not sorted by last_seen descending")
	}
}

func TestBadger_ListGroups_SortByFirstSeen(t *testing.T) {
	s := openTestStore(t)
	base := time.Now()
	g1 := makeGroup(t, "p", "fp1", domain.GroupStatusOpen, base, 1)
	g1.FirstSeen = base.Add(1 * time.Hour)
	g2 := makeGroup(t, "p", "fp2", domain.GroupStatusOpen, base, 1)
	g2.FirstSeen = base.Add(3 * time.Hour)
	g3 := makeGroup(t, "p", "fp3", domain.GroupStatusOpen, base, 1)
	g3.FirstSeen = base.Add(2 * time.Hour)
	_ = s.SaveGroup(ctx, g1)
	_ = s.SaveGroup(ctx, g2)
	_ = s.SaveGroup(ctx, g3)

	got, _ := s.ListGroups(ctx, ListGroupsOpts{ProjectID: "p", SortBy: GroupSortFirstSeen})
	if len(got) != 3 {
		t.Fatalf("got %d groups, want 3", len(got))
	}
	if !got[0].FirstSeen.After(got[1].FirstSeen) || !got[1].FirstSeen.After(got[2].FirstSeen) {
		t.Error("not sorted by first_seen descending")
	}
}

func TestBadger_ListGroups_LimitAndOffset(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	for i := 0; i < 5; i++ {
		_ = s.SaveGroup(ctx, makeGroup(t, "p", string(rune('a'+i)), domain.GroupStatusOpen, now, 1))
	}

	tests := []struct{ limit, offset, want int }{
		{2, 0, 2},
		{2, 3, 2},
		{0, 0, 5},
		{10, 0, 5},
		{3, 4, 1},
	}
	for _, tc := range tests {
		got, _ := s.ListGroups(ctx, ListGroupsOpts{ProjectID: "p", Limit: tc.limit, Offset: tc.offset})
		if len(got) != tc.want {
			t.Errorf("limit=%d offset=%d: got %d, want %d", tc.limit, tc.offset, len(got), tc.want)
		}
	}
}

func TestBadger_IncrementGroupCount(t *testing.T) {
	s := openTestStore(t)
	g := makeGroup(t, "proj1", "fp1", domain.GroupStatusOpen, time.Now(), 1)
	_ = s.SaveGroup(ctx, g)

	newSeen := time.Now().Add(time.Minute).Truncate(time.Millisecond)
	lastEvt := newULID(t)
	if err := s.IncrementGroupCount(ctx, "proj1", g.ID, newSeen, lastEvt); err != nil {
		t.Fatalf("IncrementGroupCount: %v", err)
	}

	got, _ := s.GetGroup(ctx, "proj1", g.ID)
	if got.Count != 2 {
		t.Errorf("got count %d, want 2", got.Count)
	}
	if !got.LastSeen.Equal(newSeen) {
		t.Errorf("got LastSeen %v, want %v", got.LastSeen, newSeen)
	}
	if got.LastEvent != lastEvt {
		t.Errorf("got LastEvent %v, want %v", got.LastEvent, lastEvt)
	}
}

func TestBadger_IncrementGroupCount_NotFound(t *testing.T) {
	s := openTestStore(t)
	err := s.IncrementGroupCount(ctx, "proj1", newULID(t), time.Now(), newULID(t))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestBadger_UpdateGroupStatus(t *testing.T) {
	s := openTestStore(t)
	g := makeGroup(t, "proj1", "fp1", domain.GroupStatusOpen, time.Now(), 1)
	_ = s.SaveGroup(ctx, g)

	if err := s.UpdateGroupStatus(ctx, "proj1", g.ID, stResolved); err != nil {
		t.Fatalf("UpdateGroupStatus: %v", err)
	}
	got, _ := s.GetGroup(ctx, "proj1", g.ID)
	if got.Status != domain.GroupStatusResolved {
		t.Errorf("got status %v, want resolved", got.Status)
	}
}

func TestBadger_UpdateGroupStatus_NotFound(t *testing.T) {
	s := openTestStore(t)
	err := s.UpdateGroupStatus(ctx, "proj1", newULID(t), stIgnored)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// PodCrash tests
// ---------------------------------------------------------------------------

func TestBadger_SaveAndGetPodCrash(t *testing.T) {
	s := openTestStore(t)
	c := makeCrash(t, "prod", time.Now())
	_ = s.SavePodCrash(ctx, c)

	got, err := s.GetPodCrash(ctx, c.ID)
	if err != nil {
		t.Fatalf("GetPodCrash: %v", err)
	}
	if got.ID != c.ID {
		t.Errorf("got ID %v, want %v", got.ID, c.ID)
	}
}

func TestBadger_GetPodCrash_NotFound(t *testing.T) {
	s := openTestStore(t)
	_, err := s.GetPodCrash(ctx, newULID(t))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestBadger_SavePodCrash_PreservesLinkedGroup(t *testing.T) {
	s := openTestStore(t)
	c := makeCrash(t, "prod", time.Now())
	gid := newULID(t)
	c.LinkedGroup = &gid
	_ = s.SavePodCrash(ctx, c)

	got, _ := s.GetPodCrash(ctx, c.ID)
	if got.LinkedGroup == nil || *got.LinkedGroup != gid {
		t.Error("LinkedGroup not preserved correctly")
	}
}

func TestBadger_ListPodCrashes_FilterByNamespace(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	_ = s.SavePodCrash(ctx, makeCrash(t, "prod", now))
	_ = s.SavePodCrash(ctx, makeCrash(t, "staging", now))
	_ = s.SavePodCrash(ctx, makeCrash(t, "prod", now.Add(time.Second)))

	got, _ := s.ListPodCrashes(ctx, ListCrashesOpts{Namespace: "prod"})
	if len(got) != 2 {
		t.Errorf("got %d crashes, want 2", len(got))
	}
	for _, c := range got {
		if c.Namespace != "prod" {
			t.Errorf("unexpected namespace %q", c.Namespace)
		}
	}
}

func TestBadger_ListPodCrashes_OrderedNewestFirst(t *testing.T) {
	s := openTestStore(t)
	base := time.Now()
	for i := 0; i < 4; i++ {
		_ = s.SavePodCrash(ctx, makeCrash(t, "ns", base.Add(time.Duration(i)*time.Second)))
	}

	got, _ := s.ListPodCrashes(ctx, ListCrashesOpts{Namespace: "ns"})
	for i := 1; i < len(got); i++ {
		if got[i].Timestamp.After(got[i-1].Timestamp) {
			t.Errorf("not in descending order at index %d", i)
		}
	}
}

func TestBadger_ListPodCrashes_LimitAndOffset(t *testing.T) {
	s := openTestStore(t)
	base := time.Now()
	for i := 0; i < 5; i++ {
		_ = s.SavePodCrash(ctx, makeCrash(t, "ns", base.Add(time.Duration(i)*time.Second)))
	}

	tests := []struct{ limit, offset, want int }{
		{2, 0, 2},
		{0, 0, 5},
		{3, 3, 2},
		{5, 10, 0},
	}
	for _, tc := range tests {
		got, _ := s.ListPodCrashes(ctx, ListCrashesOpts{Namespace: "ns", Limit: tc.limit, Offset: tc.offset})
		if len(got) != tc.want {
			t.Errorf("limit=%d offset=%d: got %d, want %d", tc.limit, tc.offset, len(got), tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Project tests
// ---------------------------------------------------------------------------

func TestBadger_SaveAndGetProject(t *testing.T) {
	s := openTestStore(t)
	p := makeProject(t, "proj1", "dsn-abc")
	_ = s.SaveProject(ctx, p)

	got, err := s.GetProject(ctx, "proj1")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Name != p.Name {
		t.Errorf("got Name %q, want %q", got.Name, p.Name)
	}
}

func TestBadger_GetProject_NotFound(t *testing.T) {
	s := openTestStore(t)
	_, err := s.GetProject(ctx, "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestBadger_GetProjectByDSNKey(t *testing.T) {
	s := openTestStore(t)
	p := makeProject(t, "proj1", "dsn-secret")
	_ = s.SaveProject(ctx, p)

	got, err := s.GetProjectByDSNKey(ctx, "dsn-secret")
	if err != nil {
		t.Fatalf("GetProjectByDSNKey: %v", err)
	}
	if got.ID != "proj1" {
		t.Errorf("got ID %q, want %q", got.ID, "proj1")
	}
}

func TestBadger_GetProjectByDSNKey_NotFound(t *testing.T) {
	s := openTestStore(t)

	t.Run("no projects", func(t *testing.T) {
		_, err := s.GetProjectByDSNKey(ctx, "nope")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("different key", func(t *testing.T) {
		_ = s.SaveProject(ctx, makeProject(t, "p1", "key-a"))
		_, err := s.GetProjectByDSNKey(ctx, "key-b")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// DeleteEventsOlderThan tests
// ---------------------------------------------------------------------------

func TestBadger_DeleteEventsOlderThan(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	old1 := makeEvent(t, "p", domain.LevelInfo, now.Add(-2*time.Hour))
	old2 := makeEvent(t, "p", domain.LevelInfo, now.Add(-3*time.Hour))
	recent := makeEvent(t, "p", domain.LevelInfo, now.Add(-30*time.Minute))
	for _, e := range []*domain.Event{old1, old2, recent} {
		_ = s.SaveEvent(ctx, e)
	}

	deleted, err := s.DeleteEventsOlderThan(ctx, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("DeleteEventsOlderThan: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted %d, want 2", deleted)
	}

	remaining, _ := s.ListEvents(ctx, ListEventsOpts{ProjectID: "p"})
	if len(remaining) != 1 {
		t.Errorf("got %d remaining events, want 1", len(remaining))
	}
	if remaining[0].ID != recent.ID {
		t.Error("wrong event retained after deletion")
	}

	// Verify the lookup key is also deleted.
	_, err = s.GetEvent(ctx, "p", old1.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("old1 lookup should be deleted, got %v", err)
	}
}

func TestBadger_DeleteEventsOlderThan_NoneMatch(t *testing.T) {
	s := openTestStore(t)
	_ = s.SaveEvent(ctx, makeEvent(t, "p", domain.LevelInfo, time.Now()))

	deleted, err := s.DeleteEventsOlderThan(ctx, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("DeleteEventsOlderThan: %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted %d, want 0", deleted)
	}
}

func TestBadger_DeleteEventsOlderThan_EmptyStore(t *testing.T) {
	s := openTestStore(t)
	deleted, err := s.DeleteEventsOlderThan(ctx, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted %d from empty store, want 0", deleted)
	}
}

// ---------------------------------------------------------------------------
// Persistence — data must survive close and reopen.
// ---------------------------------------------------------------------------

func TestBadger_DataSurvivesReopen(t *testing.T) {
	dir := t.TempDir()

	s1, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	p := makeProject(t, "proj1", "dsn-key")
	g := makeGroup(t, "proj1", "fp-reopen", domain.GroupStatusOpen, time.Now(), 3)
	e := makeEvent(t, "proj1", domain.LevelError, time.Now())
	_ = s1.SaveProject(ctx, p)
	_ = s1.SaveGroup(ctx, g)
	_ = s1.SaveEvent(ctx, e)
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	if _, err := s2.GetProject(ctx, "proj1"); err != nil {
		t.Errorf("project not found after reopen: %v", err)
	}
	if _, err := s2.GetGroupByFingerprint(ctx, "proj1", "fp-reopen"); err != nil {
		t.Errorf("group not found after reopen: %v", err)
	}
	if _, err := s2.GetEvent(ctx, "proj1", e.ID); err != nil {
		t.Errorf("event not found after reopen: %v", err)
	}
}

// ---------------------------------------------------------------------------
// parseEventKey unit test
// ---------------------------------------------------------------------------

func TestParseEventKey(t *testing.T) {
	projectID := "my-project"
	ts := time.Unix(0, 1700000000000000000)
	id := newULID(t)
	key := eventKey(projectID, ts, id)

	gotProj, gotNano, gotEvt, ok := parseEventKey(key)
	if !ok {
		t.Fatal("parseEventKey returned ok=false")
	}
	if gotProj != projectID {
		t.Errorf("project: got %q, want %q", gotProj, projectID)
	}
	if gotNano != ts.UnixNano() {
		t.Errorf("nano: got %d, want %d", gotNano, ts.UnixNano())
	}
	if gotEvt != id.String() {
		t.Errorf("eventID: got %q, want %q", gotEvt, id.String())
	}
}
