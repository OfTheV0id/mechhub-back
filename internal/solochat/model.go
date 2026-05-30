package solochat

import (
	"encoding/json"
	"time"
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
	ID        string    `gorm:"primaryKey;type:char(36)"`
	UserID    string    `gorm:"type:char(36);not null;index:idx_user_updated,priority:1"`
	Title     string    `gorm:"type:varchar(120)"`
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null;index:idx_user_updated,priority:2,sort:desc"`
	// TitleGenerated 标记是否已经为本会话生成过自动标题(AI 总结或兜底)。
	// 取代旧的 `CreatedAt.Equal(UpdatedAt)` 判定:一旦 rename / touch 改过
	// UpdatedAt,旧逻辑就永远不再尝试生成 AI 标题。
	TitleGenerated bool `gorm:"not null;default:false"`
}

func (Conversation) TableName() string { return "solochat_conversations" }

type UploadedFile struct {
	ID           string    `gorm:"primaryKey;type:char(36)"`
	OwnerUserID  string    `gorm:"type:char(36);not null;index"`
	OSSKey       string    `gorm:"type:varchar(255)"`
	OriginalName string    `gorm:"type:varchar(255)"`
	MimeType     string    `gorm:"type:varchar(64)"`
	Kind         string    `gorm:"type:varchar(16)"`
	Size         int64
	CreatedAt    time.Time `gorm:"not null"`
}

func (UploadedFile) TableName() string { return "uploaded_files" }

// MessagePart 是前端渲染的最小单位,由 Go 把 Python AgentMessagePart 转一下
// 复用本结构。不落库(消息源全部在 ADK MySQL),只作 DTO 字段。
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

type UpdateConversationReq struct {
	Title string `json:"title" binding:"required,min=1,max=100"`
}

type SendMessageReq struct {
	Content     string   `json:"content" binding:"max=8000"`
	Attachments []string `json:"attachments"`
	Grading     bool     `json:"grading,omitempty"`
	WebSearch   bool     `json:"web_search,omitempty"`
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
		ID:        c.ID,
		Title:     c.Title,
		CreatedAt: c.CreatedAt.Format(time.RFC3339),
		UpdatedAt: c.UpdatedAt.Format(time.RFC3339),
	}
}
