package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
	"gorm.io/gorm"
)

// StreamFrame 是 ADK 事件翻译出的 SSE 帧。Service 层把它们包成
// `data: {json}\n\n` 写到 HTTP 响应。
type StreamFrame struct {
	Type         string          `json:"type"`
	MessageID    string          `json:"message_id,omitempty"`
	Model        string          `json:"model,omitempty"`
	Delta        string          `json:"delta,omitempty"`
	Text         string          `json:"text,omitempty"`
	ToolUseID    string          `json:"tool_use_id,omitempty"`
	Name         string          `json:"name,omitempty"`
	Input        json.RawMessage `json:"input,omitempty"`
	Output       json.RawMessage `json:"output,omitempty"`
	IsError      bool            `json:"is_error,omitempty"`
	FinishReason string          `json:"finish_reason,omitempty"`
	Code         string          `json:"code,omitempty"`
	Message      string          `json:"message,omitempty"`
}

const (
	FrameMessageStart   = "message_start"
	FrameReasoningDelta = "reasoning_delta"
	FrameTextDelta      = "text_delta"
	FrameTextComplete   = "text_complete"
	FrameToolCallStart  = "tool_call_start"
	FrameToolResult     = "tool_result"
	FrameError          = "error"
	FrameMessageDone    = "message_done"
)

// StreamOptions 调用 StreamChat 时可选的参数。
type StreamOptions struct {
	FileIDs    []string
	StateDelta map[string]any // 在 runner.Run 启动前合入 session state
}

// StreamChat 跑一轮 agent,把 ADK events 翻译成 StreamFrame 推给 yield 回调。
// 等价于 Round 6 Python `server/sse.py::stream_chat`。
//
// 流程:
//  1. 入口发 `message_start`
//  2. 遍历 ADK iter.Seq2[*Event, error]:
//     a. 捕获第一个 event 的 invocation_id(用于后续附件绑定)
//     b. text part(thought=false)→ text_delta;切换时 flush 上一个 text 块
//     c. text part(thought=true)→ reasoning_delta(先 flush text)
//     d. function_call → tool_call_start
//     e. function_response → tool_result
//  3. 流末:把残留 text buffer flush 成 text_complete;若有 fileIDs,写
//     state_delta `_solochat_attachments_<inv>` = file_ids
//  4. 最后发 `message_done`
func (s *Service) StreamChat(
	ctx context.Context,
	userID, sessionID, messageID string,
	content *genai.Content,
	opts StreamOptions,
	yield func(StreamFrame) bool,
) error {
	// 绕过 ADK Go v1.2.0 的 1µs stale-session bug:`runner.Run` 内部如果走
	// Create 分支,会用纳秒精度的内存时间戳,而 MySQL DATETIME(6) 把它四舍
	// 五入到微秒;新建后的首条 AppendEvent 乐观锁就因 1µs 差报 stale。
	// 这里在 runner 跑前 Get-or-Create 一遍,让 runner.Run 内部走 Get 分支,
	// 拿到的就是 DB 截断后的时间戳,两边对齐。
	if err := s.ensureSession(ctx, userID, sessionID); err != nil {
		yield(StreamFrame{
			Type: FrameError, MessageID: messageID,
			Code: "session_init_failed", Message: err.Error(),
		})
		yield(StreamFrame{Type: FrameMessageDone, MessageID: messageID, FinishReason: "error"})
		return err
	}

	if !yield(StreamFrame{Type: FrameMessageStart, MessageID: messageID, Model: s.modelName()}) {
		return nil
	}

	var (
		textBuf      strings.Builder
		finishReason = "stop"
		invocation   string
		gotErr       error
		seenToolIDs  = map[string]bool{}
	)

	flushText := func() {
		if textBuf.Len() == 0 {
			return
		}
		yield(StreamFrame{Type: FrameTextComplete, MessageID: messageID, Text: textBuf.String()})
		textBuf.Reset()
	}

	runOpts := []runner.RunOption{runner.WithStateDelta(opts.StateDelta)}
	for ev, err := range s.runner.Run(ctx, userID, sessionID, content, agent.RunConfig{
		StreamingMode: agent.StreamingModeSSE,
	}, runOpts...) {
		if err != nil {
			gotErr = err
			if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
				finishReason = "cancelled"
				// 用户主动停,不算 error,跳过 error 帧
				continue
			}
			finishReason = "error"
			yield(StreamFrame{
				Type: FrameError, MessageID: messageID,
				Code: "agent_error", Message: err.Error(),
			})
			continue
		}
		if invocation == "" && ev.InvocationID != "" {
			invocation = ev.InvocationID
		}
		emitEvent(ev, messageID, &textBuf, seenToolIDs, yield)
	}

	// runner.Run 可能在 ctx 取消后悄无声息退出(没有 yield error),
	// 这里兜一次底:只要 ctx 已取消就标 cancelled。
	if finishReason == "stop" && errors.Is(ctx.Err(), context.Canceled) {
		finishReason = "cancelled"
	}

	flushText()

	// 流末把附件绑定写到 session.state,用 round 6 沿用的 key 格式。
	// 用户取消时也写一份 —— 部分 events 已落 MySQL,绑定保留方便复用。
	if len(opts.FileIDs) > 0 && invocation != "" && finishReason != "error" {
		// 取消的 ctx 已经 done,但 append_event 的写操作要新 ctx 撑过去
		bindCtx, bindCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := s.appendAttachmentBinding(bindCtx, userID, sessionID, invocation, opts.FileIDs); err != nil {
			fmt.Printf("llm: append attachment binding: %v\n", err)
		}
		bindCancel()
	}

	yield(StreamFrame{Type: FrameMessageDone, MessageID: messageID, FinishReason: finishReason})
	return gotErr
}

