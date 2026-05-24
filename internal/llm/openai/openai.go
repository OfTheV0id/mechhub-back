// Package openai 是 ADK Go 的 OpenAI ChatCompletions-兼容 model 适配器。
//
// 改造自 github.com/byebyebruce/adk-go-openai(MIT)。原版钉 ADK v0.2.0,
// 这里对齐 v1.2.0 接口,并打开 DeepSeek 的 reasoning_content 通道 ——
// V4-Pro / R1 系列的思考链通过 ChatCompletionStreamChoiceDelta.ReasoningContent
// 流式返回,翻译成 genai.Part{Thought: true},上层 SSE 层会出
// reasoning_delta 帧(与 Gemini thinking 路径一致)。
package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"sort"
	"strings"

	"github.com/sashabaranov/go-openai"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

var _ model.LLM = &OpenAIModel{}

var (
	ErrNoChoicesInResponse = errors.New("no choices in OpenAI response")
)

// OpenAIModel 满足 ADK model.LLM,任何兼容 OpenAI ChatCompletions 的后端都能用
// (DeepSeek / Qwen / OpenRouter / vLLM / Ollama 等)。
//
// Vision:OpenAI ChatCompletions 多模态走 `content: [{type:"image_url",...}]`,
// 但大量纯文本模型(DeepSeek V4 主线、Qwen-max 文本版等)收到 image_url 会
// 400 报 `unknown variant image_url`。Vision 字段控制是否真把 InlineData 翻成
// image_url:false(默认)时跳过图片(solochat 已在 prompt 文本里列出图片
// index,工具按 index 从 session 缓存取真图,LLM 不需要"看"原图);true 时
// 走 image_url(留给 qwen-vl-max / GPT-4o 这类带视觉的)。
type OpenAIModel struct {
	Client    *openai.Client
	ModelName string
	Vision    bool
}

func NewOpenAIModelWithAPIKey(modelName, apiKey string) *OpenAIModel {
	cfg := openai.DefaultConfig(apiKey)
	return NewOpenAIModel(modelName, cfg)
}

func NewOpenAIModel(modelName string, cfg openai.ClientConfig) *OpenAIModel {
	return &OpenAIModel{
		Client:    openai.NewClientWithConfig(cfg),
		ModelName: modelName,
	}
}

// WithVision 启用 image_url 多模态字段。
func (o *OpenAIModel) WithVision(v bool) *OpenAIModel {
	o.Vision = v
	return o
}

func (o *OpenAIModel) Name() string { return o.ModelName }

func (o *OpenAIModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	if stream {
		return o.generateStream(ctx, req)
	}
	return o.generate(ctx, req)
}

func (o *OpenAIModel) generate(ctx context.Context, req *model.LLMRequest) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		openaiReq, err := toOpenAIRequest(req, o.ModelName, o.Vision)
		if err != nil {
			yield(nil, err)
			return
		}
		resp, err := o.Client.CreateChatCompletion(ctx, openaiReq)
		if err != nil {
			yield(nil, err)
			return
		}
		llmResp, err := convertChatCompletionResponse(&resp)
		if err != nil {
			yield(nil, err)
			return
		}
		yield(llmResp, nil)
	}
}

