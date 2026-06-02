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
	"mechhub-back/internal/reference"
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
		g, ok := reference.ExtractGrading(m)
		if !ok {
			return nil, ErrShareSourceInvalid
		}
		grading = g
		fileIDs = append(fileIDs, reference.GradingFileIDs(g)...)
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
			fileIDs = append(fileIDs, reference.ThreadFileIDs(m)...)
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
	cf := s.copiedRefMap(copied)
	ref := &MessageReference{Type: share.Type, SourceChatID: share.SourceChatID}
	switch share.Type {
	case ReferenceTypeGrading:
		ref.Grading = reference.BuildGrading(grading, cf)
	case ReferenceTypeThread:
		ref.Segments = reference.BuildThread(threadMsgs, cf)
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

// copiedRefMap 把「solochat file id → 复制后的频道附件」转成 reference 包要的 CopiedFile 映射。
func (s *Service) copiedRefMap(copied map[string]*Attachment) map[string]reference.CopiedFile {
	cf := make(map[string]reference.CopiedFile, len(copied))
	for srcID, a := range copied {
		cf[srcID] = reference.CopiedFile{
			ID:           a.ID,
			OriginalName: a.OriginalName,
			MimeType:     a.MimeType,
			URL:          s.toAttachmentDTO(a).URL,
		}
	}
	return cf
}
