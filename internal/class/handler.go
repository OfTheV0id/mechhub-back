package class

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"mechhub-back/internal/config"
	"mechhub-back/internal/middleware"
	"mechhub-back/internal/response"
)

var allowedAvatarExt = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
}

type Handler struct {
	svc *Service
	cfg *config.Config
}

func NewHandler(svc *Service, cfg *config.Config) *Handler {
	return &Handler{svc: svc, cfg: cfg}
}

// ============ 班级 CRUD ============

func (h *Handler) ListClasses(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	list, err := h.svc.ListForUser(c.Request.Context(), uid)
	if err != nil {
		response.Fail(c, 500, response.CodeInternal, err.Error())
		return
	}
	response.OK(c, gin.H{"classes": list})
}

func (h *Handler) CreateClass(c *gin.Context) {
	var req CreateClassReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	uid := c.MustGet(middleware.CtxUserID).(string)
	detail, err := h.svc.Create(c.Request.Context(), uid, req.Name, req.Description)
	if err != nil {
		h.failClassErr(c, err)
		return
	}
	response.OK(c, detail)
}

func (h *Handler) GetClass(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	detail, err := h.svc.GetForUser(c.Request.Context(), c.Param("classId"), uid)
	if err != nil {
		h.failClassErr(c, err)
		return
	}
	response.OK(c, detail)
}

func (h *Handler) UpdateClass(c *gin.Context) {
	var req UpdateClassReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	if req.Name == nil && req.Description == nil {
		response.Fail(c, 400, response.CodeBadRequest, "至少提供一个可更新字段")
		return
	}
	uid := c.MustGet(middleware.CtxUserID).(string)
	detail, err := h.svc.Update(c.Request.Context(), c.Param("classId"), uid, req)
	if err != nil {
		h.failClassErr(c, err)
		return
	}
	response.OK(c, detail)
}

func (h *Handler) DeleteClass(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	if err := h.svc.Delete(c.Request.Context(), c.Param("classId"), uid); err != nil {
		h.failClassErr(c, err)
		return
	}
	response.OK(c, gin.H{"message": "已删除"})
}

// ============ 邀请链接 ============

// GetInvite owner GET /api/classes/:classId/invite
func (h *Handler) GetInvite(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	info, err := h.svc.GetInvite(c.Request.Context(), c.Param("classId"), uid)
	if err != nil {
		h.failClassErr(c, err)
		return
	}
	response.OK(c, info)
}

// RegenerateInvite owner POST /api/classes/:classId/invite/regenerate
func (h *Handler) RegenerateInvite(c *gin.Context) {
	var req RegenerateInviteReq
	// body 可选 —— 空 body 用默认 TTL,带 expires_at 覆盖
	_ = c.ShouldBindJSON(&req)
	uid := c.MustGet(middleware.CtxUserID).(string)
	info, err := h.svc.RegenerateInvite(c.Request.Context(), c.Param("classId"), uid, req)
	if err != nil {
		h.failClassErr(c, err)
		return
	}
	response.OK(c, info)
}

// DisableInvite owner DELETE /api/classes/:classId/invite
func (h *Handler) DisableInvite(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	if err := h.svc.DisableInvite(c.Request.Context(), c.Param("classId"), uid); err != nil {
		h.failClassErr(c, err)
		return
	}
	response.OK(c, gin.H{"message": "邀请已禁用"})
}

// PreviewInvite GET /api/classes/invite/:token,任意登录用户预览
func (h *Handler) PreviewInvite(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	preview, err := h.svc.PreviewInvite(c.Request.Context(), uid, c.Param("token"))
	if err != nil {
		h.failClassErr(c, err)
		return
	}
	response.OK(c, preview)
}

// AcceptInvite POST /api/classes/invite/:token/accept,任意登录用户入班
func (h *Handler) AcceptInvite(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	detail, err := h.svc.JoinByInviteToken(c.Request.Context(), uid, c.Param("token"))
	if err != nil {
		h.failClassErr(c, err)
		return
	}
	response.OK(c, detail)
}

