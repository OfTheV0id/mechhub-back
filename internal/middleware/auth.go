package middleware

import (
	"github.com/gin-gonic/gin"

	"mechhub-back/internal/response"
	"mechhub-back/internal/session"
)

const CtxUserID = "user_id"

func Auth(store *session.Store, cookieName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		sid, err := c.Cookie(cookieName)
		if err != nil || sid == "" {
			response.Fail(c, 401, response.CodeUnauthorized, "unauthorized")
			return
		}
		sess, err := store.Get(c.Request.Context(), sid)
		if err != nil {
			response.Fail(c, 401, response.CodeUnauthorized, "unauthorized")
			return
		}
		c.Set(CtxUserID, sess.UserID)
		c.Next()
	}
}
