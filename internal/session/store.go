package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"gorm.io/gorm"
)

var ErrNotFound = errors.New("session not found")

type Store struct {
	db  *gorm.DB
	ttl time.Duration
}

func NewStore(db *gorm.DB, ttl time.Duration) *Store {
	return &Store{db: db, ttl: ttl}
}

func (s *Store) New(ctx context.Context, userID string) (*Session, error) {
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	sess := &Session{
		ID:        id,
		UserID:    userID,
		ExpiresAt: now.Add(s.ttl),
		CreatedAt: now,
	}
	if err := s.db.WithContext(ctx).Create(sess).Error; err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *Store) Get(ctx context.Context, id string) (*Session, error) {
	var sess Session
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&sess).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if time.Now().After(sess.ExpiresAt) {
		return nil, ErrNotFound
	}
	return &sess, nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Where("id = ?", id).Delete(&Session{}).Error
}

func (s *Store) DeleteByUser(ctx context.Context, userID string) error {
	return s.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&Session{}).Error
}

func randomID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
