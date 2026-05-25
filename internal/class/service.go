package class

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"mechhub-back/internal/config"
	"mechhub-back/internal/realtime"
	"mechhub-back/internal/storage"
	"mechhub-back/internal/user"
)

var (
	ErrForbidden            = errors.New("class: forbidden")
	ErrNotTeacher           = errors.New("class: not a teacher")
	ErrAlreadyJoined        = errors.New("class: already joined")
	ErrOwnerCannotLeave     = errors.New("class: owner cannot leave")
	ErrOwnerRoleImmutable   = errors.New("class: owner role immutable")
	ErrInviteExpired        = errors.New("class: invite expired")
	ErrInviteDisabled       = errors.New("class: invite disabled")
	ErrInviteRetryExhausted = errors.New("class: invite generation failed")
)

type Service struct {
	repo        *Repo
	userRepo    *user.Repo
	oss         *storage.OSS
	hub         *realtime.Hub
	channelHook ChannelHook
	cfg         *config.Config
}

func NewService(repo *Repo, userRepo *user.Repo, oss *storage.OSS, hub *realtime.Hub, channelHook ChannelHook, cfg *config.Config) *Service {
	return &Service{repo: repo, userRepo: userRepo, oss: oss, hub: hub, channelHook: channelHook, cfg: cfg}
}

// ============ 读 ============

func (s *Service) ListForUser(ctx context.Context, userID string) ([]ClassListItem, error) {
	rows, err := s.repo.ListForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]ClassListItem, 0, len(rows))
	for i := range rows {
		out = append(out, s.toListItem(&rows[i], userID))
	}
	return out, nil
}

func (s *Service) GetForUser(ctx context.Context, classID, userID string) (*ClassDetail, error) {
	row, err := s.repo.GetForUser(ctx, classID, userID)
	if err != nil {
		return nil, err
	}
	d := s.toDetail(row, userID)
	return &d, nil
}

func (s *Service) ListMembers(ctx context.Context, classID, userID string) ([]MemberDTO, error) {
	row, err := s.repo.GetForUser(ctx, classID, userID)
	if err != nil {
		return nil, err
	}
	members, err := s.repo.ListMembers(ctx, classID, row.OwnerUserID)
	if err != nil {
		return nil, err
	}
	out := make([]MemberDTO, 0, len(members))
	for i := range members {
		out = append(out, s.toMemberDTO(&members[i], row.OwnerUserID))
	}
	return out, nil
}

// ============ 写 ============

func (s *Service) Create(ctx context.Context, ownerUserID, name, description string) (*ClassDetail, error) {
	u, err := s.userRepo.FindByID(ctx, ownerUserID)
	if err != nil {
		return nil, err
	}
	if u.Role != user.UserRoleTeacher {
		return nil, ErrNotTeacher
	}

	description = strings.TrimSpace(description)
	if description == "" {
		description = "这个班级很神秘 什么也没留下"
	}

	expires := time.Now().Add(DefaultInviteTTL)

	var c *Class
	for attempt := 0; attempt < 5; attempt++ {
		token, err := newInviteToken()
		if err != nil {
			return nil, err
		}
		candidate := &Class{
			ID:              uuid.NewString(),
			Name:            strings.TrimSpace(name),
			Description:     description,
			OwnerUserID:     ownerUserID,
			Status:          StatusActive,
			InviteToken:     token,
			InviteExpiresAt: &expires,
			CreatedAt:       time.Now(),
		}
		if err := s.repo.InsertClass(ctx, candidate); err != nil {
			if s.repo.IsDuplicateKey(err) {
				continue
			}
			return nil, err
		}
		c = candidate
		break
	}
	if c == nil {
		return nil, ErrInviteRetryExhausted
	}

	// owner 自动入班为 teacher
	if err := s.repo.InsertMember(ctx, &Member{
		ID:       uuid.NewString(),
		ClassID:  c.ID,
		UserID:   ownerUserID,
		Role:     RoleTeacher,
		JoinedAt: time.Now(),
	}); err != nil {
		return nil, err
	}

	// 自动建 #general
	if s.channelHook != nil {
		if err := s.channelHook.OnClassCreated(ctx, c.ID, ownerUserID); err != nil {
			return nil, err
		}
	}

	// owner 已经活跃 WS 连接的话,即刻订阅这个新班级
	s.hub.AddUserToClass(ownerUserID, c.ID)

	row, err := s.repo.GetForUser(ctx, c.ID, ownerUserID)
	if err != nil {
		return nil, err
	}
	d := s.toDetail(row, ownerUserID)
	return &d, nil
}

