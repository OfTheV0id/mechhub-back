package channel

import "time"

// ============ 表 ============

type Channel struct {
	ID          string    `gorm:"primaryKey;type:char(36)"`
	ClassID     string    `gorm:"type:char(36);not null;uniqueIndex:idx_class_channel_name,priority:1;index:idx_class_position,priority:1"`
	Name        string    `gorm:"type:varchar(64);not null;uniqueIndex:idx_class_channel_name,priority:2"`
	Description string    `gorm:"type:varchar(500);not null;default:''"`
	Topic       string    `gorm:"type:varchar(120);not null;default:''"`
	IsDefault   bool      `gorm:"not null;default:false"`
	Position    int       `gorm:"not null;default:0;index:idx_class_position,priority:2"`
	CreatedBy   string    `gorm:"type:char(36);not null"`
	CreatedAt   time.Time `gorm:"not null"`
	UpdatedAt   time.Time `gorm:"not null"`
}

func (Channel) TableName() string { return "channels" }

type Message struct {
	ID           string     `gorm:"primaryKey;type:char(36)"`
	ChannelID    string     `gorm:"type:char(36);not null;index:idx_channel_created,priority:1"`
	ClassID      string     `gorm:"type:char(36);not null"`
	AuthorUserID string     `gorm:"type:char(36);not null;index"`
	Content      string     `gorm:"type:text;not null"`
	EditedAt     *time.Time `gorm:""`
	CreatedAt    time.Time  `gorm:"not null;index:idx_channel_created,priority:2,sort:desc"`
}

func (Message) TableName() string { return "channel_messages" }

type Attachment struct {
	ID           string    `gorm:"primaryKey;type:char(36)"`
	ChannelID    string    `gorm:"type:char(36);not null;index"`
	UploaderID   string    `gorm:"type:char(36);not null;index"`
	OSSKey       string    `gorm:"type:varchar(255);not null"`
	OriginalName string    `gorm:"type:varchar(255)"`
	MimeType     string    `gorm:"type:varchar(64)"`
	SizeBytes    int64     `gorm:"not null"`
	MessageID    *string   `gorm:"type:char(36);index"`
	CreatedAt    time.Time `gorm:"not null"`
}

func (Attachment) TableName() string { return "channel_attachments" }

// 默认频道
const (
	DefaultChannelName  = "general"
	DefaultChannelTopic = "班级公共讨论区"
)

// 消息编辑/删除最大长度
const (
	MaxContentLen     = 4000
	MaxNameLen        = 64
	MaxDescriptionLen = 500
	MaxTopicLen       = 120
	MaxAttachmentsPerMessage = 5
	MaxAttachmentBytes       = 20 * 1024 * 1024 // 20 MiB
)

// ============ Repo join 行 ============

type MessageWithAuthor struct {
	Message
	AuthorEmail     string    `gorm:"column:author_email"`
	AuthorName      string    `gorm:"column:author_name"`
	AuthorRole      string    `gorm:"column:author_role"`
	AuthorAvatarKey string    `gorm:"column:author_avatar_key"`
	AuthorCreatedAt time.Time `gorm:"column:author_created_at"`
}

// ============ Request DTOs ============

type CreateChannelReq struct {
	Name        string `json:"name"                  binding:"required,min=1,max=64"`
	Description string `json:"description,omitempty" binding:"max=500"`
	Topic       string `json:"topic,omitempty"       binding:"max=120"`
	Position    *int   `json:"position,omitempty"`
}

type UpdateChannelReq struct {
	Name        *string `json:"name,omitempty"        binding:"omitempty,min=1,max=64"`
	Description *string `json:"description,omitempty" binding:"omitempty,max=500"`
	Topic       *string `json:"topic,omitempty"       binding:"omitempty,max=120"`
	Position    *int    `json:"position,omitempty"`
}

type SendMessageReq struct {
	Content       string   `json:"content"                  binding:"required,min=1,max=4000"`
	AttachmentIDs []string `json:"attachment_ids,omitempty" binding:"max=5,dive,len=36"`
}

type EditMessageReq struct {
	Content string `json:"content" binding:"required,min=1,max=4000"`
}

// ============ Response DTOs ============

type ChannelDTO struct {
	ID          string `json:"id"`
	ClassID     string `json:"class_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Topic       string `json:"topic"`
	IsDefault   bool   `json:"is_default"`
	Position    int    `json:"position"`
	CreatedBy   string `json:"created_by"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type MessageDTO struct {
	ID          string          `json:"id"`
	ChannelID   string          `json:"channel_id"`
	ClassID     string          `json:"class_id"`
	Content     string          `json:"content"`
	Author      MessageAuthor   `json:"author"`
	Attachments []AttachmentDTO `json:"attachments,omitempty"`
	EditedAt    *string         `json:"edited_at,omitempty"`
	CreatedAt   string          `json:"created_at"`
}

type MessageAuthor struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

type AttachmentDTO struct {
	ID           string `json:"id"`
	OriginalName string `json:"original_name"`
	MimeType     string `json:"mime_type"`
	SizeBytes    int64  `json:"size_bytes"`
	URL          string `json:"url"`
}

// MessageDeletedFrame WS 推送 channel.message.deleted 时的 payload
type MessageDeletedFrame struct {
	Type      string `json:"type"`
	ChannelID string `json:"channel_id"`
	ClassID   string `json:"class_id"`
	MessageID string `json:"message_id"`
}

// MessageFrame WS 推送 channel.message.created/updated 时的 payload
type MessageFrame struct {
	Type      string     `json:"type"`
	ChannelID string     `json:"channel_id"`
	ClassID   string     `json:"class_id"`
	Message   MessageDTO `json:"message"`
}
