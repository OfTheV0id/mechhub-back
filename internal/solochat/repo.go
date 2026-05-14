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

type Repo struct {
	conversations    *mongo.Collection
	messages         *mongo.Collection
	files            *mongo.Collection
	messageFiles     *mongo.Collection
	gradingTasks     *mongo.Collection
	gradingTaskFiles *mongo.Collection
	annotations      *mongo.Collection
}

func NewRepo(db *mongo.Database) *Repo {
	return &Repo{
		conversations:    db.Collection("solochat_conversations"),
		messages:         db.Collection("solochat_messages"),
		files:            db.Collection("uploaded_files"),
		messageFiles:     db.Collection("solochat_message_files"),
		gradingTasks:     db.Collection("solochat_grading_tasks"),
		gradingTaskFiles: db.Collection("solochat_grading_task_files"),
		annotations:      db.Collection("solochat_grading_annotations"),
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
	_, err = r.messages.DeleteMany(ctx, bson.M{"conversation_id": id})
	return err
}

func (r *Repo) CountConversationMessages(ctx context.Context, conversationID bson.ObjectID) (int64, error) {
	return r.messages.CountDocuments(ctx, bson.M{"conversation_id": conversationID})
}

func (r *Repo) InsertMessage(ctx context.Context, m *Message) error {
	_, err := r.messages.InsertOne(ctx, m)
	return err
}

func (r *Repo) ListMessages(ctx context.Context, conversationID bson.ObjectID) ([]Message, error) {
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}})
	cur, err := r.messages.Find(ctx, bson.M{"conversation_id": conversationID}, opts)
	if err != nil {
		return nil, err
	}
	var list []Message
	if err := cur.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (r *Repo) ListRecentMessages(ctx context.Context, conversationID bson.ObjectID, limit int) ([]Message, error) {
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}).SetLimit(int64(limit))
	cur, err := r.messages.Find(ctx, bson.M{"conversation_id": conversationID}, opts)
	if err != nil {
		return nil, err
	}
	var list []Message
	if err := cur.All(ctx, &list); err != nil {
		return nil, err
	}
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	return list, nil
}

func (r *Repo) UpdateMessageStatus(ctx context.Context, id bson.ObjectID, status, content string) error {
	update := bson.M{"$set": bson.M{"status": status}}
	if content != "" {
		update["$set"].(bson.M)["content"] = content
	}
	_, err := r.messages.UpdateOne(ctx, bson.M{"_id": id}, update)
	return err
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

func (r *Repo) BindMessageFiles(ctx context.Context, messageID bson.ObjectID, fileIDs []bson.ObjectID) error {
	if len(fileIDs) == 0 {
		return nil
	}
	docs := make([]any, len(fileIDs))
	for i, fid := range fileIDs {
		docs[i] = MessageFile{ID: bson.NewObjectID(), MessageID: messageID, FileID: fid}
	}
	_, err := r.messageFiles.InsertMany(ctx, docs)
	return err
}

func (r *Repo) InsertGradingTask(ctx context.Context, t *GradingTask) error {
	_, err := r.gradingTasks.InsertOne(ctx, t)
	return err
}

func (r *Repo) FindGradingTask(ctx context.Context, id, userID bson.ObjectID) (*GradingTask, error) {
	var t GradingTask
	err := r.gradingTasks.FindOne(ctx, bson.M{"_id": id, "user_id": userID}).Decode(&t)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *Repo) ListGradingTasks(ctx context.Context, conversationID bson.ObjectID) ([]GradingTask, error) {
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})
	cur, err := r.gradingTasks.Find(ctx, bson.M{"conversation_id": conversationID}, opts)
	if err != nil {
		return nil, err
	}
	var list []GradingTask
	if err := cur.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (r *Repo) UpdateGradingTask(ctx context.Context, id bson.ObjectID, update bson.M) error {
	update["updated_at"] = time.Now()
	_, err := r.gradingTasks.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": update})
	return err
}

func (r *Repo) MarkAllProcessingFailed(ctx context.Context) error {
	_, err := r.gradingTasks.UpdateMany(
		ctx,
		bson.M{"status": TaskStatusProcessing},
		bson.M{"$set": bson.M{
			"status":        TaskStatusFailed,
			"error_message": "服务重启,任务中断",
			"updated_at":    time.Now(),
		}},
	)
	return err
}

func (r *Repo) BindGradingTaskFiles(ctx context.Context, taskID bson.ObjectID, fileIDs []bson.ObjectID, role string) error {
	if len(fileIDs) == 0 {
		return nil
	}
	docs := make([]any, len(fileIDs))
	for i, fid := range fileIDs {
		docs[i] = GradingTaskFile{ID: bson.NewObjectID(), TaskID: taskID, FileID: fid, Role: role}
	}
	_, err := r.gradingTaskFiles.InsertMany(ctx, docs)
	return err
}

func (r *Repo) ListAnnotations(ctx context.Context, taskID bson.ObjectID) ([]GradingAnnotation, error) {
	cur, err := r.annotations.Find(ctx, bson.M{"task_id": taskID})
	if err != nil {
		return nil, err
	}
	var list []GradingAnnotation
	if err := cur.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (r *Repo) InsertAnnotations(ctx context.Context, anns []GradingAnnotation) error {
	if len(anns) == 0 {
		return nil
	}
	docs := make([]any, len(anns))
	for i := range anns {
		docs[i] = anns[i]
	}
	_, err := r.annotations.InsertMany(ctx, docs)
	return err
}
