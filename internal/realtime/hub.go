package realtime

import "sync"

// Hub 维护所有 WebSocket 连接 + 用户/班级订阅索引,
// 提供精确广播(按 user / 按 class)。单例,router.New 里创建。
type Hub struct {
	mu            sync.RWMutex
	byUser        map[string]map[*Conn]struct{} // userID → tabs
	byClass       map[string]map[*Conn]struct{} // classID → 当前订阅该班的连接
	classesByConn map[*Conn]map[string]struct{} // 反查:连接订阅了哪些班(unregister 时用)
}

func NewHub() *Hub {
	return &Hub{
		byUser:        make(map[string]map[*Conn]struct{}),
		byClass:       make(map[string]map[*Conn]struct{}),
		classesByConn: make(map[*Conn]map[string]struct{}),
	}
}

// Register 把新连接加入索引,启动 read/write pump。classIDs = 连接时用户所属的班级
func (h *Hub) Register(conn *Conn, classIDs []string) {
	h.mu.Lock()
	if _, ok := h.byUser[conn.userID]; !ok {
		h.byUser[conn.userID] = make(map[*Conn]struct{})
	}
	h.byUser[conn.userID][conn] = struct{}{}

	classSet := make(map[string]struct{}, len(classIDs))
	for _, cid := range classIDs {
		if cid == "" {
			continue
		}
		classSet[cid] = struct{}{}
		if _, ok := h.byClass[cid]; !ok {
			h.byClass[cid] = make(map[*Conn]struct{})
		}
		h.byClass[cid][conn] = struct{}{}
	}
	h.classesByConn[conn] = classSet
	h.mu.Unlock()

	go conn.writePump()
	go conn.readPump()
}

// Unregister 在连接断开时清所有索引并关闭 send chan(writePump 自己退出)。
// 幂等:多次调用安全。
func (h *Hub) Unregister(conn *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if classes, ok := h.classesByConn[conn]; ok {
		for cid := range classes {
			if set, ok2 := h.byClass[cid]; ok2 {
				delete(set, conn)
				if len(set) == 0 {
					delete(h.byClass, cid)
				}
			}
		}
		delete(h.classesByConn, conn)
	}
	if set, ok := h.byUser[conn.userID]; ok {
		delete(set, conn)
		if len(set) == 0 {
			delete(h.byUser, conn.userID)
		}
	}
	// 关闭 send chan 让 writePump 自然退出。再次 Unregister 安全(once 保护)
	conn.closeOnce.Do(func() { close(conn.send) })
}

// AddUserToClass 用户加入新班时调:给该用户所有活跃连接补订阅。
func (h *Hub) AddUserToClass(userID, classID string) {
	if userID == "" || classID == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	conns, ok := h.byUser[userID]
	if !ok {
		return
	}
	for conn := range conns {
		if _, hasClass := h.byClass[classID]; !hasClass {
			h.byClass[classID] = make(map[*Conn]struct{})
		}
		h.byClass[classID][conn] = struct{}{}
		if _, hasSet := h.classesByConn[conn]; !hasSet {
			h.classesByConn[conn] = make(map[string]struct{})
		}
		h.classesByConn[conn][classID] = struct{}{}
	}
}

// RemoveUserFromClass 用户离班 / 被移除 / 班被删时调。
func (h *Hub) RemoveUserFromClass(userID, classID string) {
	if userID == "" || classID == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	conns, ok := h.byUser[userID]
	if !ok {
		return
	}
	for conn := range conns {
		if set, ok2 := h.byClass[classID]; ok2 {
			delete(set, conn)
			if len(set) == 0 {
				delete(h.byClass, classID)
			}
		}
		if set, ok2 := h.classesByConn[conn]; ok2 {
			delete(set, classID)
		}
	}
}

// BroadcastToClass 给所有订阅了 classID 的连接(可能跨多用户)发同一帧。
func (h *Hub) BroadcastToClass(classID string, payload any) {
	h.mu.RLock()
	conns := make([]*Conn, 0, len(h.byClass[classID]))
	for conn := range h.byClass[classID] {
		conns = append(conns, conn)
	}
	h.mu.RUnlock()
	for _, c := range conns {
		c.SendJSON(payload)
	}
}

// SendToUsers 按用户列表精确投递(例如:成员被移除,只给被移除的人 + owner)。
func (h *Hub) SendToUsers(userIDs []string, payload any) {
	if len(userIDs) == 0 {
		return
	}
	h.mu.RLock()
	seen := make(map[*Conn]struct{})
	for _, uid := range userIDs {
		for conn := range h.byUser[uid] {
			seen[conn] = struct{}{}
		}
	}
	conns := make([]*Conn, 0, len(seen))
	for c := range seen {
		conns = append(conns, c)
	}
	h.mu.RUnlock()
	for _, c := range conns {
		c.SendJSON(payload)
	}
}
