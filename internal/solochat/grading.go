package solochat

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"

	"mechhub-back/internal/agent"
)

func (s *Service) CreateGradingTaskStream(c *gin.Context, conversationID, userID bson.ObjectID, req CreateGradingTaskReq) {
	ctx := c.Request.Context()
	w := newNDJSON(c)

	conv, err := s.repo.FindConversation(ctx, conversationID, userID)
	if err != nil {
		w.write(StreamEvent{Type: StreamAssistantError, Error: "对话不存在"})
		return
	}

	attachmentIDs, err := parseAttachmentIDs(req.Attachments)
	if err != nil {
		w.write(StreamEvent{Type: StreamAssistantError, Error: "附件 ID 无效"})
		return
	}
	files, err := s.repo.FindFilesByIDs(ctx, attachmentIDs, userID)
	if err != nil {
		w.write(StreamEvent{Type: StreamAssistantError, Error: err.Error()})
		return
	}
	if len(files) != len(attachmentIDs) {
		w.write(StreamEvent{Type: StreamAssistantError, Error: "部分附件不存在或无权访问"})
		return
	}

	count, _ := s.repo.CountConversationMessages(ctx, conversationID)
	isFirst := count == 0

	now := time.Now()
	userMsg := &Message{
		ID:             bson.NewObjectID(),
		ConversationID: conversationID,
		Role:           RoleUser,
		Type:           MessageTypeGrading,
		Content:        req.PromptText,
		Status:         MessageStatusCompleted,
		CreatedAt:      now,
	}
	if err := s.repo.InsertMessage(ctx, userMsg); err != nil {
		w.write(StreamEvent{Type: StreamAssistantError, Error: err.Error()})
		return
	}
	fileIDs := make([]bson.ObjectID, len(files))
	for i, f := range files {
		fileIDs[i] = f.ID
	}
	_ = s.repo.BindMessageFiles(ctx, userMsg.ID, fileIDs)
	userDTO := toMessageDTO(userMsg)
	w.write(StreamEvent{Type: StreamUserInput, Message: &userDTO})

	task := &GradingTask{
		ID:                 bson.NewObjectID(),
		ConversationID:     conversationID,
		UserID:             userID,
		MessageID:          userMsg.ID,
		PromptText:         req.PromptText,
		Status:             TaskStatusPending,
		SelectedImageCount: len(files),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := s.repo.InsertGradingTask(ctx, task); err != nil {
		w.write(StreamEvent{Type: StreamAssistantError, Error: err.Error()})
		return
	}
	_ = s.repo.BindGradingTaskFiles(ctx, task.ID, fileIDs, TaskFileRoleImage)

	taskDTO := toGradingTaskDTO(task)
	w.writeRaw(map[string]any{
		"type":    StreamGradingStart,
		"task":    taskDTO,
		"message": userDTO,
	})

	if isFirst {
		title := autoTitle(req.PromptText)
		if title != "" {
			_ = s.repo.UpdateConversationTitle(context.Background(), conversationID, userID, title)
			conv.Title = title
			conv.UpdatedAt = time.Now()
			convDTO := toConversationDTO(conv)
			w.write(StreamEvent{Type: StreamConversationName, Conversation: &convDTO})
		}
	} else {
		_ = s.repo.TouchConversation(ctx, conversationID)
	}

	go s.runGradingTask(task, files)
}

func (s *Service) RetryGradingTask(ctx context.Context, taskID, userID bson.ObjectID) (*GradingTask, error) {
	task, err := s.repo.FindGradingTask(ctx, taskID, userID)
	if err != nil {
		return nil, err
	}
	files, err := s.collectTaskImages(ctx, taskID, userID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	_ = s.repo.UpdateGradingTask(ctx, taskID, bson.M{
		"status":        TaskStatusPending,
		"error_message": "",
	})
	task.Status = TaskStatusPending
	task.UpdatedAt = now
	go s.runGradingTask(task, files)
	return task, nil
}

func (s *Service) collectTaskImages(ctx context.Context, taskID, userID bson.ObjectID) ([]UploadedFile, error) {
	cur, err := s.repo.gradingTaskFiles.Find(ctx, bson.M{"task_id": taskID, "role": TaskFileRoleImage})
	if err != nil {
		return nil, err
	}
	var binds []GradingTaskFile
	if err := cur.All(ctx, &binds); err != nil {
		return nil, err
	}
	ids := make([]bson.ObjectID, len(binds))
	for i, b := range binds {
		ids[i] = b.FileID
	}
	return s.repo.FindFilesByIDs(ctx, ids, userID)
}

func (s *Service) runGradingTask(task *GradingTask, files []UploadedFile) {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.Agent.Timeout)
	defer cancel()

	defer s.hub.Close(task.ID.Hex())

	if err := s.repo.UpdateGradingTask(ctx, task.ID, bson.M{"status": TaskStatusProcessing}); err != nil {
		s.failTask(ctx, task, err.Error())
		return
	}
	task.Status = TaskStatusProcessing
	s.hub.Publish(task.ID.Hex(), GradingEvent{Type: StreamGradingStatus, Task: ptrToGradingTaskDTO(task)})

	images, closers, err := s.openAttachmentsForAgent(ctx, files)
	if err != nil {
		closeAll(closers)
		s.failTask(ctx, task, err.Error())
		return
	}

	events, err := s.agent.Chat(ctx, agent.ChatRequest{
		SessionID: task.ID.Hex(),
		Message:   task.PromptText,
		Images:    images,
	})
	closeAll(closers)
	if err != nil {
		s.failTask(ctx, task, err.Error())
		return
	}

	var comment strings.Builder
	var streamErr string
	for ev := range events {
		switch ev.Type {
		case agent.EventText:
			comment.WriteString(ev.Content)
		case agent.EventToolDone:
			if ev.Tool == "grade_with_ocr" && ev.Summary != "" {
				if score, ok := parseOverallScore(ev.Summary); ok {
					task.OverallScore = &score
				}
			}
		case agent.EventError:
			streamErr = ev.Message
		}
	}

	if streamErr != "" {
		s.failTask(ctx, task, streamErr)
		return
	}

	task.OverallComment = strings.TrimSpace(comment.String())
	task.Status = TaskStatusCompleted
	update := bson.M{
		"status":          TaskStatusCompleted,
		"overall_comment": task.OverallComment,
	}
	if task.OverallScore != nil {
		update["overall_score"] = *task.OverallScore
	}
	_ = s.repo.UpdateGradingTask(ctx, task.ID, update)
	s.hub.Publish(task.ID.Hex(), GradingEvent{Type: StreamGradingStatus, Task: ptrToGradingTaskDTO(task)})
}

func (s *Service) failTask(ctx context.Context, task *GradingTask, msg string) {
	task.Status = TaskStatusFailed
	task.ErrorMessage = msg
	_ = s.repo.UpdateGradingTask(ctx, task.ID, bson.M{
		"status":        TaskStatusFailed,
		"error_message": msg,
	})
	s.hub.Publish(task.ID.Hex(), GradingEvent{Type: StreamGradingStatus, Task: ptrToGradingTaskDTO(task)})
}

func ptrToGradingTaskDTO(t *GradingTask) *GradingTaskDTO {
	d := toGradingTaskDTO(t)
	return &d
}

func parseAttachmentIDs(ss []string) ([]bson.ObjectID, error) {
	out := make([]bson.ObjectID, len(ss))
	for i, s := range ss {
		id, err := bson.ObjectIDFromHex(s)
		if err != nil {
			return nil, err
		}
		out[i] = id
	}
	return out, nil
}

func parseOverallScore(summary string) (float64, bool) {
	const prefix = "overallScore="
	idx := strings.Index(summary, prefix)
	if idx < 0 {
		return 0, false
	}
	rest := summary[idx+len(prefix):]
	end := strings.IndexAny(rest, ", ")
	if end < 0 {
		end = len(rest)
	}
	val, err := strconv.ParseFloat(strings.TrimSpace(rest[:end]), 64)
	if err != nil {
		return 0, false
	}
	return val, true
}
