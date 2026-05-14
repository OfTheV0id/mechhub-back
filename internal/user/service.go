package user

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/crypto/bcrypt"

	"mechhub-back/internal/config"
	"mechhub-back/internal/mail"
	"mechhub-back/internal/oauth"
	"mechhub-back/internal/session"
	"mechhub-back/internal/storage"
)

var (
	ErrEmailExists        = errors.New("email already registered")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrEmailNotVerified   = errors.New("email not verified")
	ErrTokenInvalid       = errors.New("token invalid or expired")
	ErrPasswordWrong      = errors.New("current password is wrong")
	ErrGoogleUnverified   = errors.New("google account email not verified")
)

type Service struct {
	repo     *Repo
	sessions *session.Store
	mailer   *mail.Sender
	oss      *storage.OSS
	google   *oauth.Google
	cfg      *config.Config
}

func NewService(repo *Repo, sessions *session.Store, mailer *mail.Sender, oss *storage.OSS, google *oauth.Google, cfg *config.Config) *Service {
	return &Service{repo: repo, sessions: sessions, mailer: mailer, oss: oss, google: google, cfg: cfg}
}

func (s *Service) Register(ctx context.Context, email, password, name, role string) (string, bool, error) {
	email = normalizeEmail(email)
	name = strings.TrimSpace(name)
	role = strings.ToLower(strings.TrimSpace(role))
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return "", false, err
	}

	existing, err := s.repo.FindByEmail(ctx, email)
	switch {
	case errors.Is(err, ErrNotFound):
		u := &User{
			ID:           bson.NewObjectID(),
			Email:        email,
			PasswordHash: string(hash),
			Name:         name,
			Role:         role,
			Verified:     false,
			CreatedAt:    time.Now(),
		}
		if err := s.repo.Insert(ctx, u); err != nil {
			if s.repo.IsDuplicateKey(err) {
				return "", false, ErrEmailExists
			}
			return "", false, err
		}
		if role == UserRoleTeacher {
			return role, false, s.sendTeacherApprovalToken(ctx, u)
		}
		return role, false, s.sendVerifyToken(ctx, u)
	case err != nil:
		return "", false, err
	case existing.Verified:
		return "", false, ErrEmailExists
	default:
		if existing.Role != role {
			if err := s.repo.UpdateRole(ctx, existing.ID, role); err != nil {
				return "", false, err
			}
			existing.Role = role
		}
		_ = s.repo.DeleteUserTokens(ctx, existing.ID, TokenKindVerify)
		_ = s.repo.DeleteUserTokens(ctx, existing.ID, TokenKindTeacherApproval)
		if role == UserRoleTeacher {
			return role, false, s.sendTeacherApprovalToken(ctx, existing)
		}
		return role, false, s.sendVerifyToken(ctx, existing)
	}
}

func (s *Service) UpdateProfile(ctx context.Context, userID bson.ObjectID, name string) (*User, error) {
	if err := s.repo.UpdateName(ctx, userID, strings.TrimSpace(name)); err != nil {
		return nil, err
	}
	return s.repo.FindByID(ctx, userID)
}

func (s *Service) UpdateAvatar(ctx context.Context, userID bson.ObjectID, body io.Reader, contentType, ext string) (string, error) {
	suffix, err := randomHex(8)
	if err != nil {
		return "", err
	}
	key := "avatars/" + userID.Hex() + "/" + suffix + ext
	if err := s.oss.Upload(ctx, key, body, contentType); err != nil {
		return "", err
	}
	oldKey, err := s.repo.SwapAvatarKey(ctx, userID, key)
	if err != nil {
		_ = s.oss.Delete(ctx, key)
		return "", err
	}
	if oldKey != "" && oldKey != key {
		_ = s.oss.Delete(ctx, oldKey)
	}
	return s.oss.PublicURL(key), nil
}

