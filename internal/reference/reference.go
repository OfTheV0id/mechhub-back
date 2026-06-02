// Package reference 是「把 solochat 批改结果 / 对话片段快照成自包含富引用」的共享逻辑,
// 由 channel(分享到频道)与 assignment(随作业提交记录)共用,单一真相源。
//
// 它只负责「从 solochat 消息构建快照」这件存储无关的事:收集需要复制的图片 file id、
// 把 parts 忠实映射成快照、并按调用方提供的 copied(solochat file id → 复制后的附件)
// 回写图片 URL/id。文件实际复制到哪个存储(频道附件 / 作业附件)由调用方负责。
package reference

import (
	"encoding/json"
	"strings"

	"mechhub-back/internal/llm/schemas"
	"mechhub-back/internal/solochat"
)

const (
	TypeGrading = "grading" // 批改结果快照
	TypeThread  = "thread"  // 对话片段快照

	// gradeToolName 与 solochat 工具名对齐:批改结果以这个名字的 tool_result 出现。
	gradeToolName = "grade_with_ocr"
)

// Reference 是落库的快照结构,同时容纳两种类型。图片附件已复制进调用方存储、
// URL 重写为调用方附件地址,因此自包含(原 solochat 对话被删也不受影响)。
type Reference struct {
	Type         string                 `json:"type"`           // grading | thread
	SourceChatID string                 `json:"source_chat_id"` // 来源 solochat 对话 id(仅溯源展示)
	SourceTitle  string                 `json:"source_title,omitempty"`
	Grading      *schemas.GradingOutput `json:"grading,omitempty"`  // type=grading
	Segments     []ThreadSegment        `json:"segments,omitempty"` // type=thread
}

type ThreadSegment struct {
	Role string `json:"role"` // user | assistant
	// Text 保留:纯文本兼容 + 浓缩卡片预览;完整渲染走 Parts。
	Text        string        `json:"text"`
	Parts       []SegmentPart `json:"parts,omitempty"`
	Attachments []Attach      `json:"attachments,omitempty"`
}

// SegmentPart 字段与 solochat MessagePart 对齐;tool_result(grade_with_ocr)的
// Output 里 imageRefs 已重写为调用方附件 URL。
type SegmentPart struct {
	Type   string          `json:"type"` // text | tool_use | tool_result
	Text   string          `json:"text,omitempty"`
	Name   string          `json:"name,omitempty"`
	Input  json.RawMessage `json:"input,omitempty"`
	Output json.RawMessage `json:"output,omitempty"`
}

type Attach struct {
	AttachmentID string `json:"attachment_id"` // 复制后的附件 id
	OriginalName string `json:"original_name"`
	MimeType     string `json:"mime_type"`
	URL          string `json:"url"`
}

// CopiedFile 是调用方把一个 solochat 文件复制进自己存储后的结果(用于回写快照里的 URL/id)。
type CopiedFile struct {
	ID           string
	OriginalName string
	MimeType     string
	URL          string
}

// ExtractGrading 从一条消息里取出 grade_with_ocr 的 GradingOutput。ok=false 表示该消息没有批改结果。
func ExtractGrading(m solochat.MessageDTO) (*schemas.GradingOutput, bool) {
	for _, p := range m.Parts {
		if p.Type == solochat.PartToolResult && p.Name == gradeToolName && len(p.Output) > 0 {
			var g schemas.GradingOutput
			if json.Unmarshal(p.Output, &g) != nil {
				return nil, false
			}
			return &g, true
		}
	}
	return nil, false
}

// FirstGrading 在一组消息里找第一条带批改结果(grade_with_ocr)的消息并返回其 GradingOutput。
func FirstGrading(msgs []solochat.MessageDTO) (*schemas.GradingOutput, bool) {
	for _, m := range msgs {
		if g, ok := ExtractGrading(m); ok {
			return g, true
		}
	}
	return nil, false
}

// GradingFileIDs 取 GradingOutput.ImageRefs 引用的 solochat 文件 id。
func GradingFileIDs(g *schemas.GradingOutput) []string {
	ids := make([]string, 0, len(g.ImageRefs))
	for _, r := range g.ImageRefs {
		ids = append(ids, r.AttachmentID)
	}
	return ids
}

// ThreadFileIDs 收集一条消息里需要复制的图片 file id:用户上传的附件 + grading imageRefs。
func ThreadFileIDs(m solochat.MessageDTO) []string {
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

// BuildGrading 按 copied 回写 GradingOutput 的 imageRefs(指向复制后附件),返回同一指针。
func BuildGrading(g *schemas.GradingOutput, copied map[string]CopiedFile) *schemas.GradingOutput {
	for i := range g.ImageRefs {
		if a, ok := copied[g.ImageRefs[i].AttachmentID]; ok {
			g.ImageRefs[i].AttachmentID = a.ID
			g.ImageRefs[i].URL = a.URL
		}
	}
	return g
}

// BuildThread 把一组消息映射成快照 segments:parts 忠实保留(丢 thinking),
// 附件与 grading 图片按 copied 回写为复制后地址。
func BuildThread(msgs []solochat.MessageDTO, copied map[string]CopiedFile) []ThreadSegment {
	segs := make([]ThreadSegment, 0, len(msgs))
	for _, m := range msgs {
		seg := ThreadSegment{Role: m.Role, Text: collectText(m.Parts)}
		for _, att := range m.Attachments {
			if a, ok := copied[att.ID]; ok {
				seg.Attachments = append(seg.Attachments, Attach{
					AttachmentID: a.ID,
					OriginalName: a.OriginalName,
					MimeType:     a.MimeType,
					URL:          a.URL,
				})
			}
		}
		seg.Parts = buildSegmentParts(m.Parts, copied)
		segs = append(segs, seg)
	}
	return segs
}

func buildSegmentParts(parts []solochat.MessagePart, copied map[string]CopiedFile) []SegmentPart {
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
				output = rewriteGradingImageRefs(p.Output, copied)
			}
			out = append(out, SegmentPart{Type: "tool_result", Name: p.Name, Output: output})
		}
	}
	return out
}

func rewriteGradingImageRefs(raw json.RawMessage, copied map[string]CopiedFile) json.RawMessage {
	var g schemas.GradingOutput
	if json.Unmarshal(raw, &g) != nil {
		return raw
	}
	for i := range g.ImageRefs {
		if a, ok := copied[g.ImageRefs[i].AttachmentID]; ok {
			g.ImageRefs[i].AttachmentID = a.ID
			g.ImageRefs[i].URL = a.URL
		}
	}
	b, err := json.Marshal(g)
	if err != nil {
		return raw
	}
	return b
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
