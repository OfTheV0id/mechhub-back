package class

import "github.com/gin-gonic/gin"

// Mount 把班级模块挂到 /api 下。所有路径需登录。
// ⚠️ /classes/invite/:token/* 必须在 /classes/:classId 之前注册,Gin 路由树才能
// 正确路由(否则会把 "invite" 当 classId 解析)。
func Mount(g *gin.RouterGroup, h *Handler, auth gin.HandlerFunc) {
	authed := g.Group("", auth)

	authed.GET("/classes", h.ListClasses)
	authed.POST("/classes", h.CreateClass)

	// 邀请链接公共流程(任意登录用户)—— 静态前缀 invite/* 优先于 :classId
	authed.GET("/classes/invite/:token", h.PreviewInvite)
	authed.POST("/classes/invite/:token/accept", h.AcceptInvite)

	authed.GET("/classes/:classId", h.GetClass)
	authed.PATCH("/classes/:classId", h.UpdateClass)
	authed.DELETE("/classes/:classId", h.DeleteClass)

	// owner 管理 invite
	authed.GET("/classes/:classId/invite", h.GetInvite)
	authed.POST("/classes/:classId/invite/regenerate", h.RegenerateInvite)
	authed.DELETE("/classes/:classId/invite", h.DisableInvite)

	authed.POST("/classes/:classId/avatar", h.UploadAvatar)
	authed.GET("/classes/:classId/avatar", h.GetAvatar)
	authed.DELETE("/classes/:classId/avatar", h.RemoveAvatar)

	authed.POST("/classes/:classId/leave", h.LeaveClass)
	authed.GET("/classes/:classId/members", h.ListMembers)
	authed.PATCH("/classes/:classId/members/:memberId", h.UpdateMemberRole)
	authed.DELETE("/classes/:classId/members/:memberId", h.RemoveMember)
}
