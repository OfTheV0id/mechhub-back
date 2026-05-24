package llm

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

const (
	titleSystemPrompt = "用 16 个汉字以内概括下面对话的主题,作为对话标题。只输出标题本身,不要引号、不要句号、不要解释、不要前后空格。"
	titleMaxRunes     = 24
)

func (s *Service) GenerateTitle(ctx context.Context, userText, assistantText string) (string, error) {
	if strings.TrimSpace(userText) == "" {
		return "", fmt.Errorf("empty user text")
	}

	userText = strings.TrimSpace(userText)
	assistantText = strings.TrimSpace(assistantText)

	if s.titleHTTP != nil {
		return s.titleHTTP.Generate(ctx, userText, assistantText)
	}

	// fallback: 通过 rootModel 的 ADK 接口生成标题
	prompt := titleSystemPrompt + "\n\n[用户]\n" + userText +
		"\n\n[助手]\n" + assistantText

	temp := float32(0.2)
	req := &model.LLMRequest{
		Contents: []*genai.Content{{
			Role:  "user",
			Parts: []*genai.Part{{Text: prompt}},
		}},
		Config: &genai.GenerateContentConfig{
			Temperature:     &temp,
			MaxOutputTokens: 256,
		},
	}

	var raw strings.Builder
	for resp, err := range s.rootModel.GenerateContent(ctx, req, false) {
		if err != nil {
			return "", err
		}
		if resp == nil || resp.Content == nil {
			continue
		}
		for _, p := range resp.Content.Parts {
			if p.Thought {
				continue
			}
			if p.Text != "" {
				raw.WriteString(p.Text)
			}
		}
	}

	title := sanitizeTitle(raw.String())
	if title == "" {
		return "", fmt.Errorf("model returned empty title")
	}
	return title, nil
}

func sanitizeTitle(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	s = strings.Trim(s, "「」『』\"'`【】《》()()。.,, \t")
	runes := []rune(s)
	if len(runes) > titleMaxRunes {
		s = string(runes[:titleMaxRunes])
	}
	return strings.TrimSpace(s)
}
