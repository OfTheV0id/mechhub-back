package class

import (
	"context"
	"time"

	"mechhub-back/internal/user"
)

type Class struct {
	ID          string    `gorm:"primaryKey;type:char(36)"`
	Name        string    `gorm:"type:varchar(120);not null"`
	Description string    `gorm:"type:varchar(1000);not null;default:''"`
	OwnerUserID string    `gorm:"type:char(36);not null;index:idx_classes_owner,priority:1"`
	Status      string    `gorm:"type:varchar(16);not null;default:'active'"`
	AvatarKey   string    `gorm:"type:varchar(255)"`

	// 邀请链接(第 2 阶段):一班一 token,可重生成,可设过期,可禁用。
	// InviteCode 已废弃 —— 列保留在 DB 直到 ALTER 一次性 drop,但代码不再读写。
	InviteToken     string     `gorm:"type:varchar(64);uniqueIndex"`
	InviteExpiresAt *time.Time `gorm:""`
	InviteDisabled  bool       `gorm:"not null;default:false"`

	CreatedAt time.Time `gorm:"not null;index:idx_classes_owner,priority:2,sort:desc"`
}

func (Class) TableName() string { return "classes" }

type Member struct {
	ID       string    `gorm:"primaryKey;type:char(36)"`
	ClassID  string    `gorm:"type:char(36);not null;uniqueIndex:idx_class_user,priority:1;index:idx_member_class"`
	UserID   string    `gorm:"type:char(36);not null;uniqueIndex:idx_class_user,priority:2;index:idx_member_user"`
	Role     string    `gorm:"type:varchar(16);not null"`
	JoinedAt time.Time `gorm:"not null"`
}

func (Member) TableName() string { return "class_members" }

const (
	StatusActive   = "active"
	StatusArchived = "archived"

	RoleTeacher = user.UserRoleTeacher
	RoleStudent = user.UserRoleStudent

	// 邀请 token 默认有效期。可被 RegenerateInvite 的 expires_at 参数覆盖;
	// 显式传 null 表示永不过期。
	DefaultInviteTTL = 30 * 24 * time.Hour
)

// ChannelHook 让 channel.Service 在不被 class 直接 import 的前提下,
// 接收"班级新建/删除"事件 —— Go 接口隐式实现,channel.Service 的方法签名匹配即可。
type ChannelHook interface {
	OnClassCreated(ctx context.Context, classID, ownerID string) error
	OnClassDeleted(ctx context.Context, classID string) error
}

// 班级详情 + 当前用户成员关系 join 行
type ClassWithRole struct {
	Class
	MembershipRole string `gorm:"column:membership_role"`
}

type MemberWithUser struct {
	Member
	Email       string    `gorm:"column:email"`
	UserName    string    `gorm:"column:user_name"`
	UserRole    string    `gorm:"column:user_role"`
	AvatarKey   string    `gorm:"column:user_avatar_key"`
	UserCreated time.Time `gorm:"column:user_created_at"`
}

// ============ Request DTOs ============

type CreateClassReq struct {
	Name        string `json:"name"        binding:"required,min=1,max=120"`
	Description string `json:"description" binding:"max=1000"`
}

type UpdateClassReq struct {
	Name        *string `json:"name,omitempty"        binding:"omitempty,min=1,max=120"`
	Description *string `json:"description,omitempty" binding:"omitempty,max=1000"`
	Status      *string `json:"status,omitempty"      binding:"omitempty,oneof=active archived"`
}

// RegenerateInviteReq POST /classes/:id/invite/regenerate 的可选 body。
// `expires_at` 为 null 或缺省 → 30 天后过期;明确 ISO 时间字符串 → 用该时间;
// 空字符串 "" → 永不过期。
type RegenerateInviteReq struct {
	ExpiresAt *string `json:"expires_at,omitempty"`
}

type UpdateMemberRoleReq struct {
	Role string `json:"role" binding:"required,oneof=teacher student"`
}

// ============ Response DTOs ============

type ClassListItem struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Status         string `json:"status"`
	MembershipRole string `json:"membership_role"`
	IsOwner        bool   `json:"is_owner"`
	AvatarURL      string `json:"avatar_url,omitempty"`
}

type ClassDetail struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	OwnerUserID    string `json:"owner_user_id"`
	Status         string `json:"status"`
	MembershipRole string `json:"membership_role"`
	IsOwner        bool   `json:"is_owner"`
	AvatarURL      string `json:"avatar_url,omitempty"`
	CreatedAt      string `json:"created_at"`
}

type MemberDTO struct {
	ID       string         `json:"id"`
	ClassID  string         `json:"class_id"`
	Role     string         `json:"role"`
	IsOwner  bool           `json:"is_owner"`
	JoinedAt string         `json:"joined_at"`
	User     MemberUserInfo `json:"user"`
}

type MemberUserInfo struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// InviteInfo owner 拿当前 invite 状态用
type InviteInfo struct {
	Token     string  `json:"token"`
	ShareURL  string  `json:"share_url"`
	ExpiresAt *string `json:"expires_at"`
	Disabled  bool    `json:"disabled"`
	Expired   bool    `json:"expired"`
}

// InvitePreview 任意登录用户 GET /classes/invite/:token 的响应
type InvitePreview struct {
	Class    ClassDetail `json:"class"`
	Joined   bool        `json:"joined"`
	Expired  bool        `json:"expired"`
	Disabled bool        `json:"disabled"`
}
