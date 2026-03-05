package storage

import (
	"context"
	"errors"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/syst3mctl/crashctl/internal/domain"
)

// ErrNotFound is returned when a requested resource does not exist.
var ErrNotFound = errors.New("not found")

// GroupSortField controls the ordering of ListGroups results.
type GroupSortField string

const (
	GroupSortLastSeen  GroupSortField = "last_seen"
	GroupSortFirstSeen GroupSortField = "first_seen"
	GroupSortCount     GroupSortField = "count"
)

// ListEventsOpts filters and paginates ListEvents results.
type ListEventsOpts struct {
	ProjectID string
	Level     *domain.Level
	Limit     int
	Offset    int
}

// ListGroupsOpts filters and paginates ListGroups results.
type ListGroupsOpts struct {
	ProjectID string
	Status    *domain.GroupStatus
	SortBy    GroupSortField
	Limit     int
	Offset    int
}

// ListCrashesOpts filters and paginates ListPodCrashes results.
type ListCrashesOpts struct {
	Namespace string
	Limit     int
	Offset    int
}

// Store is the persistence interface for all crashctl entities.
// All implementations must be safe for concurrent use.
type Store interface {
	// Event operations

	// SaveEvent persists a new event. The event must have a valid ID and ProjectID.
	SaveEvent(ctx context.Context, event *domain.Event) error

	// GetEvent retrieves a single event by ID within a project.
	GetEvent(ctx context.Context, projectID string, id ulid.ULID) (*domain.Event, error)

	// ListEvents returns a paginated, optionally filtered slice of events.
	ListEvents(ctx context.Context, opts ListEventsOpts) ([]*domain.Event, error)

	// ErrorGroup operations

	// SaveGroup persists a new or updated ErrorGroup.
	SaveGroup(ctx context.Context, group *domain.ErrorGroup) error

	// GetGroup retrieves an ErrorGroup by its ID within a project.
	GetGroup(ctx context.Context, projectID string, id ulid.ULID) (*domain.ErrorGroup, error)

	// GetGroupByFingerprint looks up the ErrorGroup matching the given fingerprint
	// in the specified project. Returns ErrNotFound if no group exists yet.
	GetGroupByFingerprint(ctx context.Context, projectID, fingerprint string) (*domain.ErrorGroup, error)

	// ListGroups returns a paginated, optionally filtered and sorted slice of groups.
	ListGroups(ctx context.Context, opts ListGroupsOpts) ([]*domain.ErrorGroup, error)

	// IncrementGroupCount atomically increments the event count for a group and
	// updates its LastSeen timestamp and LastEvent reference.
	IncrementGroupCount(ctx context.Context, projectID string, id ulid.ULID, lastSeen time.Time, lastEventID ulid.ULID) error

	// UpdateGroupStatus changes the status of an ErrorGroup.
	UpdateGroupStatus(ctx context.Context, projectID string, id ulid.ULID, status domain.GroupStatus) error

	// PodCrash operations

	// SavePodCrash persists a new PodCrash record.
	SavePodCrash(ctx context.Context, crash *domain.PodCrash) error

	// GetPodCrash retrieves a single PodCrash by ID.
	GetPodCrash(ctx context.Context, id ulid.ULID) (*domain.PodCrash, error)

	// ListPodCrashes returns a paginated, optionally namespace-filtered slice of crashes.
	ListPodCrashes(ctx context.Context, opts ListCrashesOpts) ([]*domain.PodCrash, error)

	// Project operations

	// SaveProject persists a new or updated Project.
	SaveProject(ctx context.Context, project *domain.Project) error

	// GetProject retrieves a Project by its ID.
	GetProject(ctx context.Context, id string) (*domain.Project, error)

	// GetProjectByDSNKey looks up a Project by its DSN key.
	// Returns ErrNotFound if no project has the given key.
	GetProjectByDSNKey(ctx context.Context, dsnKey string) (*domain.Project, error)

	// Maintenance

	// DeleteEventsOlderThan removes all events with a timestamp before the given
	// cutoff and returns the number of deleted records.
	DeleteEventsOlderThan(ctx context.Context, cutoff time.Time) (int, error)
}
