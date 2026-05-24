package solochat

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

var ErrNotFound = errors.New("solochat: not found")

// Repo 只剩 conversation 元数据 + 附件元信息。消息流(events / state /
// OCR 缓存)在 ADK Go 的 sessions/events 表里,本仓库不直接操作,通过
// internal/llm 包封装的 session.Service 读写。
type Repo struct {
	db *gorm.DB
}

func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

func (r *Repo) InsertConversation(ctx context.Context, c *Conversation) error {
	return r.db.WithContext(ctx).Create(c).Error
}

func (r *Repo) ListConversations(ctx context.Context, userID string) ([]Conversation, error) {
	var list []Conversation
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("updated_at DESC").
		Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *Repo) FindConversation(ctx context.Context, id, userID string) (*Conversation, error) {
	var c Conversation
	err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// UpdateConversationTitle 不管是 AI 自动生成、autoTitle 兜底,还是用户手动
// rename,都标记 title_generated=true,后续 stream 不会再尝试覆盖。
func (r *Repo) UpdateConversationTitle(ctx context.Context, id, userID, title string) error {
	res := r.db.WithContext(ctx).Model(&Conversation{}).
		Where("id = ? AND user_id = ?", id, userID).
		Updates(map[string]any{
			"title":           title,
			"title_generated": true,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repo) TouchConversation(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Model(&Conversation{}).
		Where("id = ?", id).
		Update("updated_at", gorm.Expr("CURRENT_TIMESTAMP(6)")).Error
}

func (r *Repo) DeleteConversation(ctx context.Context, id, userID string) error {
	res := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", id, userID).
		Delete(&Conversation{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repo) InsertFile(ctx context.Context, f *UploadedFile) error {
	return r.db.WithContext(ctx).Create(f).Error
}

func (r *Repo) FindFile(ctx context.Context, id, ownerID string) (*UploadedFile, error) {
	var f UploadedFile
	err := r.db.WithContext(ctx).Where("id = ? AND owner_user_id = ?", id, ownerID).First(&f).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *Repo) FindFilesByIDs(ctx context.Context, ids []string, ownerID string) ([]UploadedFile, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var list []UploadedFile
	if err := r.db.WithContext(ctx).
		Where("id IN ? AND owner_user_id = ?", ids, ownerID).
		Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}
