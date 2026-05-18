package main

import (
	"context"
	"log"

	"mechhub-back/internal/config"
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
	google := oauth.NewGoogle(cfg.Google)

	llmSvc, err := llm.Bootstrap(ctx, llm.Config{
		MySQLDSN:      cfg.MySQL.DSN,
		GeminiAPIKey:  cfg.LLM.GeminiAPIKey,
		GeminiBaseURL: cfg.LLM.GeminiBaseURL,
		GeminiModel:   cfg.LLM.GeminiModel,
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