func (o *OpenAIModel) generateStream(ctx context.Context, req *model.LLMRequest) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		openaiReq, err := toOpenAIRequest(req, o.ModelName, o.Vision)
		if err != nil {
			yield(nil, err)
			return
		}
		openaiReq.Stream = true

		stream, err := o.Client.CreateChatCompletionStream(ctx, openaiReq)
		if err != nil {
			yield(nil, err)
			return
		}
		defer stream.Close()

		// aggregated 是收尾时回灌给上层的"完整内容"event,与 Gemini 流式行为对齐
		// (ADK runner 只对 Partial=false 的 final event 做完整处理)。
		aggregated := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{}}
		var finishReason genai.FinishReason
		var usage *genai.GenerateContentResponseUsageMetadata

		// 工具调用要按 tool_call.index 跨 chunk 累积 args(OpenAI 流式 spec
		// 把 JSON 字符串切片送回来,聚合完才是完整 args)。
		toolBuilders := make(map[int]*toolCallBuilder)

		// 收尾文本/思考累积进 aggregated,partial event 也用同一个 buffer 的 last part
		var lastTextPart, lastThoughtPart *genai.Part

		for {
			chunk, err := stream.Recv()
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				yield(nil, err)
				return
			}
			if len(chunk.Choices) == 0 {
				// 末尾可能只带 usage,无 choices
				if chunk.Usage != nil {
					usage = toUsage(chunk.Usage)
				}
				continue
			}
			choice := chunk.Choices[0]

			// DeepSeek 风格的 reasoning_content(R1/V4)→ Thought=true partial
			if choice.Delta.ReasoningContent != "" {
				part := &genai.Part{Text: choice.Delta.ReasoningContent, Thought: true}
				if lastThoughtPart != nil {
					lastThoughtPart.Text += part.Text
				} else {
					p := &genai.Part{Text: part.Text, Thought: true}
					aggregated.Parts = append(aggregated.Parts, p)
					lastThoughtPart = p
				}
				if !yield(&model.LLMResponse{
					Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{part}},
					Partial: true,
				}, nil) {
					return
				}
				// 切换到 reasoning 通道后,后续 text delta 应起新 part
				lastTextPart = nil
			}

			// 普通文本 delta
			if choice.Delta.Content != "" {
				part := &genai.Part{Text: choice.Delta.Content}
				if lastTextPart != nil {
					lastTextPart.Text += part.Text
				} else {
					p := &genai.Part{Text: part.Text}
					aggregated.Parts = append(aggregated.Parts, p)
					lastTextPart = p
				}
				if !yield(&model.LLMResponse{
					Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{part}},
					Partial: true,
				}, nil) {
					return
				}
				lastThoughtPart = nil
			}

			// 跨 chunk 累积 tool call
			for _, tc := range choice.Delta.ToolCalls {
				idx := 0
				if tc.Index != nil {
					idx = *tc.Index
				}
				b, ok := toolBuilders[idx]
				if !ok {
					b = &toolCallBuilder{}
					toolBuilders[idx] = b
				}
				if tc.ID != "" {
					b.id = tc.ID
				}
				if tc.Function.Name != "" {
					b.name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					b.args += tc.Function.Arguments
				}
				// 切到工具调用通道后,文本 part 也起新一段
				lastTextPart = nil
				lastThoughtPart = nil
			}

			if choice.FinishReason != "" {
				finishReason = convertFinishReason(string(choice.FinishReason))
			}
			if chunk.Usage != nil {
				usage = toUsage(chunk.Usage)
			}
		}

		// 把累积完的 tool call 转成 final event 的 FunctionCall parts
		if len(toolBuilders) > 0 {
			indices := make([]int, 0, len(toolBuilders))
			for i := range toolBuilders {
				indices = append(indices, i)
			}
			sort.Ints(indices)
			for _, i := range indices {
				b := toolBuilders[i]
				aggregated.Parts = append(aggregated.Parts, &genai.Part{
					FunctionCall: &genai.FunctionCall{
						ID:   b.id,
						Name: b.name,
						Args: parseJSONArgs(b.args),
					},
				})
			}
		}

		yield(&model.LLMResponse{
			Content:       aggregated,
			UsageMetadata: usage,
			FinishReason:  finishReason,
			Partial:       false,
			TurnComplete:  true,
		}, nil)
	}
}

type toolCallBuilder struct {
	id   string
	name string
	args string
}

// ----- request 翻译 -----

