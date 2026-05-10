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
	AvatarKey    string        `bson:"avatar_key"`
	Verified     bool          `bson:"verified"`
	CreatedAt    time.Time     `bson:"created_at"`
}

const (
	TokenKindVerify = "verify"
	TokenKindReset  = "reset"
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
	AvatarURL string `json:"avatar_url"`
	Verified  bool   `json:"verified"`
}

type UploadAvatarResp struct {
	AvatarURL string `json:"avatar_url"`
}