// emitEvent 把单个 ADK event 翻译成 SSE 帧。
//
// ADK Go 在 StreamingModeSSE 下对纯文本会先 yield 若干 Partial=true 增量
// event,然后 yield 一个 Partial=false 的 aggregated event(回灌整段文本)。
// 我们只用 partial 阶段累积 text_delta;non-partial 的 text part 直接丢弃,
// 否则末尾 text_delta 会变成"全文",text_complete 会重复一份。
//
// function_call / function_response 不参与 partial 流;但 ADK 偶尔会对同一
// 个工具调用 yield 两次相同 event(observed 行为),用 seenToolIDs 按
// tool_use_id 去重。
func emitEvent(ev *session.Event, messageID string, textBuf *strings.Builder, seenToolIDs map[string]bool, yield func(StreamFrame) bool) {
	c := ev.Content
	if c == nil {
		return
	}
	for _, p := range c.Parts {
		switch {
		case p.FunctionCall != nil:
			toolID := idOrFallback(p.FunctionCall.ID, ev.ID)
			if seenToolIDs["call:"+toolID] {
				continue
			}
			seenToolIDs["call:"+toolID] = true
			// flush 在前的文本
			if textBuf.Len() > 0 {
				yield(StreamFrame{Type: FrameTextComplete, MessageID: messageID, Text: textBuf.String()})
				textBuf.Reset()
			}
			input, _ := json.Marshal(p.FunctionCall.Args)
			yield(StreamFrame{
				Type: FrameToolCallStart, MessageID: messageID,
				ToolUseID: toolID,
				Name:      p.FunctionCall.Name,
				Input:     json.RawMessage(input),
			})
		case p.FunctionResponse != nil:
			toolID := idOrFallback(p.FunctionResponse.ID, ev.ID)
			if seenToolIDs["resp:"+toolID] {
				continue
			}
			seenToolIDs["resp:"+toolID] = true
			out, _ := json.Marshal(p.FunctionResponse.Response)
			yield(StreamFrame{
				Type: FrameToolResult, MessageID: messageID,
				ToolUseID: toolID,
				Name:      p.FunctionResponse.Name,
				Output:    json.RawMessage(out),
				IsError:   isErrorResponse(p.FunctionResponse.Response),
			})
		case p.Text != "":
			// non-partial 的 text 是 ADK 回灌的全文,已经在 partial 阶段累积过了 → 丢弃
			if !ev.Partial {
				continue
			}
			if p.Thought {
				// flush 文本再发思考
				if textBuf.Len() > 0 {
					yield(StreamFrame{Type: FrameTextComplete, MessageID: messageID, Text: textBuf.String()})
					textBuf.Reset()
				}
				yield(StreamFrame{Type: FrameReasoningDelta, MessageID: messageID, Delta: p.Text})
				continue
			}
			textBuf.WriteString(p.Text)
			yield(StreamFrame{Type: FrameTextDelta, MessageID: messageID, Delta: p.Text})
		}
	}
}

func idOrFallback(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func isErrorResponse(resp map[string]any) bool {
	if resp == nil {
		return false
	}
	// 框架级工具错误统一是 {"error": "..."}(base_flow.go callTool)。
	if _, hasErr := resp["error"]; hasErr {
		return true
	}
	ok, exists := resp["ok"]
	if !exists {
		return false
	}
	b, isBool := ok.(bool)
	return isBool && !b
}

func (s *Service) modelName() string {
	// Runner 没暴露 model 名;直接读 env 兜底,后续可改成 inject。
	return "" // 留空即可,前端只是展示用
}

// ensureSession 保证 (userID, sessionID) 对应的 ADK session 在 DB 中已存在。
// 若不存在则我们自己 Create 一次 —— 这次 Create 写入 DB 后,在内存里的
// session 对象虽仍带纳秒精度,但我们立刻丢弃。后续 runner.Run 会重新 Get,
// 拿到的是 DB 已截断到微秒的 UpdateTime,与 Go UnixMicro 截断一致,绕开
// ADK Go v1.2.0 的 stale-session 误报。
func (s *Service) ensureSession(ctx context.Context, userID, sessionID string) error {
	_, err := s.sessionSvc.Get(ctx, &session.GetRequest{
		AppName:   AppName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("get session: %w", err)
	}
	_, err = s.sessionSvc.Create(ctx, &session.CreateRequest{
		AppName:   AppName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// appendAttachmentBinding 流末向 ADK session.state 写一条
// `_solochat_attachments_<invocation_id>` = file_ids。用 EventActions.StateDelta
// 实现 —— append 一个空 content 的 user event,只携带 state_delta。
func (s *Service) appendAttachmentBinding(ctx context.Context, userID, sessionID, invocation string, fileIDs []string) error {
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
	ev := session.NewEvent(invocation)
	ev.Author = "user"
	ev.Actions = session.EventActions{
		StateDelta: map[string]any{
			"_solochat_attachments_" + invocation: stringsToAny(fileIDs),
		},
	}
	return s.sessionSvc.AppendEvent(ctx, resp.Session, ev)
}

func stringsToAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// 防止 unused import 警告:run config 类型用作 type compile-check
var _ = runner.WithStateDelta
