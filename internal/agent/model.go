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
	FileIDs   []string
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

// AgentMessage 镜像 Python /sessions/{id}/messages 返回的单条消息。
// Attachments 只含 file_id,Go 端 hydrate 成完整 AttachmentDTO 再回给前端。
type AgentMessage struct {
	ID             string             `json:"id"`
	ConversationID string             `json:"conversation_id"`
	Role           string             `json:"role"`
	Parts          []AgentMessagePart `json:"parts"`
	Attachments    []AgentAttachment  `json:"attachments,omitempty"`
	Status         string             `json:"status"`
	FinishReason   string             `json:"finish_reason,omitempty"`
	CreatedAt      string             `json:"created_at"`
}

type AgentMessagePart struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Output    json.RawMessage `json:"output,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type AgentAttachment struct {
	ID string `json:"id"`
}
