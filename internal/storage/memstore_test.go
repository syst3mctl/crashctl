package storage

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/syst3mctl/crashctl/internal/domain"
)

// newULID generates a unique ULID for test fixtures using crypto/rand entropy.
func newULID(t *testing.T) ulid.ULID {
	t.Helper()
	return ulid.MustNew(ulid.Now(), rand.Reader)
}

// makeEvent returns a minimal valid Event for testing.
func makeEvent(t *testing.T, projectID string, level domain.Level, ts time.Time) *domain.Event {
	t.Helper()
	return &domain.Event{
		ID:        newULID(t),
		ProjectID: projectID,
		Timestamp: ts,
		Level:     level,
		Message:   "test error",
	}
}

// makeGroup returns a minimal valid ErrorGroup for testing.
func makeGroup(t *testing.T, projectID, fingerprint string, status domain.GroupStatus, lastSeen time.Time, count int64) *domain.ErrorGroup {
	t.Helper()
	return &domain.ErrorGroup{
		ID:          newULID(t),
		ProjectID:   projectID,
		Fingerprint: fingerprint,
		Title:       "test group",
		Level:       domain.LevelError,
		FirstSeen:   lastSeen.Add(-time.Hour),
		LastSeen:    lastSeen,
		Count:       count,
		Status:      status,
	}
}

// makeCrash returns a minimal valid PodCrash for testing.
func makeCrash(t *testing.T, namespace string, ts time.Time) *domain.PodCrash {
	t.Helper()
	return &domain.PodCrash{
		ID:        newULID(t),
		Timestamp: ts,
		Namespace: namespace,
		PodName:   "my-pod",
		Container: "app",
		CrashType: domain.CrashTypeOOMKill,
	}
}

// makeProject returns a valid Project for testing.
func makeProject(t *testing.T, id, dsnKey string) *domain.Project {
	t.Helper()
	return &domain.Project{
		ID:        id,
		Name:      "test-project",
		DSNKey:    dsnKey,
		CreatedAt: time.Now(),
	}
}

var ctx = context.Background()

// ---------------------------------------------------------------------------
// Event tests
// ---------------------------------------------------------------------------

