package session

import "time"

// Session 是 cookie session;表名 user_sessions 避免和 ADK Go 的 sessions 表冲突。
type Session struct {
	ID        string    `gorm:"primaryKey;type:varchar(64)"`
	UserID    string    `gorm:"type:char(36);not null;index"`
	ExpiresAt time.Time `gorm:"not null;index"`
	CreatedAt time.Time `gorm:"not null"`
}

func (Session) TableName() string { return "user_sessions" }
