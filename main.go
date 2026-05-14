package main

import (
	"context"
	"log"

	"mechhub-back/internal/agent"
	"mechhub-back/internal/config"
	"mechhub-back/internal/db"
	"mechhub-back/internal/mail"
	"mechhub-back/internal/oauth"
	"mechhub-back/internal/router"
	"mechhub-back/internal/session"
	"mechhub-back/internal/storage"
)

func main() {
	cfg := config.Load()

	ctx := context.Background()
	mongoDB, err := db.Connect(ctx, cfg.Mongo.URI, cfg.Mongo.DB)
	if err != nil {
		log.Fatalf("mongo connect: %v", err)
	}
	if err := db.EnsureIndexes(ctx, mongoDB); err != nil {
		log.Fatalf("ensure indexes: %v", err)
	}
	if err := db.MaybeDropLegacyGradingCollections(ctx, mongoDB); err != nil {
		log.Fatalf("drop legacy grading collections: %v", err)
	}

	sessions := session.NewStore(mongoDB, cfg.Session.TTL)
	mailer := mail.New(cfg)
	oss, err := storage.NewOSS(cfg.OSS)
	if err != nil {
		log.Fatalf("oss init: %v", err)
	}
	google := oauth.NewGoogle(cfg.Google)
	agentClient := agent.NewClient(cfg.Agent)

	r := router.New(cfg, mongoDB, sessions, mailer, oss, google, agentClient)

	addr := ":" + cfg.Port
	log.Printf("listening on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("listen: %v", err)
	}
}
