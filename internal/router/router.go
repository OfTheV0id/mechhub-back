package router

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"mechhub-back/internal/assignment"
	"mechhub-back/internal/channel"
	"mechhub-back/internal/class"
	"mechhub-back/internal/config"
	"mechhub-back/internal/course"
	"mechhub-back/internal/llm"
	"mechhub-back/internal/mail"
	"mechhub-back/internal/middleware"
	"mechhub-back/internal/oauth"
	"mechhub-back/internal/realtime"
	"mechhub-back/internal/session"
	"mechhub-back/internal/solochat"
	"mechhub-back/internal/storage"
	"mechhub-back/internal/user"
)

func New(cfg *config.Config, db *gorm.DB, sessions *session.Store, mailer *mail.Sender, oss *storage.OSS, google *oauth.Google, llmSvc *llm.Service) *gin.Engine {
	r := gin.Default()

	if cfg.CORS.Enabled {
		r.Use(middleware.CORS(cfg.CORS))
	}

	api := r.Group("/api")
	auth := middleware.Auth(sessions, cfg.Session.CookieName)

	// User
	userRepo := user.NewRepo(db)
	userSvc := user.NewService(userRepo, sessions, mailer, oss, google, cfg)
	userHandler := user.NewHandler(userSvc, cfg)
	user.Mount(api, userHandler, auth)

	// Solochat
	solochatRepo := solochat.NewRepo(db)
	solochatSvc := solochat.NewService(solochatRepo, llmSvc, oss, cfg)
	solochatHandler := solochat.NewHandler(solochatSvc)
	solochat.Mount(api, solochatHandler, auth)

	// Course(学习板块)
	courseRepo := course.NewRepo(db)
	courseSvc := course.NewService(courseRepo, userRepo, userSvc, oss, cfg)
	courseHandler := course.NewHandler(courseSvc)
	course.Mount(api, courseHandler, auth)

	// Realtime + Class + Channel —— 三者环环相扣,装配顺序如下:
	//   1) classRepo 先 —— realtime / channel 都需要它做成员查询
	//   2) realtime.Hub 单例
	//   3) channelSvc(实现 class.ChannelHook,且 hub 已建好可以注入)
	//   4) classSvc 装配 hub + channelSvc(作为 ChannelHook)
	//   5) realtime.Handler 装配 hub + classRepo(作为 MembershipResolver)
	classRepo := class.NewRepo(db)
	hub := realtime.NewHub()

	channelRepo := channel.NewRepo(db)
	channelSvc := channel.NewService(channelRepo, classRepo, userRepo, solochatSvc, oss, hub, cfg)
	channelHandler := channel.NewHandler(channelSvc)
	channel.Mount(api, channelHandler, auth)

	classSvc := class.NewService(classRepo, userRepo, oss, hub, channelSvc, cfg)
	classHandler := class.NewHandler(classSvc, cfg)
	class.Mount(api, classHandler, auth)

	realtimeHandler := realtime.NewHandler(hub, classRepo)
	realtime.Mount(api, realtimeHandler, auth)

	// Assignment(作业板块)—— 复用 classRepo 做成员/角色校验,userRepo 取角色/学生信息,
	// hub 推实时失效,solochatSvc 取学生 SoloChat 会话(读消息 + 拷贝图片)快照成提交记录
	assignmentRepo := assignment.NewRepo(db)
	assignmentSvc := assignment.NewService(assignmentRepo, classRepo, userRepo, oss, hub, solochatSvc, cfg)
	assignmentHandler := assignment.NewHandler(assignmentSvc)
	assignment.Mount(api, assignmentHandler, auth)

	return r
}
