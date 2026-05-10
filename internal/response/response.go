package response

import "github.com/gin-gonic/gin"

const (
	CodeOK                 = 0
	CodeBadRequest         = 400
	CodeUnauthorized       = 401
	CodeForbidden          = 403
	CodeNotFound           = 404
	CodeInternal           = 500

	CodeEmailExists        = 1001
	CodeInvalidCredentials = 1002
	CodeEmailNotVerified   = 1003
	CodeTokenInvalid       = 1004
	CodeUserNotFound       = 1005
	CodePasswordWrong      = 1006
)

type Body struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data,omitempty"`
}

func OK(c *gin.Context, data any) {
	c.JSON(200, Body{Code: CodeOK, Msg: "ok", Data: data})
}

func Fail(c *gin.Context, httpStatus, code int, msg string) {
	c.AbortWithStatusJSON(httpStatus, Body{Code: code, Msg: msg})
}
