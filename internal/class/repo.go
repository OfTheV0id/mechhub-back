package class

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

var ErrNotFound = errors.New("class: not found")

type Repo struct {
	db *gorm.DB
}

func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

func (r *Repo) InsertClass(ctx context.Context, c *Class) error {
	return r.db.WithContext(ctx).Create(c).Error
}

func (r *Repo) FindByID(ctx context.Context, id string) (*Class, error) {
	var c Class
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repo) FindByInviteToken(ctx context.Context, token string) (*Class, error) {
	var c Class
	err := r.db.WithContext(ctx).Where("invite_token = ?", token).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// UpdateInvite 更新邀请 token / 过期时间 / 禁用标志。原子操作。
func (r *Repo) UpdateInvite(ctx context.Context, classID, token string, expiresAt *time.Time, disabled bool) error {
	return r.db.WithContext(ctx).Model(&Class{}).Where("id = ?", classID).Updates(map[string]any{
		"invite_token":      token,
		"invite_expires_at": expiresAt,
		"invite_disabled":   disabled,
	}).Error
}

func (r *Repo) ListForUser(ctx context.Context, userID string) ([]Class, error) {
	var rows []Class
	err := r.db.WithContext(ctx).
		Table("classes AS c").
		Select("c.*").
		Joins("INNER JOIN class_members AS cm ON cm.class_id = c.id").
		Where("cm.user_id = ?", userID).
		Order("c.created_at DESC, c.id DESC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *Repo) GetForUser(ctx context.Context, classID, userID string) (*Class, error) {
	var row Class
	err := r.db.WithContext(ctx).
		Table("classes AS c").
		Select("c.*").
		Joins("INNER JOIN class_members AS cm ON cm.class_id = c.id").
		Where("c.id = ? AND cm.user_id = ?", classID, userID).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *Repo) UpdateClass(ctx context.Context, classID string, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&Class{}).Where("id = ?", classID).Updates(updates).Error
}

// UpdateAvatarKey 原子换 avatar_key,返回旧 key 让 service 去 OSS 删旧文件。
func (r *Repo) UpdateAvatarKey(ctx context.Context, classID, newKey string) (string, error) {
	var oldKey string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var c Class
		if err := tx.Select("avatar_key").Where("id = ?", classID).First(&c).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		oldKey = c.AvatarKey
		return tx.Model(&Class{}).Where("id = ?", classID).Update("avatar_key", newKey).Error
	})
	return oldKey, err
}

// DeleteClass 联带删 members,事务里完成。返回被删班级的 avatar_key 供 service
// 异步删 OSS 文件。
func (r *Repo) DeleteClass(ctx context.Context, classID string) (string, error) {
	var avatarKey string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var c Class
		if err := tx.Select("avatar_key").Where("id = ?", classID).First(&c).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		avatarKey = c.AvatarKey
		if err := tx.Where("class_id = ?", classID).Delete(&Member{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", classID).Delete(&Class{}).Error
	})
	return avatarKey, err
}

func (r *Repo) InsertMember(ctx context.Context, m *Member) error {
	return r.db.WithContext(ctx).Create(m).Error
}

func (r *Repo) FindMembership(ctx context.Context, classID, userID string) (*Member, error) {
	var m Member
	err := r.db.WithContext(ctx).Where("class_id = ? AND user_id = ?", classID, userID).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *Repo) ListMembers(ctx context.Context, classID, ownerUserID string) ([]MemberWithUser, error) {
	var rows []MemberWithUser
	err := r.db.WithContext(ctx).
		Table("class_members AS cm").
		Select(`cm.*, u.email AS email, u.name AS user_name, u.role AS user_role,
		        u.avatar_key AS user_avatar_key, u.created_at AS user_created_at`).
		Joins("INNER JOIN users AS u ON u.id = cm.user_id").
		Where("cm.class_id = ?", classID).
		Order(gorm.Expr("CASE WHEN cm.user_id = ? THEN 0 ELSE 1 END, cm.joined_at ASC, cm.id ASC", ownerUserID)).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// ListClassIDsForUser 列出该用户加入的所有班级 ID。realtime.Handler 在 WS
// upgrade 时调用,作为该连接的订阅范围。
func (r *Repo) ListClassIDsForUser(ctx context.Context, userID string) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).
		Model(&Member{}).
		Where("user_id = ?", userID).
		Pluck("class_id", &ids).Error
	return ids, err
}

func (r *Repo) ListMemberUserIDs(ctx context.Context, classID string) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).
		Model(&Member{}).
		Where("class_id = ?", classID).
		Pluck("user_id", &ids).Error
	return ids, err
}

func (r *Repo) DeleteMembership(ctx context.Context, classID, userID string) error {
	return r.db.WithContext(ctx).Where("class_id = ? AND user_id = ?", classID, userID).Delete(&Member{}).Error
}

// IsDuplicateKey 识别 MySQL 1062(unique 冲突),用于把 invite_code 撞库或
// (class_id, user_id) 重复加入翻成 409 业务错误。
func (r *Repo) IsDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	var me *mysql.MySQLError
	if errors.As(err, &me) && me.Number == 1062 {
		return true
	}
	return strings.Contains(err.Error(), "1062")
}
