package session

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type Session struct {
	ID        string        `bson:"_id"`
	UserID    bson.ObjectID `bson:"user_id"`
	ExpiresAt time.Time     `bson:"expires_at"`
	CreatedAt time.Time     `bson:"created_at"`
}
