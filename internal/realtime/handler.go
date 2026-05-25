package realtime

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"mechhub-back/internal/middleware"
	"mechhub-back/internal/response"
)

// MembershipResolver 给 realtime 解耦 class 依赖。任何能"按 userID 列出其
// 所属 class_id 列表"的对象都行 —— router.New 里传 class.Repo 即可。
type MembershipResolver interface {
	ListClassIDsForUser(ctx context.Context, userID string) ([]string, error)
}

// wsUpgrader 不严格校验 Origin —— 鉴权由 session cookie 保证。生产部署如要限制
// Origin,在 cfg.CORS 里加白名单后,这里改 CheckOrigin。
var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type Handler struct {
	hub      *Hub
	resolver MembershipResolver
}

func NewHandler(hub *Hub, resolver MembershipResolver) *Handler {
	return &Handler{hub: hub, resolver: resolver}
}

// Upgrade GET /api/ws — session cookie 鉴权(已由 auth middleware 做),升级后注册到 hub。
func (h *Handler) Upgrade(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)

	classIDs, err := h.resolver.ListClassIDsForUser(c.Request.Context(), uid)
	if err != nil {
		response.Fail(c, 500, response.CodeInternal, err.Error())
		return
	}

	ws, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// upgrader 在错误时已写过 status code,直接返回
		return
	}

	conn := &Conn{
		hub:    h.hub,
		userID: uid,
		ws:     ws,
		send:   make(chan []byte, sendBufferSize),
	}
	h.hub.Register(conn, classIDs)
	conn.SendJSON(ReadyFrame{Type: FrameReady, UserID: uid, ClassIDs: classIDs})
}
