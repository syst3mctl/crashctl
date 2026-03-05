package domain

import (
	"time"

	"github.com/oklog/ulid/v2"
)

// Event is a single captured error or message from an application.
type Event struct {
	ID          ulid.ULID         `json:"id"`
	ProjectID   string            `json:"project_id"`
	Timestamp   time.Time         `json:"timestamp"`
	Level       Level             `json:"level"`
	Message     string            `json:"message"`
	Fingerprint string            `json:"fingerprint,omitempty"`
	GroupID     ulid.ULID         `json:"group_id,omitempty"`
	StackTrace  []Frame           `json:"stack_trace,omitempty"`
	ErrorChain  []ChainedError    `json:"error_chain,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	Context     map[string]any    `json:"context,omitempty"`
	Service     string            `json:"service,omitempty"`
	Version     string            `json:"version,omitempty"`
	Hostname    string            `json:"hostname,omitempty"`
	Namespace   string            `json:"namespace,omitempty"`
}
