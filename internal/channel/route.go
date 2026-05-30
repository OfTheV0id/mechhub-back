package channel

import "github.com/gin-gonic/gin"

// Mount 挂频道相关路由。所有端点需登录。
// ⚠️ /classes/:classId/channels/* 用 classId 鉴权;/channels/:channelId/* 用
// 频道反查所属班级再鉴权(走 service 内部),路径设计上是平的。
func Mount(g *gin.RouterGroup, h *Handler, auth gin.HandlerFunc) {
	authed := g.Group("", auth)

	// 频道在班级下:挂 /api/classes/:classId/channels/*
	authed.GET("/classes/:classId/channels", h.ListChannels)
	authed.POST("/classes/:classId/channels", h.CreateChannel)
	authed.GET("/classes/:classId/channels/:channelId", h.GetChannel)
	authed.PATCH("/classes/:classId/channels/:channelId", h.UpdateChannel)
	authed.DELETE("/classes/:classId/channels/:channelId", h.DeleteChannel)

	// 消息与附件直接在频道下:更简洁,不依赖 classId 二次定位
	authed.GET("/channels/:channelId/messages", h.ListMessages)
	authed.POST("/channels/:channelId/messages", h.SendMessage)
	authed.POST("/channels/:channelId/messages/:messageId/fork", h.ForkMessage)
	authed.PATCH("/channels/:channelId/messages/:messageId", h.EditMessage)
	authed.DELETE("/channels/:channelId/messages/:messageId", h.DeleteMessage)
	authed.POST("/channels/:channelId/messages/:messageId/reactions", h.ToggleReaction)

	authed.POST("/channels/:channelId/attachments", h.UploadAttachments)
	authed.GET("/channels/:channelId/attachments/:fileId", h.GetAttachment)
}
