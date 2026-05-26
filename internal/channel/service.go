package channel

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"mime/multipart"
	stdmime "mime"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"mechhub-back/internal/class"
	"mechhub-back/internal/config"
	"mechhub-back/internal/realtime"
	"mechhub-back/internal/storage"
	"mechhub-back/internal/user"
)

var (
	ErrForbidden            = errors.New("channel: forbidden")
	ErrDefaultChannelLocked = errors.New("channel: default channel locked")
	ErrAttachmentInvalid    = errors.New("channel: attachment invalid")
	ErrTooManyAttachments   = errors.New("channel: too many attachments")
	ErrAttachmentTooLarge   = errors.New("channel: attachment too large")
)

type Service struct {
	repo      *Repo
	classRepo *class.Repo
	userRepo  *user.Repo
	oss       *storage.OSS
	hub       *realtime.Hub
	cfg       *config.Config
}

func NewService(repo *Repo, classRepo *class.Repo, userRepo *user.Repo, oss *storage.OSS, hub *realtime.Hub, cfg *config.Config) *Service {
	return &Service{repo: repo, classRepo: classRepo, userRepo: userRepo, oss: oss, hub: hub, cfg: cfg}
}

// ============ class.ChannelHook 实现 ============

// OnClassCreated 班级建好后调,自动建 #general 默认频道。
func (s *Service) OnClassCreated(ctx context.Context, classID, ownerID string) error {
	now := time.Now()
	c := &Channel{
		ID:          uuid.NewString(),
		ClassID:     classID,
		Name:        DefaultChannelName,
		Description: "",
		Topic:       DefaultChannelTopic,
		IsDefault:   true,
		Position:    0,
		CreatedBy:   ownerID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	return s.repo.InsertChannel(ctx, c)
}

// OnClassDeleted 班级删除时调,联带删该班所有频道 + 消息 + 附件 + OSS 文件。
func (s *Service) OnClassDeleted(ctx context.Context, classID string) error {
	keys, err := s.repo.DeleteByClass(ctx, classID)
	if err != nil {
		return err
	}
	for _, k := range keys {
		if k == "" {
			continue
		}
		_ = s.oss.Delete(context.Background(), k)
	}
	return nil
}

// ============ 频道 CRUD ============

func (s *Service) ListChannels(ctx context.Context, classID, userID string) ([]ChannelDTO, error) {
	if _, err := s.classRepo.FindMembership(ctx, classID, userID); err != nil {
		return nil, err // ErrNotFound → handler 翻 403/404
	}
	rows, err := s.repo.ListChannelsByClass(ctx, classID)
	if err != nil {
		return nil, err
	}
	out := make([]ChannelDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toChannelDTO(&rows[i]))
	}
	return out, nil
}

