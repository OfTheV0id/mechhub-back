package realtime

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 25 * time.Second
	maxMessageSize = 4 * 1024 // 客户端目前没业务向上发,只接 ping/pong
	sendBufferSize = 64
)

// Conn 包装一条已升级的 WebSocket 连接。每个用户每个 tab 一份。
type Conn struct {
	hub       *Hub
	userID    string
	ws        *websocket.Conn
	send      chan []byte // 写出帧 buffered;满了就丢(实时事件丢一条不致命)
	closeOnce sync.Once
}

// SendJSON 把 payload 序列化后塞进 send chan。非阻塞 —— buffer 满直接丢。
func (c *Conn) SendJSON(payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	select {
	case c.send <- data:
	default:
		// buffer 满,丢弃该帧;客户端漏了 invalidate 会通过下一次失效或者
		// 用户手动操作触发重拉,不致命。
	}
}

// readPump 处理客户端发来的(主要是 pong)。出错 / 断开就关闭并触发 unregister。
func (c *Conn) readPump() {
	defer func() {
		c.hub.Unregister(c)
		_ = c.ws.Close()
	}()
	c.ws.SetReadLimit(maxMessageSize)
	_ = c.ws.SetReadDeadline(time.Now().Add(pongWait))
	c.ws.SetPongHandler(func(string) error {
		_ = c.ws.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		// 不关心内容,只为让 read 持续推进,从而 pong handler 能跑
		if _, _, err := c.ws.ReadMessage(); err != nil {
			return
		}
	}
}

// writePump 从 send chan 取帧写出,加 ping 心跳。chan 被 Unregister 关闭后 writePump 自己退出。
func (c *Conn) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.ws.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.ws.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.ws.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
