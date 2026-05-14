package router

import (
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"mechhub-back/internal/agent"
	"mechhub-back/internal/config"
	"mechhub-back/internal/mail"
	"mechhub-back/internal/middleware"
	"mechhub-back/internal/oauth"
	"mechhub-back/internal/session"
	"mechhub-back/internal/solochat"
	"mechhub-back/internal/storage"
	"mechhub-back/internal/user"
)

func New(cfg *config.Config, db *mongo.Database, sessions *session.Store, mailer *mail.Sender, oss *storage.OSS, google *oauth.Google, agentClient *agent.Client) *gin.Engine {
	r := gin.Default()

	if cfg.CORS.Enabled {
		r.Use(middleware.CORS(cfg.CORS))
	}

	api := r.Group("/api")

	auth := middleware.Auth(sessions, cfg.Session.CookieName)

	userRepo := user.NewRepo(db)
	userSvc := user.NewService(userRepo, sessions, mailer, oss, google, cfg)
	userHandler := user.NewHandler(userSvc, cfg)
	user.Mount(api, userHandler, auth)

	solochatRepo := solochat.NewRepo(db)
	solochatSvc := solochat.NewService(solochatRepo, agentClient, oss, cfg)
	solochatHandler := solochat.NewHandler(solochatSvc)
	solochat.Mount(api, solochatHandler, auth)

	return r
}