// JoinByInviteToken 凭分享链接里的 token 加入班级。
// 4 道闸:token 存在 / 班级 active / 邀请未禁用 / 邀请未过期。
func (s *Service) JoinByInviteToken(ctx context.Context, userID, token string) (*ClassDetail, error) {
	u, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	c, err := s.repo.FindByInviteToken(ctx, strings.TrimSpace(token))
	if err != nil {
		return nil, err
	}
	if c.Status != StatusActive || c.InviteDisabled {
		return nil, ErrInviteDisabled
	}
	if c.InviteExpiresAt != nil && time.Now().After(*c.InviteExpiresAt) {
		return nil, ErrInviteExpired
	}

	if _, err := s.repo.FindMembership(ctx, c.ID, userID); err == nil {
		return nil, ErrAlreadyJoined
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	m := &Member{
		ID:       uuid.NewString(),
		ClassID:  c.ID,
		UserID:   userID,
		Role:     u.Role,
		JoinedAt: time.Now(),
	}
	if err := s.repo.InsertMember(ctx, m); err != nil {
		if s.repo.IsDuplicateKey(err) {
			return nil, ErrAlreadyJoined
		}
		return nil, err
	}

	s.hub.AddUserToClass(userID, c.ID)
	go s.emit(ctx, c.ID, []string{realtime.TargetMembers}, realtime.ReasonMemberJoined, []string{userID})

	row, err := s.repo.GetForUser(ctx, c.ID, userID)
	if err != nil {
		return nil, err
	}
	d := s.toDetail(row, userID)
	return &d, nil
}

// PreviewInvite 任意登录用户用 token 预览班级信息(不入班)。
func (s *Service) PreviewInvite(ctx context.Context, userID, token string) (*InvitePreview, error) {
	c, err := s.repo.FindByInviteToken(ctx, strings.TrimSpace(token))
	if err != nil {
		return nil, err
	}
	preview := &InvitePreview{
		Class: ClassDetail{
			ID:          c.ID,
			Name:        c.Name,
			Description: c.Description,
			OwnerUserID: c.OwnerUserID,
			Status:      c.Status,
			AvatarURL:   s.AvatarURL(c.ID, c.AvatarKey),
			CreatedAt:   c.CreatedAt.Format(time.RFC3339),
		},
		Disabled: c.InviteDisabled || c.Status != StatusActive,
		Expired:  c.InviteExpiresAt != nil && time.Now().After(*c.InviteExpiresAt),
	}
	if _, err := s.repo.FindMembership(ctx, c.ID, userID); err == nil {
		preview.Joined = true
	}
	return preview, nil
}

// GetInvite owner 拿当前 invite 状态。
func (s *Service) GetInvite(ctx context.Context, classID, userID string) (*InviteInfo, error) {
	c, err := s.repo.FindByID(ctx, classID)
	if err != nil {
		return nil, err
	}
	if c.OwnerUserID != userID {
		return nil, ErrForbidden
	}
	return s.toInviteInfo(c), nil
}

// RegenerateInvite owner 换新 token。expiresAt 解析见 RegenerateInviteReq 注释。
func (s *Service) RegenerateInvite(ctx context.Context, classID, userID string, req RegenerateInviteReq) (*InviteInfo, error) {
	c, err := s.repo.FindByID(ctx, classID)
	if err != nil {
		return nil, err
	}
	if c.OwnerUserID != userID {
		return nil, ErrForbidden
	}

	var expiresAt *time.Time
	if req.ExpiresAt == nil {
		// 缺省 → 30 天
		t := time.Now().Add(DefaultInviteTTL)
		expiresAt = &t
	} else if *req.ExpiresAt == "" {
		// 显式空字符串 → 永不过期
		expiresAt = nil
	} else {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			return nil, errors.New("expires_at 必须是 RFC3339 时间字符串或空")
		}
		expiresAt = &t
	}

	var token string
	for attempt := 0; attempt < 5; attempt++ {
		t, err := newInviteToken()
		if err != nil {
			return nil, err
		}
		if err := s.repo.UpdateInvite(ctx, classID, t, expiresAt, false); err != nil {
			if s.repo.IsDuplicateKey(err) {
				continue
			}
			return nil, err
		}
		token = t
		break
	}
	if token == "" {
		return nil, ErrInviteRetryExhausted
	}
	c.InviteToken = token
	c.InviteExpiresAt = expiresAt
	c.InviteDisabled = false
	return s.toInviteInfo(c), nil
}

