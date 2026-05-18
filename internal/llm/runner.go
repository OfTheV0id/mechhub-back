// Package llm wraps ADK Go: Gemini model, agent definition, runner +
// database-backed session service, plus event-to-SSE translation.
// Replaces the HTTP-to-Python `internal/agent/` client from rounds 4-6.
package llm

import (
	"context"
	"fmt"

	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	sessdb "google.golang.org/adk/session/database"
	adktool "google.golang.org/adk/tool"
	"google.golang.org/genai"

	"gorm.io/driver/mysql"

	"mechhub-back/internal/llm/prompts"
	"mechhub-back/internal/llm/tools"
)

const AppName = "mechhub_tutor"

// Bootstrap 配置:从 .env 读到的 LLM 相关参数。
type Config struct {
	MySQLDSN      string
	GeminiAPIKey  string
	GeminiBaseURL string
	GeminiModel   string
}

// Service 把 ADK Go 的 runner + sessionService 打包成业务可注入的依赖。
type Service struct {
	runner     *runner.Runner
	sessionSvc session.Service
}

func Bootstrap(ctx context.Context, cfg Config) (*Service, error) {
	clientCfg := &genai.ClientConfig{
		APIKey:  cfg.GeminiAPIKey,
		Backend: genai.BackendGeminiAPI,
	}
	if cfg.GeminiBaseURL != "" {
		clientCfg.HTTPOptions.BaseURL = cfg.GeminiBaseURL
	}
	model, err := gemini.NewModel(ctx, cfg.GeminiModel, clientCfg)
	if err != nil {
		return nil, fmt.Errorf("gemini.NewModel: %w", err)
	}

	ocrTool, err := tools.NewOCRTool()
	if err != nil {
		return nil, fmt.Errorf("ocr tool: %w", err)
	}
	graderTool, err := tools.NewGraderTool()
	if err != nil {
		return nil, fmt.Errorf("grader tool: %w", err)
	}

	agent, err := llmagent.New(llmagent.Config{
		Name:        AppName,
		Model:       model,
		Description: "MechHub 学习助手 —— 讲解概念 / 批改作业 / 答疑",
		Instruction: prompts.RootSystemPrompt,
		Tools:       []adktool.Tool{ocrTool, graderTool},
	})
	if err != nil {
		return nil, fmt.Errorf("llmagent.New: %w", err)
	}

	sessSvc, err := sessdb.NewSessionService(mysql.Open(cfg.MySQLDSN))
	if err != nil {
		return nil, fmt.Errorf("session db: %w", err)
	}
	if err := sessdb.AutoMigrate(sessSvc); err != nil {
		return nil, fmt.Errorf("session db migrate: %w", err)
	}

	r, err := runner.New(runner.Config{
		AppName:           AppName,
		Agent:             agent,
		SessionService:    sessSvc,
		AutoCreateSession: true,
	})
	if err != nil {
		return nil, fmt.Errorf("runner.New: %w", err)
	}

	return &Service{runner: r, sessionSvc: sessSvc}, nil
}

// Runner 暴露底层 runner 给 sse.go(它要按 iter.Seq2 流式读 events)。
func (s *Service) Runner() *runner.Runner { return s.runner }

// SessionService 暴露给 sessions.go(它需要 Get + AppendEvent 读历史 + 写绑定)。
func (s *Service) SessionService() session.Service { return s.sessionSvc }