func (s *Service) GetChannel(ctx context.Context, classID, channelID, userID string) (*ChannelDTO, error) {
	if _, err := s.classRepo.FindMembership(ctx, classID, userID); err != nil {
		return nil, err
	}
	ch, err := s.repo.FindChannel(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if ch.ClassID != classID {
		return nil, ErrNotFound
	}
	d := toChannelDTO(ch)
	return &d, nil
}

func (s *Service) CreateChannel(ctx context.Context, classID, userID string, req CreateChannelReq) (*ChannelDTO, error) {
	if err := s.requireChannelAdmin(ctx, classID, userID); err != nil {
		return nil, err
	}
	now := time.Now()
	c := &Channel{
		ID:          uuid.NewString(),
		ClassID:     classID,
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
		Topic:       strings.TrimSpace(req.Topic),
		IsDefault:   false,
		CreatedBy:   userID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if req.Position != nil {
		c.Position = *req.Position
	}
	if err := s.repo.InsertChannel(ctx, c); err != nil {
		if s.repo.IsDuplicateKey(err) {
			return nil, errors.New("该频道名已存在")
		}
		return nil, err
	}

	d := toChannelDTO(c)
	go s.hub.BroadcastToClass(classID, realtime.ClassInvalidate{
		Type:    realtime.FrameClassInvalidate,
		ClassID: classID,
		Targets: []string{realtime.TargetChannels},
		Reason:  realtime.ReasonChannelCreated,
	})
	return &d, nil
}

func (s *Service) UpdateChannel(ctx context.Context, classID, channelID, userID string, req UpdateChannelReq) (*ChannelDTO, error) {
	if err := s.requireChannelAdmin(ctx, classID, userID); err != nil {
		return nil, err
	}
	ch, err := s.repo.FindChannel(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if ch.ClassID != classID {
		return nil, ErrNotFound
	}

	updates := make(map[string]any)
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if ch.IsDefault && name != ch.Name {
			return nil, ErrDefaultChannelLocked
		}
		updates["name"] = name
	}
	if req.Description != nil {
		updates["description"] = strings.TrimSpace(*req.Description)
	}
	if req.Topic != nil {
		updates["topic"] = strings.TrimSpace(*req.Topic)
	}
	if req.Position != nil {
		updates["position"] = *req.Position
	}
	if len(updates) == 0 {
		d := toChannelDTO(ch)
		return &d, nil
	}
	updates["updated_at"] = time.Now()

	if err := s.repo.UpdateChannel(ctx, channelID, updates); err != nil {
		if s.repo.IsDuplicateKey(err) {
			return nil, errors.New("该频道名已存在")
		}
		return nil, err
	}
	updated, err := s.repo.FindChannel(ctx, channelID)
	if err != nil {
		return nil, err
	}
	d := toChannelDTO(updated)
	go s.hub.BroadcastToClass(classID, realtime.ClassInvalidate{
		Type:    realtime.FrameClassInvalidate,
		ClassID: classID,
		Targets: []string{realtime.TargetChannels},
		Reason:  realtime.ReasonChannelUpdated,
	})
	return &d, nil
}

func (s *Service) DeleteChannel(ctx context.Context, classID, channelID, userID string) error {
	if err := s.requireChannelAdmin(ctx, classID, userID); err != nil {
		return err
	}
	ch, err := s.repo.FindChannel(ctx, channelID)
	if err != nil {
		return err
	}
	if ch.ClassID != classID {
		return ErrNotFound
	}
	if ch.IsDefault {
		return ErrDefaultChannelLocked
	}
	keys, err := s.repo.DeleteChannel(ctx, channelID)
	if err != nil {
		return err
	}
	for _, k := range keys {
		if k != "" {
			_ = s.oss.Delete(context.Background(), k)
		}
	}
	go s.hub.BroadcastToClass(classID, realtime.ClassInvalidate{
		Type:    realtime.FrameClassInvalidate,
		ClassID: classID,
		Targets: []string{realtime.TargetChannels},
		Reason:  realtime.ReasonChannelDeleted,
	})
	return nil
}

// ============ 消息 ============

func (s *Service) ListMessages(ctx context.Context, channelID, userID, before string, limit int) ([]MessageDTO, error) {
	ch, err := s.repo.FindChannel(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if _, err := s.classRepo.FindMembership(ctx, ch.ClassID, userID); err != nil {
		return nil, err
	}
	limit = clampLimit(limit)
	rows, err := s.repo.ListMessagesPage(ctx, channelID, before, limit)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []MessageDTO{}, nil
	}

	msgIDs := make([]string, 0, len(rows))
	for i := range rows {
		msgIDs = append(msgIDs, rows[i].ID)
	}
	atts, err := s.repo.FindAttachmentsByMessageIDs(ctx, msgIDs)
	if err != nil {
		return nil, err
	}
	attsByMsg := make(map[string][]Attachment, len(atts))
	for _, a := range atts {
		if a.MessageID == nil {
			continue
		}
		attsByMsg[*a.MessageID] = append(attsByMsg[*a.MessageID], a)
	}

	out := make([]MessageDTO, 0, len(rows))
	for i := range rows {
		out = append(out, s.toMessageDTOWithAuthor(&rows[i], attsByMsg[rows[i].ID]))
	}
	return out, nil
}

func (s *Service) SendMessage(ctx context.Context, channelID, userID string, req SendMessageReq) (*MessageDTO, error) {
	ch, err := s.repo.FindChannel(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if _, err := s.classRepo.FindMembership(ctx, ch.ClassID, userID); err != nil {
		return nil, err
	}
	if len(req.AttachmentIDs) > MaxAttachmentsPerMessage {
		return nil, ErrTooManyAttachments
	}

	now := time.Now()
	m := &Message{
		ID:           uuid.NewString(),
		ChannelID:    channelID,
		ClassID:      ch.ClassID,
		AuthorUserID: userID,
		Content:      strings.TrimSpace(req.Content),
		CreatedAt:    now,
	}
	if err := s.repo.InsertMessage(ctx, m); err != nil {
		return nil, err
	}
	if len(req.AttachmentIDs) > 0 {
		if err := s.repo.BindAttachmentsToMessage(ctx, req.AttachmentIDs, channelID, userID, m.ID); err != nil {
			// 绑定失败:回滚消息行,避免出现"消息存在但附件丢失"
			_, _ = s.repo.DeleteMessage(ctx, m.ID)
			return nil, ErrAttachmentInvalid
		}
	}

	dto, err := s.hydrateMessageDTO(ctx, m)
	if err != nil {
		return nil, err
	}

	go s.hub.BroadcastToClass(ch.ClassID, MessageFrame{
		Type:      realtime.FrameChannelMessageCreated,
		ChannelID: channelID,
		ClassID:   ch.ClassID,
		Message:   *dto,
	})
	return dto, nil
}

func (s *Service) EditMessage(ctx context.Context, channelID, messageID, userID, content string) (*MessageDTO, error) {
	m, err := s.repo.FindMessage(ctx, messageID)
	if err != nil {
		return nil, err
	}
	if m.ChannelID != channelID {
		return nil, ErrNotFound
	}
	if m.AuthorUserID != userID {
		return nil, ErrForbidden
	}
	content = strings.TrimSpace(content)
	if err := s.repo.UpdateMessageContent(ctx, messageID, content); err != nil {
		return nil, err
	}
	updated, err := s.repo.FindMessage(ctx, messageID)
	if err != nil {
		return nil, err
	}
	dto, err := s.hydrateMessageDTO(ctx, updated)
	if err != nil {
		return nil, err
	}
	go s.hub.BroadcastToClass(m.ClassID, MessageFrame{
		Type:      realtime.FrameChannelMessageUpdated,
		ChannelID: channelID,
		ClassID:   m.ClassID,
		Message:   *dto,
	})
	return dto, nil
}

func (s *Service) DeleteMessage(ctx context.Context, channelID, messageID, userID string) error {
	m, err := s.repo.FindMessage(ctx, messageID)
	if err != nil {
		return err
	}
	if m.ChannelID != channelID {
		return ErrNotFound
	}
	// 作者本人 OR 班级 owner
	authorized := m.AuthorUserID == userID
	if !authorized {
		c, err := s.classRepo.FindByID(ctx, m.ClassID)
		if err != nil {
			return err
		}
		if c.OwnerUserID == userID {
			authorized = true
		}
	}
	if !authorized {
		return ErrForbidden
	}
	keys, err := s.repo.DeleteMessage(ctx, messageID)
	if err != nil {
		return err
	}
	for _, k := range keys {
		if k != "" {
			_ = s.oss.Delete(context.Background(), k)
		}
	}
	go s.hub.BroadcastToClass(m.ClassID, MessageDeletedFrame{
		Type:      realtime.FrameChannelMessageDeleted,
		ChannelID: channelID,
		ClassID:   m.ClassID,
		MessageID: messageID,
	})
	return nil
}

// ============ 附件 ============

func (s *Service) UploadAttachments(ctx context.Context, channelID, userID string, files []*multipart.FileHeader) ([]AttachmentDTO, error) {
	ch, err := s.repo.FindChannel(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if _, err := s.classRepo.FindMembership(ctx, ch.ClassID, userID); err != nil {
		return nil, err
	}
	if len(files) > MaxAttachmentsPerMessage {
		return nil, ErrTooManyAttachments
	}

	out := make([]AttachmentDTO, 0, len(files))
	for _, fh := range files {
		if fh.Size > MaxAttachmentBytes {
			return nil, ErrAttachmentTooLarge
		}
		src, err := fh.Open()
		if err != nil {
			return nil, err
		}
		suffix, err := randomHex(8)
		if err != nil {
			src.Close()
			return nil, err
		}
		ext := filepath.Ext(fh.Filename)
		key := "channels/" + channelID + "/" + suffix + ext
		mimeType := resolveMime(fh)
		if err := s.oss.Upload(ctx, key, src, mimeType); err != nil {
			src.Close()
			return nil, err
		}
		src.Close()

		a := &Attachment{
			ID:           uuid.NewString(),
			ChannelID:    channelID,
			UploaderID:   userID,
			OSSKey:       key,
			OriginalName: fh.Filename,
			MimeType:     mimeType,
			SizeBytes:    fh.Size,
			MessageID:    nil,
			CreatedAt:    time.Now(),
		}
		if err := s.repo.InsertAttachment(ctx, a); err != nil {
			_ = s.oss.Delete(context.Background(), key)
			return nil, err
		}
		out = append(out, s.toAttachmentDTO(a))
	}
	return out, nil
}

// OpenAttachment 鉴权:必须是该附件所在班级的成员。caller 拿到 ReadCloser 后必须 Close。
func (s *Service) OpenAttachment(ctx context.Context, channelID, fileID, userID string) (*Attachment, io.ReadCloser, error) {
	a, err := s.repo.FindAttachment(ctx, fileID)
	if err != nil {
		return nil, nil, err
	}
	if a.ChannelID != channelID {
		return nil, nil, ErrNotFound
	}
	ch, err := s.repo.FindChannel(ctx, channelID)
	if err != nil {
		return nil, nil, err
	}
	if _, err := s.classRepo.FindMembership(ctx, ch.ClassID, userID); err != nil {
		return nil, nil, err
	}
	body, err := s.oss.Download(ctx, a.OSSKey)
	if err != nil {
		return nil, nil, err
	}
	return a, body, nil
}

// ============ helpers ============

// requireChannelAdmin 班级 owner 或 teacher 账号才行。其他人 / 非成员都 403/404。
func (s *Service) requireChannelAdmin(ctx context.Context, classID, userID string) error {
	if _, err := s.classRepo.FindMembership(ctx, classID, userID); err != nil {
		return err
	}
	c, err := s.classRepo.FindByID(ctx, classID)
	if err != nil {
		return err
	}
	if c.OwnerUserID == userID {
		return nil
	}
	u, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	if u.Role == user.UserRoleTeacher {
		return nil
	}
	return ErrForbidden
}

func (s *Service) hydrateMessageDTO(ctx context.Context, m *Message) (*MessageDTO, error) {
	atts, err := s.repo.FindAttachmentsByMessageIDs(ctx, []string{m.ID})
	if err != nil {
		return nil, err
	}
	u, err := s.userRepo.FindByID(ctx, m.AuthorUserID)
	if err != nil {
		return nil, err
	}
	row := MessageWithAuthor{
		Message:         *m,
		AuthorEmail:     u.Email,
		AuthorName:      u.Name,
		AuthorRole:      u.Role,
		AuthorAvatarKey: u.AvatarKey,
		AuthorCreatedAt: u.CreatedAt,
	}
	dto := s.toMessageDTOWithAuthor(&row, atts)
	return &dto, nil
}

func (s *Service) toMessageDTOWithAuthor(row *MessageWithAuthor, atts []Attachment) MessageDTO {
	d := MessageDTO{
		ID:        row.ID,
		ChannelID: row.ChannelID,
		ClassID:   row.ClassID,
		Content:   row.Content,
		Author: MessageAuthor{
			ID:        row.AuthorUserID,
			Email:     row.AuthorEmail,
			Name:      row.AuthorName,
			Role:      row.AuthorRole,
			AvatarURL: s.userAvatarURL(row.AuthorUserID, row.AuthorAvatarKey),
		},
		CreatedAt: row.CreatedAt.Format(time.RFC3339),
	}
	if row.EditedAt != nil {
		ts := row.EditedAt.Format(time.RFC3339)
		d.EditedAt = &ts
	}
	for i := range atts {
		d.Attachments = append(d.Attachments, s.toAttachmentDTO(&atts[i]))
	}
	return d
}

func (s *Service) toAttachmentDTO(a *Attachment) AttachmentDTO {
	return AttachmentDTO{
		ID:           a.ID,
		OriginalName: a.OriginalName,
		MimeType:     a.MimeType,
		SizeBytes:    a.SizeBytes,
		URL:          s.cfg.App.BackendBaseURL + "/api/channels/" + a.ChannelID + "/attachments/" + a.ID,
	}
}

func (s *Service) userAvatarURL(userID, key string) string {
	if key == "" {
		return ""
	}
	return s.cfg.App.BackendBaseURL + "/api/user/avatar/" + userID + "?v=" + cacheBust(key)
}

func toChannelDTO(c *Channel) ChannelDTO {
	return ChannelDTO{
		ID:          c.ID,
		ClassID:     c.ClassID,
		Name:        c.Name,
		Description: c.Description,
		Topic:       c.Topic,
		IsDefault:   c.IsDefault,
		Position:    c.Position,
		CreatedBy:   c.CreatedBy,
		CreatedAt:   c.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   c.UpdatedAt.Format(time.RFC3339),
	}
}

func cacheBust(key string) string {
	slash := strings.LastIndex(key, "/")
	rest := key
	if slash >= 0 {
		rest = key[slash+1:]
	}
	if dot := strings.LastIndex(rest, "."); dot > 0 {
		rest = rest[:dot]
	}
	return rest
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func resolveMime(fh *multipart.FileHeader) string {
	raw := strings.TrimSpace(fh.Header.Get("Content-Type"))
	if raw != "" {
		if media, _, err := stdmime.ParseMediaType(raw); err == nil {
			raw = media
		}
	}
	if raw == "" || raw == "application/octet-stream" {
		// 兜底:用扩展名查
		return stdmime.TypeByExtension(filepath.Ext(fh.Filename))
	}
	return raw
}