// DisableInvite owner 禁用 invite 链接。Token 留着以便 owner 再 regenerate 时换新。
func (s *Service) DisableInvite(ctx context.Context, classID, userID string) error {
	c, err := s.repo.FindByID(ctx, classID)
	if err != nil {
		return err
	}
	if c.OwnerUserID != userID {
		return ErrForbidden
	}
	return s.repo.UpdateInvite(ctx, classID, c.InviteToken, c.InviteExpiresAt, true)
}

func (s *Service) Update(ctx context.Context, classID, userID string, req UpdateClassReq) (*ClassDetail, error) {
	row, err := s.repo.GetForUser(ctx, classID, userID)
	if err != nil {
		return nil, err
	}
	if row.OwnerUserID != userID {
		return nil, ErrForbidden
	}
	updates := make(map[string]any)
	if req.Name != nil {
		updates["name"] = strings.TrimSpace(*req.Name)
	}
	if req.Description != nil {
		updates["description"] = strings.TrimSpace(*req.Description)
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if len(updates) == 0 {
		d := s.toDetail(row, userID)
		return &d, nil
	}
	if err := s.repo.UpdateClass(ctx, classID, updates); err != nil {
		return nil, err
	}
	go s.emit(ctx, classID, []string{realtime.TargetClasses, realtime.TargetClassDetail}, realtime.ReasonClassUpdated, []string{userID})

	updated, err := s.repo.GetForUser(ctx, classID, userID)
	if err != nil {
		return nil, err
	}
	d := s.toDetail(updated, userID)
	return &d, nil
}

func (s *Service) Delete(ctx context.Context, classID, userID string) error {
	row, err := s.repo.GetForUser(ctx, classID, userID)
	if err != nil {
		return err
	}
	if row.OwnerUserID != userID {
		return ErrForbidden
	}
	memberIDs, _ := s.repo.ListMemberUserIDs(ctx, classID)

	// 联带删 channels / messages / attachments(走 hook)。失败时也继续删班级本体,
	// 留下的频道孤儿可以后续手工清理 —— 班级删了拉不到频道,影响有限。
	if s.channelHook != nil {
		_ = s.channelHook.OnClassDeleted(ctx, classID)
	}

	avatarKey, err := s.repo.DeleteClass(ctx, classID)
	if err != nil {
		return err
	}
	if avatarKey != "" {
		_ = s.oss.Delete(context.Background(), avatarKey)
	}

	// 推送 + 解绑 WS 订阅
	s.hub.SendToUsers(memberIDs, realtime.ClassInvalidate{
		Type:    realtime.FrameClassInvalidate,
		ClassID: classID,
		Targets: []string{realtime.TargetClasses, realtime.TargetClassDetail, realtime.TargetMembers},
		Reason:  realtime.ReasonClassDeleted,
	})
	for _, uid := range memberIDs {
		s.hub.RemoveUserFromClass(uid, classID)
	}
	return nil
}

func (s *Service) Leave(ctx context.Context, classID, userID string) error {
	row, err := s.repo.GetForUser(ctx, classID, userID)
	if err != nil {
		return err
	}
	if row.OwnerUserID == userID {
		return ErrOwnerCannotLeave
	}
	if err := s.repo.DeleteMembership(ctx, classID, userID); err != nil {
		return err
	}
	s.hub.RemoveUserFromClass(userID, classID)
	go s.emit(ctx, classID, []string{realtime.TargetMembers}, realtime.ReasonMemberLeft, []string{userID})
	return nil
}

func (s *Service) UpdateMemberRole(ctx context.Context, classID, userID, memberID, role string) (*MemberDTO, error) {
	row, err := s.repo.GetForUser(ctx, classID, userID)
	if err != nil {
		return nil, err
	}
	if row.OwnerUserID != userID {
		return nil, ErrForbidden
	}
	m, err := s.repo.FindMemberByID(ctx, classID, memberID)
	if err != nil {
		return nil, err
	}
	if m.UserID == row.OwnerUserID {
		return nil, ErrOwnerRoleImmutable
	}
	if err := s.repo.UpdateMemberRole(ctx, memberID, role); err != nil {
		return nil, err
	}
	go s.emit(ctx, classID, []string{realtime.TargetMembers}, realtime.ReasonMemberRoleUpdated, []string{m.UserID, userID})
	s.hub.SendToUsers([]string{m.UserID}, realtime.ClassInvalidate{
		Type:    realtime.FrameClassInvalidate,
		ClassID: classID,
		Targets: []string{realtime.TargetClasses, realtime.TargetClassDetail, realtime.TargetMembers},
		Reason:  realtime.ReasonMemberRoleUpdated,
	})

	members, err := s.repo.ListMembers(ctx, classID, row.OwnerUserID)
	if err != nil {
		return nil, err
	}
	for i := range members {
		if members[i].ID == memberID {
			dto := s.toMemberDTO(&members[i], row.OwnerUserID)
			return &dto, nil
		}
	}
	return nil, ErrNotFound
}

func (s *Service) RemoveMember(ctx context.Context, classID, userID, memberID string) error {
	row, err := s.repo.GetForUser(ctx, classID, userID)
	if err != nil {
		return err
	}
	if row.OwnerUserID != userID {
		return ErrForbidden
	}
	m, err := s.repo.FindMemberByID(ctx, classID, memberID)
	if err != nil {
		return err
	}
	if m.UserID == row.OwnerUserID {
		return ErrOwnerCannotLeave
	}
	if err := s.repo.DeleteMember(ctx, classID, memberID); err != nil {
		return err
	}
	s.hub.RemoveUserFromClass(m.UserID, classID)
	go s.emit(ctx, classID, []string{realtime.TargetMembers}, realtime.ReasonMemberRemoved, []string{userID})
	s.hub.SendToUsers([]string{m.UserID}, realtime.ClassInvalidate{
		Type:    realtime.FrameClassInvalidate,
		ClassID: classID,
		Targets: []string{realtime.TargetClasses, realtime.TargetClassDetail, realtime.TargetMembers},
		Reason:  realtime.ReasonMemberRemoved,
	})
	return nil
}

// ============ 头像 ============

func (s *Service) UploadAvatar(ctx context.Context, classID, userID string, body io.Reader, contentType, ext string) (*ClassDetail, error) {
	row, err := s.repo.GetForUser(ctx, classID, userID)
	if err != nil {
		return nil, err
	}
	if row.OwnerUserID != userID {
		return nil, ErrForbidden
	}
	suffix, err := randomHex(8)
	if err != nil {
		return nil, err
	}
	key := "class-avatars/" + classID + "/" + suffix + ext
	if err := s.oss.Upload(ctx, key, body, contentType); err != nil {
		return nil, err
	}
	oldKey, err := s.repo.UpdateAvatarKey(ctx, classID, key)
	if err != nil {
		_ = s.oss.Delete(ctx, key)
		return nil, err
	}
	if oldKey != "" && oldKey != key {
		_ = s.oss.Delete(ctx, oldKey)
	}
	go s.emit(ctx, classID, []string{realtime.TargetClasses, realtime.TargetClassDetail}, realtime.ReasonAvatarUpdated, []string{userID})

	updated, err := s.repo.GetForUser(ctx, classID, userID)
	if err != nil {
		return nil, err
	}
	d := s.toDetail(updated, userID)
	return &d, nil
}

func (s *Service) RemoveAvatar(ctx context.Context, classID, userID string) error {
	row, err := s.repo.GetForUser(ctx, classID, userID)
	if err != nil {
		return err
	}
	if row.OwnerUserID != userID {
		return ErrForbidden
	}
	if row.AvatarKey == "" {
		return nil
	}
	oldKey, err := s.repo.UpdateAvatarKey(ctx, classID, "")
	if err != nil {
		return err
	}
	if oldKey != "" {
		_ = s.oss.Delete(ctx, oldKey)
	}
	go s.emit(ctx, classID, []string{realtime.TargetClasses, realtime.TargetClassDetail}, realtime.ReasonAvatarRemoved, []string{userID})
	return nil
}

func (s *Service) OpenAvatar(ctx context.Context, classID, userID string) (io.ReadCloser, string, error) {
	row, err := s.repo.GetForUser(ctx, classID, userID)
	if err != nil {
		return nil, "", err
	}
	if row.AvatarKey == "" {
		return nil, "", ErrNotFound
	}
	body, err := s.oss.Download(ctx, row.AvatarKey)
	if err != nil {
		return nil, "", err
	}
	return body, mimeFromKey(row.AvatarKey), nil
}

// ============ helpers ============

func (s *Service) AvatarURL(classID, key string) string {
	if key == "" {
		return ""
	}
	return s.cfg.App.BackendBaseURL + "/api/classes/" + classID + "/avatar?v=" + cacheBust(key)
}

func (s *Service) userAvatarURL(userID, key string) string {
	if key == "" {
		return ""
	}
	return s.cfg.App.BackendBaseURL + "/api/user/avatar/" + userID + "?v=" + cacheBust(key)
}

func (s *Service) toListItem(row *ClassWithRole, currentUserID string) ClassListItem {
	return ClassListItem{
		ID:             row.ID,
		Name:           row.Name,
		Status:         row.Status,
		MembershipRole: row.MembershipRole,
		IsOwner:        row.OwnerUserID == currentUserID,
		AvatarURL:      s.AvatarURL(row.ID, row.AvatarKey),
	}
}

func (s *Service) toDetail(row *ClassWithRole, currentUserID string) ClassDetail {
	return ClassDetail{
		ID:             row.ID,
		Name:           row.Name,
		Description:    row.Description,
		OwnerUserID:    row.OwnerUserID,
		Status:         row.Status,
		MembershipRole: row.MembershipRole,
		IsOwner:        row.OwnerUserID == currentUserID,
		AvatarURL:      s.AvatarURL(row.ID, row.AvatarKey),
		CreatedAt:      row.CreatedAt.Format(time.RFC3339),
	}
}

func (s *Service) toMemberDTO(row *MemberWithUser, ownerUserID string) MemberDTO {
	return MemberDTO{
		ID:       row.ID,
		ClassID:  row.ClassID,
		Role:     row.Role,
		IsOwner:  row.UserID == ownerUserID,
		JoinedAt: row.JoinedAt.Format(time.RFC3339),
		User: MemberUserInfo{
			ID:        row.UserID,
			Email:     row.Email,
			Name:      row.UserName,
			Role:      row.UserRole,
			AvatarURL: s.userAvatarURL(row.UserID, row.AvatarKey),
		},
	}
}

func (s *Service) toInviteInfo(c *Class) *InviteInfo {
	info := &InviteInfo{
		Token:    c.InviteToken,
		ShareURL: s.cfg.App.BaseURL + "/invite/" + c.InviteToken,
		Disabled: c.InviteDisabled,
	}
	if c.InviteExpiresAt != nil {
		ts := c.InviteExpiresAt.Format(time.RFC3339)
		info.ExpiresAt = &ts
		if time.Now().After(*c.InviteExpiresAt) {
			info.Expired = true
		}
	}
	return info
}

func (s *Service) emit(_ context.Context, classID string, targets []string, reason string, excludeUserIDs []string) {
	memberIDs, err := s.repo.ListMemberUserIDs(context.Background(), classID)
	if err != nil {
		return
	}
	exclude := make(map[string]struct{}, len(excludeUserIDs))
	for _, uid := range excludeUserIDs {
		exclude[uid] = struct{}{}
	}
	out := make([]string, 0, len(memberIDs))
	for _, uid := range memberIDs {
		if _, skip := exclude[uid]; skip {
			continue
		}
		out = append(out, uid)
	}
	s.hub.SendToUsers(out, realtime.ClassInvalidate{
		Type:    realtime.FrameClassInvalidate,
		ClassID: classID,
		Targets: targets,
		Reason:  reason,
	})
}

func newInviteToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
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

func mimeFromKey(key string) string {
	switch strings.ToLower(filepath.Ext(key)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}
