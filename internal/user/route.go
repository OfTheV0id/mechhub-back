package user

import "github.com/gin-gonic/gin"

func Mount(g *gin.RouterGroup, h *Handler, auth gin.HandlerFunc) {
	g.POST("/auth/register", h.Register)
	g.GET("/auth/verify", h.VerifyEmail)
	g.POST("/auth/login", h.Login)
	g.POST("/auth/logout", h.Logout)
	g.POST("/auth/forgot-password", h.ForgotPassword)
	g.POST("/auth/reset-password", h.ResetPassword)

	authed := g.Group("", auth)
	authed.GET("/user/me", h.Me)
	authed.POST("/user/update-profile", h.UpdateProfile)
	authed.POST("/user/avatar", h.UploadAvatar)
	authed.POST("/user/change-password", h.ChangePassword)
}
