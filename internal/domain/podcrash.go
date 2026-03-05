package domain

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
)

// CrashType categorises the reason a pod was terminated.
type CrashType int8

const (
	CrashTypeOOMKill     CrashType = iota // oomkill
	CrashTypeCrashLoop                    // crashloop
	CrashTypeEviction                     // eviction
	CrashTypeInitFail                     // init_fail
	CrashTypeRestartLimit                 // restart_limit
)

var crashTypeStrings = map[CrashType]string{
	CrashTypeOOMKill:     "oomkill",
	CrashTypeCrashLoop:   "crashloop",
	CrashTypeEviction:    "eviction",
	CrashTypeInitFail:    "init_fail",
	CrashTypeRestartLimit: "restart_limit",
}

var stringCrashTypes = map[string]CrashType{
	"oomkill":       CrashTypeOOMKill,
	"crashloop":     CrashTypeCrashLoop,
	"eviction":      CrashTypeEviction,
	"init_fail":     CrashTypeInitFail,
	"restart_limit": CrashTypeRestartLimit,
}

func (c CrashType) String() string {
	if s, ok := crashTypeStrings[c]; ok {
		return s
	}
	return "unknown"
}

func ParseCrashType(s string) (CrashType, error) {
	if ct, ok := stringCrashTypes[s]; ok {
		return ct, nil
	}
	return CrashTypeOOMKill, fmt.Errorf("unknown crash type: %q", s)
}

func (c CrashType) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.String())
}

func (c *CrashType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := ParseCrashType(s)
	if err != nil {
		return err
	}
	*c = parsed
	return nil
}

// PodCrash records a Kubernetes pod termination event detected by the watcher.
type PodCrash struct {
	ID          ulid.ULID  `json:"id"`
	Timestamp   time.Time  `json:"timestamp"`
	Namespace   string     `json:"namespace"`
	PodName     string     `json:"pod_name"`
	Container   string     `json:"container"`
	CrashType   CrashType  `json:"crash_type"`
	ExitCode    int        `json:"exit_code"`
	Restarts    int32      `json:"restarts"`
	MemoryLimit string     `json:"memory_limit,omitempty"`
	MemoryUsage string     `json:"memory_usage,omitempty"`
	LastLogs    string     `json:"last_logs,omitempty"`
	NodeName    string     `json:"node_name,omitempty"`
	LinkedGroup *ulid.ULID `json:"linked_group,omitempty"`
}
