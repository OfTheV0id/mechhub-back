package llm

import (
	"context"
	"encoding/json"
	"strings"

	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// MessagePart 是前端可渲染的最小单位。和 internal/solochat.MessagePart
// 字段一致(json tag 完全对齐),由 solochat.Service 直接转换复用。
type MessagePart struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Output    json.RawMessage `json:"output,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

// Message 是单条 user 或 assistant 消息的中间表示;solochat.Service
// 拿到后会包一层加上 ConversationID + 附件元数据。
type Message struct {
	ID          string        `json:"id"`
	Role        string        `json:"role"`
	Parts       []MessagePart `json:"parts"`
	Attachments []string      `json:"attachments,omitempty"` // file_id list
	Status      string        `json:"status"`
	CreatedAt   string        `json:"created_at"`
}

// ListMessages 把 ADK session 的 events 列表按 invocation_id 分组,
// 翻译成 user / assistant 两类 Message。等价于 Round 6 Python
// `server/routes/sessions.py::_events_to_messages`。
func (s *Service) ListMessages(ctx context.Context, userID, sessionID string) ([]Message, error) {
	resp, err := s.sessionSvc.Get(ctx, &session.GetRequest{
		AppName:   AppName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.Session == nil {
		return nil, nil
	}

	// 1. 收集 state 里的附件绑定:_solochat_attachments_<inv> → []file_id
	attachmentsByInv := make(map[string][]string)
	state := resp.Session.State()
	for k, v := range state.All() {
		if !strings.HasPrefix(k, "_solochat_attachments_") {
			continue
		}
		inv := strings.TrimPrefix(k, "_solochat_attachments_")
		switch arr := v.(type) {
		case []any:
			for _, item := range arr {
				if s, ok := item.(string); ok {
					attachmentsByInv[inv] = append(attachmentsByInv[inv], s)
				}
			}
		case []string:
			attachmentsByInv[inv] = append(attachmentsByInv[inv], arr...)
		}
	}

	// 2. 按 (invocation_id, role) 分组,合并同组的 parts
	type key struct{ inv, role string }
	byKey := make(map[key]*Message)
	order := make([]*Message, 0)

	for ev := range resp.Session.Events().All() {
		if ev.Author == "" {
			continue
		}
		role := "assistant"
		if ev.Author == "user" {
			role = "user"
		}
		k := key{ev.InvocationID, role}
		msg, ok := byKey[k]
		if !ok {
			msg = &Message{
				ID:        firstNonEmpty(ev.ID, ev.InvocationID),
				Role:      role,
				Status:    "completed",
				CreatedAt: ev.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
			}
			if role == "user" {
				msg.Attachments = attachmentsByInv[ev.InvocationID]
			}
			byKey[k] = msg
			order = append(order, msg)
		}
		accumulateParts(&msg.Parts, ev)
	}

	out := make([]Message, len(order))
	for i, m := range order {
		out[i] = *m
	}
	return out, nil
}

func accumulateParts(parts *[]MessagePart, ev *session.Event) {
	if ev.Content == nil {
		return
	}
	for _, p := range ev.Content.Parts {
		switch {
		case p.FunctionCall != nil:
			input, _ := json.Marshal(p.FunctionCall.Args)
			*parts = append(*parts, MessagePart{
				Type:      "tool_use",
				ToolUseID: idOrFallback(p.FunctionCall.ID, ev.ID),
				Name:      p.FunctionCall.Name,
				Input:     json.RawMessage(input),
			})
		case p.FunctionResponse != nil:
			out, _ := json.Marshal(p.FunctionResponse.Response)
			*parts = append(*parts, MessagePart{
				Type:      "tool_result",
				ToolUseID: idOrFallback(p.FunctionResponse.ID, ev.ID),
				Name:      p.FunctionResponse.Name,
				Output:    json.RawMessage(out),
				IsError:   isErrorResponse(p.FunctionResponse.Response),
			})
		case p.Text != "":
			t := "text"
			if p.Thought {
				t = "thinking"
			}
			// 合并相邻同类型 text part
			if n := len(*parts); n > 0 && (*parts)[n-1].Type == t {
				(*parts)[n-1].Text += p.Text
				continue
			}
			*parts = append(*parts, MessagePart{Type: t, Text: p.Text})
		}
	}
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// Compile-time check: genai.Content 是 ADK 事件 content 类型,这里只是
// 让本文件保持 import 引用,以便后续若加 multimodal listing 不缺类型。
var _ = &genai.Content{}
