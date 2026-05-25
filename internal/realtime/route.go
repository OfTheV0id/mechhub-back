package realtime

import "github.com/gin-gonic/gin"

// Mount 挂 /api/ws,需登录。
func Mount(g *gin.RouterGroup, h *Handler, auth gin.HandlerFunc) {
	g.GET("/ws", auth, h.Upgrade)
}
