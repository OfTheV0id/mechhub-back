package channel

import (
	"context"
	"errors"
	"strings"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

var ErrNotFound = errors.New("channel: not found")

type Repo struct {
	db *gorm.DB
}

func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

// ============ 频道 ============

func (r *Repo) InsertChannel(ctx context.Context, c *Channel) error {
	return r.db.WithContext(ctx).Create(c).Error
}

func (r *Repo) FindChannel(ctx context.Context, id string) (*Channel, error) {
	var c Channel
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repo) ListChannelsByClass(ctx context.Context, classID string) ([]Channel, error) {
	var rows []Channel
	err := r.db.WithContext(ctx).
		Where("class_id = ?", classID).
		Order("position ASC, created_at ASC").
		Find(&rows).Error
	return rows, err
}

func (r *Repo) UpdateChannel(ctx context.Context, channelID string, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&Channel{}).Where("id = ?", channelID).Updates(updates).Error
}

// DeleteChannel 联带删 messages + attachments(DB 行)。OSS 文件清理由 service 调用方负责。
// 返回被删 attachments 的 OSS key 列表,让 service 异步删 OSS。
func (r *Repo) DeleteChannel(ctx context.Context, channelID string) ([]string, error) {
	var keys []string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Attachment{}).Where("channel_id = ?", channelID).Pluck("oss_key", &keys).Error; err != nil {
			return err
		}
		if err := tx.Where("channel_id = ?", channelID).Delete(&Attachment{}).Error; err != nil {
			return err
		}
		if err := tx.Where("channel_id = ?", channelID).Delete(&MessageReaction{}).Error; err != nil {
			return err
		}
		if err := tx.Where("channel_id = ?", channelID).Delete(&Message{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", channelID).Delete(&Channel{}).Error
	})
	return keys, err
}

// DeleteByClass 删除一个班级下的所有频道及其消息/附件。供 OnClassDeleted 调。
func (r *Repo) DeleteByClass(ctx context.Context, classID string) ([]string, error) {
	var keys []string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Attachment{}).
			Joins("INNER JOIN channels ON channels.id = channel_attachments.channel_id").
			Where("channels.class_id = ?", classID).
			Pluck("channel_attachments.oss_key", &keys).Error; err != nil {
			return err
		}
		// 直接 raw 删,避免 join delete 的 GORM 兼容问题
		if err := tx.Exec(`DELETE FROM channel_attachments WHERE channel_id IN (SELECT id FROM channels WHERE class_id = ?)`, classID).Error; err != nil {
			return err
		}
		if err := tx.Exec(`DELETE FROM channel_message_reactions WHERE class_id = ?`, classID).Error; err != nil {
			return err
		}
		if err := tx.Exec(`DELETE FROM channel_messages WHERE class_id = ?`, classID).Error; err != nil {
			return err
		}
		return tx.Where("class_id = ?", classID).Delete(&Channel{}).Error
	})
	return keys, err
}

// ============ 消息 ============

func (r *Repo) InsertMessage(ctx context.Context, m *Message) error {
	return r.db.WithContext(ctx).Create(m).Error
}

func (r *Repo) FindMessage(ctx context.Context, id string) (*Message, error) {
	var m Message
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *Repo) UpdateMessageContent(ctx context.Context, messageID, content string) error {
	return r.db.WithContext(ctx).
		Model(&Message{}).
		Where("id = ?", messageID).
		Updates(map[string]any{"content": content, "edited_at": gorm.Expr("NOW()")}).Error
}

// DeleteMessage 联带删该消息的 attachments + reactions(DB)。返回被删 OSS keys。
func (r *Repo) DeleteMessage(ctx context.Context, messageID string) ([]string, error) {
	var keys []string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Attachment{}).Where("message_id = ?", messageID).Pluck("oss_key", &keys).Error; err != nil {
			return err
		}
		if err := tx.Where("message_id = ?", messageID).Delete(&Attachment{}).Error; err != nil {
			return err
		}
		if err := tx.Where("message_id = ?", messageID).Delete(&MessageReaction{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", messageID).Delete(&Message{}).Error
	})
	return keys, err
}

// ============ 反应 ============

// AddReaction 幂等加一条反应:命中唯一索引(同人同 emoji 已存在)时静默成功。
func (r *Repo) AddReaction(ctx context.Context, reaction *MessageReaction) error {
	err := r.db.WithContext(ctx).Create(reaction).Error
	if err != nil && r.IsDuplicateKey(err) {
		return nil
	}
	return err
}

