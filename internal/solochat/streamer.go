package solochat

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
)

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

func (w *sseWriter) write(ev StreamEvent) bool {
	data, err := json.Marshal(ev)
	if err != nil {
		return false
	}
	if _, err := w.c.Writer.Write([]byte("data: ")); err != nil {
		return false
	}
	if _, err := w.c.Writer.Write(data); err != nil {
		return false
	}
	if _, err := w.c.Writer.Write([]byte("\n\n")); err != nil {
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
