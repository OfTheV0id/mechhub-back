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
	"github.com/google/uuid"

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

func (s *Service) UploadAttachments(ctx context.Context, ownerID string, files []*multipart.FileHeader) ([]UploadedFile, error) {
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
		key := "solochat/" + ownerID + "/" + suffix + ext
		if err := s.oss.Upload(ctx, key, src, mime); err != nil {
			src.Close()
			return nil, err
		}
		src.Close()

		f := UploadedFile{
			ID:           uuid.NewString(),
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

func (s *Service) GetAttachment(ctx context.Context, id, ownerID string) (*UploadedFile, error) {
	return s.repo.FindFile(ctx, id, ownerID)
}

func (s *Service) AttachmentURL(key string) string {
	return s.oss.PublicURL(key)
}

func (s *Service) ToAttachmentDTO(f *UploadedFile) AttachmentDTO {
	return AttachmentDTO{
		ID:           f.ID,
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

func (s *Service) CreateConversation(ctx context.Context, userID, title string) (*Conversation, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "新对话"
	}
	now := time.Now()
	c := &Conversation{
		ID:        uuid.NewString(),
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

func (s *Service) ListConversations(ctx context.Context, userID string) ([]Conversation, error) {
	return s.repo.ListConversations(ctx, userID)
}

func (s *Service) UpdateConversation(ctx context.Context, id, userID, title string) (*Conversation, error) {
	title = strings.TrimSpace(title)
	if err := s.repo.UpdateConversationTitle(ctx, id, userID, title); err != nil {
		return nil, err
	}
	return s.repo.FindConversation(ctx, id, userID)
}

func (s *Service) DeleteConversation(ctx context.Context, id, userID string) error {
	return s.repo.DeleteConversation(ctx, id, userID)
}

// ListMessages 代理 Python /sessions/{id}/messages,拿到翻译好的 DTO 后
// 把每条 user message 的 attachments(只含 file_id)hydrate 成完整
// AttachmentDTO(URL / MIME / 文件名 / 大小)。
func (s *Service) ListMessages(ctx context.Context, conversationID, userID string) ([]MessageDTO, error) {
	if _, err := s.repo.FindConversation(ctx, conversationID, userID); err != nil {
		return nil, err
	}
	rows, err := s.agent.FetchMessages(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []MessageDTO{}, nil
	}

	fileIDSet := make(map[string]struct{})
	for _, r := range rows {
		for _, a := range r.Attachments {
			fileIDSet[a.ID] = struct{}{}
		}
	}
	fileMap := make(map[string]UploadedFile)
	if len(fileIDSet) > 0 {
		ids := make([]string, 0, len(fileIDSet))
		for fid := range fileIDSet {
			ids = append(ids, fid)
		}
		files, err := s.repo.FindFilesByIDs(ctx, ids, userID)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			fileMap[f.ID] = f
		}
	}

	out := make([]MessageDTO, 0, len(rows))
	for _, r := range rows {
		parts := make([]MessagePart, 0, len(r.Parts))
		for _, p := range r.Parts {
			parts = append(parts, MessagePart{
				Type:      p.Type,
				Text:      p.Text,
				ToolUseID: p.ToolUseID,
				Name:      p.Name,
				Input:     p.Input,
				Output:    p.Output,
				IsError:   p.IsError,
			})
		}
		dto := MessageDTO{
			ID:             r.ID,
			ConversationID: conversationID,
			Role:           r.Role,
			Parts:          parts,
			Status:         r.Status,
			FinishReason:   r.FinishReason,
			CreatedAt:      r.CreatedAt,
		}
		for _, a := range r.Attachments {
			f, ok := fileMap[a.ID]
			if !ok {
				continue
			}
			dto.Attachments = append(dto.Attachments, s.ToAttachmentDTO(&f))
		}
		out = append(out, dto)
	}
	return out, nil
}

// SendMessageStream 收到前端发消息请求后,Go 端不再 insert/finalize 消息,
// 只:校验权限 + 下载附件 + 转发到 Python /chat + 把 SSE 帧透给前端。
// 真正的持久化(events / state / OCR 缓存 / 附件绑定)全在 Python 那边
// 通过 ADK 的 DatabaseSessionService 完成。Stage 3 后改为直接调 ADK Go,
// 不再走 HTTP 到 Python。
func (s *Service) SendMessageStream(c *gin.Context, conversationID, userID, content string, attachmentIDs []string) {
	ctx := c.Request.Context()
	w := newSSE(c)

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

	// 用 conv.UpdatedAt == conv.CreatedAt 判定首条消息(尚未触发过 TouchConversation)
	isFirstMessage := conv.CreatedAt.Equal(conv.UpdatedAt)

	// 立即把用户消息回显给前端,体感更快;真正 ID 由 ADK 持久化后产生,
	// 前端在 stream 结束后用 GET /messages 拿回 canonical ID。
	userPending := MessageDTO{
		ID:             "pending-user-" + uuid.NewString(),
		ConversationID: conversationID,
		Role:           RoleUser,
		Parts:          []MessagePart{{Type: PartText, Text: content}},
		Status:         "completed",
		CreatedAt:      time.Now().Format(time.RFC3339),
	}
	for _, f := range files {
		userPending.Attachments = append(userPending.Attachments, s.ToAttachmentDTO(&f))
	}
	w.write(StreamEvent{Type: StreamUserInput, Message: &userPending})

	inputs, closers, err := s.openAttachmentsForAgent(ctx, files)
	if err != nil {
		closeAll(closers)
		w.write(StreamEvent{Type: StreamError, ErrorMsg: err.Error()})
		return
	}

	fileIDs := make([]string, len(files))
	for i, f := range files {
		fileIDs[i] = f.ID
	}

	events, err := s.agent.Chat(ctx, agent.ChatRequest{
		SessionID: conversationID,
		Message:   content,
		Files:     inputs,
		FileIDs:   fileIDs,
	})
	closeAll(closers)
	if err != nil {
		w.write(StreamEvent{Type: StreamError, ErrorMsg: err.Error()})
		return
	}

	s.forwardAgentStream(events, w)

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

// forwardAgentStream 把 Python agent SSE 事件 1:1 转成 Go 端 SSE 帧,
// 同时维持 25s 心跳。不再做 parts 累积(ADK 自己持久化)。
func (s *Service) forwardAgentStream(events <-chan agent.Event, w *sseWriter) {
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !w.heartbeat() {
				return
			}
		case ev, ok := <-events:
			if !ok {
				return
			}
			switch ev.Type {
			case agent.EventMessageStart:
				w.write(StreamEvent{Type: StreamMessageStart, MessageID: ev.MessageID, Model: ev.Model})
			case agent.EventReasoningDelta:
				w.write(StreamEvent{Type: StreamReasoningDelta, MessageID: ev.MessageID, Delta: ev.Delta})
			case agent.EventTextDelta:
				w.write(StreamEvent{Type: StreamTextDelta, MessageID: ev.MessageID, Delta: ev.Delta})
			case agent.EventTextComplete:
				w.write(StreamEvent{Type: StreamTextComplete, MessageID: ev.MessageID, Text: ev.Text})
			case agent.EventToolCallStart:
				w.write(StreamEvent{Type: StreamToolCallStart, MessageID: ev.MessageID, ToolUseID: ev.ToolUseID, Name: ev.Name, Input: cloneRaw(ev.Input)})
			case agent.EventToolResult:
				w.write(StreamEvent{Type: StreamToolResult, MessageID: ev.MessageID, ToolUseID: ev.ToolUseID, Name: ev.Name, Output: cloneRaw(ev.Output), IsError: ev.IsError, ElapsedMS: ev.ElapsedMS})
			case agent.EventError:
				errMsg := ev.Message
				if errMsg == "" {
					errMsg = "agent error"
				}
				w.write(StreamEvent{Type: StreamError, MessageID: ev.MessageID, Code: ev.Code, ErrorMsg: errMsg})
			case agent.EventMessageDone:
				w.write(StreamEvent{Type: StreamMessageDone, MessageID: ev.MessageID, FinishReason: ev.FinishReason})
			}
		}
	}
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
