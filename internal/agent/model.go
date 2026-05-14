package agent

import (
	"encoding/json"
	"io"
)

const (
	EventMessageStart    = "message_start"
	EventReasoningDelta  = "reasoning_delta"
	EventTextDelta       = "text_delta"
	EventTextComplete    = "text_complete"
	EventToolCallStart   = "tool_call_start"
	EventToolResult      = "tool_result"
	EventError           = "error"
	EventMessageDone     = "message_done"
)

type ChatRequest struct {
	SessionID string
	Message   string
	Files     []FileInput
}

type FileInput struct {
	Filename    string
	ContentType string
	Body        io.Reader
}

type Event struct {
	Type         string          `json:"type"`
	MessageID    string          `json:"message_id,omitempty"`
	Model        string          `json:"model,omitempty"`
	Delta        string          `json:"delta,omitempty"`
	Text         string          `json:"text,omitempty"`
	ToolUseID    string          `json:"tool_use_id,omitempty"`
	Name         string          `json:"name,omitempty"`
	Input        json.RawMessage `json:"input,omitempty"`
	Output       json.RawMessage `json:"output,omitempty"`
	IsError      bool            `json:"is_error,omitempty"`
	ElapsedMS    int             `json:"elapsed_ms,omitempty"`
	FinishReason string          `json:"finish_reason,omitempty"`
	Code         string          `json:"code,omitempty"`
	Message      string          `json:"message,omitempty"`
}
