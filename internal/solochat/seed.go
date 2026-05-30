package solochat

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"mechhub-back/internal/llm"
	"mechhub-back/internal/llm/schemas"
)

const gradeToolName = "grade_with_ocr"

// SeedSourceFile 描述一个待复制进 fork 用户 solochat 的源文件(频道附件)。
// RefKey 是来源侧的稳定标识(频道 attachment_id),用于把复制后的新文件
// 回填到 grading.imageRefs / segment 附件上。
type SeedSourceFile struct {
	RefKey       string
	OSSKey       string
	OriginalName string
	MimeType     string
	Size         int64
}

// SeedSegmentPart 对应 channel.SegmentPart:对话片段一条消息的内容块。
type SeedSegmentPart struct {
	Type   string
	Text   string
	Name   string
	Input  json.RawMessage
	Output json.RawMessage
}

// SeedSegment 是对话片段 fork 的一条消息。Parts 非空时按 parts 忠实重建
// (含 tool_use / tool_result),否则回落到纯文本 Text。
type SeedSegment struct {
	Role              string
	Text              string
	Parts             []SeedSegmentPart
	AttachmentRefKeys []string
}

// SeedSpec 是 channel.fork 交给 solochat 的高层规格:创建对话 + 复制附件 +
// 注入历史事件,全由 SeedConversation 在 solochat 域内完成。
type SeedSpec struct {
	Title       string
	Type        string                 // "grading" | "thread"
	Grading     *schemas.GradingOutput // type=grading;ImageRefs[].AttachmentID == 对应 SeedSourceFile.RefKey
	Segments    []SeedSegment          // type=thread
	Attachments []SeedSourceFile
}

