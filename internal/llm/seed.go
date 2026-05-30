package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// SeedPart 是注入历史事件的一个内容块。Type: "text" | "tool_use" | "tool_result"。
type SeedPart struct {
	Type   string
	Text   string
	Name   string          // tool_use / tool_result 的工具名
	Input  json.RawMessage // tool_use 参数(JSON object)
	Output json.RawMessage // tool_result 结果(JSON object)
}

// SeedTurn 是注入的一条历史消息。Role: "user" | "assistant"。
// AttachmentIDs 仅对 user turn 生效:写 _solochat_attachments_<inv> 绑定,
// 供 ListMessages 还原 user 消息的附件(与正常 stream 落盘格式一致)。
type SeedTurn struct {
	Role          string
	Parts         []SeedPart
	AttachmentIDs []string
}

// SeedSession 把一组历史 turn 直接写进 (userID, sessionID) 的 ADK session,
// 完全不经过 LLM —— 用于频道 fork 回 solochat 的忠实复制。每个 turn 一个
// invocation;事件结构与正常 stream 落下的对齐,使 ListMessages 能原样还原。
func (s *Service) SeedSession(ctx context.Context, userID, sessionID string, turns []SeedTurn) error {
	if err := s.ensureSession(ctx, userID, sessionID); err != nil {
		return err
	}
	for _, t := range turns {
		invocation := "fork-" + uuid.NewString()
		content, err := seedContent(t)
		if err != nil {
			return err
		}
		ev := session.NewEvent(invocation)
		if t.Role == "user" {
			ev.Author = "user"
		} else {
			ev.Author = AppName
		}
		ev.Content = content
		if err := s.appendFreshEvent(ctx, userID, sessionID, ev); err != nil {
			return err
		}
		if t.Role == "user" && len(t.AttachmentIDs) > 0 {
			if err := s.appendAttachmentBinding(ctx, userID, sessionID, invocation, t.AttachmentIDs); err != nil {
				return err
			}
		}
	}
	return nil
}

// appendFreshEvent 每次 Get 最新 session 再 AppendEvent,避开 ADK 乐观锁 stale 报错
// (同 appendAttachmentBinding 的处理)。
func (s *Service) appendFreshEvent(ctx context.Context, userID, sessionID string, ev *session.Event) error {
	resp, err := s.sessionSvc.Get(ctx, &session.GetRequest{
		AppName:   AppName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		return err
	}
	if resp == nil || resp.Session == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	return s.sessionSvc.AppendEvent(ctx, resp.Session, ev)
}

func seedContent(t SeedTurn) (*genai.Content, error) {
	role := "model"
	if t.Role == "user" {
		role = "user"
	}
	parts := make([]*genai.Part, 0, len(t.Parts))
	for _, p := range t.Parts {
		switch p.Type {
		case "text":
			if p.Text == "" {
				continue
			}
			parts = append(parts, &genai.Part{Text: p.Text})
		case "tool_use":
			args := map[string]any{}
			if len(p.Input) > 0 {
				if err := json.Unmarshal(p.Input, &args); err != nil {
					return nil, err
				}
			}
			parts = append(parts, &genai.Part{FunctionCall: &genai.FunctionCall{
				ID:   "fc-" + uuid.NewString()[:8],
				Name: p.Name,
				Args: args,
			}})
		case "tool_result":
			respObj := map[string]any{}
			if len(p.Output) > 0 {
				if err := json.Unmarshal(p.Output, &respObj); err != nil {
					return nil, err
				}
			}
			parts = append(parts, &genai.Part{FunctionResponse: &genai.FunctionResponse{
				ID:       "fr-" + uuid.NewString()[:8],
				Name:     p.Name,
				Response: respObj,
			}})
		}
	}
	return &genai.Content{Role: role, Parts: parts}, nil
}
