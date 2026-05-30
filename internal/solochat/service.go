package solochat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	stdmime "mime"
	"mime/multipart"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/genai"

	"mechhub-back/internal/config"
	"mechhub-back/internal/llm"
	"mechhub-back/internal/llm/tools"
	"mechhub-back/internal/sseutil"
	"mechhub-back/internal/storage"
)

var (
	ErrFileTooLarge       = errors.New("solochat: file too large")
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
	"image/jpg":       FileKindImage,
	"image/webp":      FileKindImage,
	"image/gif":       FileKindImage,
	"text/plain":      FileKindText,
	"text/markdown":   FileKindText,
	"application/pdf": FileKindDocument,
}

var (
	imageMimes = map[string]bool{
		"image/png": true, "image/jpeg": true, "image/jpg": true, "image/webp": true, "image/gif": true,
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
		mime := resolveMime(fh)
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

// OpenAttachment 鉴权后打开附件字节流。调用方拿到 ReadCloser 后必须 Close。
func (s *Service) OpenAttachment(ctx context.Context, id, ownerID string) (*UploadedFile, io.ReadCloser, error) {
	f, err := s.repo.FindFile(ctx, id, ownerID)
	if err != nil {
		return nil, nil, err
	}
	body, err := s.oss.Download(ctx, f.OSSKey)
	if err != nil {
		return nil, nil, err
	}
	return f, body, nil
}

// AttachmentURL 拼出后端 stream-through URL。
func (s *Service) AttachmentURL(fileID string) string {
	return s.cfg.App.BackendBaseURL + "/api/solochat/attachments/" + fileID
}

func (s *Service) ToAttachmentDTO(f *UploadedFile) AttachmentDTO {
	return AttachmentDTO{
		ID:           f.ID,
		Kind:         f.Kind,
		MimeType:     f.MimeType,
		OriginalName: f.OriginalName,
		Size:         f.Size,
		URL:          s.AttachmentURL(f.ID),
	}
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// resolveMime 从 multipart part 的 Content-Type 头读 MIME,剥参数;
// 头缺失或是 application/octet-stream 时,根据文件扩展名兜底。
// 这是因为部分 Postman / curl 调用不写 part-level Content-Type。
func resolveMime(fh *multipart.FileHeader) string {
	raw := strings.TrimSpace(fh.Header.Get("Content-Type"))
	if raw != "" {
		if media, _, err := stdmime.ParseMediaType(raw); err == nil {
			raw = media
		}
	}
	if raw == "" || raw == "application/octet-stream" {
		return mimeFromExt(filepath.Ext(fh.Filename))
	}
	return raw
}

func mimeFromExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".pdf":
		return "application/pdf"
	case ".txt":
		return "text/plain"
	case ".md", ".markdown":
		return "text/markdown"
	default:
		return ""
	}
}

func (s *Service) CreateConversation(ctx context.Context, userID string) (*Conversation, error) {
	now := time.Now()
	c := &Conversation{
		ID: uuid.NewString(), UserID: userID, Title: "新对话",
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

// FindFiles 按 id 拉本人拥有的上传文件元数据(含 OSSKey)。供 channel 模块分享
// 时把 solochat 附件复制成频道附件用;ownerID 约束保证只能复制自己的文件。
func (s *Service) FindFiles(ctx context.Context, ids []string, ownerID string) ([]UploadedFile, error) {
	return s.repo.FindFilesByIDs(ctx, ids, ownerID)
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
func (s *Service) SendMessageStream(c *gin.Context, conversationID, userID, content string, attachmentIDs []string, grading, webSearch bool) {
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

	w := sseutil.New(c)

	conv, err := s.repo.FindConversation(ctx, conversationID, userID)
	if err != nil {
		w.Write(StreamEvent{Type: StreamError, ErrorMsg: "对话不存在"})
		return
	}

	var files []UploadedFile
	if len(attachmentIDs) > 0 {
		files, err = s.repo.FindFilesByIDs(ctx, attachmentIDs, userID)
		if err != nil {
			w.Write(StreamEvent{Type: StreamError, ErrorMsg: err.Error()})
			return
		}
		if len(files) != len(attachmentIDs) {
			w.Write(StreamEvent{Type: StreamError, ErrorMsg: "部分附件不存在或无权访问"})
			return
		}
	}

	// 用持久化的 TitleGenerated 标志判断,比 CreatedAt.Equal(UpdatedAt) 鲁棒:
	// rename / touch / 二次 stream 都不会误关掉这扇门。
	isFirstMessage := !conv.TitleGenerated

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
	w.Write(StreamEvent{Type: StreamUserInput, Message: &userPending})

	// 组装 user content,图片数据存入内存缓存供 OCR / grading 工具用
	userContent, err := s.buildUserContent(ctx, conversationID, content, files, grading, webSearch)
	if err != nil {
		w.Write(StreamEvent{Type: StreamError, ErrorMsg: err.Error()})
		return
	}
	defer tools.DeleteSessionImages(conversationID)

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
			llm.StreamOptions{FileIDs: fileIDs, StateDelta: map[string]any{"_solochat_session": conversationID}},
			func(f llm.StreamFrame) bool {
				select {
				case <-ctx.Done():
					return false
				case frames <- f:
					return true
				}
			})
	}()

	// 累积 assistant 文本。第一轮会在 stream 进行中提前 spawn 标题 goroutine,
	// 流末用 select 等结果(命中时几乎零额外等待)。
	var assistantBuf strings.Builder
	var finishReason string

	// 标题并行启动控制:
	//   - titleStarted: 只启动一次
	//   - titleCh:      goroutine 把结果写回(带 buffer 1,防止流末没等就 cancel)
	//   - titleThreshold: 累积到这么多字节就提前启 LLM(典型用户提问 + 一两句回答足够说明话题)
	var (
		titleStarted   bool
		titleCh        chan string
		titleThreshold = 80
	)
	maybeStartTitle := func() {
		if !isFirstMessage || titleStarted {
			return
		}
		snapshot := assistantBuf.String()
		if strings.TrimSpace(snapshot) == "" {
			return
		}
		titleStarted = true
		titleCh = make(chan string, 1)
		go func() {
			titleCh <- generateFirstTitle(s.llm, content, snapshot, "")
		}()
	}

	for {
		select {
		case <-ticker.C:
			if !w.Heartbeat() {
				return
			}
		case f, ok := <-frames:
			if !ok {
				goto done
			}
			switch f.Type {
			case llm.FrameTextDelta:
				assistantBuf.WriteString(f.Delta)
				// 累到阈值就 fire-and-async:让标题 LLM 跟主回答并行跑,
				// 长回答场景下流末几乎零等待。
				if assistantBuf.Len() >= titleThreshold {
					maybeStartTitle()
				}
			case llm.FrameTextComplete:
				// 某些 provider(OpenAI 兼容)只发 text_complete 不发 text_delta;
				// 此处补累,顺便兜底触发标题启动。
				if assistantBuf.Len() == 0 {
					assistantBuf.WriteString(f.Text)
				}
				maybeStartTitle()
			case llm.FrameMessageDone:
				finishReason = f.FinishReason
				// 极短回答可能没到阈值也没 text_complete,流末再补一次。
				maybeStartTitle()
			}
			s.writeFrame(w, f)
		}
	}
done:
	_ = <-streamErr // 不阻塞,streamErr 已被发送

	if isFirstMessage {
		title := awaitTitle(titleCh, content)
		_ = finishReason // 保留累积,将来若想区分场景再用
		if title != "" {
			_ = s.repo.UpdateConversationTitle(context.Background(), conversationID, userID, title)
			conv.Title = title
			conv.TitleGenerated = true
			conv.UpdatedAt = time.Now()
			dto := toConversationDTO(conv)
			w.Write(StreamEvent{Type: StreamConversationName, Conversation: &dto})
		}
	} else {
		_ = s.repo.TouchConversation(ctx, conversationID)
	}
}

// awaitTitle 等并行启动的标题 goroutine。若根本没启起来(纯工具调用 / 全程
// 没有 text 输出),直接走兜底。命中即返回,超时 8s 也兜底。
func awaitTitle(ch chan string, userText string) string {
	if ch == nil {
		// 流里完全没文本 → 没启过 goroutine,直接用用户消息兜底。
		return autoTitle(userText)
	}
	select {
	case t := <-ch:
		if t == "" {
			return autoTitle(userText)
		}
		return t
	case <-time.After(8 * time.Second):
		return autoTitle(userText)
	}
}

// generateFirstTitle 首选 LLM 总结,失败回退用户消息前 20 字 + ellipsis。
//
// 设计:即使 stream 是 cancelled / error 结束,只要 assistant 已经说了
// 一些内容,就值得让 LLM 起标题(用户已经看到部分回答,知道在聊什么)。
// 只有 assistantText 真的为空才直接走兜底。
//
// finishReason 形参保留是为了将来可能区分不同场景(比如 fatal error 时
// 走特别 prompt),目前不参与判定。
func generateFirstTitle(llmSvc *llm.Service, userText, assistantText, _ string) string {
	if strings.TrimSpace(assistantText) == "" {
		return autoTitle(userText)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	title, err := llmSvc.GenerateTitle(ctx, userText, assistantText)
	if err != nil || title == "" {
		return autoTitle(userText)
	}
	return title
}

func (s *Service) writeFrame(w *sseutil.Writer, f llm.StreamFrame) {
	switch f.Type {
	case llm.FrameMessageStart:
		w.Write(StreamEvent{Type: StreamMessageStart, MessageID: f.MessageID, Model: f.Model})
	case llm.FrameReasoningDelta:
		w.Write(StreamEvent{Type: StreamReasoningDelta, MessageID: f.MessageID, Delta: f.Delta})
	case llm.FrameTextDelta:
		w.Write(StreamEvent{Type: StreamTextDelta, MessageID: f.MessageID, Delta: f.Delta})
	case llm.FrameTextComplete:
		w.Write(StreamEvent{Type: StreamTextComplete, MessageID: f.MessageID, Text: f.Text})
	case llm.FrameToolCallStart:
		w.Write(StreamEvent{Type: StreamToolCallStart, MessageID: f.MessageID, ToolUseID: f.ToolUseID, Name: f.Name, Input: f.Input})
	case llm.FrameToolResult:
		w.Write(StreamEvent{Type: StreamToolResult, MessageID: f.MessageID, ToolUseID: f.ToolUseID, Name: f.Name, Output: f.Output, IsError: f.IsError})
	case llm.FrameError:
		w.Write(StreamEvent{Type: StreamError, MessageID: f.MessageID, Code: f.Code, ErrorMsg: f.Message})
	case llm.FrameMessageDone:
		w.Write(StreamEvent{Type: StreamMessageDone, MessageID: f.MessageID, FinishReason: f.FinishReason})
	}
}

// buildUserContent 把文本 + 附件组装成 ADK 用的 genai.Content。
//   - 图片 → Part.InlineData + 存内存缓存供 OCR/grading 工具用
//   - PDF → 直接 Part.InlineData
//   - text/markdown → 读内容 inline 拼到 prompt 文本
func (s *Service) buildUserContent(ctx context.Context, sessionID, text string, files []UploadedFile, grading, webSearch bool) (*genai.Content, error) {
	parts := make([]*genai.Part, 0, len(files)+1)
	var inlineBlocks []string
	var cachedImages []tools.CachedImage
	var imageIndices []string

	for i, f := range files {
		body, err := s.oss.Download(ctx, f.OSSKey)
		if err != nil {
			return nil, err
		}
		data, err := readAll(body)
		_ = body.Close()
		if err != nil {
			return nil, err
		}

		mime := f.MimeType
		switch {
		case imageMimes[mime]:
			parts = append(parts, &genai.Part{InlineData: &genai.Blob{Data: data, MIMEType: mime}})
			cachedImages = append(cachedImages, tools.CachedImage{
				Data:         data,
				MimeType:     mime,
				OrigName:     f.OriginalName,
				AttachmentID: f.ID,
				URL:          s.AttachmentURL(f.ID),
			})
			imageIndices = append(imageIndices, fmt.Sprintf("[%d] %s", i, f.OriginalName))
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

	if len(cachedImages) > 0 {
		tools.StoreSessionImages(sessionID, cachedImages)
	}

	composed := strings.TrimSpace(text)
	if grading {
		composed += "\n\n[本轮用户开启了\"批改作业\"模式,请以批改老师视角分析作业内容、指出错误并给出评分建议;若包含图片,优先调用 grade_with_ocr 工具。]"
	}
	if webSearch {
		composed += "\n\n[本轮用户开启了\"网页搜索\"模式,如需最新或站外信息请优先调用 web_search 工具检索。引用了检索内容的句子,请在该句末尾用 Markdown 链接内联标注来源(如 [Reuters](https://...)),来源紧贴对应内容;不要在文末单独堆一个链接清单。]"
	}
	if len(imageIndices) > 0 {
		composed += "\n\n[本轮上传图片,供 grade_with_ocr / ocr_images_cached 工具使用]\n"
		composed += "可用图片:\n"
		for _, idx := range imageIndices {
			composed += idx + "\n"
		}
	}
	if len(inlineBlocks) > 0 {
		composed += "\n\n" + strings.Join(inlineBlocks, "\n\n")
	}

	leading := &genai.Part{Text: composed}
	parts = append([]*genai.Part{leading}, parts...)
	return &genai.Content{Role: "user", Parts: parts}, nil
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
