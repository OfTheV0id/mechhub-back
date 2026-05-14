package solochat

import "sync"

type Hub struct {
	mu     sync.Mutex
	subs   map[string][]chan GradingEvent
	closed map[string]bool
}

func NewHub() *Hub {
	return &Hub{
		subs:   map[string][]chan GradingEvent{},
		closed: map[string]bool{},
	}
}

func (h *Hub) Subscribe(taskID string) <-chan GradingEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan GradingEvent, 16)
	if h.closed[taskID] {
		close(ch)
		return ch
	}
	h.subs[taskID] = append(h.subs[taskID], ch)
	return ch
}

func (h *Hub) Unsubscribe(taskID string, ch <-chan GradingEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	subs := h.subs[taskID]
	for i, s := range subs {
		if s == ch {
			close(s)
			h.subs[taskID] = append(subs[:i], subs[i+1:]...)
			return
		}
	}
}

func (h *Hub) Publish(taskID string, ev GradingEvent) {
	h.mu.Lock()
	subs := append([]chan GradingEvent(nil), h.subs[taskID]...)
	h.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (h *Hub) Close(taskID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.subs[taskID] {
		close(ch)
	}
	delete(h.subs, taskID)
	h.closed[taskID] = true
}
