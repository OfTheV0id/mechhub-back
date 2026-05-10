package router

import (
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"mechhub-back/internal/config"
	"mechhub-back/internal/mail"
	"mechhub-back/internal/middleware"
	"mechhub-back/internal/session"
	"mechhub-back/internal/storage"
	"mechhub-back/internal/user"
)

func New(cfg *config.Config, db *mongo.Database, sessions *session.Store, mailer *mail.Sender, oss *storage.OSS) *gin.Engine {
	r := gin.Default()

	if cfg.CORS.Enabled {
		r.Use(middleware.CORS(cfg.CORS))
	}

	api := r.Group("/api")

	userRepo := user.NewRepo(db)
	userSvc := user.NewService(userRepo, sessions, mailer, oss, cfg)
	userHandler := user.NewHandler(userSvc, cfg)
	user.Mount(api, userHandler, middleware.Auth(sessions, cfg.Session.CookieName))

	return r
}
