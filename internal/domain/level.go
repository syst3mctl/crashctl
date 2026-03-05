package domain

import (
	"encoding/json"
	"fmt"
)

// Level represents the severity of an error event.
type Level int8

const (
	LevelInfo    Level = iota // info
	LevelWarning              // warning
	LevelError                // error
	LevelPanic                // panic
)

var levelStrings = map[Level]string{
	LevelInfo:    "info",
	LevelWarning: "warning",
	LevelError:   "error",
	LevelPanic:   "panic",
}

var stringLevels = map[string]Level{
	"info":    LevelInfo,
	"warning": LevelWarning,
	"error":   LevelError,
	"panic":   LevelPanic,
}

func (l Level) String() string {
	if s, ok := levelStrings[l]; ok {
		return s
	}
	return "unknown"
}

func ParseLevel(s string) (Level, error) {
	if l, ok := stringLevels[s]; ok {
		return l, nil
	}
	return LevelInfo, fmt.Errorf("unknown level: %q", s)
}

func (l Level) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

func (l *Level) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := ParseLevel(s)
	if err != nil {
		return err
	}
	*l = parsed
	return nil
}
