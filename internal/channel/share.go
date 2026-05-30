package channel

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"mechhub-back/internal/llm/schemas"
	"mechhub-back/internal/realtime"
	"mechhub-back/internal/solochat"
)

// gradeToolName 与 solochat 工具名对齐:批改结果以这个名字的 tool_result 出现。
const gradeToolName = "grade_with_ocr"

var ErrShareSourceInvalid = errors.New("channel: share source invalid")

// SendShareMessage 把 solochat 的批改结果 / 对话片段分享成一条频道消息。
// 快照内容全部由后端按 source 反查 solochat 生成,绝不信前端传入,防伪造他人内容。
// 被引用的附件(批改图片 / 片段附件)会复制成本频道的附件,使消息自包含。
func (s *Service) SendShareMessage(ctx context.Context, channelID, userID string, req SendMessageReq) (*MessageDTO, error) {
	share := req.Share

	ch, err := s.repo.FindChannel(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if _, err := s.classRepo.FindMembership(ctx, ch.ClassID, userID); err != nil {
		return nil, err
	}

	// ListMessages 内部 FindConversation 强制 user_id 归属:非本人对话 → ErrNotFound。
	msgs, err := s.solochatSvc.ListMessages(ctx, share.SourceChatID, userID)
	if err != nil {
		if errors.Is(err, solochat.ErrNotFound) {
			return nil, ErrForbidden
		}
		return nil, err
	}
	byID := make(map[string]solochat.MessageDTO, len(msgs))
	for _, m := range msgs {
		byID[m.ID] = m
	}

	// 第一遍:定位源数据 + 收集待复制的 solochat 文件 id。
	var fileIDs []string
	var grading *schemas.GradingOutput
	var threadMsgs []solochat.MessageDTO
	switch share.Type {
	case ReferenceTypeGrading:
		m, ok := byID[share.SourceMessageID]
		if !ok {
			return nil, ErrShareSourceInvalid
		}
		g, err := extractGrading(m)
		if err != nil {
			return nil, err
		}
		grading = g
		for _, r := range g.ImageRefs {
			fileIDs = append(fileIDs, r.AttachmentID)
		}
	case ReferenceTypeThread:
		if len(share.SourceMessageIDs) == 0 {
			return nil, ErrShareSourceInvalid
		}
		for _, mid := range share.SourceMessageIDs {
			m, ok := byID[mid]
			if !ok {
				return nil, ErrShareSourceInvalid
			}
			threadMsgs = append(threadMsgs, m)
			fileIDs = append(fileIDs, threadFileIDs(m)...)
		}
	default:
		return nil, ErrShareSourceInvalid
	}

	// 复制图片附件到频道:全成功才继续;任一失败回滚已复制的对象 + 行。
	copied, keys, err := s.copySolochatFilesToChannel(ctx, userID, channelID, fileIDs)
	if err != nil {
		s.rollbackCopied(copied, keys)
		return nil, err
	}

	// 第二遍:用复制后的频道附件重写快照(URL/id 指向频道附件,顺序保持不变)。
	ref := &MessageReference{Type: share.Type, SourceChatID: share.SourceChatID}
	switch share.Type {
	case ReferenceTypeGrading:
		for i := range grading.ImageRefs {
			if a, ok := copied[grading.ImageRefs[i].AttachmentID]; ok {
				grading.ImageRefs[i].AttachmentID = a.ID
				grading.ImageRefs[i].URL = s.toAttachmentDTO(a).URL
			}
		}
		ref.Grading = grading
	case ReferenceTypeThread:
		for _, m := range threadMsgs {
			seg := ThreadSegment{Role: m.Role, Text: collectText(m.Parts)}
			for _, att := range m.Attachments {
				if a, ok := copied[att.ID]; ok {
					seg.Attachments = append(seg.Attachments, ReferenceAttach{
						AttachmentID: a.ID,
						OriginalName: a.OriginalName,
						MimeType:     a.MimeType,
						URL:          s.toAttachmentDTO(a).URL,
					})
				}
			}
			seg.Parts = s.buildSegmentParts(m.Parts, copied)
			ref.Segments = append(ref.Segments, seg)
		}
	}

	refJSON, err := json.Marshal(ref)
	if err != nil {
		s.rollbackCopied(copied, keys)
		return nil, err
	}
	refStr := string(refJSON)

	m := &Message{
		ID:           uuid.NewString(),
		ChannelID:    channelID,
		ClassID:      ch.ClassID,
		AuthorUserID: userID,
		Content:      strings.TrimSpace(req.Content),
		Reference:    &refStr,
		CreatedAt:    time.Now(),
	}
	if err := s.repo.InsertMessage(ctx, m); err != nil {
		s.rollbackCopied(copied, keys)
		return nil, err
	}

	attIDs := make([]string, 0, len(copied))
	for _, a := range copied {
		attIDs = append(attIDs, a.ID)
	}
	if len(attIDs) > 0 {
		if err := s.repo.BindAttachmentsToMessage(ctx, attIDs, channelID, userID, m.ID); err != nil {
			_, _ = s.repo.DeleteMessage(ctx, m.ID)
			s.rollbackCopied(copied, keys)
			return nil, ErrAttachmentInvalid
		}
	}

	dto, err := s.hydrateMessageDTO(ctx, m)
	if err != nil {
		return nil, err
	}
	go s.hub.BroadcastToClass(ch.ClassID, MessageFrame{
		Type:      realtime.FrameChannelMessageCreated,
		ChannelID: channelID,
		ClassID:   ch.ClassID,
		Message:   *dto,
	})
	return dto, nil
}

// copySolochatFilesToChannel 把一组 solochat 文件(本人拥有)复制成频道附件(message_id=nil)。
// 任一文件缺失(已删 / 非本人)→ 整体拒绝,不静默丢图(否则批改可视化页↔图错位)。
// 返回 solochatFileID→频道附件 的映射 + 已写入 OSS 的 dest key(供失败回滚)。
func (s *Service) copySolochatFilesToChannel(ctx context.Context, userID, channelID string, fileIDs []string) (map[string]*Attachment, []string, error) {
	seen := make(map[string]struct{}, len(fileIDs))
	uniq := make([]string, 0, len(fileIDs))
	for _, id := range fileIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	copied := make(map[string]*Attachment, len(uniq))
	var keys []string
	if len(uniq) == 0 {
		return copied, keys, nil
	}

	files, err := s.solochatSvc.FindFiles(ctx, uniq, userID)
	if err != nil {
		return copied, keys, err
	}
	if len(files) != len(uniq) {
		return copied, keys, ErrShareSourceInvalid
	}

	now := time.Now()
	for i := range files {
		f := files[i]
		suffix, err := randomHex(8)
		if err != nil {
			return copied, keys, err
		}
		destKey := "channels/" + channelID + "/" + suffix + filepath.Ext(f.OriginalName)
		if err := s.oss.Copy(ctx, f.OSSKey, destKey); err != nil {
			return copied, keys, err
		}
		keys = append(keys, destKey)

		a := &Attachment{
			ID:           uuid.NewString(),
			ChannelID:    channelID,
			UploaderID:   userID,
			OSSKey:       destKey,
			OriginalName: f.OriginalName,
			MimeType:     f.MimeType,
			SizeBytes:    f.Size,
			MessageID:    nil,
			CreatedAt:    now,
		}
		if err := s.repo.InsertAttachment(ctx, a); err != nil {
			return copied, keys, err
		}
		copied[f.ID] = a
	}
	return copied, keys, nil
}

// rollbackCopied best-effort 清掉复制出来的频道附件行 + OSS 对象(分享中途失败时)。
func (s *Service) rollbackCopied(copied map[string]*Attachment, keys []string) {
	if len(copied) > 0 {
		ids := make([]string, 0, len(copied))
		for _, a := range copied {
			ids = append(ids, a.ID)
		}
		_ = s.repo.DeleteAttachmentsByIDs(context.Background(), ids)
	}
	for _, k := range keys {
		_ = s.oss.Delete(context.Background(), k)
	}
}

// threadFileIDs 收集一条 solochat 消息里需要复制进频道的所有图片 file id:
// 用户上传的附件 + grading tool_result(grade_with_ocr)里 imageRefs 引用的图片。
func threadFileIDs(m solochat.MessageDTO) []string {
	var ids []string
	for _, a := range m.Attachments {
		ids = append(ids, a.ID)
	}
	for _, p := range m.Parts {
		if p.Type == solochat.PartToolResult && p.Name == gradeToolName && len(p.Output) > 0 {
			var g schemas.GradingOutput
			if json.Unmarshal(p.Output, &g) == nil {
				for _, r := range g.ImageRefs {
					ids = append(ids, r.AttachmentID)
				}
			}
		}
	}
	return ids
}

// buildSegmentParts 把 solochat 消息 parts 映射成快照 SegmentPart(保留 text /
// tool_use / tool_result,丢 thinking)。grading 的 tool_result Output 按 copied
// 映射回写 imageRefs(指向频道附件),使分享片段也能开 OCR 可视化。
func (s *Service) buildSegmentParts(parts []solochat.MessagePart, copied map[string]*Attachment) []SegmentPart {
	out := make([]SegmentPart, 0, len(parts))
	for _, p := range parts {
		switch p.Type {
		case solochat.PartText:
			if p.Text == "" {
				continue
			}
			out = append(out, SegmentPart{Type: "text", Text: p.Text})
		case solochat.PartToolUse:
			out = append(out, SegmentPart{Type: "tool_use", Name: p.Name, Input: p.Input})
		case solochat.PartToolResult:
			output := p.Output
			if p.Name == gradeToolName && len(p.Output) > 0 {
				output = s.rewriteGradingImageRefs(p.Output, copied)
			}
			out = append(out, SegmentPart{Type: "tool_result", Name: p.Name, Output: output})
		}
	}
	return out
}

// rewriteGradingImageRefs 解析 GradingOutput,把 imageRefs 里命中 copied 的
// 项改写成频道附件 id/url;失败时原样返回。
func (s *Service) rewriteGradingImageRefs(raw json.RawMessage, copied map[string]*Attachment) json.RawMessage {
	var g schemas.GradingOutput
	if json.Unmarshal(raw, &g) != nil {
		return raw
	}
	for i := range g.ImageRefs {
		if a, ok := copied[g.ImageRefs[i].AttachmentID]; ok {
			g.ImageRefs[i].AttachmentID = a.ID
			g.ImageRefs[i].URL = s.toAttachmentDTO(a).URL
		}
	}
	b, err := json.Marshal(g)
	if err != nil {
		return raw
	}
	return b
}

func extractGrading(m solochat.MessageDTO) (*schemas.GradingOutput, error) {
	for _, p := range m.Parts {
		if p.Type == solochat.PartToolResult && p.Name == gradeToolName && len(p.Output) > 0 {
			var g schemas.GradingOutput
			if err := json.Unmarshal(p.Output, &g); err != nil {
				return nil, ErrShareSourceInvalid
			}
			return &g, nil
		}
	}
	return nil, ErrShareSourceInvalid
}

func collectText(parts []solochat.MessagePart) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Type == solochat.PartText && p.Text != "" {
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(p.Text)
		}
	}
	return b.String()
}
