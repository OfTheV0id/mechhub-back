package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

var ErrNotFound = errors.New("session not found")

type Store struct {
	col *mongo.Collection
	ttl time.Duration
}

func NewStore(db *mongo.Database, ttl time.Duration) *Store {
	return &Store{col: db.Collection("sessions"), ttl: ttl}
}

func (s *Store) New(ctx context.Context, userID bson.ObjectID) (*Session, error) {
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	sess := &Session{
		ID:        id,
		UserID:    userID,
		ExpiresAt: now.Add(s.ttl),
		CreatedAt: now,
	}
	if _, err := s.col.InsertOne(ctx, sess); err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *Store) Get(ctx context.Context, id string) (*Session, error) {
	var sess Session
	err := s.col.FindOne(ctx, bson.M{"_id": id}).Decode(&sess)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if time.Now().After(sess.ExpiresAt) {
		return nil, ErrNotFound
	}
	return &sess, nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	_, err := s.col.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (s *Store) DeleteByUser(ctx context.Context, userID bson.ObjectID) error {
	_, err := s.col.DeleteMany(ctx, bson.M{"user_id": userID})
	return err
}

func randomID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
