package user

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

var ErrNotFound = errors.New("user not found")

type Repo struct {
	db *gorm.DB
}

func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

func (r *Repo) Insert(ctx context.Context, u *User) error {
	return r.db.WithContext(ctx).Create(u).Error
}

func (r *Repo) FindByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	if err := r.db.WithContext(ctx).Where("email = ?", email).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}

func (r *Repo) FindByID(ctx context.Context, id string) (*User, error) {
	var u User
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}

func (r *Repo) SetVerified(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Model(&User{}).Where("id = ?", id).Update("verified", true).Error
}

func (r *Repo) UpdatePassword(ctx context.Context, id, hash string) error {
	return r.db.WithContext(ctx).Model(&User{}).Where("id = ?", id).Update("password_hash", hash).Error
}

func (r *Repo) UpdateName(ctx context.Context, id, name string) error {
	return r.db.WithContext(ctx).Model(&User{}).Where("id = ?", id).Update("name", name).Error
}

func (r *Repo) UpdateRole(ctx context.Context, id, role string) error {
	return r.db.WithContext(ctx).Model(&User{}).Where("id = ?", id).Update("role", role).Error
}

func (r *Repo) SetGoogleSub(ctx context.Context, id, sub string) error {
	return r.db.WithContext(ctx).Model(&User{}).Where("id = ?", id).Update("google_sub", sub).Error
}

// SwapAvatarKey 在事务里读出当前 key + 更新成新 key,返回旧 key 给调用方清理 OSS。
func (r *Repo) SwapAvatarKey(ctx context.Context, id, newKey string) (oldKey string, err error) {
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var prev User
		if err := tx.Select("avatar_key").Where("id = ?", id).First(&prev).Error; err != nil {
			return err
		}
		oldKey = prev.AvatarKey
		return tx.Model(&User{}).Where("id = ?", id).Update("avatar_key", newKey).Error
	})
	return oldKey, err
}

func (r *Repo) IsDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// MySQL: "Error 1062: Duplicate entry"; gorm wraps it but message stays
	return strings.Contains(msg, "1062") || strings.Contains(msg, "Duplicate entry")
}

func (r *Repo) InsertToken(ctx context.Context, t *Token) error {
	return r.db.WithContext(ctx).Create(t).Error
}

// FindAndDeleteToken 原子查 + 删,且过滤掉已过期 token。
func (r *Repo) FindAndDeleteToken(ctx context.Context, id, kind string) (*Token, error) {
	var t Token
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ? AND kind = ? AND expires_at > ?", id, kind, time.Now()).First(&t).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&Token{}).Error
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &t, nil
}

func (r *Repo) DeleteUserTokens(ctx context.Context, userID, kind string) error {
	return r.db.WithContext(ctx).Where("user_id = ? AND kind = ?", userID, kind).Delete(&Token{}).Error
}
