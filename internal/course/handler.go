package course

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

// failErr 把 service 业务错误翻成统一响应。
func (h *Handler) failErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		response.Fail(c, 404, response.CodeNotFound, "资源不存在")
	case errors.Is(err, ErrNotTeacher):
		response.Fail(c, 403, response.CodeForbidden, "只有教师可以创建课程")
	case errors.Is(err, ErrForbidden):
		response.Fail(c, 403, response.CodeForbidden, "无权操作")
	case errors.Is(err, ErrParentNotSection):
		response.Fail(c, 400, response.CodeBadRequest, "只有「章节」能包含子节点")
	case errors.Is(err, ErrInvalidState):
		response.Fail(c, 400, response.CodeBadRequest, "当前操作不合法")
	case errors.Is(err, ErrFileTooLarge):
		response.Fail(c, 413, response.CodeBadRequest, "文件过大")
	case errors.Is(err, ErrFileTypeNotAllowed):
		response.Fail(c, 400, response.CodeBadRequest, "不支持的文件类型")
	default:
		response.Fail(c, 500, response.CodeInternal, err.Error())
	}
}

func uid(c *gin.Context) string {
	return c.MustGet(middleware.CtxUserID).(string)
}

// ---- Course ----

func (h *Handler) ListCourses(c *gin.Context) {
	list, err := h.svc.ListPublished(c.Request.Context(), uid(c))
	if err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, gin.H{"courses": list})
}

func (h *Handler) ListMyCourses(c *gin.Context) {
	list, err := h.svc.ListMyCourses(c.Request.Context(), uid(c))
	if err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, gin.H{"courses": list})
}

func (h *Handler) GetCourse(c *gin.Context) {
	dto, err := h.svc.GetCourseDetail(c.Request.Context(), uid(c), c.Param("id"))
	if err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, dto)
}

func (h *Handler) CreateCourse(c *gin.Context) {
	var req CreateCourseReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	dto, err := h.svc.CreateCourse(c.Request.Context(), uid(c), req)
	if err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, dto)
}

func (h *Handler) UpdateCourse(c *gin.Context) {
	var req UpdateCourseReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	dto, err := h.svc.UpdateCourse(c.Request.Context(), uid(c), c.Param("id"), req)
	if err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, dto)
}

func (h *Handler) DeleteCourse(c *gin.Context) {
	if err := h.svc.DeleteCourse(c.Request.Context(), uid(c), c.Param("id")); err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, gin.H{"message": "已删除"})
}

// ---- Node ----

func (h *Handler) GetNode(c *gin.Context) {
	dto, err := h.svc.GetNode(c.Request.Context(), uid(c), c.Param("id"))
	if err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, dto)
}

func (h *Handler) CreateNode(c *gin.Context) {
	var req CreateNodeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	dto, err := h.svc.CreateNode(c.Request.Context(), uid(c), c.Param("id"), req)
	if err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, dto)
}

func (h *Handler) UpdateNode(c *gin.Context) {
	var req UpdateNodeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	dto, err := h.svc.UpdateNode(c.Request.Context(), uid(c), c.Param("id"), req)
	if err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, dto)
}

func (h *Handler) DeleteNode(c *gin.Context) {
	if err := h.svc.DeleteNode(c.Request.Context(), uid(c), c.Param("id")); err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, gin.H{"message": "已删除"})
}

func (h *Handler) MoveNode(c *gin.Context) {
	var req MoveNodeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	if err := h.svc.MoveNode(c.Request.Context(), uid(c), c.Param("id"), req); err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, gin.H{"message": "已更新"})
}

// ---- Progress ----

func (h *Handler) AssessNode(c *gin.Context) {
	var req AssessNodeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	dto, err := h.svc.AssessNode(c.Request.Context(), uid(c), c.Param("id"), req)
	if err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, dto)
}

func (h *Handler) AssessStep(c *gin.Context) {
	var req AssessNodeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	dto, err := h.svc.AssessStep(c.Request.Context(), uid(c), c.Param("id"), c.Param("stepId"), req)
	if err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, dto)
}

func (h *Handler) FBDSolution(c *gin.Context) {
	dto, err := h.svc.FBDSolution(c.Request.Context(), uid(c), c.Param("id"))
	if err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, dto)
}

func (h *Handler) GetCourseProgress(c *gin.Context) {
	dto, err := h.svc.GetCourseProgress(c.Request.Context(), uid(c), c.Param("id"))
	if err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, dto)
}

// ---- Annotation ----

func (h *Handler) ListAnnotations(c *gin.Context) {
	list, err := h.svc.ListAnnotations(c.Request.Context(), uid(c), c.Param("id"))
	if err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, gin.H{"annotations": list})
}

func (h *Handler) CreateAnnotation(c *gin.Context) {
	var req CreateAnnotationReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	dto, err := h.svc.CreateAnnotation(c.Request.Context(), uid(c), c.Param("id"), req)
	if err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, dto)
}

func (h *Handler) UpdateAnnotation(c *gin.Context) {
	var req UpdateAnnotationReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	dto, err := h.svc.UpdateAnnotation(c.Request.Context(), uid(c), c.Param("id"), req)
	if err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, dto)
}

func (h *Handler) DeleteAnnotation(c *gin.Context) {
	if err := h.svc.DeleteAnnotation(c.Request.Context(), uid(c), c.Param("id")); err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, gin.H{"message": "已删除"})
}

// ---- 媒体 ----

func (h *Handler) UploadMedia(c *gin.Context) {
	form, err := c.MultipartForm()
	if err != nil {
		response.Fail(c, 400, response.CodeBadRequest, "invalid multipart form")
		return
	}
	files := form.File["files"]
	if len(files) == 0 {
		response.Fail(c, 400, response.CodeBadRequest, "missing files")
		return
	}
	out, err := h.svc.UploadMedia(c.Request.Context(), uid(c), files)
	if err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, gin.H{"media": out})
}

func (h *Handler) GetMedia(c *gin.Context) {
	f, body, err := h.svc.OpenMedia(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.failErr(c, err)
		return
	}
	defer body.Close()
	c.Header("Cache-Control", "public, max-age=86400")
	c.DataFromReader(200, f.Size, f.MimeType, body, nil)
}
