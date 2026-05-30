// Package llm wraps ADK Go: Gemini model, agent definition, runner +
// database-backed session service, plus event-to-SSE translation.
// Replaces the HTTP-to-Python `internal/agent/` client from rounds 4-6.
package llm

import (
	"context"
	"fmt"

	goopenai "github.com/sashabaranov/go-openai"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	sessdb "google.golang.org/adk/session/database"
	adktool "google.golang.org/adk/tool"
	"google.golang.org/genai"

	"gorm.io/driver/mysql"

	"mechhub-back/internal/llm/openai"
	"mechhub-back/internal/llm/prompts"
	"mechhub-back/internal/llm/tools"
)

const AppName = "mechhub_tutor"

// Bootstrap 配置:从 .env 读到的 LLM 相关参数。
type Config struct {
	MySQLDSN string

	// Provider 选 root agent 后端:"gemini"(默认)或 "openai-compat"。
	// grade_with_ocr 内部 vision+structured 恒走 Gemini,与此无关。
	Provider string

	GeminiAPIKey  string
	GeminiBaseURL string
	GeminiModel   string

	// 当 Provider="openai-compat" 时必填。任何兼容 OpenAI ChatCompletions
	// 的后端(DeepSeek / Qwen / OpenRouter / vLLM / Ollama 等)。
	OpenAICompatBaseURL string
	OpenAICompatAPIKey  string
	OpenAICompatModel   string
	OpenAICompatVision  bool

	// 标题专用模型(OpenAI 兼容)。三项任一为空 → 标题回退 rootModel。
	// 用 DeepSeek V4 Flash 之类的轻量快模型,关 thinking 直接 HTTP 调。
	TitleModelBaseURL string
	TitleModelAPIKey  string
	TitleModelName    string
}

// Service 把 ADK Go 的 runner + sessionService 打包成业务可注入的依赖。
type Service struct {
	runner     *runner.Runner
	sessionSvc session.Service
	rootModel model.LLM          // agent 内部 / 工具 / 其他非标题任务
	titleHTTP *titleHTTPClient   // 标题专用 HTTP 客户端;nil = 回退 rootModel
}

func Bootstrap(ctx context.Context, cfg Config) (*Service, error) {
	rootModel, err := buildRootModel(ctx, cfg)
	if err != nil {
		return nil, err
	}

	var titleHTTP *titleHTTPClient
	if cfg.TitleModelBaseURL != "" && cfg.TitleModelAPIKey != "" && cfg.TitleModelName != "" {
		titleHTTP = newTitleHTTPClient(cfg.TitleModelBaseURL, cfg.TitleModelAPIKey, cfg.TitleModelName)
	}

	ocrTool, err := tools.NewOCRTool()
	if err != nil {
		return nil, fmt.Errorf("ocr tool: %w", err)
	}
	graderTool, err := tools.NewGraderTool()
	if err != nil {
		return nil, fmt.Errorf("grader tool: %w", err)
	}
	searchTool, err := tools.NewSearchTool()
	if err != nil {
		return nil, fmt.Errorf("search tool: %w", err)
	}

	agent, err := llmagent.New(llmagent.Config{
		Name:        AppName,
		Model:       rootModel,
		Description: "MechHub 学习助手 —— 讲解概念 / 批改作业 / 答疑",
		Instruction: prompts.RootSystemPrompt,
		Tools:       []adktool.Tool{ocrTool, graderTool, searchTool},
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

	return &Service{
		runner:     r,
		sessionSvc: sessSvc,
		rootModel:  rootModel,
		titleHTTP: titleHTTP,
	}, nil
}

// Runner 暴露底层 runner 给 sse.go(它要按 iter.Seq2 流式读 events)。
func (s *Service) Runner() *runner.Runner { return s.runner }

// SessionService 暴露给 sessions.go(它需要 Get + AppendEvent 读历史 + 写绑定)。
func (s *Service) SessionService() session.Service { return s.sessionSvc }

// buildRootModel 按 cfg.Provider 选 root agent 后端。默认 gemini;
// openai-compat 走 internal/llm/openai 适配器。
func buildRootModel(ctx context.Context, cfg Config) (model.LLM, error) {
	switch cfg.Provider {
	case "", "gemini":
		clientCfg := &genai.ClientConfig{
			APIKey:  cfg.GeminiAPIKey,
			Backend: genai.BackendGeminiAPI,
		}
		if cfg.GeminiBaseURL != "" {
			clientCfg.HTTPOptions.BaseURL = cfg.GeminiBaseURL
		}
		m, err := gemini.NewModel(ctx, cfg.GeminiModel, clientCfg)
		if err != nil {
			return nil, fmt.Errorf("gemini.NewModel: %w", err)
		}
		return m, nil

	case "openai-compat":
		if cfg.OpenAICompatAPIKey == "" || cfg.OpenAICompatBaseURL == "" || cfg.OpenAICompatModel == "" {
			return nil, fmt.Errorf("openai-compat provider 需要 OPENAI_COMPAT_BASE_URL / OPENAI_COMPAT_API_KEY / OPENAI_COMPAT_MODEL 三个 env")
		}
		oaCfg := goopenai.DefaultConfig(cfg.OpenAICompatAPIKey)
		oaCfg.BaseURL = cfg.OpenAICompatBaseURL
		return openai.NewOpenAIModel(cfg.OpenAICompatModel, oaCfg).WithVision(cfg.OpenAICompatVision), nil

	default:
		return nil, fmt.Errorf("unknown LLM_PROVIDER %q (allowed: gemini, openai-compat)", cfg.Provider)
	}
}