// ============ 头像 ============

func (h *Handler) UploadAvatar(c *gin.Context) {
	header, err := c.FormFile("avatar")
	if err != nil {
		response.Fail(c, 400, response.CodeBadRequest, "缺少头像文件")
		return
	}
	if header.Size > h.cfg.Avatar.MaxBytes {
		response.Fail(c, 413, response.CodeBadRequest, "头像文件过大")
		return
	}
	ext := strings.ToLower(filepath.Ext(header.Filename))
	contentType, ok := allowedAvatarExt[ext]
	if !ok {
		response.Fail(c, 400, response.CodeBadRequest, "不支持的文件类型,允许 png/jpg/jpeg/webp")
		return
	}
	file, err := header.Open()
	if err != nil {
		response.Fail(c, 500, response.CodeInternal, err.Error())
		return
	}
	defer file.Close()

	uid := c.MustGet(middleware.CtxUserID).(string)
	detail, err := h.svc.UploadAvatar(c.Request.Context(), c.Param("classId"), uid, file, contentType, ext)
	if err != nil {
		h.failClassErr(c, err)
		return
	}
	response.OK(c, detail)
}

func (h *Handler) GetAvatar(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	body, mime, err := h.svc.OpenAvatar(c.Request.Context(), c.Param("classId"), uid)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.Status(404)
			return
		}
		response.Fail(c, 500, response.CodeInternal, err.Error())
		return
	}
	defer body.Close()
	c.Header("Cache-Control", "public, max-age=86400")
	c.DataFromReader(200, -1, mime, body, nil)
}

func (h *Handler) RemoveAvatar(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	if err := h.svc.RemoveAvatar(c.Request.Context(), c.Param("classId"), uid); err != nil {
		h.failClassErr(c, err)
		return
	}
	response.OK(c, gin.H{"message": "已删除头像"})
}

// ============ 成员 ============

func (h *Handler) LeaveClass(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	if err := h.svc.Leave(c.Request.Context(), c.Param("classId"), uid); err != nil {
		h.failClassErr(c, err)
		return
	}
	response.OK(c, gin.H{"message": "已退出班级"})
}

func (h *Handler) ListMembers(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	members, err := h.svc.ListMembers(c.Request.Context(), c.Param("classId"), uid)
	if err != nil {
		h.failClassErr(c, err)
		return
	}
	response.OK(c, gin.H{"members": members})
}

func (h *Handler) RemoveMember(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	if err := h.svc.RemoveMember(c.Request.Context(), c.Param("classId"), uid, c.Param("userId")); err != nil {
		h.failClassErr(c, err)
		return
	}
	response.OK(c, gin.H{"message": "已移除成员"})
}

// ============ 错误翻译 ============

func (h *Handler) failClassErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		response.Fail(c, 404, response.CodeNotFound, "班级不存在或您无权访问")
	case errors.Is(err, ErrForbidden):
		response.Fail(c, 403, response.CodeForbidden, "无操作权限")
	case errors.Is(err, ErrNotTeacher):
		response.Fail(c, 403, response.CodeForbidden, "仅教师可创建班级")
	case errors.Is(err, ErrAlreadyJoined):
		response.Fail(c, 409, response.CodeBadRequest, "你已加入该班级")
	case errors.Is(err, ErrInviteExpired):
		response.Fail(c, 410, response.CodeBadRequest, "邀请链接已过期")
	case errors.Is(err, ErrInviteDisabled):
		response.Fail(c, 410, response.CodeBadRequest, "邀请链接已失效")
	case errors.Is(err, ErrOwnerCannotLeave):
		response.Fail(c, 400, response.CodeBadRequest, "班级所有者不能退出/被移除,请改用删除班级")
	default:
		response.Fail(c, 500, response.CodeInternal, err.Error())
	}
}
