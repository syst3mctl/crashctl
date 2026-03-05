package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
)

// helpers

func mustNewULID(t *testing.T) ulid.ULID {
	t.Helper()
	id, err := ulid.New(ulid.Now(), ulid.DefaultEntropy())
	if err != nil {
		t.Fatalf("ulid.New: %v", err)
	}
	return id
}

func roundtrip[T any](t *testing.T, v T) T {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

// Level

func TestLevel_MarshalUnmarshal(t *testing.T) {
	cases := []struct {
		level Level
		want  string
	}{
		{LevelInfo, `"info"`},
		{LevelWarning, `"warning"`},
		{LevelError, `"error"`},
		{LevelPanic, `"panic"`},
	}

	for _, tc := range cases {
		t.Run(tc.level.String(), func(t *testing.T) {
			data, err := json.Marshal(tc.level)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(data) != tc.want {
				t.Errorf("got %s, want %s", data, tc.want)
			}

			var got Level
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got != tc.level {
				t.Errorf("roundtrip: got %v, want %v", got, tc.level)
			}
		})
	}
}

func TestLevel_Unknown(t *testing.T) {
	var l Level
	if err := json.Unmarshal([]byte(`"bogus"`), &l); err == nil {
		t.Error("expected error for unknown level string")
	}
}

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in   string
		want Level
	}{
		{"info", LevelInfo},
		{"warning", LevelWarning},
		{"error", LevelError},
		{"panic", LevelPanic},
	}
	for _, tc := range cases {
		got, err := ParseLevel(tc.in)
		if err != nil {
			t.Errorf("ParseLevel(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
	if _, err := ParseLevel("bogus"); err == nil {
		t.Error("expected error for unknown level")
	}
}

// GroupStatus

func TestGroupStatus_MarshalUnmarshal(t *testing.T) {
	cases := []struct {
		status GroupStatus
		want   string
	}{
		{GroupStatusOpen, `"open"`},
		{GroupStatusResolved, `"resolved"`},
		{GroupStatusIgnored, `"ignored"`},
	}

	for _, tc := range cases {
		t.Run(tc.status.String(), func(t *testing.T) {
			data, err := json.Marshal(tc.status)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(data) != tc.want {
				t.Errorf("got %s, want %s", data, tc.want)
			}

			var got GroupStatus
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got != tc.status {
				t.Errorf("roundtrip: got %v, want %v", got, tc.status)
			}
		})
	}
}

// CrashType

func TestCrashType_MarshalUnmarshal(t *testing.T) {
	cases := []struct {
		ct   CrashType
		want string
	}{
		{CrashTypeOOMKill, `"oomkill"`},
		{CrashTypeCrashLoop, `"crashloop"`},
		{CrashTypeEviction, `"eviction"`},
		{CrashTypeInitFail, `"init_fail"`},
		{CrashTypeRestartLimit, `"restart_limit"`},
	}

	for _, tc := range cases {
		t.Run(tc.ct.String(), func(t *testing.T) {
			data, err := json.Marshal(tc.ct)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(data) != tc.want {
				t.Errorf("got %s, want %s", data, tc.want)
			}

			var got CrashType
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got != tc.ct {
				t.Errorf("roundtrip: got %v, want %v", got, tc.ct)
			}
		})
	}
}

// Event

func TestEvent_RoundtripJSON(t *testing.T) {
	groupID := mustNewULID(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	orig := Event{
		ID:          mustNewULID(t),
		ProjectID:   "proj-1",
		Timestamp:   now,
		Level:       LevelError,
		Message:     "something went wrong",
		Fingerprint: "abc123",
		GroupID:     groupID,
		StackTrace: []Frame{
			{
				File:     "main.go",
				Function: "main.handler",
				Line:     42,
				InApp:    true,
				Source: []SourceLine{
					{Line: 41, Source: "func handler() {"},
					{Line: 42, Source: "\treturn errors.New(\"oops\")"},
					{Line: 43, Source: "}"},
				},
			},
		},
		ErrorChain: []ChainedError{
			{Type: "*errors.errorString", Message: "something went wrong"},
			{Type: "*os.PathError", Message: "file not found"},
		},
		Tags:      map[string]string{"env": "prod", "region": "us-east-1"},
		Context:   map[string]any{"user_id": "u-99", "request_id": "req-42"},
		Service:   "api",
		Version:   "1.2.3",
		Hostname:  "api-pod-abc",
		Namespace: "default",
	}

	got := roundtrip(t, orig)

	if got.ID != orig.ID {
		t.Errorf("ID: got %v, want %v", got.ID, orig.ID)
	}
	if got.ProjectID != orig.ProjectID {
		t.Errorf("ProjectID: got %v, want %v", got.ProjectID, orig.ProjectID)
	}
	if !got.Timestamp.Equal(orig.Timestamp) {
		t.Errorf("Timestamp: got %v, want %v", got.Timestamp, orig.Timestamp)
	}
	if got.Level != orig.Level {
		t.Errorf("Level: got %v, want %v", got.Level, orig.Level)
	}
	if got.Message != orig.Message {
		t.Errorf("Message: got %v, want %v", got.Message, orig.Message)
	}
	if got.Fingerprint != orig.Fingerprint {
		t.Errorf("Fingerprint: got %v, want %v", got.Fingerprint, orig.Fingerprint)
	}
	if got.GroupID != orig.GroupID {
		t.Errorf("GroupID: got %v, want %v", got.GroupID, orig.GroupID)
	}
	if len(got.StackTrace) != 1 {
		t.Fatalf("StackTrace len: got %d, want 1", len(got.StackTrace))
	}
	if got.StackTrace[0].File != "main.go" || got.StackTrace[0].Line != 42 {
		t.Errorf("StackTrace[0]: got %+v", got.StackTrace[0])
	}
	if len(got.StackTrace[0].Source) != 3 {
		t.Errorf("Source lines: got %d, want 3", len(got.StackTrace[0].Source))
	}
	if len(got.ErrorChain) != 2 {
		t.Fatalf("ErrorChain len: got %d, want 2", len(got.ErrorChain))
	}
	if got.Tags["env"] != "prod" {
		t.Errorf("Tags[env]: got %v, want prod", got.Tags["env"])
	}
	if got.Service != orig.Service {
		t.Errorf("Service: got %v, want %v", got.Service, orig.Service)
	}
}

func TestEvent_ZeroValue(t *testing.T) {
	var e Event
	got := roundtrip(t, e)
	if got.Level != LevelInfo {
		t.Errorf("zero Level: got %v, want info", got.Level)
	}
}

// ErrorGroup

func TestErrorGroup_RoundtripJSON(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	lastEvent := mustNewULID(t)

	orig := ErrorGroup{
		ID:          mustNewULID(t),
		ProjectID:   "proj-1",
		Fingerprint: "fp-xyz",
		Title:       "nil pointer dereference",
		Level:       LevelPanic,
		FirstSeen:   now,
		LastSeen:    now,
		Count:       42,
		Status:      GroupStatusOpen,
		Service:     "worker",
		LastEvent:   lastEvent,
	}

	got := roundtrip(t, orig)

	if got.ID != orig.ID {
		t.Errorf("ID: got %v, want %v", got.ID, orig.ID)
	}
	if got.Count != 42 {
		t.Errorf("Count: got %v, want 42", got.Count)
	}
	if got.Status != GroupStatusOpen {
		t.Errorf("Status: got %v, want open", got.Status)
	}
	if got.Level != LevelPanic {
		t.Errorf("Level: got %v, want panic", got.Level)
	}
	if got.LastEvent != lastEvent {
		t.Errorf("LastEvent: got %v, want %v", got.LastEvent, lastEvent)
	}
}

// PodCrash

func TestPodCrash_RoundtripJSON(t *testing.T) {
	linkedGroup := mustNewULID(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	orig := PodCrash{
		ID:          mustNewULID(t),
		Timestamp:   now,
		Namespace:   "production",
		PodName:     "api-pod-abc123",
		Container:   "api",
		CrashType:   CrashTypeOOMKill,
		ExitCode:    137,
		Restarts:    5,
		MemoryLimit: "256Mi",
		MemoryUsage: "260Mi",
		LastLogs:    "fatal: out of memory\n",
		NodeName:    "node-1",
		LinkedGroup: &linkedGroup,
	}

	got := roundtrip(t, orig)

	if got.ID != orig.ID {
		t.Errorf("ID: got %v, want %v", got.ID, orig.ID)
	}
	if got.CrashType != CrashTypeOOMKill {
		t.Errorf("CrashType: got %v, want oomkill", got.CrashType)
	}
	if got.ExitCode != 137 {
		t.Errorf("ExitCode: got %v, want 137", got.ExitCode)
	}
	if got.Restarts != 5 {
		t.Errorf("Restarts: got %v, want 5", got.Restarts)
	}
	if got.LinkedGroup == nil || *got.LinkedGroup != linkedGroup {
		t.Errorf("LinkedGroup: got %v, want %v", got.LinkedGroup, linkedGroup)
	}
}

func TestPodCrash_NilLinkedGroup(t *testing.T) {
	orig := PodCrash{
		ID:        mustNewULID(t),
		Timestamp: time.Now().UTC(),
		CrashType: CrashTypeCrashLoop,
	}
	got := roundtrip(t, orig)
	if got.LinkedGroup != nil {
		t.Errorf("LinkedGroup should be nil, got %v", got.LinkedGroup)
	}
}

// Project

func TestProject_RoundtripJSON(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)

	orig := Project{
		ID:        "proj-1",
		Name:      "My Service",
		DSNKey:    "abcdef1234567890abcdef1234567890",
		CreatedAt: now,
	}

	got := roundtrip(t, orig)

	if got.ID != orig.ID {
		t.Errorf("ID: got %v, want %v", got.ID, orig.ID)
	}
	if got.Name != orig.Name {
		t.Errorf("Name: got %v, want %v", got.Name, orig.Name)
	}
	if got.DSNKey != orig.DSNKey {
		t.Errorf("DSNKey: got %v, want %v", got.DSNKey, orig.DSNKey)
	}
	if !got.CreatedAt.Equal(orig.CreatedAt) {
		t.Errorf("CreatedAt: got %v, want %v", got.CreatedAt, orig.CreatedAt)
	}
}

// Frame and ChainedError

func TestFrame_RoundtripJSON(t *testing.T) {
	orig := Frame{
		File:     "internal/handler/api.go",
		Function: "(*Server).handleEvent",
		Line:     99,
		InApp:    true,
		Source: []SourceLine{
			{Line: 98, Source: "if err != nil {"},
			{Line: 99, Source: "\treturn err"},
			{Line: 100, Source: "}"},
		},
	}
	got := roundtrip(t, orig)
	if got.Function != orig.Function {
		t.Errorf("Function: got %v, want %v", got.Function, orig.Function)
	}
	if !got.InApp {
		t.Error("InApp should be true")
	}
	if len(got.Source) != 3 {
		t.Errorf("Source len: got %d, want 3", len(got.Source))
	}
}

func TestChainedError_RoundtripJSON(t *testing.T) {
	orig := ChainedError{
		Type:    "*fmt.wrapError",
		Message: "wrap: base error",
	}
	got := roundtrip(t, orig)
	if got.Type != orig.Type {
		t.Errorf("Type: got %v, want %v", got.Type, orig.Type)
	}
	if got.Message != orig.Message {
		t.Errorf("Message: got %v, want %v", got.Message, orig.Message)
	}
}
