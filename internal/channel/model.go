package channel

import (
	"time"

	"mechhub-back/internal/reference"
)

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
	ID           string `gorm:"primaryKey;type:char(36)"`
	ChannelID    string `gorm:"type:char(36);not null;index:idx_channel_created,priority:1"`
	ClassID      string `gorm:"type:char(36);not null"`
	AuthorUserID string `gorm:"type:char(36);not null;index"`
	Content      string `gorm:"type:text;not null"`
	// Reference 富引用快照 JSON(序列化 MessageReference)。nil = 普通消息。
	// 从 solochat 分享批改 / 对话片段时写入,内容已自包含(图片 URL 指向频道附件)。
	Reference *string    `gorm:"type:json"`
	EditedAt  *time.Time `gorm:""`
	CreatedAt time.Time  `gorm:"not null;index:idx_channel_created,priority:2,sort:desc"`
}

func (Message) TableName() string { return "channel_messages" }

// MessageReaction 一行 = 某用户对某消息的一个 emoji 反应。
// (message_id, user_id, emoji) 唯一,防同人同 emoji 重复反应。
type MessageReaction struct {
	ID        string    `gorm:"primaryKey;type:char(36)"`
	MessageID string    `gorm:"type:char(36);not null;index;uniqueIndex:idx_msg_user_emoji,priority:1"`
	ChannelID string    `gorm:"type:char(36);not null"`
	ClassID   string    `gorm:"type:char(36);not null"`
	UserID    string    `gorm:"type:char(36);not null;uniqueIndex:idx_msg_user_emoji,priority:2"`
	Emoji     string    `gorm:"type:varchar(32);not null;uniqueIndex:idx_msg_user_emoji,priority:3"`
	CreatedAt time.Time `gorm:"not null"`
}

func (MessageReaction) TableName() string { return "channel_message_reactions" }

// ============ 富引用快照 ============
//
// 类型与构建逻辑统一在 internal/reference(channel 与 assignment 共用,单一真相源)。
// 这里用别名保留 channel 内既有命名,JSON 形状完全一致 —— 前端与库里历史快照零影响。

const (
	ReferenceTypeGrading = reference.TypeGrading
	ReferenceTypeThread  = reference.TypeThread
)

type (
	MessageReference = reference.Reference
	ThreadSegment    = reference.ThreadSegment
	SegmentPart      = reference.SegmentPart
	ReferenceAttach  = reference.Attach
)

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
	MaxContentLen            = 4000
	MaxNameLen               = 64
	MaxDescriptionLen        = 500
	MaxTopicLen              = 120
	MaxAttachmentsPerMessage = 5
	MaxAttachmentBytes       = 20 * 1024 * 1024 // 20 MiB
	MaxReactionLen           = 32
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
	// Share 非 nil 时本条是"分享自 solochat"的富引用消息,后端反查生成快照。
	Share *ShareRefInput `json:"share,omitempty"`
}

// ShareRefInput 前端只传"分享什么"的定位信息,绝不传快照内容本身 —— 快照由
// 后端按 source_chat_id + source_message_id(s) 反查 ADK session 生成,防伪造。
type ShareRefInput struct {
	Type             string   `json:"type"           binding:"required,oneof=grading thread"`
	SourceChatID     string   `json:"source_chat_id" binding:"required,len=36"`
	SourceMessageID  string   `json:"source_message_id,omitempty"`  // grading:含 grade part 的消息
	SourceMessageIDs []string `json:"source_message_ids,omitempty"` // thread:勾选的多条消息
}

type EditMessageReq struct {
	Content string `json:"content" binding:"required,min=1,max=4000"`
}

type ToggleReactionReq struct {
	Emoji string `json:"emoji" binding:"required,min=1,max=32"`
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
	ID          string            `json:"id"`
	ChannelID   string            `json:"channel_id"`
	ClassID     string            `json:"class_id"`
	Content     string            `json:"content"`
	Author      MessageAuthor     `json:"author"`
	Attachments []AttachmentDTO   `json:"attachments,omitempty"`
	Reference   *MessageReference `json:"reference,omitempty"`
	Reactions   []ReactionDTO     `json:"reactions,omitempty"`
	EditedAt    *string           `json:"edited_at,omitempty"`
	CreatedAt   string            `json:"created_at"`
}

// ReactionDTO 一个 emoji 的反应聚合。UserIDs 给前端自行派生 count / "是不是我反应过"——
// 这样一份广播 DTO 对所有接收者都正确,避免服务端按观察者算 me 的错位。
type ReactionDTO struct {
	Emoji   string   `json:"emoji"`
	UserIDs []string `json:"user_ids"`
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
