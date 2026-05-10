package user

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/crypto/bcrypt"

	"mechhub-back/internal/config"
	"mechhub-back/internal/mail"
	"mechhub-back/internal/session"
)

var (
	ErrEmailExists        = errors.New("email already registered")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrEmailNotVerified   = errors.New("email not verified")
	ErrTokenInvalid       = errors.New("token invalid or expired")
	ErrPasswordWrong      = errors.New("current password is wrong")
)

type Service struct {
	repo     *Repo
	sessions *session.Store
	mailer   *mail.Sender
	cfg      *config.Config
}

func NewService(repo *Repo, sessions *session.Store, mailer *mail.Sender, cfg *config.Config) *Service {
	return &Service{repo: repo, sessions: sessions, mailer: mailer, cfg: cfg}
}

func (s *Service) Register(ctx context.Context, email, password string) error {
	email = normalizeEmail(email)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return err
	}
	u := &User{
		ID:           bson.NewObjectID(),
		Email:        email,
		PasswordHash: string(hash),
		Verified:     false,
		CreatedAt:    time.Now(),
	}
	if err := s.repo.Insert(ctx, u); err != nil {
		if s.repo.IsDuplicateKey(err) {
			return ErrEmailExists
		}
		return err
	}
	return s.sendVerifyToken(ctx, u)
}

func (s *Service) sendVerifyToken(ctx context.Context, u *User) error {
	tok, err := newToken()
	if err != nil {
		return err
	}
	if err := s.repo.InsertToken(ctx, &Token{
		ID:        tok,
		UserID:    u.ID,
		Kind:      TokenKindVerify,
		ExpiresAt: time.Now().Add(s.cfg.Token.VerifyTTL),
	}); err != nil {
		return err
	}
	return s.mailer.SendVerifyEmail(u.Email, tok)
}

func (s *Service) VerifyEmail(ctx context.Context, token string) error {
	t, err := s.repo.FindAndDeleteToken(ctx, token, TokenKindVerify)
	if errors.Is(err, ErrNotFound) {
		return ErrTokenInvalid
	}
	if err != nil {
		return err
	}
	if time.Now().After(t.ExpiresAt) {
		return ErrTokenInvalid
	}
	return s.repo.SetVerified(ctx, t.UserID)
}

func (s *Service) Login(ctx context.Context, email, password string) (*session.Session, error) {
	u, err := s.repo.FindByEmail(ctx, normalizeEmail(email))
	if errors.Is(err, ErrNotFound) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}
	if !u.Verified {
		return nil, ErrEmailNotVerified
	}
	return s.sessions.New(ctx, u.ID)
}

func (s *Service) Logout(ctx context.Context, sid string) error {
	return s.sessions.Delete(ctx, sid)
}

func (s *Service) ForgotPassword(ctx context.Context, email string) error {
	u, err := s.repo.FindByEmail(ctx, normalizeEmail(email))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	_ = s.repo.DeleteUserTokens(ctx, u.ID, TokenKindReset)
	tok, err := newToken()
	if err != nil {
		return err
	}
	if err := s.repo.InsertToken(ctx, &Token{
		ID:        tok,
		UserID:    u.ID,
		Kind:      TokenKindReset,
		ExpiresAt: time.Now().Add(s.cfg.Token.ResetTTL),
	}); err != nil {
		return err
	}
	return s.mailer.SendResetEmail(u.Email, tok)
}

func (s *Service) ResetPassword(ctx context.Context, token, newPassword string) error {
	t, err := s.repo.FindAndDeleteToken(ctx, token, TokenKindReset)
	if errors.Is(err, ErrNotFound) {
		return ErrTokenInvalid
	}
	if err != nil {
		return err
	}
	if time.Now().After(t.ExpiresAt) {
		return ErrTokenInvalid
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), 12)
	if err != nil {
		return err
	}
	if err := s.repo.UpdatePassword(ctx, t.UserID, string(hash)); err != nil {
		return err
	}
	return s.sessions.DeleteByUser(ctx, t.UserID)
}

func (s *Service) ChangePassword(ctx context.Context, userID bson.ObjectID, oldPwd, newPwd string) error {
	u, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(oldPwd)); err != nil {
		return ErrPasswordWrong
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPwd), 12)
	if err != nil {
		return err
	}
	if err := s.repo.UpdatePassword(ctx, userID, string(hash)); err != nil {
		return err
	}
	return s.sessions.DeleteByUser(ctx, userID)
}

func (s *Service) Me(ctx context.Context, userID bson.ObjectID) (*User, error) {
	return s.repo.FindByID(ctx, userID)
}

func normalizeEmail(e string) string {
	return strings.ToLower(strings.TrimSpace(e))
}
