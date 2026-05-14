package solochat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"mime/multipart"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"

	"mechhub-back/internal/agent"
	"mechhub-back/internal/config"
	"mechhub-back/internal/storage"
)

var ErrFileTooLarge = errors.New("solochat: file too large")
var ErrFileTypeNotAllowed = errors.New("solochat: file type not allowed")
var ErrTooManyAttachments = errors.New("solochat: too many attachments")

type Service struct {
	repo  *Repo
	agent *agent.Client
	oss   *storage.OSS
	cfg   *config.Config
	hub   *Hub
}

func NewService(repo *Repo, agentClient *agent.Client, oss *storage.OSS, cfg *config.Config) *Service {
	return &Service{repo: repo, agent: agentClient, oss: oss, cfg: cfg, hub: NewHub()}
}

func (s *Service) Hub() *Hub {
	return s.hub
}

func (s *Service) RecoverPendingTasks(ctx context.Context) error {
	return s.repo.MarkAllProcessingFailed(ctx)
}

func (s *Service) ListGradingTasks(ctx context.Context, conversationID, userID bson.ObjectID) ([]GradingTask, error) {
	if _, err := s.repo.FindConversation(ctx, conversationID, userID); err != nil {
		return nil, err
	}
	return s.repo.ListGradingTasks(ctx, conversationID)
}

func (s *Service) GetGradingTaskWithAnnotations(ctx context.Context, taskID, userID bson.ObjectID) (*GradingTask, []GradingAnnotation, error) {
	t, err := s.repo.FindGradingTask(ctx, taskID, userID)
	if err != nil {
		return nil, nil, err
	}
	anns, err := s.repo.ListAnnotations(ctx, taskID)
	if err != nil {
		return nil, nil, err
	}
	return t, anns, nil
}

var allowedMimeKind = map[string]string{
	"image/png":       FileKindImage,
	"image/jpeg":      FileKindImage,
	"image/webp":      FileKindImage,
	"image/gif":       FileKindImage,
	"text/plain":      FileKindText,
	"application/pdf": FileKindDocument,
}

