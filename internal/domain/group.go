package domain

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
)

// GroupStatus represents the lifecycle state of an error group.
type GroupStatus int8

const (
	GroupStatusOpen     GroupStatus = iota // open
	GroupStatusResolved                    // resolved
	GroupStatusIgnored                     // ignored
)

var groupStatusStrings = map[GroupStatus]string{
	GroupStatusOpen:     "open",
	GroupStatusResolved: "resolved",
	GroupStatusIgnored:  "ignored",
}

var stringGroupStatuses = map[string]GroupStatus{
	"open":     GroupStatusOpen,
	"resolved": GroupStatusResolved,
	"ignored":  GroupStatusIgnored,
}

func (s GroupStatus) String() string {
	if str, ok := groupStatusStrings[s]; ok {
		return str
	}
	return "unknown"
}

func ParseGroupStatus(s string) (GroupStatus, error) {
	if gs, ok := stringGroupStatuses[s]; ok {
		return gs, nil
	}
	return GroupStatusOpen, fmt.Errorf("unknown group status: %q", s)
}

func (s GroupStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

func (s *GroupStatus) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	parsed, err := ParseGroupStatus(str)
	if err != nil {
		return err
	}
	*s = parsed
	return nil
}

// ErrorGroup aggregates Events that share the same fingerprint.
type ErrorGroup struct {
	ID          ulid.ULID   `json:"id"`
	ProjectID   string      `json:"project_id"`
	Fingerprint string      `json:"fingerprint"`
	Title       string      `json:"title"`
	Level       Level       `json:"level"`
	FirstSeen   time.Time   `json:"first_seen"`
	LastSeen    time.Time   `json:"last_seen"`
	Count       int64       `json:"count"`
	Status      GroupStatus `json:"status"`
	Service     string      `json:"service,omitempty"`
	LastEvent   ulid.ULID   `json:"last_event,omitempty"`
}