func toOpenAIRequest(req *model.LLMRequest, modelName string, vision bool) (openai.ChatCompletionRequest, error) {
	msgs := make([]openai.ChatCompletionMessage, 0, len(req.Contents))
	for _, c := range req.Contents {
		converted, err := contentToOpenAIMessages(c, vision)
		if err != nil {
			return openai.ChatCompletionRequest{}, err
		}
		msgs = append(msgs, converted...)
	}

	out := openai.ChatCompletionRequest{
		Model:    modelName,
		Messages: msgs,
	}

	if req.Config == nil {
		return out, nil
	}

	// system instruction 拍到 messages 头
	if req.Config.SystemInstruction != nil {
		sys := openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: extractText(req.Config.SystemInstruction),
		}
		out.Messages = append([]openai.ChatCompletionMessage{sys}, out.Messages...)
	}

	// thinking level → reasoning_effort(OpenAI o1 / DeepSeek 部分模型支持)
	if req.Config.ThinkingConfig != nil {
		switch req.Config.ThinkingConfig.ThinkingLevel {
		case genai.ThinkingLevelLow:
			out.ReasoningEffort = "low"
		case genai.ThinkingLevelHigh:
			out.ReasoningEffort = "high"
		default:
			out.ReasoningEffort = "medium"
		}
	}

	// structured output
	if req.Config.ResponseSchema != nil {
		out.ResponseFormat = &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONSchema,
			JSONSchema: &openai.ChatCompletionResponseFormatJSONSchema{
				Name:        "response",
				Description: req.Config.ResponseSchema.Description,
				Strict:      true,
				Schema:      schemaMarshaler{req.Config.ResponseSchema},
			},
		}
	} else if req.Config.ResponseMIMEType == "application/json" {
		out.ResponseFormat = &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		}
	}

	// tools
	if len(req.Config.Tools) > 0 {
		tools, err := convertTools(req.Config.Tools)
		if err != nil {
			return openai.ChatCompletionRequest{}, err
		}
		out.Tools = tools
	}

	if req.Config.Temperature != nil {
		out.Temperature = *req.Config.Temperature
	}
	if req.Config.MaxOutputTokens > 0 {
		out.MaxTokens = int(req.Config.MaxOutputTokens)
	}
	if req.Config.TopP != nil {
		out.TopP = *req.Config.TopP
	}
	if len(req.Config.StopSequences) > 0 {
		out.Stop = req.Config.StopSequences
	}
	return out, nil
}