func (s *Service) UploadAttachments(ctx context.Context, ownerID bson.ObjectID, files []*multipart.FileHeader) ([]UploadedFile, error) {
	if len(files) > s.cfg.Solochat.MaxAttachmentsPerMessage {
		return nil, ErrTooManyAttachments
	}
	out := make([]UploadedFile, 0, len(files))
	for _, fh := range files {
		if fh.Size > s.cfg.Solochat.MaxFileSize {
			return nil, ErrFileTooLarge
		}
		mime := fh.Header.Get("Content-Type")
		if mime == "" {
			mime = "application/octet-stream"
		}
		kind, ok := allowedMimeKind[mime]
		if !ok {
			return nil, ErrFileTypeNotAllowed
		}

		src, err := fh.Open()
		if err != nil {
			return nil, err
		}
		suffix, err := randomHex(8)
		if err != nil {
			src.Close()
			return nil, err
		}
		ext := filepath.Ext(fh.Filename)
		key := "solochat/" + ownerID.Hex() + "/" + suffix + ext
		if err := s.oss.Upload(ctx, key, src, mime); err != nil {
			src.Close()
			return nil, err
		}
		src.Close()

		f := UploadedFile{
			ID:           bson.NewObjectID(),
			OwnerUserID:  ownerID,
			OSSKey:       key,
			OriginalName: fh.Filename,
			MimeType:     mime,
			Kind:         kind,
			Size:         fh.Size,
			CreatedAt:    time.Now(),
		}
		if err := s.repo.InsertFile(ctx, &f); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

func (s *Service) GetAttachment(ctx context.Context, id, ownerID bson.ObjectID) (*UploadedFile, error) {
	return s.repo.FindFile(ctx, id, ownerID)
}

func (s *Service) AttachmentURL(key string) string {
	return s.oss.PublicURL(key)
}

func (s *Service) ToAttachmentDTO(f *UploadedFile) AttachmentDTO {
	return AttachmentDTO{
		ID:           f.ID.Hex(),
		Kind:         f.Kind,
		MimeType:     f.MimeType,
		OriginalName: f.OriginalName,
		Size:         f.Size,
		URL:          s.oss.PublicURL(f.OSSKey),
	}
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *Service) openAttachmentsForAgent(ctx context.Context, files []UploadedFile) ([]agent.ImageInput, []io.Closer, error) {
	images := make([]agent.ImageInput, 0, len(files))
	closers := make([]io.Closer, 0, len(files))
	for _, f := range files {
		if f.Kind != FileKindImage {
			continue
		}
		body, err := s.oss.Download(ctx, f.OSSKey)
		if err != nil {
			return nil, closers, err
		}
		closers = append(closers, body)
		images = append(images, agent.ImageInput{
			Filename:    f.OriginalName,
			ContentType: f.MimeType,
			Body:        body,
		})
	}
	return images, closers, nil
}

func closeAll(cs []io.Closer) {
	for _, c := range cs {
		_ = c.Close()
	}
}

func (s *Service) CreateConversation(ctx context.Context, userID bson.ObjectID, title string) (*Conversation, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "新对话"
	}
	now := time.Now()
	c := &Conversation{
		ID:        bson.NewObjectID(),
		UserID:    userID,
		Title:     title,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repo.InsertConversation(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Service) ListConversations(ctx context.Context, userID bson.ObjectID) ([]Conversation, error) {
	return s.repo.ListConversations(ctx, userID)
}

func (s *Service) UpdateConversation(ctx context.Context, id, userID bson.ObjectID, title string) (*Conversation, error) {
	title = strings.TrimSpace(title)
	if err := s.repo.UpdateConversationTitle(ctx, id, userID, title); err != nil {
		return nil, err
	}
	return s.repo.FindConversation(ctx, id, userID)
}

func (s *Service) DeleteConversation(ctx context.Context, id, userID bson.ObjectID) error {
	return s.repo.DeleteConversation(ctx, id, userID)
}

func (s *Service) ListMessages(ctx context.Context, conversationID, userID bson.ObjectID) ([]Message, error) {
	if _, err := s.repo.FindConversation(ctx, conversationID, userID); err != nil {
		return nil, err
	}
	return s.repo.ListMessages(ctx, conversationID)
}

func (s *Service) SendMessageStream(c *gin.Context, conversationID, userID bson.ObjectID, content string, attachmentIDs []bson.ObjectID) {
	ctx := c.Request.Context()
	w := newNDJSON(c)

	conv, err := s.repo.FindConversation(ctx, conversationID, userID)
	if err != nil {
		w.write(StreamEvent{Type: StreamAssistantError, Error: "对话不存在"})
		return
	}

	var files []UploadedFile
	if len(attachmentIDs) > 0 {
		files, err = s.repo.FindFilesByIDs(ctx, attachmentIDs, userID)
		if err != nil {
			w.write(StreamEvent{Type: StreamAssistantError, Error: err.Error()})
			return
		}
		if len(files) != len(attachmentIDs) {
			w.write(StreamEvent{Type: StreamAssistantError, Error: "部分附件不存在或无权访问"})
			return
		}
	}

	count, _ := s.repo.CountConversationMessages(ctx, conversationID)
	isFirstMessage := count == 0

	now := time.Now()
	userMsg := &Message{
		ID:             bson.NewObjectID(),
		ConversationID: conversationID,
		Role:           RoleUser,
		Type:           MessageTypeText,
		Content:        content,
		Status:         MessageStatusCompleted,
		CreatedAt:      now,
	}
	if err := s.repo.InsertMessage(ctx, userMsg); err != nil {
		w.write(StreamEvent{Type: StreamAssistantError, Error: err.Error()})
		return
	}
	if len(files) > 0 {
		fileIDs := make([]bson.ObjectID, len(files))
		for i, f := range files {
			fileIDs[i] = f.ID
		}
		_ = s.repo.BindMessageFiles(ctx, userMsg.ID, fileIDs)
	}
	userDTO := toMessageDTO(userMsg)
	w.write(StreamEvent{Type: StreamUserInput, Message: &userDTO})

	assistantMsg := &Message{
		ID:             bson.NewObjectID(),
		ConversationID: conversationID,
		Role:           RoleAssistant,
		Type:           MessageTypeText,
		Content:        "",
		Status:         MessageStatusStreaming,
		CreatedAt:      time.Now(),
	}
	if err := s.repo.InsertMessage(ctx, assistantMsg); err != nil {
		w.write(StreamEvent{Type: StreamAssistantError, Error: err.Error()})
		return
	}
	asstDTO := toMessageDTO(assistantMsg)
	w.write(StreamEvent{Type: StreamAssistantStart, Message: &asstDTO})

	images, closers, err := s.openAttachmentsForAgent(ctx, files)
	if err != nil {
		closeAll(closers)
		_ = s.repo.UpdateMessageStatus(ctx, assistantMsg.ID, MessageStatusFailed, "")
		w.write(StreamEvent{Type: StreamAssistantError, MessageID: assistantMsg.ID.Hex(), Error: err.Error()})
		return
	}

	events, err := s.agent.Chat(ctx, agent.ChatRequest{
		SessionID: conversationID.Hex(),
		Message:   content,
		Images:    images,
	})
	closeAll(closers)
	if err != nil {
		_ = s.repo.UpdateMessageStatus(ctx, assistantMsg.ID, MessageStatusFailed, "")
		w.write(StreamEvent{Type: StreamAssistantError, MessageID: assistantMsg.ID.Hex(), Error: err.Error()})
		return
	}

	var buf strings.Builder
	var streamErr string
	for ev := range events {
		switch ev.Type {
		case agent.EventText:
			if ev.Content == "" {
				continue
			}
			buf.WriteString(ev.Content)
			w.write(StreamEvent{Type: StreamAssistantDelta, MessageID: assistantMsg.ID.Hex(), Delta: ev.Content})
		case agent.EventError:
			streamErr = ev.Message
			if streamErr == "" {
				streamErr = "agent error"
			}
		}
	}

	if streamErr != "" {
		_ = s.repo.UpdateMessageStatus(ctx, assistantMsg.ID, MessageStatusFailed, buf.String())
		w.write(StreamEvent{Type: StreamAssistantError, MessageID: assistantMsg.ID.Hex(), Error: streamErr})
		return
	}

	finalContent := buf.String()
	if err := s.repo.UpdateMessageStatus(ctx, assistantMsg.ID, MessageStatusCompleted, finalContent); err != nil {
		w.write(StreamEvent{Type: StreamAssistantError, MessageID: assistantMsg.ID.Hex(), Error: err.Error()})
		return
	}
	assistantMsg.Status = MessageStatusCompleted
	assistantMsg.Content = finalContent

	if isFirstMessage {
		title := autoTitle(content)
		if title != "" {
			_ = s.repo.UpdateConversationTitle(ctx, conversationID, userID, title)
			conv.Title = title
			conv.UpdatedAt = time.Now()
			dto := toConversationDTO(conv)
			w.write(StreamEvent{Type: StreamConversationName, Conversation: &dto})
		}
	} else {
		_ = s.repo.TouchConversation(ctx, conversationID)
	}

	doneDTO := toMessageDTO(assistantMsg)
	w.write(StreamEvent{Type: StreamAssistantDone, Message: &doneDTO})
}

func autoTitle(firstMessage string) string {
	t := strings.TrimSpace(firstMessage)
	if t == "" {
		return ""
	}
	runes := []rune(t)
	if len(runes) > 24 {
		t = string(runes[:24]) + "…"
	}
	return t
}

