package domain

// SourceLine is a single line of source code with its line number.
type SourceLine struct {
	Line   int    `json:"line"`
	Source string `json:"source"`
}

// Frame is a single stack frame.
type Frame struct {
	File     string       `json:"file"`
	Function string       `json:"function"`
	Line     int          `json:"line"`
	InApp    bool         `json:"in_app"`
	Source   []SourceLine `json:"source,omitempty"`
}

// ChainedError represents one error in a Go error chain (errors.Unwrap).
type ChainedError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}
