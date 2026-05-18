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
	authed.POST("/solochat/conversations/:id/messages/stop", h.StopMessage)

	authed.POST("/solochat/attachments", h.UploadAttachment)
	authed.GET("/solochat/attachments/:id", h.GetAttachment)
}
