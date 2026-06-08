package course

import "github.com/gin-gonic/gin"

func Mount(g *gin.RouterGroup, h *Handler, auth gin.HandlerFunc) {
	authed := g.Group("", auth)

	// 课程
	authed.GET("/course/courses", h.ListCourses)
	authed.GET("/course/mine", h.ListMyCourses)
	authed.POST("/course/courses", h.CreateCourse)
	authed.GET("/course/courses/:id", h.GetCourse)
	authed.PATCH("/course/courses/:id", h.UpdateCourse)
	authed.DELETE("/course/courses/:id", h.DeleteCourse)
	authed.GET("/course/courses/:id/progress", h.GetCourseProgress)

	// 章节节点
	authed.POST("/course/courses/:id/nodes", h.CreateNode)
	authed.POST("/course/courses/:id/nodes/move", h.MoveNode)
	authed.GET("/course/nodes/:id", h.GetNode)
	authed.PATCH("/course/nodes/:id", h.UpdateNode)
	authed.DELETE("/course/nodes/:id", h.DeleteNode)
	authed.POST("/course/nodes/:id/assess", h.AssessNode)
	authed.GET("/course/nodes/:id/fbd/solution", h.FBDSolution)

	// 批注
	authed.GET("/course/nodes/:id/annotations", h.ListAnnotations)
	authed.POST("/course/nodes/:id/annotations", h.CreateAnnotation)
	authed.PATCH("/course/annotations/:id", h.UpdateAnnotation)
	authed.DELETE("/course/annotations/:id", h.DeleteAnnotation)

	// 媒体
	authed.POST("/course/attachments", h.UploadMedia)
	authed.GET("/course/attachments/:id", h.GetMedia)
}
