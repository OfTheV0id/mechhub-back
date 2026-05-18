package solochat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/genai"

	"mechhub-back/internal/config"
	"mechhub-back/internal/llm"
	"mechhub-back/internal/storage"
)

var (
	ErrFileTooLarge      = errors.New("solochat: file too large")
	ErrFileTypeNotAllowed = errors.New("solochat: file type not allowed")
	ErrTooManyAttachments = errors.New("solochat: too many attachments")
)

// streamHandle 持有 in-flight stream 的 cancel,挂在 Service.activeStreams 上。
// 用 pointer 作 value 是为了 sync.Map.CompareAndDelete 能精确比对 —— 防止
// 新 stream 替换旧 entry 后,旧 stream 退出时把新 entry 误删。
type streamHandle struct {
	cancel context.CancelFunc
}

type Service struct {
	repo          *Repo
	llm           *llm.Service
	oss           *storage.OSS
	cfg           *config.Config
	activeStreams sync.Map // key: userID + ":" + conversationID → *streamHandle
}

func NewService(repo *Repo, llmSvc *llm.Service, oss *storage.OSS, cfg *config.Config) *Service {
	return &Service{repo: repo, llm: llmSvc, oss: oss, cfg: cfg}
}

// StopStream 取消指定用户在指定对话内正在跑的 stream(如果有)。多次调用幂等。
// 真正的 sync.Map 删除留给 SendMessageStream 自己 defer 干,避免删错(新 stream
// 可能在 cancel 后立刻替换了 entry)。
func (s *Service) StopStream(userID, conversationID string) {
	if h, ok := s.activeStreams.Load(streamKey(userID, conversationID)); ok {
		h.(*streamHandle).cancel()
	}
}

func streamKey(userID, conversationID string) string {
	return userID + ":" + conversationID
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

var (
	imageMimes = map[string]bool{
		"image/png": true, "image/jpeg": true, "image/webp": true, "image/gif": true,
	}
	textMimes = map[string]bool{
		"text/plain": true, "text/markdown": true,
	}
)

const maxInlineTextChars = 32_000

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

func (s *Service) CreateConversation(ctx context.Context, userID, title string) (*Conversation, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "新对话"
	}
	now := time.Now()
	c := &Conversation{
		ID: uuid.NewString(), UserID: userID, Title: title,
		CreatedAt: now, UpdatedAt: now,
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

// ListMessages 查 ADK session 的 events,翻译成 MessageDTO,再用 Mongo /
// MySQL 业务表的附件元数据 hydrate user message 的 attachments。
func (s *Service) ListMessages(ctx context.Context, conversationID, userID string) ([]MessageDTO, error) {
	if _, err := s.repo.FindConversation(ctx, conversationID, userID); err != nil {
		return nil, err
	}
	rows, err := s.llm.ListMessages(ctx, llmUserID(userID), conversationID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []MessageDTO{}, nil
	}

	// 收集所有 file_id 一次查 uploaded_files
	idSet := make(map[string]struct{})
	for _, r := range rows {
		for _, fid := range r.Attachments {
			idSet[fid] = struct{}{}
		}
	}
	fileMap := make(map[string]UploadedFile)
	if len(idSet) > 0 {
		ids := make([]string, 0, len(idSet))
		for fid := range idSet {
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
				Type: p.Type, Text: p.Text, ToolUseID: p.ToolUseID, Name: p.Name,
				Input: p.Input, Output: p.Output, IsError: p.IsError,
			})
		}
		dto := MessageDTO{
			ID: r.ID, ConversationID: conversationID, Role: r.Role,
			Parts: parts, Status: r.Status, CreatedAt: r.CreatedAt,
		}
		for _, fid := range r.Attachments {
			if f, ok := fileMap[fid]; ok {
				dto.Attachments = append(dto.Attachments, s.ToAttachmentDTO(&f))
			}
		}
		out = append(out, dto)
	}
	return out, nil
}