func (s *Service) AvatarURL(key string) string {
	return s.oss.PublicURL(key)
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
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

func (s *Service) sendTeacherApprovalToken(ctx context.Context, u *User) error {
	tok, err := newToken()
	if err != nil {
		return err
	}
	if err := s.repo.InsertToken(ctx, &Token{
		ID:        tok,
		UserID:    u.ID,
		Kind:      TokenKindTeacherApproval,
		ExpiresAt: time.Now().Add(s.cfg.Token.TeacherApprovalTTL),
	}); err != nil {
		return err
	}
	return s.mailer.SendTeacherApprovalEmail(s.cfg.Mail.AdminEmails, u.Name, u.Email, tok)
}

func (s *Service) VerifyEmail(ctx context.Context, token string) (string, bool, error) {
	t, err := s.repo.FindAndDeleteToken(ctx, token, TokenKindVerify)
	if errors.Is(err, ErrNotFound) {
		return "", false, ErrTokenInvalid
	}
	if err != nil {
		return "", false, err
	}
	if time.Now().After(t.ExpiresAt) {
		return "", false, ErrTokenInvalid
	}
	if err := s.repo.SetVerified(ctx, t.UserID); err != nil {
		return "", false, err
	}
	u, err := s.repo.FindByID(ctx, t.UserID)
	if err != nil {
		return "", false, err
	}
	return u.Role, u.Verified, nil
}

func (s *Service) ApproveTeacher(ctx context.Context, token string) (string, bool, error) {
	t, err := s.repo.FindAndDeleteToken(ctx, token, TokenKindTeacherApproval)
	if errors.Is(err, ErrNotFound) {
		return "", false, ErrTokenInvalid
	}
	if err != nil {
		return "", false, err
	}
	if time.Now().After(t.ExpiresAt) {
		return "", false, ErrTokenInvalid
	}
	if err := s.repo.SetVerified(ctx, t.UserID); err != nil {
		return "", false, err
	}
	u, err := s.repo.FindByID(ctx, t.UserID)
	if err != nil {
		return "", false, err
	}
	return u.Role, u.Verified, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (*session.Session, *User, error) {
	u, err := s.repo.FindByEmail(ctx, normalizeEmail(email))
	if errors.Is(err, ErrNotFound) {
		return nil, nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, nil, ErrInvalidCredentials
	}
	if !u.Verified {
		return nil, nil, ErrEmailNotVerified
	}
	sess, err := s.sessions.New(ctx, u.ID)
	if err != nil {
		return nil, nil, err
	}
	return sess, u, nil
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

func (s *Service) GoogleAuthURL(state string) string {
	return s.google.AuthURL(state)
}

func (s *Service) GoogleSignIn(ctx context.Context, code string) (*session.Session, error) {
	tok, err := s.google.Exchange(ctx, code)
	if err != nil {
		return nil, err
	}
	info, err := s.google.FetchUser(ctx, tok)
	if err != nil {
		return nil, err
	}
	if !info.EmailVerified {
		return nil, ErrGoogleUnverified
	}

	email := normalizeEmail(info.Email)
	u, err := s.repo.FindByEmail(ctx, email)
	switch {
	case errors.Is(err, ErrNotFound):
		u = &User{
			ID:        bson.NewObjectID(),
			Email:     email,
			Name:      strings.TrimSpace(info.Name),
			Role:      UserRoleStudent,
			GoogleSub: info.Sub,
			Verified:  true,
			CreatedAt: time.Now(),
		}
		if err := s.repo.Insert(ctx, u); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	default:
		if u.GoogleSub == "" {
			if err := s.repo.SetGoogleSub(ctx, u.ID, info.Sub); err != nil {
				return nil, err
			}
			u.GoogleSub = info.Sub
		}
		if !u.Verified {
			if err := s.repo.SetVerified(ctx, u.ID); err != nil {
				return nil, err
			}
			u.Verified = true
		}
	}

	if u.AvatarKey == "" && info.Picture != "" {
		_ = s.mirrorGoogleAvatar(ctx, u, info.Picture)
	}

	return s.sessions.New(ctx, u.ID)
}

func (s *Service) mirrorGoogleAvatar(ctx context.Context, u *User, pictureURL string) error {
	blob, err := s.google.DownloadPicture(ctx, largeGooglePictureURL(pictureURL))
	if err != nil {
		return err
	}
	defer blob.Body.Close()
	ext := extFromContentType(blob.ContentType)
	if ext == "" {
		return nil
	}
	suffix, err := randomHex(8)
	if err != nil {
		return err
	}
	key := "avatars/" + u.ID.Hex() + "/" + suffix + ext
	if err := s.oss.Upload(ctx, key, blob.Body, blob.ContentType); err != nil {
		return err
	}
	_, err = s.repo.SwapAvatarKey(ctx, u.ID, key)
	return err
}

func extFromContentType(ct string) string {
	switch strings.ToLower(strings.SplitN(ct, ";", 2)[0]) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}

var googleSizeRe = regexp.MustCompile(`=s\d+[^&]*`)

func largeGooglePictureURL(raw string) string {
	if !strings.Contains(raw, "googleusercontent.com") {
		return raw
	}
	return googleSizeRe.ReplaceAllString(raw, "=s512-c")
}
