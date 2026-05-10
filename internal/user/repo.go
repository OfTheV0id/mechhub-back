package user

import (
	"context"
	"errors"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

var ErrNotFound = errors.New("user not found")

type Repo struct {
	users  *mongo.Collection
	tokens *mongo.Collection
}

func NewRepo(db *mongo.Database) *Repo {
	return &Repo{
		users:  db.Collection("users"),
		tokens: db.Collection("tokens"),
	}
}

func (r *Repo) Insert(ctx context.Context, u *User) error {
	_, err := r.users.InsertOne(ctx, u)
	return err
}

func (r *Repo) FindByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	err := r.users.FindOne(ctx, bson.M{"email": email}).Decode(&u)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *Repo) FindByID(ctx context.Context, id bson.ObjectID) (*User, error) {
	var u User
	err := r.users.FindOne(ctx, bson.M{"_id": id}).Decode(&u)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *Repo) SetVerified(ctx context.Context, id bson.ObjectID) error {
	_, err := r.users.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"verified": true}})
	return err
}

func (r *Repo) UpdatePassword(ctx context.Context, id bson.ObjectID, hash string) error {
	_, err := r.users.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"password_hash": hash}})
	return err
}

func (r *Repo) IsDuplicateKey(err error) bool {
	return mongo.IsDuplicateKeyError(err)
}

func (r *Repo) InsertToken(ctx context.Context, t *Token) error {
	_, err := r.tokens.InsertOne(ctx, t)
	return err
}

func (r *Repo) FindAndDeleteToken(ctx context.Context, id, kind string) (*Token, error) {
	var t Token
	err := r.tokens.FindOneAndDelete(ctx, bson.M{"_id": id, "kind": kind}).Decode(&t)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *Repo) DeleteUserTokens(ctx context.Context, userID bson.ObjectID, kind string) error {
	_, err := r.tokens.DeleteMany(ctx, bson.M{"user_id": userID, "kind": kind})
	return err
}
