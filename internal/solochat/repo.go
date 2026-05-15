package solochat

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var ErrNotFound = errors.New("solochat: not found")

// Repo 只剩 conversation 元数据(title/userID/时间戳)与附件元信息
// (uploaded_files 集合)。消息流(events / state / OCR 缓存)整体迁到
// ADK 的 MySQL,Go 通过 agent.Client.FetchMessages 拿翻译好的 DTO。
type Repo struct {
	conversations *mongo.Collection
	files         *mongo.Collection
}

func NewRepo(db *mongo.Database) *Repo {
	return &Repo{
		conversations: db.Collection("solochat_conversations"),
		files:         db.Collection("uploaded_files"),
	}
}

func (r *Repo) InsertConversation(ctx context.Context, c *Conversation) error {
	_, err := r.conversations.InsertOne(ctx, c)
	return err
}

func (r *Repo) ListConversations(ctx context.Context, userID bson.ObjectID) ([]Conversation, error) {
	opts := options.Find().SetSort(bson.D{{Key: "updated_at", Value: -1}})
	cur, err := r.conversations.Find(ctx, bson.M{"user_id": userID}, opts)
	if err != nil {
		return nil, err
	}
	var list []Conversation
	if err := cur.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (r *Repo) FindConversation(ctx context.Context, id, userID bson.ObjectID) (*Conversation, error) {
	var c Conversation
	err := r.conversations.FindOne(ctx, bson.M{"_id": id, "user_id": userID}).Decode(&c)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repo) UpdateConversationTitle(ctx context.Context, id, userID bson.ObjectID, title string) error {
	res, err := r.conversations.UpdateOne(
		ctx,
		bson.M{"_id": id, "user_id": userID},
		bson.M{"$set": bson.M{"title": title, "updated_at": time.Now()}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repo) TouchConversation(ctx context.Context, id bson.ObjectID) error {
	_, err := r.conversations.UpdateOne(
		ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{"updated_at": time.Now()}},
	)
	return err
}

func (r *Repo) DeleteConversation(ctx context.Context, id, userID bson.ObjectID) error {
	res, err := r.conversations.DeleteOne(ctx, bson.M{"_id": id, "user_id": userID})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repo) InsertFile(ctx context.Context, f *UploadedFile) error {
	_, err := r.files.InsertOne(ctx, f)
	return err
}

func (r *Repo) FindFile(ctx context.Context, id, ownerID bson.ObjectID) (*UploadedFile, error) {
	var f UploadedFile
	err := r.files.FindOne(ctx, bson.M{"_id": id, "owner_user_id": ownerID}).Decode(&f)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *Repo) FindFilesByIDs(ctx context.Context, ids []bson.ObjectID, ownerID bson.ObjectID) ([]UploadedFile, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	cur, err := r.files.Find(ctx, bson.M{"_id": bson.M{"$in": ids}, "owner_user_id": ownerID})
	if err != nil {
		return nil, err
	}
	var list []UploadedFile
	if err := cur.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}
