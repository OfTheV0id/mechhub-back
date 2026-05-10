package middleware

import (
	"github.com/gin-gonic/gin"

	"mechhub-back/internal/config"
)

func CORS(cfg config.CORSConfig) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(cfg.Origins))
	for _, o := range cfg.Origins {
		allowed[o] = struct{}{}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if _, ok := allowed[origin]; ok {
			h := c.Writer.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Origin,Content-Type,Accept,Authorization")
			h.Set("Access-Control-Max-Age", "600")
			h.Add("Vary", "Origin")
		}
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}
