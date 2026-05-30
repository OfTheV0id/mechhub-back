package channel

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"

	"mechhub-back/internal/class"
	"mechhub-back/internal/middleware"
	"mechhub-back/internal/response"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// ============ 频道 CRUD ============

func (h *Handler) ListChannels(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	list, err := h.svc.ListChannels(c.Request.Context(), c.Param("classId"), uid)
	if err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, gin.H{"channels": list})
}

func (h *Handler) GetChannel(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	dto, err := h.svc.GetChannel(c.Request.Context(), c.Param("classId"), c.Param("channelId"), uid)
	if err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, dto)
}

func (h *Handler) CreateChannel(c *gin.Context) {
	var req CreateChannelReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	uid := c.MustGet(middleware.CtxUserID).(string)
	dto, err := h.svc.CreateChannel(c.Request.Context(), c.Param("classId"), uid, req)
	if err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, dto)
}

func (h *Handler) UpdateChannel(c *gin.Context) {
	var req UpdateChannelReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	uid := c.MustGet(middleware.CtxUserID).(string)
	dto, err := h.svc.UpdateChannel(c.Request.Context(), c.Param("classId"), c.Param("channelId"), uid, req)
	if err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, dto)
}

func (h *Handler) DeleteChannel(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	if err := h.svc.DeleteChannel(c.Request.Context(), c.Param("classId"), c.Param("channelId"), uid); err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, gin.H{"message": "已删除"})
}

// ============ 消息 ============

func (h *Handler) ListMessages(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	before := c.Query("before")
	limit := 0
	if s := c.Query("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			limit = n
		}
	}
	out, err := h.svc.ListMessages(c.Request.Context(), c.Param("channelId"), uid, before, limit)
	if err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, gin.H{"messages": out})
}

func (h *Handler) SendMessage(c *gin.Context) {
	var req SendMessageReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	uid := c.MustGet(middleware.CtxUserID).(string)
	var (
		dto *MessageDTO
		err error
	)
	if req.Share != nil {
		dto, err = h.svc.SendShareMessage(c.Request.Context(), c.Param("channelId"), uid, req)
	} else {
		dto, err = h.svc.SendMessage(c.Request.Context(), c.Param("channelId"), uid, req)
	}
	if err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, dto)
}

func (h *Handler) ForkMessage(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	dto, err := h.svc.ForkMessageToSolochat(c.Request.Context(), c.Param("channelId"), c.Param("messageId"), uid)
	if err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, dto)
}

func (h *Handler) EditMessage(c *gin.Context) {
	var req EditMessageReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	uid := c.MustGet(middleware.CtxUserID).(string)
	dto, err := h.svc.EditMessage(c.Request.Context(), c.Param("channelId"), c.Param("messageId"), uid, req.Content)
	if err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, dto)
}

func (h *Handler) DeleteMessage(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	if err := h.svc.DeleteMessage(c.Request.Context(), c.Param("channelId"), c.Param("messageId"), uid); err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, gin.H{"message": "已删除"})
}

func (h *Handler) ToggleReaction(c *gin.Context) {
	var req ToggleReactionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	uid := c.MustGet(middleware.CtxUserID).(string)
	dto, err := h.svc.ToggleReaction(c.Request.Context(), c.Param("channelId"), c.Param("messageId"), uid, req.Emoji)
	if err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, dto)
}

// ============ 附件 ============

func (h *Handler) UploadAttachments(c *gin.Context) {
	form, err := c.MultipartForm()
	if err != nil {
		response.Fail(c, 400, response.CodeBadRequest, "无效的 multipart form")
		return
	}
	files := form.File["files"]
	if len(files) == 0 {
		response.Fail(c, 400, response.CodeBadRequest, "缺少文件字段 files")
		return
	}
	uid := c.MustGet(middleware.CtxUserID).(string)
	out, err := h.svc.UploadAttachments(c.Request.Context(), c.Param("channelId"), uid, files)
	if err != nil {
		h.failErr(c, err)
		return
	}
	response.OK(c, gin.H{"attachments": out})
}

func (h *Handler) GetAttachment(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	a, body, err := h.svc.OpenAttachment(c.Request.Context(), c.Param("channelId"), c.Param("fileId"), uid)
	if err != nil {
		h.failErr(c, err)
		return
	}
	defer body.Close()
	c.Header("Cache-Control", "private, max-age=86400")
	c.DataFromReader(200, a.SizeBytes, a.MimeType, body, map[string]string{
		"Content-Disposition": `inline; filename="` + a.OriginalName + `"`,
	})
}

// ============ 错误翻译 ============

func (h *Handler) failErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, class.ErrNotFound):
		response.Fail(c, 404, response.CodeNotFound, "资源不存在或你不是班级成员")
	case errors.Is(err, ErrForbidden), errors.Is(err, class.ErrForbidden):
		response.Fail(c, 403, response.CodeForbidden, "无操作权限")
	case errors.Is(err, ErrDefaultChannelLocked):
		response.Fail(c, 400, response.CodeBadRequest, "默认频道不可删除或改名")
	case errors.Is(err, ErrTooManyAttachments):
		response.Fail(c, 400, response.CodeBadRequest, "附件数量超过上限")
	case errors.Is(err, ErrAttachmentTooLarge):
		response.Fail(c, 413, response.CodeBadRequest, "附件文件过大")
	case errors.Is(err, ErrAttachmentInvalid):
		response.Fail(c, 400, response.CodeBadRequest, "附件无效或权限不符")
	case errors.Is(err, ErrReactionInvalid):
		response.Fail(c, 400, response.CodeBadRequest, "表情无效")
	case errors.Is(err, ErrShareSourceInvalid):
		response.Fail(c, 400, response.CodeBadRequest, "分享来源无效或图片已不存在")
	default:
		response.Fail(c, 500, response.CodeInternal, err.Error())
	}
}
