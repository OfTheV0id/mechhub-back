package play

import "github.com/gin-gonic/gin"

func Mount(g *gin.RouterGroup, h *Handler, auth gin.HandlerFunc) {
	authed := g.Group("", auth)

	authed.GET("/play/scenarios", h.ListScenarios)
	authed.POST("/play/scenarios", h.CreateScenario)
	authed.GET("/play/scenarios/:id", h.GetScenario)
	authed.PATCH("/play/scenarios/:id", h.UpdateScenario)
	authed.DELETE("/play/scenarios/:id", h.DeleteScenario)
}
