package solochat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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
}

func NewService(repo *Repo, agentClient *agent.Client, oss *storage.OSS, cfg *config.Config) *Service {
	return &Service{repo: repo, agent: agentClient, oss: oss, cfg: cfg}
}

var allowedMimeKind = map[string]string{
	"image/png":       FileKindImage,
	"image/jpeg":      FileKindImage,
	"image/webp":      FileKindImage,
	"image/gif":       FileKindImage,
	"text/plain":      FileKindText,
	"text/markdown":   FileKindText,
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

func (s *Service) openAttachmentsForAgent(ctx context.Context, files []UploadedFile) ([]agent.FileInput, []io.Closer, error) {
	inputs := make([]agent.FileInput, 0, len(files))
	closers := make([]io.Closer, 0, len(files))
	for _, f := range files {
		body, err := s.oss.Download(ctx, f.OSSKey)
		if err != nil {
			return nil, closers, err
		}
		closers = append(closers, body)
		inputs = append(inputs, agent.FileInput{
			Filename:    f.OriginalName,
			ContentType: f.MimeType,
			Body:        body,
		})
	}
	return inputs, closers, nil
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

func (s *Service) ListMessages(ctx context.Context, conversationID, userID bson.ObjectID) ([]Message, []MessageDTO, error) {
	if _, err := s.repo.FindConversation(ctx, conversationID, userID); err != nil {
		return nil, nil, err
	}
	msgs, err := s.repo.ListMessages(ctx, conversationID)
	if err != nil {
		return nil, nil, err
	}
	messageIDs := make([]bson.ObjectID, len(msgs))
	for i, m := range msgs {
		messageIDs[i] = m.ID
	}
	bindings, err := s.repo.FindMessageFiles(ctx, messageIDs)
	if err != nil {
		return nil, nil, err
	}
	allFileIDs := make([]bson.ObjectID, 0)
	for _, ids := range bindings {
		allFileIDs = append(allFileIDs, ids...)
	}
	files, err := s.repo.FindFilesByIDs(ctx, allFileIDs, userID)
	if err != nil {
		return nil, nil, err
	}
	fileByID := make(map[bson.ObjectID]UploadedFile, len(files))
	for _, f := range files {
		fileByID[f.ID] = f
	}

	dtos := make([]MessageDTO, len(msgs))
	for i := range msgs {
		dto := toMessageDTO(&msgs[i])
		for _, fid := range bindings[msgs[i].ID] {
			if f, ok := fileByID[fid]; ok {
				dto.Attachments = append(dto.Attachments, s.ToAttachmentDTO(&f))
			}
		}
		dtos[i] = dto
	}
	return msgs, dtos, nil
}

func (s *Service) SendMessageStream(c *gin.Context, conversationID, userID bson.ObjectID, content string, attachmentIDs []bson.ObjectID) {
	ctx := c.Request.Context()
	w := newNDJSON(c)

	conv, err := s.repo.FindConversation(ctx, conversationID, userID)
	if err != nil {
		w.write(StreamEvent{Type: StreamError, ErrorMsg: "对话不存在"})
		return
	}

	var files []UploadedFile
	if len(attachmentIDs) > 0 {
		files, err = s.repo.FindFilesByIDs(ctx, attachmentIDs, userID)
		if err != nil {
			w.write(StreamEvent{Type: StreamError, ErrorMsg: err.Error()})
			return
		}
		if len(files) != len(attachmentIDs) {
			w.write(StreamEvent{Type: StreamError, ErrorMsg: "部分附件不存在或无权访问"})
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
		Parts:          []MessagePart{textPart(content)},
		Status:         MessageStatusCompleted,
		CreatedAt:      now,
	}
	if err := s.repo.InsertMessage(ctx, userMsg); err != nil {
		w.write(StreamEvent{Type: StreamError, ErrorMsg: err.Error()})
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
	for _, f := range files {
		userDTO.Attachments = append(userDTO.Attachments, s.ToAttachmentDTO(&f))
	}
	w.write(StreamEvent{Type: StreamUserInput, Message: &userDTO})

	assistantMsg := &Message{
		ID:             bson.NewObjectID(),
		ConversationID: conversationID,
		Role:           RoleAssistant,
		Parts:          []MessagePart{},
		Status:         MessageStatusStreaming,
		CreatedAt:      time.Now(),
	}
	if err := s.repo.InsertMessage(ctx, assistantMsg); err != nil {
		w.write(StreamEvent{Type: StreamError, ErrorMsg: err.Error()})
		return
	}

	inputs, closers, err := s.openAttachmentsForAgent(ctx, files)
	if err != nil {
		closeAll(closers)
		_ = s.repo.FinalizeMessage(ctx, assistantMsg.ID, MessageStatusFailed, assistantMsg.Parts, "error")
		w.write(StreamEvent{Type: StreamError, MessageID: assistantMsg.ID.Hex(), ErrorMsg: err.Error()})
		return
	}

	events, err := s.agent.Chat(ctx, agent.ChatRequest{
		SessionID: conversationID.Hex(),
		Message:   content,
		Files:     inputs,
	})
	closeAll(closers)
	if err != nil {
		_ = s.repo.FinalizeMessage(ctx, assistantMsg.ID, MessageStatusFailed, assistantMsg.Parts, "error")
		w.write(StreamEvent{Type: StreamError, MessageID: assistantMsg.ID.Hex(), ErrorMsg: err.Error()})
		return
	}

	parts, finishReason, streamErr := s.consumeAgentStream(events, assistantMsg.ID.Hex(), w)
	status := MessageStatusCompleted
	if streamErr != "" {
		status = MessageStatusFailed
	}
	assistantMsg.Parts = parts
	assistantMsg.Status = status
	assistantMsg.FinishReason = finishReason
	_ = s.repo.FinalizeMessage(ctx, assistantMsg.ID, status, parts, finishReason)

	if isFirstMessage {
		title := autoTitle(content)
		if title != "" {
			_ = s.repo.UpdateConversationTitle(context.Background(), conversationID, userID, title)
			conv.Title = title
			conv.UpdatedAt = time.Now()
			dto := toConversationDTO(conv)
			w.write(StreamEvent{Type: StreamConversationName, Conversation: &dto})
		}
	} else {
		_ = s.repo.TouchConversation(ctx, conversationID)
	}
}

func (s *Service) consumeAgentStream(events <-chan agent.Event, messageID string, w *ndjsonWriter) ([]MessagePart, string, string) {
	parts := make([]MessagePart, 0, 8)
	finishReason := "stop"
	streamErr := ""

	var prevKind string
	var textBuf strings.Builder
	var thinkBuf strings.Builder

	flushBuffers := func() {
		if textBuf.Len() > 0 {
			parts = append(parts, MessagePart{Type: PartText, Text: textBuf.String()})
			textBuf.Reset()
		}
		if thinkBuf.Len() > 0 {
			parts = append(parts, MessagePart{Type: PartThinking, Text: thinkBuf.String()})
			thinkBuf.Reset()
		}
		prevKind = ""
	}

	for ev := range events {
		ev.MessageID = messageID
		switch ev.Type {
		case agent.EventMessageStart:
			w.write(StreamEvent{Type: StreamMessageStart, MessageID: messageID, Model: ev.Model})
		case agent.EventReasoningDelta:
			if prevKind == "text" {
				if textBuf.Len() > 0 {
					parts = append(parts, MessagePart{Type: PartText, Text: textBuf.String()})
					textBuf.Reset()
				}
			}
			thinkBuf.WriteString(ev.Delta)
			prevKind = "thinking"
			w.write(StreamEvent{Type: StreamReasoningDelta, MessageID: messageID, Delta: ev.Delta})
		case agent.EventTextDelta:
			if prevKind == "thinking" {
				if thinkBuf.Len() > 0 {
					parts = append(parts, MessagePart{Type: PartThinking, Text: thinkBuf.String()})
					thinkBuf.Reset()
				}
			}
			textBuf.WriteString(ev.Delta)
			prevKind = "text"
			w.write(StreamEvent{Type: StreamTextDelta, MessageID: messageID, Delta: ev.Delta})
		case agent.EventTextComplete:
			if ev.Text != "" {
				textBuf.Reset()
				textBuf.WriteString(ev.Text)
			}
			if textBuf.Len() > 0 {
				parts = append(parts, MessagePart{Type: PartText, Text: textBuf.String()})
				textBuf.Reset()
			}
			prevKind = ""
			w.write(StreamEvent{Type: StreamTextComplete, MessageID: messageID, Text: ev.Text})
		case agent.EventToolCallStart:
			flushBuffers()
			parts = append(parts, MessagePart{
				Type:      PartToolUse,
				ToolUseID: ev.ToolUseID,
				Name:      ev.Name,
				Input:     cloneRaw(ev.Input),
			})
			w.write(StreamEvent{Type: StreamToolCallStart, MessageID: messageID, ToolUseID: ev.ToolUseID, Name: ev.Name, Input: ev.Input})
		case agent.EventToolResult:
			parts = append(parts, MessagePart{
				Type:      PartToolResult,
				ToolUseID: ev.ToolUseID,
				Name:      ev.Name,
				Output:    cloneRaw(ev.Output),
				IsError:   ev.IsError,
				ElapsedMS: ev.ElapsedMS,
			})
			w.write(StreamEvent{Type: StreamToolResult, MessageID: messageID, ToolUseID: ev.ToolUseID, Name: ev.Name, Output: ev.Output, IsError: ev.IsError, ElapsedMS: ev.ElapsedMS})
		case agent.EventError:
			streamErr = ev.Message
			if streamErr == "" {
				streamErr = "agent error"
			}
			finishReason = "error"
			w.write(StreamEvent{Type: StreamError, MessageID: messageID, Code: ev.Code, ErrorMsg: streamErr})
		case agent.EventMessageDone:
			flushBuffers()
			if ev.FinishReason != "" {
				finishReason = ev.FinishReason
			}
			w.write(StreamEvent{Type: StreamMessageDone, MessageID: messageID, FinishReason: finishReason})
		}
	}
	flushBuffers()
	return parts, finishReason, streamErr
}

func cloneRaw(r json.RawMessage) json.RawMessage {
	if len(r) == 0 {
		return nil
	}
	out := make(json.RawMessage, len(r))
	copy(out, r)
	return out
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
