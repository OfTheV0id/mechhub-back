// Round 7 Stage 1 PoC: verify ADK Go + GORM + SQLite/MySQL works.
//
// Runs an end-to-end chat with a stub LLM, persists to a database
// session service, then re-fetches the session to verify events
// survived. Sanity check before committing to the big refactor.
//
// Usage:
//
//	go run ./cmd/adkpoc                          # sqlite ephemeral
//	ADK_POC_DSN="sqlite:./adkpoc.db" go run …    # sqlite file
//	ADK_POC_DSN="mysql:DSN" go run …             # mysql
package main

import (
	"context"
	"fmt"
	"iter"
	"log"
	"os"
	"strings"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/session/database"
	"google.golang.org/genai"

	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	appName = "mechhub_poc"
	userID  = "anon"
)

type stubModel struct{}

func (stubModel) Name() string { return "stub" }

func (stubModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		yield(&model.LLMResponse{
			Content: &genai.Content{
				Role:  "model",
				Parts: []*genai.Part{{Text: "stub reply: hello from PoC"}},
			},
			TurnComplete: true,
		}, nil)
	}
}

func main() {
	ctx := context.Background()

	dialector := pickDialector()
	sessSvc, err := database.NewSessionService(dialector)
	if err != nil {
		log.Fatalf("NewSessionService: %v", err)
	}
	if err := database.AutoMigrate(sessSvc); err != nil {
		log.Fatalf("AutoMigrate: %v", err)
	}

	a, err := llmagent.New(llmagent.Config{
		Name:        "tutor",
		Model:       stubModel{},
		Description: "stub tutor for PoC",
		Instruction: "you are stub",
	})
	if err != nil {
		log.Fatalf("llmagent.New: %v", err)
	}

	r, err := runner.New(runner.Config{
		AppName:           appName,
		Agent:             a,
		SessionService:    sessSvc,
		AutoCreateSession: true,
	})
	if err != nil {
		log.Fatalf("runner.New: %v", err)
	}

	sessionID := "sess-poc-1"
	msg := &genai.Content{Role: "user", Parts: []*genai.Part{{Text: "hello agent"}}}

	fmt.Println("=== Run with state_delta ===")
	for ev, err := range r.Run(ctx, userID, sessionID, msg, agent.RunConfig{StreamingMode: agent.StreamingModeNone},
		runner.WithStateDelta(map[string]any{"_solochat_attachments_test": []any{"fid1", "fid2"}})) {
		if err != nil {
			log.Fatalf("Run yielded error: %v", err)
		}
		fmt.Printf("  event id=%s author=%s inv=%s\n", ev.ID, ev.Author, ev.InvocationID)
		if ev.Content != nil {
			for _, p := range ev.Content.Parts {
				if p.Text != "" {
					fmt.Printf("    text: %q\n", p.Text)
				}
			}
		}
	}

	fmt.Println()
	fmt.Println("=== Re-fetch session ===")
	got, err := sessSvc.Get(ctx, &session.GetRequest{AppName: appName, UserID: userID, SessionID: sessionID})
	if err != nil {
		log.Fatalf("Get: %v", err)
	}
	if got == nil {
		log.Fatal("session not found after run")
	}
	evs := got.Session.Events()
	fmt.Printf("persisted events: %d\n", evs.Len())
	for ev := range evs.All() {
		fmt.Printf("  %s | %s | parts=%d\n", ev.Author, ev.InvocationID, partCount(ev.Content))
	}
	fmt.Println()
	fmt.Println("=== Session state ===")
	st := got.Session.State()
	for k, v := range st.All() {
		fmt.Printf("  %s = %v\n", k, v)
	}
}

func partCount(c *genai.Content) int {
	if c == nil {
		return 0
	}
	return len(c.Parts)
}

func pickDialector() gorm.Dialector {
	dsn := os.Getenv("ADK_POC_DSN")
	if dsn == "" {
		dsn = "sqlite::memory:"
	}
	switch {
	case strings.HasPrefix(dsn, "mysql:"):
		return mysql.Open(strings.TrimPrefix(dsn, "mysql:"))
	case strings.HasPrefix(dsn, "sqlite:"):
		return sqlite.Open(strings.TrimPrefix(dsn, "sqlite:"))
	default:
		return sqlite.Open(dsn)
	}
}
