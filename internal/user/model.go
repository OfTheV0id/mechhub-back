package user

import "time"

type User struct {
	ID           string    `gorm:"primaryKey;type:char(36)"`
	Email        string    `gorm:"uniqueIndex;type:varchar(255);not null"`
	PasswordHash string    `gorm:"type:varchar(72)"`
	Name         string    `gorm:"type:varchar(50)"`
	Role         string    `gorm:"type:varchar(16);index"`
	AvatarKey    string    `gorm:"type:varchar(255)"`
	GoogleSub    string    `gorm:"type:varchar(64);index"`
	Verified     bool      `gorm:"not null;default:false"`
	CreatedAt    time.Time `gorm:"not null"`
}

const (
	UserRoleStudent = "student"
	UserRoleTeacher = "teacher"

	TokenKindVerify          = "verify"
	TokenKindReset           = "reset"
	TokenKindTeacherApproval = "teacher_approval"
)

type Token struct {
	ID        string    `gorm:"primaryKey;type:varchar(64)"`
	UserID    string    `gorm:"type:char(36);not null;index:idx_user_kind,priority:1"`
	Kind      string    `gorm:"type:varchar(32);not null;index:idx_user_kind,priority:2"`
	ExpiresAt time.Time `gorm:"not null;index"`
}

type RegisterReq struct {
	Email    string `json:"email"    binding:"required,email"`
	Password string `json:"password" binding:"required,min=8,max=128"`
	Name     string `json:"name"     binding:"required,min=1,max=50"`
	Role     string `json:"role"     binding:"required,oneof=student teacher"`
}

type RegisterResp struct {
	Message  string `json:"message"`
	Role     string `json:"role"`
	Verified bool   `json:"verified"`
}

type UpdateProfileReq struct {
	Name string `json:"name" binding:"required,min=1,max=50"`
}

type LoginReq struct {
	Email    string `json:"email"    binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type ForgotPasswordReq struct {
	Email string `json:"email" binding:"required,email"`
}

type ResetPasswordReq struct {
	Token    string `json:"token"    binding:"required"`
	Password string `json:"password" binding:"required,min=8,max=128"`
}

type ChangePasswordReq struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=8,max=128"`
}

type MeResp struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	AvatarURL string `json:"avatar_url"`
	Verified  bool   `json:"verified"`
	CreatedAt string `json:"created_at"`
}

type UploadAvatarResp struct {
	AvatarURL string `json:"avatar_url"`
}
