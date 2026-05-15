package db

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func Connect(ctx context.Context, uri, dbName string) (*mongo.Database, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, nil); err != nil {
		return nil, err
	}
	return client.Database(dbName), nil
}

func EnsureIndexes(ctx context.Context, db *mongo.Database) error {
	if _, err := db.Collection("users").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		return err
	}

	if _, err := db.Collection("sessions").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "expires_at", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(0),
	}); err != nil {
		return err
	}

	if _, err := db.Collection("tokens").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(0),
		},
		{
			Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "kind", Value: 1}},
		},
	}); err != nil {
		return err
	}

	if _, err := db.Collection("solochat_conversations").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "updated_at", Value: -1}}},
	}); err != nil {
		return err
	}

	if _, err := db.Collection("uploaded_files").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "owner_user_id", Value: 1}},
	}); err != nil {
		return err
	}

	return nil
}

// MaybeDropLegacyGradingCollections 在 SOLOCHAT_MIGRATE_DROP_GRADING=true 时
// 把旧的批改专属集合一次性删掉。轮 4 重构后 grading 并入通用 chat 流,
// 这三张表不再使用。线上首次部署后,把 env 改回 false / 移除即可。
func MaybeDropLegacyGradingCollections(ctx context.Context, db *mongo.Database) error {
	if strings.ToLower(os.Getenv("SOLOCHAT_MIGRATE_DROP_GRADING")) != "true" {
		return nil
	}
	for _, name := range []string{
		"solochat_grading_tasks",
		"solochat_grading_task_files",
		"solochat_grading_annotations",
	} {
		if err := db.Collection(name).Drop(ctx); err != nil {
			return err
		}
		log.Printf("dropped legacy collection: %s", name)
	}
	return nil
}

// MaybeDropLegacyMessages 在 SOLOCHAT_MIGRATE_DROP_MESSAGES=true 时把
// 轮 6 之前的 solochat_messages 与 solochat_message_files 一次性删掉。
// 新流程下消息源(events / state / 附件绑定)全部在 mechhub-agent 的
// ADK MySQL session 里;Mongo 这两张表彻底不再使用。
func MaybeDropLegacyMessages(ctx context.Context, db *mongo.Database) error {
	if strings.ToLower(os.Getenv("SOLOCHAT_MIGRATE_DROP_MESSAGES")) != "true" {
		return nil
	}
	for _, name := range []string{
		"solochat_messages",
		"solochat_message_files",
	} {
		if err := db.Collection(name).Drop(ctx); err != nil {
			return err
		}
		log.Printf("dropped legacy collection: %s", name)
	}
	return nil
}
