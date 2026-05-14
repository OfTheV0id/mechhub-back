package solochat

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"

	MessageTypeText    = "text"
	MessageTypeGrading = "grading"

	MessageStatusStreaming = "streaming"
	MessageStatusCompleted = "completed"
	MessageStatusFailed    = "failed"

	TaskStatusPending    = "pending"
	TaskStatusProcessing = "processing"
	TaskStatusCompleted  = "completed"
	TaskStatusFailed     = "failed"

	FileKindImage    = "image"
	FileKindText     = "text"
	FileKindDocument = "document"

	TaskFileRoleImage   = "image"
	TaskFileRoleContext = "context"
)

type Conversation struct {
	ID        bson.ObjectID `bson:"_id"`
	UserID    bson.ObjectID `bson:"user_id"`
	Title     string        `bson:"title"`
	CreatedAt time.Time     `bson:"created_at"`
	UpdatedAt time.Time     `bson:"updated_at"`
}

type Message struct {
	ID             bson.ObjectID `bson:"_id"`
	ConversationID bson.ObjectID `bson:"conversation_id"`
	Role           string        `bson:"role"`
	Type           string        `bson:"type"`
	Content        string        `bson:"content"`
	Status         string        `bson:"status"`
	CreatedAt      time.Time     `bson:"created_at"`
}

type UploadedFile struct {
	ID          bson.ObjectID `bson:"_id"`
	OwnerUserID bson.ObjectID `bson:"owner_user_id"`
	OSSKey      string        `bson:"oss_key"`
	OriginalName string       `bson:"original_name"`
	MimeType    string        `bson:"mime_type"`
	Kind        string        `bson:"kind"`
	Size        int64         `bson:"size"`
	CreatedAt   time.Time     `bson:"created_at"`
}

type MessageFile struct {
	ID        bson.ObjectID `bson:"_id"`
	MessageID bson.ObjectID `bson:"message_id"`
	FileID    bson.ObjectID `bson:"file_id"`
}

type GradingTask struct {
	ID                 bson.ObjectID `bson:"_id"`
	ConversationID     bson.ObjectID `bson:"conversation_id"`
	UserID             bson.ObjectID `bson:"user_id"`
	MessageID          bson.ObjectID `bson:"message_id"`
	PromptText         string        `bson:"prompt_text"`
	Status             string        `bson:"status"`
	SelectedImageCount int           `bson:"selected_image_count"`
	OverallScore       *float64      `bson:"overall_score,omitempty"`
	OverallComment     string        `bson:"overall_comment,omitempty"`
	ErrorMessage       string        `bson:"error_message,omitempty"`
	CreatedAt          time.Time     `bson:"created_at"`
	UpdatedAt          time.Time     `bson:"updated_at"`
}

type GradingTaskFile struct {
	ID     bson.ObjectID `bson:"_id"`
	TaskID bson.ObjectID `bson:"task_id"`
	FileID bson.ObjectID `bson:"file_id"`
	Role   string        `bson:"role"`
}

type GradingAnnotation struct {
	ID                bson.ObjectID `bson:"_id"`
	TaskID            bson.ObjectID `bson:"task_id"`
	FileID            bson.ObjectID `bson:"file_id"`
	BBoxX             float64       `bson:"bbox_x"`
	BBoxY             float64       `bson:"bbox_y"`
	BBoxW             float64       `bson:"bbox_w"`
	BBoxH             float64       `bson:"bbox_h"`
	RecognizedText    string        `bson:"recognized_text"`
	RecognizedFormula string        `bson:"recognized_formula"`
	Commentary        string        `bson:"commentary"`
	Severity          string        `bson:"severity"`
}

type GradingTaskDTO struct {
	ID                 string   `json:"id"`
	ConversationID     string   `json:"conversation_id"`
	MessageID          string   `json:"message_id"`
	PromptText         string   `json:"prompt_text"`
	Status             string   `json:"status"`
	SelectedImageCount int      `json:"selected_image_count"`
	OverallScore       *float64 `json:"overall_score,omitempty"`
	OverallComment     string   `json:"overall_comment,omitempty"`
	ErrorMessage       string   `json:"error_message,omitempty"`
	CreatedAt          string   `json:"created_at"`
	UpdatedAt          string   `json:"updated_at"`
}

