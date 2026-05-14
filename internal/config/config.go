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
	Port    string
	Mongo   MongoConfig
	CORS    CORSConfig
	Session SessionConfig
	Mail    MailConfig
	App     AppConfig
	Token   TokenConfig
	OSS     OSSConfig
	Avatar  AvatarConfig
	Google  GoogleConfig
}

type MongoConfig struct {
	URI string
	DB  string
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
	BaseURL string
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
	PublicBaseURL   string
}

type AvatarConfig struct {
	MaxBytes int64
}

type GoogleConfig struct {
	ClientID         string
	ClientSecret     string
	RedirectURL      string
	DefaultReturnURL string
}

func Load() *Config {
	_ = godotenv.Load()

	cfg := &Config{
		Port: getEnv("PORT", "8080"),
		Mongo: MongoConfig{
			URI: requireEnv("MONGO_URI"),
			DB:  requireEnv("MONGO_DB"),
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
			PublicBaseURL:   requireEnv("OSS_PUBLIC_BASE_URL"),
		},
		Avatar: AvatarConfig{
			MaxBytes: int64(getInt("AVATAR_MAX_BYTES", 2*1024*1024)),
		},
		Google: GoogleConfig{
			ClientID:         requireEnv("GOOGLE_CLIENT_ID"),
			ClientSecret:     requireEnv("GOOGLE_CLIENT_SECRET"),
			RedirectURL:      requireEnv("GOOGLE_REDIRECT_URL"),
			DefaultReturnURL: requireEnv("GOOGLE_DEFAULT_RETURN_URL"),
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
