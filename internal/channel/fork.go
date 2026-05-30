package channel

import (
	"context"
	"encoding/json"

	"mechhub-back/internal/llm/schemas"
	"mechhub-back/internal/solochat"
)

// ForkMessageToSolochat 把一条分享消息(批改 / 对话片段)忠实复制成 userID
// 自己的 solochat 新对话。完全基于消息里自包含的快照 + 频道附件重建,不读
// 原 solochat 对话(那属于分享者),因此任何班级成员都能 fork。
func (s *Service) ForkMessageToSolochat(ctx context.Context, channelID, messageID, userID string) (*solochat.ConversationDTO, error) {
	m, err := s.repo.FindMessage(ctx, messageID)
	if err != nil {
		return nil, err
	}
	if m.ChannelID != channelID {
		return nil, ErrNotFound
	}
	ch, err := s.repo.FindChannel(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if _, err := s.classRepo.FindMembership(ctx, ch.ClassID, userID); err != nil {
		return nil, err
	}
	if m.Reference == nil || *m.Reference == "" {
		return nil, ErrShareSourceInvalid
	}

	var ref MessageReference
	if err := json.Unmarshal([]byte(*m.Reference), &ref); err != nil {
		return nil, ErrShareSourceInvalid
	}

	// 该消息绑定的频道附件:attachment_id → 行(含 OSSKey)。
	atts, err := s.repo.FindAttachmentsByMessageIDs(ctx, []string{messageID})
	if err != nil {
		return nil, err
	}
	attByID := make(map[string]Attachment, len(atts))
	for _, a := range atts {
		attByID[a.ID] = a
	}

	spec := solochat.SeedSpec{Title: ref.SourceTitle}
	srcSeen := make(map[string]struct{})
	addSrc := func(attID string) {
		if attID == "" {
			return
		}
		if _, ok := srcSeen[attID]; ok {
			return
		}
		a, ok := attByID[attID]
		if !ok {
			return
		}
		srcSeen[attID] = struct{}{}
		spec.Attachments = append(spec.Attachments, solochat.SeedSourceFile{
			RefKey:       a.ID,
			OSSKey:       a.OSSKey,
			OriginalName: a.OriginalName,
			MimeType:     a.MimeType,
			Size:         a.SizeBytes,
		})
	}

	switch ref.Type {
	case ReferenceTypeGrading:
		if ref.Grading == nil {
			return nil, ErrShareSourceInvalid
		}
		spec.Type = "grading"
		spec.Grading = ref.Grading
		if spec.Title == "" {
			spec.Title = "来自频道的批改"
		}
		for _, r := range ref.Grading.ImageRefs {
			addSrc(r.AttachmentID)
		}
	case ReferenceTypeThread:
		spec.Type = "thread"
		if spec.Title == "" {
			spec.Title = "来自频道的对话片段"
		}
		for _, seg := range ref.Segments {
			ss := solochat.SeedSegment{Role: seg.Role, Text: seg.Text}
			for _, a := range seg.Attachments {
				ss.AttachmentRefKeys = append(ss.AttachmentRefKeys, a.AttachmentID)
				addSrc(a.AttachmentID)
			}
			for _, p := range seg.Parts {
				ss.Parts = append(ss.Parts, solochat.SeedSegmentPart{
					Type:   p.Type,
					Text:   p.Text,
					Name:   p.Name,
					Input:  p.Input,
					Output: p.Output,
				})
				// grading tool_result 引用的图片也要复制回 solochat 并回写
				if p.Type == "tool_result" && p.Name == gradeToolName && len(p.Output) > 0 {
					var g schemas.GradingOutput
					if json.Unmarshal(p.Output, &g) == nil {
						for _, r := range g.ImageRefs {
							addSrc(r.AttachmentID)
						}
					}
				}
			}
			spec.Segments = append(spec.Segments, ss)
		}
	default:
		return nil, ErrShareSourceInvalid
	}

	return s.solochatSvc.SeedConversation(ctx, userID, spec)
}
