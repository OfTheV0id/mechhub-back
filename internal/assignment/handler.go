package assignment

import (
	"errors"

	"github.com/gin-gonic/gin"

	"mechhub-back/internal/middleware"
	"mechhub-back/internal/response"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// ============ 总览 ============

func (h *Handler) Hub(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	hub, err := h.svc.Hub(c.Request.Context(), uid)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.OK(c, hub)
}

// ============ 作业 ============

func (h *Handler) ListAssignments(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	list, err := h.svc.ListByClass(c.Request.Context(), c.Param("classId"), uid)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.OK(c, gin.H{"assignments": list})
}

func (h *Handler) CreateAssignment(c *gin.Context) {
	var req CreateAssignmentReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	uid := c.MustGet(middleware.CtxUserID).(string)
	dto, err := h.svc.Create(c.Request.Context(), c.Param("classId"), uid, req)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.OK(c, dto)
}

func (h *Handler) GetAssignment(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	detail, err := h.svc.GetDetail(c.Request.Context(), c.Param("assignmentId"), uid)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.OK(c, detail)
}

func (h *Handler) UpdateAssignment(c *gin.Context) {
	var req UpdateAssignmentReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	uid := c.MustGet(middleware.CtxUserID).(string)
	dto, err := h.svc.Update(c.Request.Context(), c.Param("assignmentId"), uid, req)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.OK(c, dto)
}

func (h *Handler) DeleteAssignment(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	if err := h.svc.Delete(c.Request.Context(), c.Param("assignmentId"), uid); err != nil {
		h.fail(c, err)
		return
	}
	response.OK(c, gin.H{"message": "已删除"})
}

func (h *Handler) Roster(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	roster, err := h.svc.Roster(c.Request.Context(), c.Param("assignmentId"), uid)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.OK(c, gin.H{"roster": roster})
}

// ============ 学生提交 ============

func (h *Handler) GetMySubmission(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	sub, err := h.svc.GetMySubmission(c.Request.Context(), c.Param("assignmentId"), uid)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.OK(c, sub)
}

func (h *Handler) SaveSubmission(c *gin.Context) {
	var req SaveSubmissionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	uid := c.MustGet(middleware.CtxUserID).(string)
	sub, err := h.svc.SaveSubmission(c.Request.Context(), c.Param("assignmentId"), uid, req)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.OK(c, sub)
}

// ============ 教师批阅 ============

func (h *Handler) GetGradeView(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	view, err := h.svc.GetGradeView(c.Request.Context(), c.Param("submissionId"), uid)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.OK(c, view)
}

func (h *Handler) GradeSubmission(c *gin.Context) {
	var req GradeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	uid := c.MustGet(middleware.CtxUserID).(string)
	sub, err := h.svc.Grade(c.Request.Context(), c.Param("submissionId"), uid, req)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.OK(c, sub)
}

// ============ 附件 ============

func (h *Handler) UploadFiles(c *gin.Context) {
	form, err := c.MultipartForm()
	if err != nil {
		response.Fail(c, 400, response.CodeBadRequest, "无效的表单")
		return
	}
	files := form.File["files"]
	if len(files) == 0 {
		response.Fail(c, 400, response.CodeBadRequest, "缺少文件")
		return
	}
	uid := c.MustGet(middleware.CtxUserID).(string)
	out, err := h.svc.UploadFiles(c.Request.Context(), c.Param("classId"), uid, files)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.OK(c, gin.H{"files": out})
}

func (h *Handler) GetFile(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	f, body, err := h.svc.OpenFile(c.Request.Context(), c.Param("fileId"), uid)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.Status(404)
			return
		}
		h.fail(c, err)
		return
	}
	defer body.Close()
	c.Header("Content-Disposition", "inline")
	c.DataFromReader(200, f.Size, f.MimeType, body, nil)
}

// ============ 错误翻译 ============

func (h *Handler) fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		response.Fail(c, 404, response.CodeNotFound, "作业不存在或您无权访问")
	case errors.Is(err, ErrForbidden):
		response.Fail(c, 403, response.CodeForbidden, "无操作权限")
	case errors.Is(err, ErrNotTeacher):
		response.Fail(c, 403, response.CodeForbidden, "仅教师可执行该操作")
	case errors.Is(err, ErrClosed):
		response.Fail(c, 400, response.CodeBadRequest, "作业已截止,无法提交")
	case errors.Is(err, ErrAlreadySubmitted):
		response.Fail(c, 409, response.CodeBadRequest, "作业已提交,不能再修改")
	case errors.Is(err, ErrBadInput):
		response.Fail(c, 400, response.CodeBadRequest, "请求参数有误")
	default:
		response.Fail(c, 500, response.CodeInternal, err.Error())
	}
}
