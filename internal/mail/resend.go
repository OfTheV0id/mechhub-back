package mail

import (
	"fmt"

	"github.com/resend/resend-go/v3"

	"mechhub-back/internal/config"
)

type Sender struct {
	client  *resend.Client
	from    string
	baseURL string
}

func New(cfg *config.Config) *Sender {
	return &Sender{
		client:  resend.NewClient(cfg.Mail.ResendAPIKey),
		from:    cfg.Mail.From,
		baseURL: cfg.App.BaseURL,
	}
}

func (s *Sender) SendVerifyEmail(to, token string) error {
	link := fmt.Sprintf("%s/verify?token=%s", s.baseURL, token)
	_, err := s.client.Emails.Send(&resend.SendEmailRequest{
		From:    s.from,
		To:      []string{to},
		Subject: "Verify your MechHub account",
		Html: fmt.Sprintf(
			`<p>Welcome to MechHub.</p><p>Click <a href="%s">here</a> to verify your email.</p><p>This link expires soon.</p>`,
			link,
		),
	})
	return err
}

func (s *Sender) SendResetEmail(to, token string) error {
	link := fmt.Sprintf("%s/reset-password?token=%s", s.baseURL, token)
	_, err := s.client.Emails.Send(&resend.SendEmailRequest{
		From:    s.from,
		To:      []string{to},
		Subject: "Reset your MechHub password",
		Html: fmt.Sprintf(
			`<p>You requested a password reset.</p><p>Click <a href="%s">here</a> to set a new password.</p><p>If you didn't request this, ignore this email.</p>`,
			link,
		),
	})
	return err
}
