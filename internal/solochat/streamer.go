package solochat

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

type ndjsonWriter struct {
	c *gin.Context
}

func newNDJSON(c *gin.Context) *ndjsonWriter {
	c.Header("Content-Type", "application/x-ndjson; charset=utf-8")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	return &ndjsonWriter{c: c}
}

func (w *ndjsonWriter) write(ev StreamEvent) bool {
	data, err := json.Marshal(ev)
	if err != nil {
		return false
	}
	if _, err := w.c.Writer.Write(append(data, '\n')); err != nil {
		return false
	}
	flusher, ok := w.c.Writer.(http.Flusher)
	if ok {
		flusher.Flush()
	}
	return true
}

func (w *ndjsonWriter) writeRaw(v any) bool {
	data, err := json.Marshal(v)
	if err != nil {
		return false
	}
	if _, err := w.c.Writer.Write(append(data, '\n')); err != nil {
		return false
	}
	if flusher, ok := w.c.Writer.(http.Flusher); ok {
		flusher.Flush()
	}
	return true
}

type sseWriter struct {
	c *gin.Context
}

func newSSE(c *gin.Context) *sseWriter {
	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.Header("Connection", "keep-alive")
	return &sseWriter{c: c}
}

func (w *sseWriter) write(event string, v any) bool {
	data, err := json.Marshal(v)
	if err != nil {
		return false
	}
	frame := fmt.Sprintf("event: %s\ndata: %s\n\n", event, data)
	if _, err := w.c.Writer.Write([]byte(frame)); err != nil {
		return false
	}
	if flusher, ok := w.c.Writer.(http.Flusher); ok {
		flusher.Flush()
	}
	return true
}

func (w *sseWriter) heartbeat() bool {
	if _, err := w.c.Writer.Write([]byte(": ping\n\n")); err != nil {
		return false
	}
	if flusher, ok := w.c.Writer.(http.Flusher); ok {
		flusher.Flush()
	}
	return true
}
