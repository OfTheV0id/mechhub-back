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
	BaseURL string // 前端 base URL,用于邮件里的链接
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
	GeminiAPIKey      string
	GeminiBaseURL     string
	GeminiModel       string
	GeminiGraderModel string
}

func Load() *Config {
	_ = godotenv.Load()

	cfg := &Config{
		Port: getEnv("PORT", "8080"),
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
			BaseURL: requireEnv("APP_BASE_URL"),
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
			GeminiAPIKey:      requireEnv("GEMINI_API_KEY"),
			GeminiBaseURL:     getEnv("GEMINI_BASE_URL", ""),
			GeminiModel:       getEnv("GEMINI_MODEL", "gemini-2.5-flash"),
			GeminiGraderModel: getEnv("GEMINI_GRADER_MODEL", ""),
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
