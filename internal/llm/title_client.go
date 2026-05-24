package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type titleHTTPClient struct {
	baseURL   string
	apiKey    string
	modelName string
	client    *http.Client
}

func newTitleHTTPClient(baseURL, apiKey, modelName string) *titleHTTPClient {
	return &titleHTTPClient{
		baseURL:   baseURL,
		apiKey:    apiKey,
		modelName: modelName,
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

type titleReq struct {
	Model       string        `json:"model"`
	Messages    []titleMsg    `json:"messages"`
	Temperature float32       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
	Thinking    *titleThink   `json:"thinking,omitempty"`
}

type titleMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type titleThink struct {
	Type string `json:"type"`
}

type titleResp struct {
	Choices []struct {
		Message struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"message"`
	} `json:"choices"`
}

func (c *titleHTTPClient) Generate(ctx context.Context, userText, assistantText string) (string, error) {
	prompt := titleSystemPrompt + "\n\n[用户]\n" + userText +
		"\n\n[助手]\n" + assistantText

	reqBody := titleReq{
		Model: c.modelName,
		Messages: []titleMsg{
			{Role: "user", Content: prompt},
		},
		Temperature: 0.2,
		MaxTokens:   64,
		Thinking:    &titleThink{Type: "disabled"},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	url := c.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("title http %d: %s", resp.StatusCode, string(b))
	}

	var tr titleResp
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", err
	}
	if len(tr.Choices) == 0 {
		return "", fmt.Errorf("empty choices")
	}

	raw := tr.Choices[0].Message.Content
	if raw == "" {
		raw = tr.Choices[0].Message.ReasoningContent
	}
	title := sanitizeTitle(raw)
	if title == "" {
		return "", fmt.Errorf("model returned empty title")
	}
	return title, nil
}