type AnnotationDTO struct {
	ID                string  `json:"id"`
	FileID            string  `json:"file_id"`
	BBoxX             float64 `json:"bbox_x"`
	BBoxY             float64 `json:"bbox_y"`
	BBoxW             float64 `json:"bbox_w"`
	BBoxH             float64 `json:"bbox_h"`
	RecognizedText    string  `json:"recognized_text,omitempty"`
	RecognizedFormula string  `json:"recognized_formula,omitempty"`
	Commentary        string  `json:"commentary"`
	Severity          string  `json:"severity"`
}

type CreateGradingTaskReq struct {
	PromptText  string   `json:"prompt_text" binding:"required,min=1,max=4000"`
	Attachments []string `json:"attachments" binding:"required,min=1"`
}

const (
	StreamGradingStart      = "grading_start"
	StreamGradingStatus     = "grading_status"
	StreamGradingAnnotation = "grading_annotation"
)

type GradingEvent struct {
	Type       string             `json:"type"`
	Task       *GradingTaskDTO    `json:"task,omitempty"`
	Annotation *AnnotationDTO     `json:"annotation,omitempty"`
	Message    *MessageDTO        `json:"message,omitempty"`
}

func toGradingTaskDTO(t *GradingTask) GradingTaskDTO {
	return GradingTaskDTO{
		ID:                 t.ID.Hex(),
		ConversationID:     t.ConversationID.Hex(),
		MessageID:          t.MessageID.Hex(),
		PromptText:         t.PromptText,
		Status:             t.Status,
		SelectedImageCount: t.SelectedImageCount,
		OverallScore:       t.OverallScore,
		OverallComment:     t.OverallComment,
		ErrorMessage:       t.ErrorMessage,
		CreatedAt:          t.CreatedAt.Format(time.RFC3339),
		UpdatedAt:          t.UpdatedAt.Format(time.RFC3339),
	}
}

func toAnnotationDTO(a *GradingAnnotation) AnnotationDTO {
	return AnnotationDTO{
		ID:                a.ID.Hex(),
		FileID:            a.FileID.Hex(),
		BBoxX:             a.BBoxX,
		BBoxY:             a.BBoxY,
		BBoxW:             a.BBoxW,
		BBoxH:             a.BBoxH,
		RecognizedText:    a.RecognizedText,
		RecognizedFormula: a.RecognizedFormula,
		Commentary:        a.Commentary,
		Severity:          a.Severity,
	}
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
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	Role           string `json:"role"`
	Type           string `json:"type"`
	Content        string `json:"content"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
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

type StreamEvent struct {
	Type         string           `json:"type"`
	Message      *MessageDTO      `json:"message,omitempty"`
	Conversation *ConversationDTO `json:"conversation,omitempty"`
	MessageID    string           `json:"messageId,omitempty"`
	Delta        string           `json:"delta,omitempty"`
	Error        string           `json:"error,omitempty"`
}

const (
	StreamUserInput        = "user_input"
	StreamAssistantStart   = "assistant_start"
	StreamAssistantDelta   = "assistant_delta"
	StreamAssistantDone    = "assistant_done"
	StreamAssistantError   = "assistant_error"
	StreamConversationName = "conversation_title"
)

func toConversationDTO(c *Conversation) ConversationDTO {
	return ConversationDTO{
		ID:        c.ID.Hex(),
		Title:     c.Title,
		CreatedAt: c.CreatedAt.Format(time.RFC3339),
		UpdatedAt: c.UpdatedAt.Format(time.RFC3339),
	}
}

func toMessageDTO(m *Message) MessageDTO {
	return MessageDTO{
		ID:             m.ID.Hex(),
		ConversationID: m.ConversationID.Hex(),
		Role:           m.Role,
		Type:           m.Type,
		Content:        m.Content,
		Status:         m.Status,
		CreatedAt:      m.CreatedAt.Format(time.RFC3339),
	}
}