func TestMemStore_SaveAndGetEvent(t *testing.T) {
	s := NewMemStore()
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

func TestMemStore_GetEvent_NotFound(t *testing.T) {
	s := NewMemStore()

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

func TestMemStore_GetEvent_ReturnsCopy(t *testing.T) {
	s := NewMemStore()
	e := makeEvent(t, "proj1", domain.LevelError, time.Now())
	e.Tags = map[string]string{"env": "test"}
	_ = s.SaveEvent(ctx, e)

	got, _ := s.GetEvent(ctx, "proj1", e.ID)
	got.Tags["env"] = "mutated"

	got2, _ := s.GetEvent(ctx, "proj1", e.ID)
	if got2.Tags["env"] != "test" {
		t.Error("GetEvent returned a reference instead of a copy")
	}
}

func TestMemStore_ListEvents_FilterByProject(t *testing.T) {
	s := NewMemStore()
	now := time.Now()
	e1 := makeEvent(t, "proj1", domain.LevelError, now)
	e2 := makeEvent(t, "proj2", domain.LevelError, now)
	e3 := makeEvent(t, "proj1", domain.LevelInfo, now.Add(time.Second))
	for _, e := range []*domain.Event{e1, e2, e3} {
		_ = s.SaveEvent(ctx, e)
	}

	got, err := s.ListEvents(ctx, ListEventsOpts{ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d events, want 2", len(got))
	}
}

func TestMemStore_ListEvents_FilterByLevel(t *testing.T) {
	s := NewMemStore()
	now := time.Now()
	errLevel := domain.LevelError
	_ = s.SaveEvent(ctx, makeEvent(t, "proj1", domain.LevelError, now))
	_ = s.SaveEvent(ctx, makeEvent(t, "proj1", domain.LevelInfo, now))
	_ = s.SaveEvent(ctx, makeEvent(t, "proj1", domain.LevelError, now.Add(time.Second)))

	got, err := s.ListEvents(ctx, ListEventsOpts{ProjectID: "proj1", Level: &errLevel})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d events, want 2", len(got))
	}
	for _, e := range got {
		if e.Level != domain.LevelError {
			t.Errorf("unexpected level %v in filtered results", e.Level)
		}
	}
}

func TestMemStore_ListEvents_OrderedNewestFirst(t *testing.T) {
	s := NewMemStore()
	base := time.Now()
	for i := 0; i < 5; i++ {
		_ = s.SaveEvent(ctx, makeEvent(t, "p", domain.LevelInfo, base.Add(time.Duration(i)*time.Second)))
	}

	got, _ := s.ListEvents(ctx, ListEventsOpts{ProjectID: "p"})
	for i := 1; i < len(got); i++ {
		if got[i].Timestamp.After(got[i-1].Timestamp) {
			t.Errorf("results not in descending order at index %d", i)
		}
	}
}

func TestMemStore_ListEvents_LimitAndOffset(t *testing.T) {
	s := NewMemStore()
	base := time.Now()
	for i := 0; i < 5; i++ {
		_ = s.SaveEvent(ctx, makeEvent(t, "p", domain.LevelInfo, base.Add(time.Duration(i)*time.Second)))
	}

	tests := []struct {
		limit, offset, wantLen int
	}{
		{2, 0, 2},
		{2, 3, 2},
		{10, 0, 5},
		{0, 0, 5}, // limit=0 means no cap
		{2, 10, 0},
	}
	for _, tc := range tests {
		got, _ := s.ListEvents(ctx, ListEventsOpts{ProjectID: "p", Limit: tc.limit, Offset: tc.offset})
		if len(got) != tc.wantLen {
			t.Errorf("limit=%d offset=%d: got %d, want %d", tc.limit, tc.offset, len(got), tc.wantLen)
		}
	}
}

// ---------------------------------------------------------------------------
// ErrorGroup tests
// ---------------------------------------------------------------------------

func TestMemStore_SaveAndGetGroup(t *testing.T) {
	s := NewMemStore()
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

func TestMemStore_GetGroup_NotFound(t *testing.T) {
	s := NewMemStore()

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

func TestMemStore_GetGroupByFingerprint(t *testing.T) {
	s := NewMemStore()
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

func TestMemStore_GetGroupByFingerprint_NotFound(t *testing.T) {
	s := NewMemStore()

	t.Run("no groups", func(t *testing.T) {
		_, err := s.GetGroupByFingerprint(ctx, "proj1", "nope")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("fingerprint exists in different project", func(t *testing.T) {
		g := makeGroup(t, "proj1", "fp1", domain.GroupStatusOpen, time.Now(), 1)
		_ = s.SaveGroup(ctx, g)
		_, err := s.GetGroupByFingerprint(ctx, "proj2", "fp1")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound for different project, got %v", err)
		}
	})
}

func TestMemStore_ListGroups_FilterByStatus(t *testing.T) {
	s := NewMemStore()
	now := time.Now()
	_ = s.SaveGroup(ctx, makeGroup(t, "p", "fp1", domain.GroupStatusOpen, now, 1))
	_ = s.SaveGroup(ctx, makeGroup(t, "p", "fp2", domain.GroupStatusResolved, now, 1))
	_ = s.SaveGroup(ctx, makeGroup(t, "p", "fp3", domain.GroupStatusOpen, now, 1))

	open := domain.GroupStatusOpen
	got, _ := s.ListGroups(ctx, ListGroupsOpts{ProjectID: "p", Status: &open})
	if len(got) != 2 {
		t.Errorf("got %d groups, want 2", len(got))
	}
	for _, g := range got {
		if g.Status != domain.GroupStatusOpen {
			t.Errorf("unexpected status %v", g.Status)
		}
	}
}

func TestMemStore_ListGroups_SortByCount(t *testing.T) {
	s := NewMemStore()
	now := time.Now()
	_ = s.SaveGroup(ctx, makeGroup(t, "p", "fp1", domain.GroupStatusOpen, now, 10))
	_ = s.SaveGroup(ctx, makeGroup(t, "p", "fp2", domain.GroupStatusOpen, now, 50))
	_ = s.SaveGroup(ctx, makeGroup(t, "p", "fp3", domain.GroupStatusOpen, now, 5))

	got, _ := s.ListGroups(ctx, ListGroupsOpts{ProjectID: "p", SortBy: GroupSortCount})
	if got[0].Count != 50 || got[1].Count != 10 || got[2].Count != 5 {
		t.Errorf("wrong sort order by count: %v %v %v", got[0].Count, got[1].Count, got[2].Count)
	}
}

func TestMemStore_ListGroups_SortByLastSeen(t *testing.T) {
	s := NewMemStore()
	base := time.Now()
	_ = s.SaveGroup(ctx, makeGroup(t, "p", "fp1", domain.GroupStatusOpen, base.Add(1*time.Hour), 1))
	_ = s.SaveGroup(ctx, makeGroup(t, "p", "fp2", domain.GroupStatusOpen, base.Add(3*time.Hour), 1))
	_ = s.SaveGroup(ctx, makeGroup(t, "p", "fp3", domain.GroupStatusOpen, base.Add(2*time.Hour), 1))

	got, _ := s.ListGroups(ctx, ListGroupsOpts{ProjectID: "p", SortBy: GroupSortLastSeen})
	if !got[0].LastSeen.After(got[1].LastSeen) || !got[1].LastSeen.After(got[2].LastSeen) {
		t.Error("results not sorted by last_seen descending")
	}
}

func TestMemStore_ListGroups_SortByFirstSeen(t *testing.T) {
	s := NewMemStore()
	base := time.Now()
	g1 := makeGroup(t, "p", "fp1", domain.GroupStatusOpen, base, 1)
	g1.FirstSeen = base.Add(1 * time.Hour)
	g2 := makeGroup(t, "p", "fp2", domain.GroupStatusOpen, base, 1)
	g2.FirstSeen = base.Add(3 * time.Hour)
	g3 := makeGroup(t, "p", "fp3", domain.GroupStatusOpen, base, 1)
	g3.FirstSeen = base.Add(2 * time.Hour)
	for _, g := range []*domain.ErrorGroup{g1, g2, g3} {
		_ = s.SaveGroup(ctx, g)
	}

	got, _ := s.ListGroups(ctx, ListGroupsOpts{ProjectID: "p", SortBy: GroupSortFirstSeen})
	if !got[0].FirstSeen.After(got[1].FirstSeen) || !got[1].FirstSeen.After(got[2].FirstSeen) {
		t.Error("results not sorted by first_seen descending")
	}
}

func TestMemStore_ListGroups_LimitAndOffset(t *testing.T) {
	s := NewMemStore()
	now := time.Now()
	for i := 0; i < 5; i++ {
		_ = s.SaveGroup(ctx, makeGroup(t, "p", string(rune('a'+i)), domain.GroupStatusOpen, now, 1))
	}

	tests := []struct {
		limit, offset, want int
	}{
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

func TestMemStore_IncrementGroupCount(t *testing.T) {
	s := NewMemStore()
	g := makeGroup(t, "proj1", "fp1", domain.GroupStatusOpen, time.Now(), 1)
	_ = s.SaveGroup(ctx, g)

	newSeen := time.Now().Add(time.Minute)
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

func TestMemStore_IncrementGroupCount_NotFound(t *testing.T) {
	s := NewMemStore()
	err := s.IncrementGroupCount(ctx, "proj1", newULID(t), time.Now(), newULID(t))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMemStore_UpdateGroupStatus(t *testing.T) {
	s := NewMemStore()
	g := makeGroup(t, "proj1", "fp1", domain.GroupStatusOpen, time.Now(), 1)
	_ = s.SaveGroup(ctx, g)

	if err := s.UpdateGroupStatus(ctx, "proj1", g.ID, domain.GroupStatusResolved); err != nil {
		t.Fatalf("UpdateGroupStatus: %v", err)
	}
	got, _ := s.GetGroup(ctx, "proj1", g.ID)
	if got.Status != domain.GroupStatusResolved {
		t.Errorf("got status %v, want resolved", got.Status)
	}
}

func TestMemStore_UpdateGroupStatus_NotFound(t *testing.T) {
	s := NewMemStore()
	err := s.UpdateGroupStatus(ctx, "proj1", newULID(t), domain.GroupStatusIgnored)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// PodCrash tests
// ---------------------------------------------------------------------------

func TestMemStore_SaveAndGetPodCrash(t *testing.T) {
	s := NewMemStore()
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

func TestMemStore_GetPodCrash_NotFound(t *testing.T) {
	s := NewMemStore()
	_, err := s.GetPodCrash(ctx, newULID(t))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMemStore_SavePodCrash_PreservesLinkedGroup(t *testing.T) {
	s := NewMemStore()
	c := makeCrash(t, "prod", time.Now())
	gid := newULID(t)
	c.LinkedGroup = &gid
	_ = s.SavePodCrash(ctx, c)

	got, _ := s.GetPodCrash(ctx, c.ID)
	if got.LinkedGroup == nil || *got.LinkedGroup != gid {
		t.Error("LinkedGroup not preserved correctly")
	}
}

func TestMemStore_GetPodCrash_ReturnsCopy(t *testing.T) {
	s := NewMemStore()
	c := makeCrash(t, "prod", time.Now())
	gid := newULID(t)
	c.LinkedGroup = &gid
	_ = s.SavePodCrash(ctx, c)

	got, _ := s.GetPodCrash(ctx, c.ID)
	newID := newULID(t)
	got.LinkedGroup = &newID

	got2, _ := s.GetPodCrash(ctx, c.ID)
	if *got2.LinkedGroup != gid {
		t.Error("GetPodCrash returned a reference instead of a copy")
	}
}

func TestMemStore_ListPodCrashes_FilterByNamespace(t *testing.T) {
	s := NewMemStore()
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

func TestMemStore_ListPodCrashes_OrderedNewestFirst(t *testing.T) {
	s := NewMemStore()
	base := time.Now()
	for i := 0; i < 4; i++ {
		_ = s.SavePodCrash(ctx, makeCrash(t, "ns", base.Add(time.Duration(i)*time.Second)))
	}

	got, _ := s.ListPodCrashes(ctx, ListCrashesOpts{})
	for i := 1; i < len(got); i++ {
		if got[i].Timestamp.After(got[i-1].Timestamp) {
			t.Errorf("results not in descending order at index %d", i)
		}
	}
}

func TestMemStore_ListPodCrashes_LimitAndOffset(t *testing.T) {
	s := NewMemStore()
	base := time.Now()
	for i := 0; i < 5; i++ {
		_ = s.SavePodCrash(ctx, makeCrash(t, "ns", base.Add(time.Duration(i)*time.Second)))
	}

	tests := []struct {
		limit, offset, want int
	}{
		{2, 0, 2},
		{0, 0, 5},
		{3, 3, 2},
		{5, 10, 0},
	}
	for _, tc := range tests {
		got, _ := s.ListPodCrashes(ctx, ListCrashesOpts{Limit: tc.limit, Offset: tc.offset})
		if len(got) != tc.want {
			t.Errorf("limit=%d offset=%d: got %d, want %d", tc.limit, tc.offset, len(got), tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Project tests
// ---------------------------------------------------------------------------

func TestMemStore_SaveAndGetProject(t *testing.T) {
	s := NewMemStore()
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

func TestMemStore_GetProject_NotFound(t *testing.T) {
	s := NewMemStore()
	_, err := s.GetProject(ctx, "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMemStore_GetProjectByDSNKey(t *testing.T) {
	s := NewMemStore()
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

func TestMemStore_GetProjectByDSNKey_NotFound(t *testing.T) {
	s := NewMemStore()

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

func TestMemStore_DeleteEventsOlderThan(t *testing.T) {
	s := NewMemStore()
	now := time.Now()
	old1 := makeEvent(t, "p", domain.LevelInfo, now.Add(-2*time.Hour))
	old2 := makeEvent(t, "p", domain.LevelInfo, now.Add(-3*time.Hour))
	recent := makeEvent(t, "p", domain.LevelInfo, now.Add(-30*time.Minute))
	for _, e := range []*domain.Event{old1, old2, recent} {
		_ = s.SaveEvent(ctx, e)
	}

	cutoff := now.Add(-time.Hour)
	deleted, err := s.DeleteEventsOlderThan(ctx, cutoff)
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
}

func TestMemStore_DeleteEventsOlderThan_NoneMatch(t *testing.T) {
	s := NewMemStore()
	_ = s.SaveEvent(ctx, makeEvent(t, "p", domain.LevelInfo, time.Now()))

	deleted, err := s.DeleteEventsOlderThan(ctx, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("DeleteEventsOlderThan: %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted %d, want 0", deleted)
	}
}

func TestMemStore_DeleteEventsOlderThan_EmptyStore(t *testing.T) {
	s := NewMemStore()
	deleted, err := s.DeleteEventsOlderThan(ctx, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted %d from empty store, want 0", deleted)
	}
}
