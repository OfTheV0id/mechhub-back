package play

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

func (h *Handler) ListScenarios(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(string)
	list, err := h.svc.ListScenarios(c.Request.Context(), uid)
	if err != nil {
		response.Fail(c, 500, response.CodeInternal, err.Error())
		return
	}
	out := make([]ScenarioDTO, len(list))
	for i := range list {
		out[i] = toScenarioDTO(&list[i])
	}
	response.OK(c, gin.H{"scenarios": out})
}

func (h *Handler) GetScenario(c *gin.Context) {
	id := c.Param("id")
	uid := c.MustGet(middleware.CtxUserID).(string)
	sc, err := h.svc.GetScenario(c.Request.Context(), id, uid)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Fail(c, 404, response.CodeNotFound, "场景不存在")
			return
		}
		response.Fail(c, 500, response.CodeInternal, err.Error())
		return
	}
	response.OK(c, toScenarioDTO(sc))
}

func (h *Handler) CreateScenario(c *gin.Context) {
	var req CreateScenarioReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	uid := c.MustGet(middleware.CtxUserID).(string)
	sc, err := h.svc.CreateScenario(c.Request.Context(), uid, req.Name, string(req.Structure))
	if err != nil {
		response.Fail(c, 500, response.CodeInternal, err.Error())
		return
	}
	response.OK(c, toScenarioDTO(sc))
}

func (h *Handler) UpdateScenario(c *gin.Context) {
	var req UpdateScenarioReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	id := c.Param("id")
	uid := c.MustGet(middleware.CtxUserID).(string)
	sc, err := h.svc.UpdateScenario(c.Request.Context(), id, uid, req.Name, string(req.Structure))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Fail(c, 404, response.CodeNotFound, "场景不存在")
			return
		}
		response.Fail(c, 500, response.CodeInternal, err.Error())
		return
	}
	response.OK(c, toScenarioDTO(sc))
}

func (h *Handler) DeleteScenario(c *gin.Context) {
	id := c.Param("id")
	uid := c.MustGet(middleware.CtxUserID).(string)
	if err := h.svc.DeleteScenario(c.Request.Context(), id, uid); err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Fail(c, 404, response.CodeNotFound, "场景不存在")
			return
		}
		response.Fail(c, 500, response.CodeInternal, err.Error())
		return
	}
	response.OK(c, gin.H{"message": "已删除"})
}