// RemoveReaction 删一条反应,返回是否真的删到行。
func (r *Repo) RemoveReaction(ctx context.Context, messageID, userID, emoji string) (bool, error) {
	res := r.db.WithContext(ctx).
		Where("message_id = ? AND user_id = ? AND emoji = ?", messageID, userID, emoji).
		Delete(&MessageReaction{})
	return res.RowsAffected > 0, res.Error
}

func (r *Repo) HasReaction(ctx context.Context, messageID, userID, emoji string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&MessageReaction{}).
		Where("message_id = ? AND user_id = ? AND emoji = ?", messageID, userID, emoji).
		Count(&count).Error
	return count > 0, err
}

// FindReactionsByMessageIDs 批量拉一组消息的全部反应,按 created_at 升序(用于稳定聚合顺序)。
func (r *Repo) FindReactionsByMessageIDs(ctx context.Context, messageIDs []string) ([]MessageReaction, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}
	var rows []MessageReaction
	err := r.db.WithContext(ctx).
		Where("message_id IN ?", messageIDs).
		Order("created_at ASC").
		Find(&rows).Error
	return rows, err
}

// ListMessagesPage 倒序拉一页消息(最新的先来)。before 是 cursor 消息 ID,
// 为空则从最新开始;limit 在外层 clamp 好。
// 返回值带 author 字段 join 用户表。
func (r *Repo) ListMessagesPage(ctx context.Context, channelID, before string, limit int) ([]MessageWithAuthor, error) {
	q := r.db.WithContext(ctx).
		Table("channel_messages AS m").
		Select(`m.*,
		        u.email AS author_email,
		        u.name AS author_name,
		        u.role AS author_role,
		        u.avatar_key AS author_avatar_key,
		        u.created_at AS author_created_at`).
		Joins("INNER JOIN users AS u ON u.id = m.author_user_id").
		Where("m.channel_id = ?", channelID)

	if before != "" {
		// 拿 before 消息的 created_at,然后只取更早的(避免依赖 UUID 顺序)
		var beforeMsg Message
		if err := r.db.WithContext(ctx).Where("id = ?", before).First(&beforeMsg).Error; err == nil {
			q = q.Where("(m.created_at < ? OR (m.created_at = ? AND m.id < ?))", beforeMsg.CreatedAt, beforeMsg.CreatedAt, before)
		}
		// before 找不到时:静默退化为"取最新"
	}

	var rows []MessageWithAuthor
	err := q.Order("m.created_at DESC, m.id DESC").Limit(limit).Scan(&rows).Error
	return rows, err
}

// ============ 附件 ============

func (r *Repo) InsertAttachment(ctx context.Context, a *Attachment) error {
	return r.db.WithContext(ctx).Create(a).Error
}

func (r *Repo) FindAttachment(ctx context.Context, id string) (*Attachment, error) {
	var a Attachment
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *Repo) FindAttachmentsByIDs(ctx context.Context, ids []string) ([]Attachment, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []Attachment
	err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error
	return rows, err
}

// DeleteAttachmentsByIDs 按 id 直接删附件行。供分享流程中途失败时回滚已复制的附件。
func (r *Repo) DeleteAttachmentsByIDs(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Where("id IN ?", ids).Delete(&Attachment{}).Error
}

func (r *Repo) FindAttachmentsByMessageIDs(ctx context.Context, messageIDs []string) ([]Attachment, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}
	var rows []Attachment
	err := r.db.WithContext(ctx).Where("message_id IN ?", messageIDs).Find(&rows).Error
	return rows, err
}

// BindAttachmentsToMessage 把一批 attachment_id 的 message_id 全部 SET 成 newMsgID。
// 用在 SendMessage 时把已上传的 channel 附件绑到刚建好的 message 上。
func (r *Repo) BindAttachmentsToMessage(ctx context.Context, attachmentIDs []string, channelID, uploaderID, messageID string) error {
	if len(attachmentIDs) == 0 {
		return nil
	}
	res := r.db.WithContext(ctx).
		Model(&Attachment{}).
		Where("id IN ? AND channel_id = ? AND uploader_id = ? AND message_id IS NULL", attachmentIDs, channelID, uploaderID).
		Update("message_id", messageID)
	if res.Error != nil {
		return res.Error
	}
	if int(res.RowsAffected) != len(attachmentIDs) {
		return ErrNotFound
	}
	return nil
}

// IsDuplicateKey MySQL 1062 unique 冲突。
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
