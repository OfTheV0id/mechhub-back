package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"

	"mechhub-back/internal/config"
)

type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(cfg config.AgentConfig) *Client {
	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		http:    &http.Client{Timeout: cfg.Timeout},
	}
}

func (c *Client) Chat(ctx context.Context, req ChatRequest) (<-chan Event, error) {
	body, contentType, err := buildMultipart(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat", body)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", contentType)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("agent: status %d", resp.StatusCode)
	}

	out := make(chan Event, 32)
	go parseSSE(resp.Body, out)
	return out, nil
}

// FetchMessages 调 Python GET /sessions/{id}/messages,拿到该 ADK session
// 翻译好的 MessageDTO 列表(parts 分类齐全:text / thinking / tool_use /
// tool_result;user message 带 attachments:[{id}],由 Go 端 hydrate)。
func (c *Client) FetchMessages(ctx context.Context, sessionID string) ([]AgentMessage, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/sessions/"+sessionID+"/messages", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, nil
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("agent: fetch messages status %d", resp.StatusCode)
	}
	var wrap struct {
		Messages []AgentMessage `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrap); err != nil {
		return nil, err
	}
	return wrap.Messages, nil
}

func buildMultipart(req ChatRequest) (io.Reader, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	if err := w.WriteField("session_id", req.SessionID); err != nil {
		return nil, "", err
	}
	if err := w.WriteField("message", req.Message); err != nil {
		return nil, "", err
	}
	if len(req.FileIDs) > 0 {
		ids, err := json.Marshal(req.FileIDs)
		if err != nil {
			return nil, "", err
		}
		if err := w.WriteField("file_ids", string(ids)); err != nil {
			return nil, "", err
		}
	}

	for _, f := range req.Files {
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="files"; filename="%s"`, f.Filename))
		if f.ContentType != "" {
			h.Set("Content-Type", f.ContentType)
		}
		part, err := w.CreatePart(h)
		if err != nil {
			return nil, "", err
		}
		if _, err := io.Copy(part, f.Body); err != nil {
			return nil, "", err
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return &buf, w.FormDataContentType(), nil
}

func parseSSE(body io.ReadCloser, out chan<- Event) {
	defer body.Close()
	defer close(out)

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 1<<20), 4<<20)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		var ev Event
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		out <- ev
		if ev.Type == EventMessageDone {
			return
		}
	}
}
