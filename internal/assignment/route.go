package assignment

import "github.com/gin-gonic/gin"

// Mount 把作业模块挂到 /api 下。所有路径需登录。
// ⚠️ /assignments/hub 静态前缀必须在 /assignments/:assignmentId 之前注册。
func Mount(g *gin.RouterGroup, h *Handler, auth gin.HandlerFunc) {
	authed := g.Group("", auth)

	// 总览
	authed.GET("/assignments/hub", h.Hub)

	// 班级作业列表 + 创建
	authed.GET("/classes/:classId/assignments", h.ListAssignments)
	authed.POST("/classes/:classId/assignments", h.CreateAssignment)

	// 单个作业
	authed.GET("/assignments/:assignmentId", h.GetAssignment)
	authed.PATCH("/assignments/:assignmentId", h.UpdateAssignment)
	authed.DELETE("/assignments/:assignmentId", h.DeleteAssignment)

	// 教师:学生提交总览(看板)
	authed.GET("/assignments/:assignmentId/roster", h.Roster)

	// 学生:自己的提交
	authed.GET("/assignments/:assignmentId/submission", h.GetMySubmission)
	authed.PUT("/assignments/:assignmentId/submission", h.SaveSubmission)

	// 图片作答 / 媒体上传
	authed.POST("/assignments/:assignmentId/files", h.UploadFiles)

	// 教师批阅
	authed.GET("/submissions/:submissionId", h.GetGradeView)
	authed.PATCH("/submissions/:submissionId/grade", h.GradeSubmission)

	// 附件读取(owner 或班级教师)
	authed.GET("/assignment/files/:fileId", h.GetFile)
}
