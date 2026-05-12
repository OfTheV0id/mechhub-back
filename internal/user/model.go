package user

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type User struct {
	ID           bson.ObjectID `bson:"_id"`
	Email        string        `bson:"email"`
	PasswordHash string        `bson:"password_hash"`
	Name         string        `bson:"name"`
	Role         string        `bson:"role"`
	AvatarKey    string        `bson:"avatar_key"`
	GoogleSub    string        `bson:"google_sub"`
	Verified     bool          `bson:"verified"`
	CreatedAt    time.Time     `bson:"created_at"`
}

const (
	UserRoleStudent = "student"
	UserRoleTeacher = "teacher"

	TokenKindVerify          = "verify"
	TokenKindReset           = "reset"
	TokenKindTeacherApproval = "teacher_approval"
)

type Token struct {
	ID        string        `bson:"_id"`
	UserID    bson.ObjectID `bson:"user_id"`
	Kind      string        `bson:"kind"`
	ExpiresAt time.Time     `bson:"expires_at"`
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

type LoginResp struct {
	Message  string `json:"message"`
	UserData MeResp `json:"userdata"`
}

type UpdateProfileResp struct {
	Message  string `json:"message"`
	UserData MeResp `json:"userdata"`
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
