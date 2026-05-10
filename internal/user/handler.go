package user

import (
	"errors"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"

	"mechhub-back/internal/config"
	"mechhub-back/internal/middleware"
	"mechhub-back/internal/response"
)

type Handler struct {
	svc *Service
	cfg *config.Config
}

func NewHandler(svc *Service, cfg *config.Config) *Handler {
	return &Handler{svc: svc, cfg: cfg}
}

func (h *Handler) Register(c *gin.Context) {
	var req RegisterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	switch err := h.svc.Register(c.Request.Context(), req.Email, req.Password); {
	case err == nil:
		response.OK(c, gin.H{"message": "verification email sent"})
	case errors.Is(err, ErrEmailExists):
		response.Fail(c, 409, response.CodeEmailExists, "email already registered")
	default:
		response.Fail(c, 500, response.CodeInternal, err.Error())
	}
}

func (h *Handler) VerifyEmail(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		response.Fail(c, 400, response.CodeBadRequest, "token required")
		return
	}
	switch err := h.svc.VerifyEmail(c.Request.Context(), token); {
	case err == nil:
		response.OK(c, gin.H{"verified": true})
	case errors.Is(err, ErrTokenInvalid):
		response.Fail(c, 400, response.CodeTokenInvalid, "token invalid or expired")
	default:
		response.Fail(c, 500, response.CodeInternal, err.Error())
	}
}

func (h *Handler) Login(c *gin.Context) {
	var req LoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	sess, err := h.svc.Login(c.Request.Context(), req.Email, req.Password)
	switch {
	case err == nil:
		h.setSessionCookie(c, sess.ID, int(h.cfg.Session.TTL.Seconds()))
		response.OK(c, gin.H{"message": "logged in"})
	case errors.Is(err, ErrInvalidCredentials):
		response.Fail(c, 401, response.CodeInvalidCredentials, "invalid email or password")
	case errors.Is(err, ErrEmailNotVerified):
		response.Fail(c, 403, response.CodeEmailNotVerified, "email not verified")
	default:
		response.Fail(c, 500, response.CodeInternal, err.Error())
	}
}

func (h *Handler) Logout(c *gin.Context) {
	if sid, err := c.Cookie(h.cfg.Session.CookieName); err == nil {
		_ = h.svc.Logout(c.Request.Context(), sid)
	}
	h.setSessionCookie(c, "", -1)
	response.OK(c, gin.H{"message": "logged out"})
}

func (h *Handler) ForgotPassword(c *gin.Context) {
	var req ForgotPasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	if err := h.svc.ForgotPassword(c.Request.Context(), req.Email); err != nil {
		response.Fail(c, 500, response.CodeInternal, err.Error())
		return
	}
	response.OK(c, gin.H{"message": "if the email is registered, a reset link has been sent"})
}

func (h *Handler) ResetPassword(c *gin.Context) {
	var req ResetPasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	switch err := h.svc.ResetPassword(c.Request.Context(), req.Token, req.Password); {
	case err == nil:
		response.OK(c, gin.H{"message": "password updated"})
	case errors.Is(err, ErrTokenInvalid):
		response.Fail(c, 400, response.CodeTokenInvalid, "token invalid or expired")
	default:
		response.Fail(c, 500, response.CodeInternal, err.Error())
	}
}

func (h *Handler) ChangePassword(c *gin.Context) {
	var req ChangePasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	uid := c.MustGet(middleware.CtxUserID).(bson.ObjectID)
	switch err := h.svc.ChangePassword(c.Request.Context(), uid, req.OldPassword, req.NewPassword); {
	case err == nil:
		h.setSessionCookie(c, "", -1)
		response.OK(c, gin.H{"message": "password updated, please log in again"})
	case errors.Is(err, ErrPasswordWrong):
		response.Fail(c, 400, response.CodePasswordWrong, "current password is wrong")
	default:
		response.Fail(c, 500, response.CodeInternal, err.Error())
	}
}

func (h *Handler) Me(c *gin.Context) {
	uid := c.MustGet(middleware.CtxUserID).(bson.ObjectID)
	u, err := h.svc.Me(c.Request.Context(), uid)
	if err != nil {
		response.Fail(c, 500, response.CodeInternal, err.Error())
		return
	}
	response.OK(c, MeResp{ID: u.ID.Hex(), Email: u.Email, Verified: u.Verified})
}

func (h *Handler) setSessionCookie(c *gin.Context, value string, maxAge int) {
	c.SetSameSite(h.cfg.Session.CookieSameSite)
	c.SetCookie(
		h.cfg.Session.CookieName,
		value,
		maxAge,
		"/",
		"",
		h.cfg.Session.CookieSecure,
		true,
	)
}
