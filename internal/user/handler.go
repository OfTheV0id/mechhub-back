package user

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"

	"mechhub-back/internal/config"
	"mechhub-back/internal/middleware"
	"mechhub-back/internal/response"
)

const (
	cookieOAuthState  = "oauth_state"
	oauthCookieMaxAge = 600
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

func (h *Handler) Register(c *gin.Context) {
	var req RegisterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	role, verified, err := h.svc.Register(c.Request.Context(), req.Email, req.Password, req.Name, req.Role)
	switch {
	case err == nil:
		message := "验证邮件已发送"
		if role == UserRoleTeacher {
			message = "教师审批邮件已发送"
		}
		response.OK(c, RegisterResp{Message: message, Role: role, Verified: verified})
	case errors.Is(err, ErrEmailExists):
		response.Fail(c, 409, response.CodeEmailExists, "该邮箱已注册")
	default:
		response.Fail(c, 500, response.CodeInternal, err.Error())
	}
}

func (h *Handler) VerifyEmail(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		response.Fail(c, 400, response.CodeBadRequest, "缺少 token 参数")
		return
	}
	role, verified, err := h.svc.VerifyEmail(c.Request.Context(), token)
	switch {
	case err == nil:
		response.OK(c, RegisterResp{Message: "邮箱验证成功", Role: role, Verified: verified})
	case errors.Is(err, ErrTokenInvalid):
		response.Fail(c, 400, response.CodeTokenInvalid, "token 无效或已过期")
	default:
		response.Fail(c, 500, response.CodeInternal, err.Error())
	}
}

func (h *Handler) ApproveTeacher(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		response.Fail(c, 400, response.CodeBadRequest, "缺少 token 参数")
		return
	}
	role, verified, err := h.svc.ApproveTeacher(c.Request.Context(), token)
	switch {
	case err == nil:
		response.OK(c, RegisterResp{Message: "教师审批通过", Role: role, Verified: verified})
	case errors.Is(err, ErrTokenInvalid):
		response.Fail(c, 400, response.CodeTokenInvalid, "token 无效或已过期")
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
	sess, u, err := h.svc.Login(c.Request.Context(), req.Email, req.Password)
	switch {
	case err == nil:
		h.setSessionCookie(c, sess.ID, int(h.cfg.Session.TTL.Seconds()))
		response.OK(c, h.userResp(u))
	case errors.Is(err, ErrInvalidCredentials):
		response.Fail(c, 401, response.CodeInvalidCredentials, "邮箱或密码错误")
	case errors.Is(err, ErrEmailNotVerified):
		response.Fail(c, 403, response.CodeEmailNotVerified, "账号尚未验证")
	default:
		response.Fail(c, 500, response.CodeInternal, err.Error())
	}
}

func (h *Handler) Logout(c *gin.Context) {
	if sid, err := c.Cookie(h.cfg.Session.CookieName); err == nil {
		_ = h.svc.Logout(c.Request.Context(), sid)
	}
	h.setSessionCookie(c, "", -1)
	response.OK(c, gin.H{"message": "已登出"})
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
	response.OK(c, gin.H{"message": "如该邮箱已注册，重置邮件已发送"})
}

func (h *Handler) ResetPassword(c *gin.Context) {
	var req ResetPasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	switch err := h.svc.ResetPassword(c.Request.Context(), req.Token, req.Password); {
	case err == nil:
		response.OK(c, gin.H{"message": "密码已更新"})
	case errors.Is(err, ErrTokenInvalid):
		response.Fail(c, 400, response.CodeTokenInvalid, "token 无效或已过期")
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
		response.OK(c, gin.H{"message": "密码已更新，请重新登录"})
	case errors.Is(err, ErrPasswordWrong):
		response.Fail(c, 400, response.CodePasswordWrong, "当前密码错误")
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
	response.OK(c, h.userResp(u))
}

func (h *Handler) userResp(u *User) MeResp {
	return MeResp{
		ID:        u.ID.Hex(),
		Email:     u.Email,
		Name:      u.Name,
		Role:      u.Role,
		AvatarURL: h.svc.AvatarURL(u.AvatarKey),
		Verified:  u.Verified,
		CreatedAt: u.CreatedAt.Format(time.RFC3339),
	}
}

func (h *Handler) UpdateProfile(c *gin.Context) {
	var req UpdateProfileReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, 400, response.CodeBadRequest, err.Error())
		return
	}
	uid := c.MustGet(middleware.CtxUserID).(bson.ObjectID)
	u, err := h.svc.UpdateProfile(c.Request.Context(), uid, req.Name)
	if err != nil {
		response.Fail(c, 500, response.CodeInternal, err.Error())
		return
	}
	response.OK(c, h.userResp(u))
}

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
		response.Fail(c, 400, response.CodeBadRequest, "不支持的文件类型，允许 png/jpg/jpeg/webp")
		return
	}
	file, err := header.Open()
	if err != nil {
		response.Fail(c, 500, response.CodeInternal, err.Error())
		return
	}
	defer file.Close()

	uid := c.MustGet(middleware.CtxUserID).(bson.ObjectID)
	url, err := h.svc.UpdateAvatar(c.Request.Context(), uid, file, contentType, ext)
	if err != nil {
		response.Fail(c, 500, response.CodeInternal, err.Error())
		return
	}
	response.OK(c, UploadAvatarResp{AvatarURL: url})
}

func (h *Handler) GoogleStart(c *gin.Context) {
	state, err := randomState()
	if err != nil {
		response.Fail(c, 500, response.CodeInternal, err.Error())
		return
	}
	h.setShortCookie(c, cookieOAuthState, state)
	c.Redirect(http.StatusFound, h.svc.GoogleAuthURL(state))
}

func (h *Handler) GoogleCallback(c *gin.Context) {
	wantState, _ := c.Cookie(cookieOAuthState)
	gotState := c.Query("state")
	h.setShortCookie(c, cookieOAuthState, "")
	if wantState == "" || gotState == "" || wantState != gotState {
		response.Fail(c, 400, response.CodeBadRequest, "OAuth 状态无效")
		return
	}

	ret := h.cfg.Google.DefaultReturnURL
	if errParam := c.Query("error"); errParam != "" {
		c.Redirect(http.StatusFound, appendQuery(ret, "oauth_error", errParam))
		return
	}
	code := c.Query("code")
	if code == "" {
		response.Fail(c, 400, response.CodeBadRequest, "缺少 code 参数")
		return
	}

	sess, err := h.svc.GoogleSignIn(c.Request.Context(), code)
	if err != nil {
		c.Redirect(http.StatusFound, appendQuery(ret, "oauth_error", "sign_in_failed"))
		return
	}
	h.setSessionCookie(c, sess.ID, int(h.cfg.Session.TTL.Seconds()))
	c.Redirect(http.StatusFound, ret)
}

func (h *Handler) setShortCookie(c *gin.Context, name, value string) {
	maxAge := oauthCookieMaxAge
	if value == "" {
		maxAge = -1
	}
	c.SetSameSite(h.cfg.Session.CookieSameSite)
	c.SetCookie(name, value, maxAge, "/", "", h.cfg.Session.CookieSecure, true)
}

func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func appendQuery(raw, key, value string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
	return u.String()
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
