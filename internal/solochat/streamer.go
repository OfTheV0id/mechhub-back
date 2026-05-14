package solochat

import (
	"encoding/json"
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
	if flusher, ok := w.c.Writer.(http.Flusher); ok {
		flusher.Flush()
	}
	return true
}
