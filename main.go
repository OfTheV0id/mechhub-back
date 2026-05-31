package main

import (
	"context"
	"log"

	"mechhub-back/internal/channel"
	"mechhub-back/internal/class"
	"mechhub-back/internal/config"
	"mechhub-back/internal/course"
	"mechhub-back/internal/db"
	"mechhub-back/internal/llm"
	"mechhub-back/internal/mail"
	"mechhub-back/internal/oauth"
	"mechhub-back/internal/router"
	"mechhub-back/internal/session"
	"mechhub-back/internal/solochat"
	"mechhub-back/internal/storage"
	"mechhub-back/internal/user"
)

func main() {
	cfg := config.Load()

	gormDB, err := db.Connect(cfg.MySQL.DSN)
	if err != nil {
		log.Fatalf("mysql connect: %v", err)
	}
	if err := gormDB.AutoMigrate(
		&user.User{},
		&user.Token{},
		&session.Session{},
		&solochat.Conversation{},
		&solochat.UploadedFile{},
		&class.Class{},
		&class.Member{},
		&channel.Channel{},
		&channel.Message{},
		&channel.Attachment{},
		&channel.MessageReaction{},
		&course.Course{},
		&course.CourseNode{},
		&course.CourseFile{},
		&course.NodeProgress{},
		&course.Annotation{},
	); err != nil {
		log.Fatalf("auto migrate: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db.StartTTLCleanup(ctx, gormDB)

	sessions := session.NewStore(gormDB, cfg.Session.TTL)
	mailer := mail.New(cfg)
	oss, err := storage.NewOSS(cfg.OSS)
	if err != nil {
		log.Fatalf("oss init: %v", err)
	}
	google := oauth.NewGoogle(cfg.Google, cfg.App.BackendBaseURL)

	llmSvc, err := llm.Bootstrap(ctx, llm.Config{
		MySQLDSN:            cfg.MySQL.DSN,
		Provider:            cfg.LLM.Provider,
		GeminiAPIKey:        cfg.LLM.GeminiAPIKey,
		GeminiBaseURL:       cfg.LLM.GeminiBaseURL,
		GeminiModel:         cfg.LLM.GeminiModel,
		OpenAICompatBaseURL: cfg.LLM.OpenAICompatBaseURL,
		OpenAICompatAPIKey:  cfg.LLM.OpenAICompatAPIKey,
		OpenAICompatModel:   cfg.LLM.OpenAICompatModel,
		OpenAICompatVision:  cfg.LLM.OpenAICompatVision,
		TitleModelBaseURL:   cfg.LLM.TitleModelBaseURL,
		TitleModelAPIKey:    cfg.LLM.TitleModelAPIKey,
		TitleModelName:      cfg.LLM.TitleModelName,
	})
	if err != nil {
		log.Fatalf("llm bootstrap: %v", err)
	}

	r := router.New(cfg, gormDB, sessions, mailer, oss, google, llmSvc)

	addr := ":" + cfg.Port
	log.Printf("listening on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("listen: %v", err)
	}
}