// SeedConversation 把一份分享快照忠实复制成 userID 自己的新 solochat 对话:
//  1. 建对话(标题取快照来源,标记 TitleGenerated 防被自动改名)
//  2. 复制涉及的附件到本人 uploaded_files(OSS 服务端拷贝)
//  3. 把 grading / 对话片段重建成 ADK 历史事件注入(不触发 AI)
//
// 任一步失败补偿:删已建附件行 + OSS 对象 + 对话行。
func (s *Service) SeedConversation(ctx context.Context, userID string, spec SeedSpec) (*ConversationDTO, error) {
	now := time.Now()
	conv := &Conversation{
		ID:             uuid.NewString(),
		UserID:         userID,
		Title:          firstNonEmptyStr(spec.Title, "来自频道的分享"),
		TitleGenerated: true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.repo.InsertConversation(ctx, conv); err != nil {
		return nil, err
	}

	copied, copiedKeys, err := s.copyFilesToSolochat(ctx, userID, spec.Attachments)
	if err != nil {
		s.rollbackSeed(conv.ID, userID, copied, copiedKeys)
		return nil, err
	}

	turns, err := buildSeedTurns(s, spec, copied)
	if err != nil {
		s.rollbackSeed(conv.ID, userID, copied, copiedKeys)
		return nil, err
	}

	if err := s.llm.SeedSession(ctx, llmUserID(userID), conv.ID, turns); err != nil {
		s.rollbackSeed(conv.ID, userID, copied, copiedKeys)
		return nil, err
	}

	dto := toConversationDTO(conv)
	return &dto, nil
}

// copyFilesToSolochat 把一组源文件(频道附件)OSS 服务端拷贝进 solochat/<user>/,
// 建 uploaded_files 行。返回 RefKey→新文件 映射 + 已写 OSS key(供回滚)。
func (s *Service) copyFilesToSolochat(ctx context.Context, userID string, srcs []SeedSourceFile) (map[string]UploadedFile, []string, error) {
	copied := make(map[string]UploadedFile, len(srcs))
	var keys []string
	for _, src := range srcs {
		if _, ok := copied[src.RefKey]; ok {
			continue
		}
		suffix, err := randomHex(8)
		if err != nil {
			return copied, keys, err
		}
		destKey := "solochat/" + userID + "/" + suffix + filepath.Ext(src.OriginalName)
		if err := s.oss.Copy(ctx, src.OSSKey, destKey); err != nil {
			return copied, keys, err
		}
		keys = append(keys, destKey)

		f := UploadedFile{
			ID:           uuid.NewString(),
			OwnerUserID:  userID,
			OSSKey:       destKey,
			OriginalName: src.OriginalName,
			MimeType:     src.MimeType,
			Kind:         kindFromMime(src.MimeType),
			Size:         src.Size,
			CreatedAt:    time.Now(),
		}
		if err := s.repo.InsertFile(ctx, &f); err != nil {
			return copied, keys, err
		}
		copied[src.RefKey] = f
	}
	return copied, keys, nil
}

func (s *Service) rollbackSeed(convID, userID string, copied map[string]UploadedFile, keys []string) {
	for _, f := range copied {
		_ = s.repo.db.WithContext(context.Background()).
			Where("id = ? AND owner_user_id = ?", f.ID, userID).
			Delete(&UploadedFile{}).Error
	}
	for _, k := range keys {
		_ = s.oss.Delete(context.Background(), k)
	}
	_ = s.repo.DeleteConversation(context.Background(), convID, userID)
}

// buildSeedTurns 把 SeedSpec(快照)映射成 llm.SeedTurn,grading 的 imageRefs
// 按 copied 映射回写到新 solochat 附件(id + url),使 fork 后 OCR 可视化可加载。
func buildSeedTurns(s *Service, spec SeedSpec, copied map[string]UploadedFile) ([]llm.SeedTurn, error) {
	switch spec.Type {
	case "grading":
		if spec.Grading == nil {
			return nil, nil
		}
		g := *spec.Grading
		refs := make([]schemas.ImageRef, len(g.ImageRefs))
		copy(refs, g.ImageRefs)
		var userFileIDs []string
		for i := range refs {
			if f, ok := copied[refs[i].AttachmentID]; ok {
				refs[i].AttachmentID = f.ID
				refs[i].URL = s.AttachmentURL(f.ID)
				userFileIDs = append(userFileIDs, f.ID)
			}
		}
		g.ImageRefs = refs

		out, err := json.Marshal(g)
		if err != nil {
			return nil, err
		}
		return []llm.SeedTurn{
			{
				Role:          RoleUser,
				Parts:         []llm.SeedPart{{Type: PartText, Text: "(来自频道分享的批改作业)"}},
				AttachmentIDs: userFileIDs,
			},
			{
				Role: RoleAssistant,
				Parts: []llm.SeedPart{
					{Type: PartToolUse, Name: gradeToolName, Input: json.RawMessage(`{}`)},
					{Type: PartToolResult, Name: gradeToolName, Output: out},
				},
			},
		}, nil
	case "thread":
		turns := make([]llm.SeedTurn, 0, len(spec.Segments))
		for _, seg := range spec.Segments {
			var parts []llm.SeedPart
			if len(seg.Parts) > 0 {
				for _, p := range seg.Parts {
					sp := llm.SeedPart{Type: p.Type, Text: p.Text, Name: p.Name, Input: p.Input, Output: p.Output}
					if p.Type == PartToolResult && p.Name == gradeToolName && len(p.Output) > 0 {
						sp.Output = rewriteSeedGrading(s, p.Output, copied)
					}
					parts = append(parts, sp)
				}
			} else {
				parts = []llm.SeedPart{{Type: PartText, Text: seg.Text}}
			}
			t := llm.SeedTurn{Role: seg.Role, Parts: parts}
			if seg.Role == RoleUser {
				for _, rk := range seg.AttachmentRefKeys {
					if f, ok := copied[rk]; ok {
						t.AttachmentIDs = append(t.AttachmentIDs, f.ID)
					}
				}
			}
			turns = append(turns, t)
		}
		return turns, nil
	}
	return nil, nil
}

// rewriteSeedGrading 把 grading tool_result 的 imageRefs(当前指向频道附件 id)
// 按 copied 改写成新 solochat 附件 id/url,使 fork 后 OCR 可视化能加载图片。
func rewriteSeedGrading(s *Service, raw json.RawMessage, copied map[string]UploadedFile) json.RawMessage {
	var g schemas.GradingOutput
	if json.Unmarshal(raw, &g) != nil {
		return raw
	}
	for i := range g.ImageRefs {
		if f, ok := copied[g.ImageRefs[i].AttachmentID]; ok {
			g.ImageRefs[i].AttachmentID = f.ID
			g.ImageRefs[i].URL = s.AttachmentURL(f.ID)
		}
	}
	b, err := json.Marshal(g)
	if err != nil {
		return raw
	}
	return b
}

func kindFromMime(mime string) string {
	if k, ok := allowedMimeKind[mime]; ok {
		return k
	}
	return FileKindDocument
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