// contentToOpenAIMessages 把一个 genai.Content 拆成 1+ 条 OpenAI message。
// 若 parts 里全是 FunctionResponse,要拆成多条 role=tool 消息;否则混合成
// 一条 assistant/user 消息(支持 MultiContent vision)。
//
// vision=false 时遇到 InlineData 直接跳过(纯文本模型如 DeepSeek V4 主线
// 收 image_url 会 400)。solochat 已在 prompt 文本里列出图片 index,工具按
// index 读 session 缓存里的真图,LLM 自身不用"看"。
func contentToOpenAIMessages(c *genai.Content, vision bool) ([]openai.ChatCompletionMessage, error) {
	// 先抽走所有 FunctionResponse → 各成一条 role=tool 消息
	var toolMsgs []openai.ChatCompletionMessage
	var rest []*genai.Part
	for _, p := range c.Parts {
		if p.FunctionResponse != nil {
			respJSON, err := json.Marshal(p.FunctionResponse.Response)
			if err != nil {
				return nil, fmt.Errorf("marshal function response: %w", err)
			}
			toolMsgs = append(toolMsgs, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				ToolCallID: p.FunctionResponse.ID,
				Content:    string(respJSON),
			})
			continue
		}
		rest = append(rest, p)
	}
	if len(rest) == 0 {
		return toolMsgs, nil
	}

	msg := openai.ChatCompletionMessage{Role: convertRole(c.Role)}

	// DeepSeek 严格要求多轮对话里要把上一轮 assistant 的 reasoning_content
	// 原样回传(`The reasoning_content in the thinking mode must be passed
	// back to the API`),否则 400。把 Thought=true 的 part 单独抽出来塞
	// ReasoningContent 字段;普通 text 走 Content / MultiContent。
	var reasoningParts []string
	var multi []openai.ChatMessagePart
	var toolCalls []openai.ToolCall
	for _, p := range rest {
		switch {
		case p.FunctionCall != nil:
			argsJSON, err := json.Marshal(p.FunctionCall.Args)
			if err != nil {
				return nil, fmt.Errorf("marshal function call args: %w", err)
			}
			toolCalls = append(toolCalls, openai.ToolCall{
				ID:   p.FunctionCall.ID,
				Type: openai.ToolTypeFunction,
				Function: openai.FunctionCall{
					Name:      p.FunctionCall.Name,
					Arguments: string(argsJSON),
				},
			})
		case p.InlineData != nil:
			if !vision {
				// 纯文本模型(DeepSeek V4 主线等)收 image_url 会 400;
				// 直接丢掉,solochat 已用文本描述了图片清单
				continue
			}
			switch p.InlineData.MIMEType {
			case "image/jpg", "image/jpeg", "image/png", "image/gif", "image/webp":
				data := base64.StdEncoding.EncodeToString(p.InlineData.Data)
				multi = append(multi, openai.ChatMessagePart{
					Type: openai.ChatMessagePartTypeImageURL,
					ImageURL: &openai.ChatMessageImageURL{
						URL:    fmt.Sprintf("data:%s;base64,%s", p.InlineData.MIMEType, data),
						Detail: openai.ImageURLDetailAuto,
					},
				})
			default:
				return nil, fmt.Errorf("openai-compat: unsupported InlineData MIME %s (vision tools 用 image/* 限定)", p.InlineData.MIMEType)
			}
		case p.Text != "":
			if p.Thought {
				reasoningParts = append(reasoningParts, p.Text)
				continue
			}
			multi = append(multi, openai.ChatMessagePart{
				Type: openai.ChatMessagePartTypeText,
				Text: p.Text,
			})
		}
	}

	if len(reasoningParts) > 0 {
		msg.ReasoningContent = joinNonEmpty(reasoningParts)
	}

	// 单 text part(无 tool call / 无 image)走简单 Content;否则 MultiContent
	if len(multi) == 1 && multi[0].Type == openai.ChatMessagePartTypeText && len(toolCalls) == 0 {
		msg.Content = multi[0].Text
	} else if len(multi) > 0 {
		msg.MultiContent = multi
	}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}
	return append(toolMsgs, msg), nil
}

func joinNonEmpty(ss []string) string { return strings.Join(ss, "\n") }

// ----- response 翻译 -----

func convertChatCompletionResponse(resp *openai.ChatCompletionResponse) (*model.LLMResponse, error) {
	if len(resp.Choices) == 0 {
		return nil, ErrNoChoicesInResponse
	}
	choice := resp.Choices[0]
	content := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{}}

	if choice.Message.ReasoningContent != "" {
		content.Parts = append(content.Parts, &genai.Part{
			Text: choice.Message.ReasoningContent, Thought: true,
		})
	}
	if choice.Message.Content != "" {
		content.Parts = append(content.Parts, &genai.Part{Text: choice.Message.Content})
	}
	for _, tc := range choice.Message.ToolCalls {
		if tc.Type != openai.ToolTypeFunction {
			continue
		}
		content.Parts = append(content.Parts, &genai.Part{
			FunctionCall: &genai.FunctionCall{
				ID:   tc.ID,
				Name: tc.Function.Name,
				Args: parseJSONArgs(tc.Function.Arguments),
			},
		})
	}

	var usage *genai.GenerateContentResponseUsageMetadata
	if resp.Usage.TotalTokens > 0 {
		usage = &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     int32(resp.Usage.PromptTokens),
			CandidatesTokenCount: int32(resp.Usage.CompletionTokens),
			TotalTokenCount:      int32(resp.Usage.TotalTokens),
		}
		if resp.Usage.PromptTokensDetails != nil {
			usage.CachedContentTokenCount = int32(resp.Usage.PromptTokensDetails.CachedTokens)
		}
	}

	return &model.LLMResponse{
		Content:       content,
		UsageMetadata: usage,
		FinishReason:  convertFinishReason(string(choice.FinishReason)),
		TurnComplete:  true,
	}, nil
}

