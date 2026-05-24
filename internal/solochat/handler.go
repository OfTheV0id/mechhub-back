package solochat

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

func (h *Handler) ListConversations(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
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
	// 不接受任何 body 字段:创建恒为"新对话",首次 AI 回复后由 LLM 总结标题。
	// 重命名走 PUT /conversations/:id。
	uid := c.MustGet(middleware.CtxUserID).(string)
	conv, err := h.svc.CreateConversation(c.Request.Context(), uid)
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
	id := c.Param("id")
	uid := c.MustGet(middleware.CtxUserID).(string)
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
	id := c.Param("id")
	uid := c.MustGet(middleware.CtxUserID).(string)
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
	id := c.Param("id")
	uid := c.MustGet(middleware.CtxUserID).(string)
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
	id := c.Param("id")
	var req SendMessageReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	uid := c.MustGet(middleware.CtxUserID).(string)
	h.svc.SendMessageStream(c, id, uid, req.Content, req.Attachments, req.Grading)
}

// StopMessage 取消当前用户在该对话内正在跑的 stream(如果有)。
// 幂等:没有 in-flight stream 也返 200。
// 触发链路:cancel ctx → runner.Run 退出 iter.Seq2 → Gemini API 收到 abort
// → StreamChat 发 message_done{finish_reason:"cancelled"} → SSE 流自然结束。
func (h *Handler) StopMessage(c *gin.Context) {
	id := c.Param("id")
	uid := c.MustGet(middleware.CtxUserID).(string)
	h.svc.StopStream(uid, id)
	response.OK(c, gin.H{"message": "已停止"})
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
	uid := c.MustGet(middleware.CtxUserID).(string)
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
	id := c.Param("id")
	uid := c.MustGet(middleware.CtxUserID).(string)
	f, body, err := h.svc.OpenAttachment(c.Request.Context(), id, uid)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Fail(c, 404, response.CodeNotFound, "附件不存在")
			return
		}
		response.Fail(c, 500, response.CodeInternal, err.Error())
		return
	}
	defer body.Close()
	c.Header("Cache-Control", "private, max-age=86400")
	c.DataFromReader(200, f.Size, f.MimeType, body, map[string]string{
		"Content-Disposition": `inline; filename="` + f.OriginalName + `"`,
	})
}
