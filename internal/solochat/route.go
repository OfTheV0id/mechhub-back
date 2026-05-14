package solochat

import "github.com/gin-gonic/gin"

func Mount(g *gin.RouterGroup, h *Handler, auth gin.HandlerFunc) {
	authed := g.Group("", auth)

	authed.GET("/solochat/conversations", h.ListConversations)
	authed.POST("/solochat/conversations", h.CreateConversation)
	authed.PATCH("/solochat/conversations/:id", h.UpdateConversation)
	authed.DELETE("/solochat/conversations/:id", h.DeleteConversation)

	authed.GET("/solochat/conversations/:id/messages", h.ListMessages)
	authed.POST("/solochat/conversations/:id/messages/stream", h.SendMessageStream)

	authed.GET("/solochat/conversations/:id/grading-tasks", h.ListGradingTasks)
	authed.POST("/solochat/conversations/:id/grading-tasks", h.CreateGradingTaskStream)
	authed.GET("/solochat/grading-tasks/:id", h.GetGradingTask)
	authed.POST("/solochat/grading-tasks/:id/retry", h.RetryGradingTask)
	authed.GET("/solochat/grading-tasks/:id/events", h.SubscribeGradingEvents)

	authed.POST("/solochat/attachments", h.UploadAttachment)
	authed.GET("/solochat/attachments/:id", h.GetAttachment)
}