// ----- 小工具 -----

func convertTools(tools []*genai.Tool) ([]openai.Tool, error) {
	var out []openai.Tool
	for _, t := range tools {
		if t == nil {
			continue
		}
		// 这些非 function 工具 OpenAI 端没有对应,直接忽略(不报错,
		// 否则 ADK 内置任何带 GoogleSearch 之类的 tool spec 全跑不起来)。
		for _, fd := range t.FunctionDeclarations {
			params, err := functionParameters(fd)
			if err != nil {
				return nil, fmt.Errorf("tool %s: %w", fd.Name, err)
			}
			out = append(out, openai.Tool{
				Type: openai.ToolTypeFunction,
				Function: &openai.FunctionDefinition{
					Name:        fd.Name,
					Description: fd.Description,
					Parameters:  params,
				},
			})
		}
	}
	return out, nil
}

func functionParameters(fd *genai.FunctionDeclaration) (any, error) {
	if fd.ParametersJsonSchema != nil {
		return fd.ParametersJsonSchema, nil
	}
	if fd.Parameters != nil {
		// 把 genai.Schema marshal 一次再 unmarshal 成通用 map,OpenAI 端要的是 JSON Schema object
		b, err := json.Marshal(fd.Parameters)
		if err != nil {
			return nil, err
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, err
		}
		return m, nil
	}
	// OpenAI 要求 Parameters 非空;无参 tool 给个空 object
	return map[string]any{"type": "object", "properties": map[string]any{}}, nil
}

// schemaMarshaler 把 *genai.Schema 适配成 json.Marshaler,塞进
// ChatCompletionResponseFormatJSONSchema.Schema。
type schemaMarshaler struct{ s *genai.Schema }

func (m schemaMarshaler) MarshalJSON() ([]byte, error) { return json.Marshal(m.s) }

func convertRole(role string) string {
	switch role {
	case "user":
		return openai.ChatMessageRoleUser
	case "model":
		return openai.ChatMessageRoleAssistant
	case "system":
		return openai.ChatMessageRoleSystem
	default:
		return openai.ChatMessageRoleUser
	}
}

func convertFinishReason(reason string) genai.FinishReason {
	switch reason {
	case "stop", "tool_calls", "function_call":
		return genai.FinishReasonStop
	case "length":
		return genai.FinishReasonMaxTokens
	case "content_filter":
		return genai.FinishReasonSafety
	default:
		return genai.FinishReasonUnspecified
	}
}

func extractText(c *genai.Content) string {
	if c == nil {
		return ""
	}
	var out string
	for i, p := range c.Parts {
		if p.Text == "" {
			continue
		}
		if i > 0 && out != "" {
			out += "\n"
		}
		out += p.Text
	}
	return out
}

func parseJSONArgs(s string) map[string]any {
	if s == "" {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return map[string]any{}
	}
	return m
}

func toUsage(u *openai.Usage) *genai.GenerateContentResponseUsageMetadata {
	out := &genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount:     int32(u.PromptTokens),
		CandidatesTokenCount: int32(u.CompletionTokens),
		TotalTokenCount:      int32(u.TotalTokens),
	}
	if u.PromptTokensDetails != nil {
		out.CachedContentTokenCount = int32(u.PromptTokensDetails.CachedTokens)
	}
	return out
}
