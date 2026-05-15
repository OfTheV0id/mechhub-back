package solochat

import (
	"encoding/json"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"

	PartText       = "text"
	PartThinking   = "thinking"
	PartToolUse    = "tool_use"
	PartToolResult = "tool_result"

	FileKindImage    = "image"
	FileKindText     = "text"
	FileKindDocument = "document"
)

type Conversation struct {
	ID        bson.ObjectID `bson:"_id"`
	UserID    bson.ObjectID `bson:"user_id"`
	Title     string        `bson:"title"`
	CreatedAt time.Time     `bson:"created_at"`
	UpdatedAt time.Time     `bson:"updated_at"`
}

type UploadedFile struct {
	ID           bson.ObjectID `bson:"_id"`
	OwnerUserID  bson.ObjectID `bson:"owner_user_id"`
	OSSKey       string        `bson:"oss_key"`
	OriginalName string        `bson:"original_name"`
	MimeType     string        `bson:"mime_type"`
	Kind         string        `bson:"kind"`
	Size         int64         `bson:"size"`
	CreatedAt    time.Time     `bson:"created_at"`
}

// MessagePart 是前端渲染的最小单位,由 Go 把 Python AgentMessagePart 转一下
// 复用本结构。落 Mongo 不再需要(消息源全部在 ADK MySQL),只用作 DTO 字段。
type MessagePart struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Output    json.RawMessage `json:"output,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	ElapsedMS int             `json:"elapsed_ms,omitempty"`
}

type AttachmentDTO struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	MimeType     string `json:"mime_type"`
	OriginalName string `json:"original_name"`
	Size         int64  `json:"size"`
	URL          string `json:"url"`
}

type ConversationDTO struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type MessageDTO struct {
	ID             string          `json:"id"`
	ConversationID string          `json:"conversation_id"`
	Role           string          `json:"role"`
	Parts          []MessagePart   `json:"parts"`
	Attachments    []AttachmentDTO `json:"attachments,omitempty"`
	Status         string          `json:"status"`
	FinishReason   string          `json:"finish_reason,omitempty"`
	CreatedAt      string          `json:"created_at"`
}

type CreateConversationReq struct {
	Title string `json:"title" binding:"max=100"`
}

type UpdateConversationReq struct {
	Title string `json:"title" binding:"required,min=1,max=100"`
}

type SendMessageReq struct {
	Content     string   `json:"content" binding:"required,min=1,max=8000"`
	Attachments []string `json:"attachments"`
}

const (
	StreamUserInput        = "user_input"
	StreamMessageStart     = "message_start"
	StreamReasoningDelta   = "reasoning_delta"
	StreamTextDelta        = "text_delta"
	StreamTextComplete     = "text_complete"
	StreamToolCallStart    = "tool_call_start"
	StreamToolResult       = "tool_result"
	StreamError            = "error"
	StreamMessageDone      = "message_done"
	StreamConversationName = "conversation_title"
)

type StreamEvent struct {
	Type         string           `json:"type"`
	Message      *MessageDTO      `json:"message,omitempty"`
	Conversation *ConversationDTO `json:"conversation,omitempty"`
	MessageID    string           `json:"message_id,omitempty"`
	Model        string           `json:"model,omitempty"`
	Delta        string           `json:"delta,omitempty"`
	Text         string           `json:"text,omitempty"`
	ToolUseID    string           `json:"tool_use_id,omitempty"`
	Name         string           `json:"name,omitempty"`
	Input        json.RawMessage  `json:"input,omitempty"`
	Output       json.RawMessage  `json:"output,omitempty"`
	IsError      bool             `json:"is_error,omitempty"`
	ElapsedMS    int              `json:"elapsed_ms,omitempty"`
	FinishReason string           `json:"finish_reason,omitempty"`
	Code         string           `json:"code,omitempty"`
	ErrorMsg     string           `json:"error,omitempty"`
}

func toConversationDTO(c *Conversation) ConversationDTO {
	return ConversationDTO{
		ID:        c.ID.Hex(),
		Title:     c.Title,
		CreatedAt: c.CreatedAt.Format(time.RFC3339),
		UpdatedAt: c.UpdatedAt.Format(time.RFC3339),
	}
}
