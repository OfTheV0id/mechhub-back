package solochat

import (
	"errors"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"

	"mechhub-back/internal/middleware"
	"mechhub-back/internal/response"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) ListConversations(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(bson.ObjectID)
	list, err := h.svc.ListConversations(c.Request.Context(), uid)
	if err != nil {
		response.Fail(c, 500, response.CodeInternal, err.Error())
		return
	}
	out := make([]ConversationDTO, len(list))
	for i := range list {
		out[i] = toConversationDTO(&list[i])
	}
	response.OK(c, gin.H{"conversations": out})
}

func (h *Handler) CreateConversation(c *gin.Context) {
	var req CreateConversationReq
	_ = c.ShouldBindJSON(&req)
	uid := c.MustGet(middleware.CtxUserID).(bson.ObjectID)
	conv, err := h.svc.CreateConversation(c.Request.Context(), uid, req.Title)
	if err != nil {
		response.Fail(c, 500, response.CodeInternal, err.Error())
		return
	}
	response.OK(c, toConversationDTO(conv))
}

func (h *Handler) UpdateConversation(c *gin.Context) {
	var req UpdateConversationReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	id, err := bson.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		response.Fail(c, 400, response.CodeBadRequest, "invalid id")
		return
	}
	uid := c.MustGet(middleware.CtxUserID).(bson.ObjectID)
	conv, err := h.svc.UpdateConversation(c.Request.Context(), id, uid, req.Title)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Fail(c, 404, response.CodeNotFound, "对话不存在")
			return
		}
		response.Fail(c, 500, response.CodeInternal, err.Error())
		return
	}
	response.OK(c, toConversationDTO(conv))
}

func (h *Handler) DeleteConversation(c *gin.Context) {
	id, err := bson.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		response.Fail(c, 400, response.CodeBadRequest, "invalid id")
		return
	}
	uid := c.MustGet(middleware.CtxUserID).(bson.ObjectID)
	if err := h.svc.DeleteConversation(c.Request.Context(), id, uid); err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Fail(c, 404, response.CodeNotFound, "对话不存在")
			return
		}
		response.Fail(c, 500, response.CodeInternal, err.Error())
		return
	}
	response.OK(c, gin.H{"message": "已删除"})
}

func (h *Handler) ListMessages(c *gin.Context) {
	id, err := bson.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		response.Fail(c, 400, response.CodeBadRequest, "invalid id")
		return
	}
	uid := c.MustGet(middleware.CtxUserID).(bson.ObjectID)
	dtos, err := h.svc.ListMessages(c.Request.Context(), id, uid)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Fail(c, 404, response.CodeNotFound, "对话不存在")
			return
		}
		response.Fail(c, 500, response.CodeInternal, err.Error())
		return
	}
	response.OK(c, gin.H{"messages": dtos})
}

func (h *Handler) SendMessageStream(c *gin.Context) {
	id, err := bson.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		response.Fail(c, 400, response.CodeBadRequest, "invalid id")
		return
	}
	var req SendMessageReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	attachmentIDs, err := parseObjectIDs(req.Attachments)
	if err != nil {
		response.Fail(c, 400, response.CodeBadRequest, "invalid attachment id")
		return
	}
	uid := c.MustGet(middleware.CtxUserID).(bson.ObjectID)
	h.svc.SendMessageStream(c, id, uid, req.Content, attachmentIDs)
}

func (h *Handler) UploadAttachment(c *gin.Context) {
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
	uid := c.MustGet(middleware.CtxUserID).(bson.ObjectID)
	uploaded, err := h.svc.UploadAttachments(c.Request.Context(), uid, files)
	if err != nil {
		switch {
		case errors.Is(err, ErrFileTooLarge):
			response.Fail(c, 413, response.CodeBadRequest, "文件过大")
		case errors.Is(err, ErrFileTypeNotAllowed):
			response.Fail(c, 400, response.CodeBadRequest, "不支持的文件类型")
		case errors.Is(err, ErrTooManyAttachments):
			response.Fail(c, 400, response.CodeBadRequest, "附件数量超出限制")
		default:
			response.Fail(c, 500, response.CodeInternal, err.Error())
		}
		return
	}
	out := make([]AttachmentDTO, len(uploaded))
	for i := range uploaded {
		out[i] = h.svc.ToAttachmentDTO(&uploaded[i])
	}
	response.OK(c, gin.H{"attachments": out})
}

func (h *Handler) GetAttachment(c *gin.Context) {
	id, err := bson.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		response.Fail(c, 400, response.CodeBadRequest, "invalid id")
		return
	}
	uid := c.MustGet(middleware.CtxUserID).(bson.ObjectID)
	f, err := h.svc.GetAttachment(c.Request.Context(), id, uid)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Fail(c, 404, response.CodeNotFound, "附件不存在")
			return
		}
		response.Fail(c, 500, response.CodeInternal, err.Error())
		return
	}
	c.Redirect(302, h.svc.AttachmentURL(f.OSSKey))
}

func parseObjectIDs(ss []string) ([]bson.ObjectID, error) {
	if len(ss) == 0 {
		return nil, nil
	}
	out := make([]bson.ObjectID, len(ss))
	for i, s := range ss {
		id, err := bson.ObjectIDFromHex(s)
		if err != nil {
			return nil, err
		}
		out[i] = id
	}
	return out, nil
}
