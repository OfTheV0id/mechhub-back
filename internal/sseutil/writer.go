package sseutil

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
)

type Writer struct {
	c *gin.Context
}

func New(c *gin.Context) *Writer {
	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.Header("Connection", "keep-alive")
	return &Writer{c: c}
}

func (w *Writer) Write(payload any) bool {
	data, err := json.Marshal(payload)
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
	w.flush()
	return true
}

func (w *Writer) Heartbeat() bool {
	if _, err := w.c.Writer.Write([]byte(": ping\n\n")); err != nil {
		return false
	}
	w.flush()
	return true
}

func (w *Writer) flush() {
	if flusher, ok := w.c.Writer.(http.Flusher); ok {
		flusher.Flush()
	}
}
