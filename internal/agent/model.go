package agent

import "io"

const (
	EventToolStart = "tool_start"
	EventToolDone  = "tool_done"
	EventText      = "text"
	EventError     = "error"
	EventDone      = "done"
)

type ChatRequest struct {
	SessionID string
	Message   string
	Images    []ImageInput
}

type ImageInput struct {
	Filename    string
	ContentType string
	Body        io.Reader
}

type Event struct {
	Type    string `json:"type"`
	Tool    string `json:"tool,omitempty"`
	Summary string `json:"summary,omitempty"`
	Content string `json:"content,omitempty"`
	Message string `json:"message,omitempty"`
}