// SendMessageStream 收到前端发消息后:
//  1. 校验权限 + 解析附件
//  2. 发 `user_input` 帧 (前端乐观渲染)
//  3. 把附件读字节、组装成 multimodal `*genai.Content` + 同时落盘
//     供 OCR/grading 工具复用(文件名前缀 = file_id,与 cache key 对齐)
//  4. 调 llm.StreamChat 把 ADK 流式事件翻译成 SSE 帧
//  5. 首条消息后自动命名 + 触发 TouchConversation
func (s *Service) SendMessageStream(c *gin.Context, conversationID, userID, content string, attachmentIDs []string) {
	// 派生一个本 stream 专属的 cancel ctx,挂到 activeStreams。同 conversation
	// 有未完的 stream(用户没等回就再发,或两个标签同时发)时,先把旧 stream
	// cancel 掉,新 stream 替换 entry,UX 跟 ChatGPT 一致。
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	handle := &streamHandle{cancel: cancel}
	key := streamKey(userID, conversationID)
	if old, loaded := s.activeStreams.Swap(key, handle); loaded {
		old.(*streamHandle).cancel()
	}
	defer s.activeStreams.CompareAndDelete(key, handle)

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

	isFirstMessage := conv.CreatedAt.Equal(conv.UpdatedAt)

	// 用户消息立刻回显(临时 id;真正的 ADK event id 流末让前端 GET /messages 重拉)
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

	// 组装 user content + 落盘临时图片(供 OCR / grading 工具反查 file_id)
	userContent, tempPaths, err := s.buildUserContent(ctx, conversationID, content, files)
	defer cleanupTempFiles(tempPaths)
	if err != nil {
		w.write(StreamEvent{Type: StreamError, ErrorMsg: err.Error()})
		return
	}

	fileIDs := make([]string, len(files))
	for i, f := range files {
		fileIDs[i] = f.ID
	}

	messageID := "msg_" + uuid.NewString()[:12]

	// ADK 流式 → 我们的 SSE 帧
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	frames := make(chan llm.StreamFrame, 32)
	streamErr := make(chan error, 1)
	go func() {
		defer close(frames)
		streamErr <- s.llm.StreamChat(ctx, llmUserID(userID), conversationID, messageID, userContent,
			llm.StreamOptions{FileIDs: fileIDs},
			func(f llm.StreamFrame) bool {
				select {
				case <-ctx.Done():
					return false
				case frames <- f:
					return true
				}
			})
	}()

	for {
		select {
		case <-ticker.C:
			if !w.heartbeat() {
				return
			}
		case f, ok := <-frames:
			if !ok {
				goto done
			}
			s.writeFrame(w, f)
		}
	}
done:
	_ = <-streamErr // 不阻塞,streamErr 已被发送

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

func (s *Service) writeFrame(w *sseWriter, f llm.StreamFrame) {
	switch f.Type {
	case llm.FrameMessageStart:
		w.write(StreamEvent{Type: StreamMessageStart, MessageID: f.MessageID, Model: f.Model})
	case llm.FrameReasoningDelta:
		w.write(StreamEvent{Type: StreamReasoningDelta, MessageID: f.MessageID, Delta: f.Delta})
	case llm.FrameTextDelta:
		w.write(StreamEvent{Type: StreamTextDelta, MessageID: f.MessageID, Delta: f.Delta})
	case llm.FrameTextComplete:
		w.write(StreamEvent{Type: StreamTextComplete, MessageID: f.MessageID, Text: f.Text})
	case llm.FrameToolCallStart:
		w.write(StreamEvent{Type: StreamToolCallStart, MessageID: f.MessageID, ToolUseID: f.ToolUseID, Name: f.Name, Input: f.Input})
	case llm.FrameToolResult:
		w.write(StreamEvent{Type: StreamToolResult, MessageID: f.MessageID, ToolUseID: f.ToolUseID, Name: f.Name, Output: f.Output, IsError: f.IsError})
	case llm.FrameError:
		w.write(StreamEvent{Type: StreamError, MessageID: f.MessageID, Code: f.Code, ErrorMsg: f.Message})
	case llm.FrameMessageDone:
		w.write(StreamEvent{Type: StreamMessageDone, MessageID: f.MessageID, FinishReason: f.FinishReason})
	}
}

// buildUserContent 把文本 + 附件组装成 ADK 用的 genai.Content。
//   - 图片 → 直接 Part.InlineData,LLM 能看;同时落盘一份让 OCR/grading 工具复用
//   - PDF → 直接 Part.InlineData
//   - text/markdown → 读内容 inline 拼到 prompt 文本
//
// 落盘文件名前缀 = file_id,与 tools.CacheKey 反查保持一致(round 6 的稳定 key 模式)。
func (s *Service) buildUserContent(ctx context.Context, sessionID, text string, files []UploadedFile) (*genai.Content, []string, error) {
	parts := make([]*genai.Part, 0, len(files)+1)
	var inlineBlocks []string
	var tempPaths []string
	var imagePathsForTools []string

	baseDir := filepath.Join(os.TempDir(), "mechhub", sessionID)
	if len(files) > 0 {
		if err := os.MkdirAll(baseDir, 0o755); err != nil {
			return nil, tempPaths, err
		}
	}

	for _, f := range files {
		body, err := s.oss.Download(ctx, f.OSSKey)
		if err != nil {
			return nil, tempPaths, err
		}
		data, err := readAll(body)
		_ = body.Close()
		if err != nil {
			return nil, tempPaths, err
		}

		mime := f.MimeType
		switch {
		case imageMimes[mime]:
			parts = append(parts, &genai.Part{InlineData: &genai.Blob{Data: data, MIMEType: mime}})
			target := filepath.Join(baseDir, f.ID+"-"+safeFilename(f.OriginalName))
			if err := os.WriteFile(target, data, 0o644); err == nil {
				tempPaths = append(tempPaths, target)
				imagePathsForTools = append(imagePathsForTools, target)
			}
		case mime == "application/pdf":
			parts = append(parts, &genai.Part{InlineData: &genai.Blob{Data: data, MIMEType: mime}})
		case textMimes[mime]:
			body := string(data)
			if len(body) > maxInlineTextChars {
				body = body[:maxInlineTextChars] + "\n\n... [truncated]"
			}
			inlineBlocks = append(inlineBlocks, "[文件 "+f.OriginalName+"]\n```\n"+body+"\n```")
		}
	}

	composed := strings.TrimSpace(text)
	if len(imagePathsForTools) > 0 {
		composed += "\n\n[本轮上传图片本地路径,供 grade_submission / ocr_images_cached 工具使用]\n"
		for _, p := range imagePathsForTools {
			composed += "- " + p + "\n"
		}
	}
	if len(inlineBlocks) > 0 {
		composed += "\n\n" + strings.Join(inlineBlocks, "\n\n")
	}

	leading := &genai.Part{Text: composed}
	parts = append([]*genai.Part{leading}, parts...)
	return &genai.Content{Role: "user", Parts: parts}, tempPaths, nil
}

func cleanupTempFiles(paths []string) {
	for _, p := range paths {
		_ = os.Remove(p)
	}
}

func safeFilename(name string) string {
	if name == "" {
		return "file"
	}
	out := make([]rune, 0, len(name))
	for _, r := range name {
		switch {
		case r >= '0' && r <= '9', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r == '.', r == '-', r == '_':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
		if len(out) >= 120 {
			break
		}
	}
	return string(out)
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

// llmUserID — ADK Go 要求所有 session 共享 app_name + user_id。我们把
// app_name 锁死 "mechhub_tutor",user_id 用业务上的真实 user_id 透传,
// 这样不同用户的对话在 ADK session 表里天然隔离。
func llmUserID(userID string) string { return userID }

// readAll 从 io.ReadCloser 读完再关闭。
func readAll(r interface{ Read(p []byte) (int, error) }) ([]byte, error) {
	var buf []byte
	chunk := make([]byte, 32*1024)
	for {
		n, err := r.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
		}
		if err != nil {
			if err.Error() == "EOF" {
				return buf, nil
			}
			return buf, err
		}
	}
}
