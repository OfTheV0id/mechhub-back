package db

import (
	"context"
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

	if _, err := db.Collection("solochat_messages").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "conversation_id", Value: 1}, {Key: "created_at", Value: 1}},
	}); err != nil {
		return err
	}

	if _, err := db.Collection("uploaded_files").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "owner_user_id", Value: 1}},
	}); err != nil {
		return err
	}

	if _, err := db.Collection("solochat_message_files").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "message_id", Value: 1}},
	}); err != nil {
		return err
	}

	if _, err := db.Collection("solochat_grading_tasks").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "conversation_id", Value: 1}, {Key: "created_at", Value: -1}}},
		{Keys: bson.D{{Key: "user_id", Value: 1}}},
		{Keys: bson.D{{Key: "status", Value: 1}}},
	}); err != nil {
		return err
	}

	if _, err := db.Collection("solochat_grading_task_files").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "task_id", Value: 1}},
	}); err != nil {
		return err
	}

	if _, err := db.Collection("solochat_grading_annotations").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "task_id", Value: 1}},
	}); err != nil {
		return err
	}

	return nil
}
