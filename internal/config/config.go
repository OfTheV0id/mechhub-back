package config

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Port     string
	MySQL    MySQLConfig
	CORS     CORSConfig
	Session  SessionConfig
	Mail     MailConfig
	App      AppConfig
	Token    TokenConfig
	OSS      OSSConfig
	Avatar   AvatarConfig
	Google   GoogleConfig
	Solochat SolochatConfig
	LLM      LLMConfig
}

type MySQLConfig struct {
	DSN string
}

type CORSConfig struct {
	Enabled bool
	Origins []string
}

type SessionConfig struct {
	CookieName     string
	TTL            time.Duration
	CookieSecure   bool
	CookieSameSite http.SameSite
}

type MailConfig struct {
	ResendAPIKey string
	From         string
	AdminEmails  []string
	LogoURL      string
	BgURL        string
}

type AppConfig struct {
	BaseURL        string // 前端 base URL,用于邮件里的链接
	BackendBaseURL string // 后端外部 URL,构造 avatar/attachment/Google callback。默认 http://localhost:<PORT>
}

type TokenConfig struct {
	VerifyTTL           time.Duration
	ResetTTL            time.Duration
	TeacherApprovalTTL  time.Duration
}

type OSSConfig struct {
	Region          string
	Bucket          string
	AccessKeyID     string
	AccessKeySecret string
}

type AvatarConfig struct {
	MaxBytes int64
}

type GoogleConfig struct {
	ClientID         string
	ClientSecret     string
	DefaultReturnURL string
}

type SolochatConfig struct {
	MaxAttachmentsPerMessage int
	MaxFileSize              int64
}

type LLMConfig struct {
	// Provider 选 root agent 的 model 后端:"gemini"(默认)或 "openai-compat"。
	// grader(grade_with_ocr 内部 vision+structured)恒走 Gemini,不受这里影响。
	Provider string

	GeminiAPIKey      string
	GeminiBaseURL     string
	GeminiModel       string
	GeminiGraderModel string

	// OpenAI ChatCompletions-兼容后端(DeepSeek / Qwen / OpenRouter / vLLM ...)
	// 只有 Provider="openai-compat" 时必填。
	OpenAICompatBaseURL string
	OpenAICompatAPIKey  string
	OpenAICompatModel   string
	// 模型是否支持 image_url 多模态(qwen-vl-max / GPT-4o = true,DeepSeek
	// V4 主线 = false)。false 时图片不发给 root LLM,只走工具缓存。
	OpenAICompatVision bool

	// 标题专用模型(OpenAI 兼容)。仅用于会话首轮自动标题生成,
	// 跟主聊天模型解耦,可以指向更快更便宜的小模型(如 DeepSeek V3.2 chat)。
	// 三项任一为空 → 回退到 root model 做标题,与旧行为一致。
	TitleModelBaseURL string
	TitleModelAPIKey  string
	TitleModelName    string
}

func Load() *Config {
	_ = godotenv.Load()

	port := getEnv("PORT", "8080")

	cfg := &Config{
		Port: port,
		MySQL: MySQLConfig{
			DSN: requireEnv("MYSQL_DSN"),
		},
		CORS: CORSConfig{
			Enabled: getBool("CORS_ENABLED", false),
			Origins: splitCSV(getEnv("CORS_ORIGINS", "")),
		},
		Session: SessionConfig{
			CookieName:     getEnv("SESSION_COOKIE_NAME", "session_id"),
			TTL:            time.Duration(getInt("SESSION_TTL_HOURS", 168)) * time.Hour,
			CookieSecure:   getBool("SESSION_COOKIE_SECURE", false),
			CookieSameSite: parseSameSite(getEnv("SESSION_COOKIE_SAMESITE", "lax")),
		},
		Mail: MailConfig{
			ResendAPIKey: requireEnv("RESEND_API_KEY"),
			From:         requireEnv("MAIL_FROM"),
			AdminEmails:  splitCSV(requireEnv("ADMIN_EMAILS")),
			LogoURL:      getEnv("MAIL_LOGO_URL", ""),
			BgURL:        getEnv("MAIL_BG_URL", ""),
		},
		App: AppConfig{
			BaseURL:        requireEnv("APP_BASE_URL"),
			BackendBaseURL: getEnv("BACKEND_BASE_URL", "http://localhost:"+port),
		},
		Token: TokenConfig{
			VerifyTTL:          time.Duration(getInt("VERIFY_TOKEN_TTL_HOURS", 24)) * time.Hour,
			ResetTTL:           time.Duration(getInt("RESET_TOKEN_TTL_MINUTES", 30)) * time.Minute,
			TeacherApprovalTTL: time.Duration(getInt("TEACHER_APPROVAL_TTL_HOURS", 168)) * time.Hour,
		},
		OSS: OSSConfig{
			Region:          requireEnv("OSS_REGION"),
			Bucket:          requireEnv("OSS_BUCKET"),
			AccessKeyID:     requireEnv("OSS_ACCESS_KEY_ID"),
			AccessKeySecret: requireEnv("OSS_ACCESS_KEY_SECRET"),
		},
		Avatar: AvatarConfig{
			MaxBytes: int64(getInt("AVATAR_MAX_BYTES", 2*1024*1024)),
		},
		Google: GoogleConfig{
			ClientID:         requireEnv("GOOGLE_CLIENT_ID"),
			ClientSecret:     requireEnv("GOOGLE_CLIENT_SECRET"),
			DefaultReturnURL: requireEnv("GOOGLE_DEFAULT_RETURN_URL"),
		},
		Solochat: SolochatConfig{
			MaxAttachmentsPerMessage: getInt("SOLOCHAT_MAX_ATTACHMENTS_PER_MESSAGE", 4),
			MaxFileSize:              int64(getInt("SOLOCHAT_MAX_FILE_SIZE_BYTES", 20*1024*1024)),
		},
		LLM: LLMConfig{
			Provider:            getEnv("LLM_PROVIDER", "gemini"),
			GeminiAPIKey:        requireEnv("GEMINI_API_KEY"),
			GeminiBaseURL:       getEnv("GEMINI_BASE_URL", ""),
			GeminiModel:         getEnv("GEMINI_MODEL", "gemini-2.5-flash"),
			GeminiGraderModel:   getEnv("GEMINI_GRADER_MODEL", ""),
			OpenAICompatBaseURL: getEnv("OPENAI_COMPAT_BASE_URL", ""),
			OpenAICompatAPIKey:  getEnv("OPENAI_COMPAT_API_KEY", ""),
			OpenAICompatModel:   getEnv("OPENAI_COMPAT_MODEL", ""),
			OpenAICompatVision:  getBool("OPENAI_COMPAT_VISION", false),
			TitleModelBaseURL:   getEnv("TITLE_MODEL_BASE_URL", ""),
			TitleModelAPIKey:    getEnv("TITLE_MODEL_API_KEY", ""),
			TitleModelName:      getEnv("TITLE_MODEL_NAME", ""),
		},
	}
	return cfg
}

func getEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func requireEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		panic(fmt.Sprintf("missing required env: %s", k))
	}
	return v
}

func getBool(k string, def bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		panic(fmt.Sprintf("invalid bool for %s: %s", k, v))
	}
	return b
}

func getInt(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		panic(fmt.Sprintf("invalid int for %s: %s", k, v))
	}
	return n
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func parseSameSite(s string) http.SameSite {
	switch strings.ToLower(s) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}
