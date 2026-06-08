package course

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	stdmime "mime"
	"mime/multipart"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"mechhub-back/internal/config"
	"mechhub-back/internal/storage"
	"mechhub-back/internal/user"
)

var (
	ErrForbidden        = errors.New("course: forbidden")
	ErrNotTeacher       = errors.New("course: not a teacher")
	ErrInvalidState     = errors.New("course: invalid state")
	ErrParentNotSection = errors.New("course: parent must be a section")

	ErrFileTooLarge       = errors.New("course: file too large")
	ErrFileTypeNotAllowed = errors.New("course: file type not allowed")
)

type Service struct {
	repo     *Repo
	userRepo *user.Repo
	userSvc  *user.Service
	oss      *storage.OSS
	cfg      *config.Config
}

func NewService(repo *Repo, userRepo *user.Repo, userSvc *user.Service, oss *storage.OSS, cfg *config.Config) *Service {
	return &Service{repo: repo, userRepo: userRepo, userSvc: userSvc, oss: oss, cfg: cfg}
}

// IsTeacher 判断用户角色是否 teacher(唯一能建课/编辑的角色)。
func (s *Service) IsTeacher(ctx context.Context, userID string) (bool, error) {
	u, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return false, err
	}
	return u.Role == user.UserRoleTeacher, nil
}

// ---- Course ----

func (s *Service) ListPublished(ctx context.Context, userID string) ([]CourseDTO, error) {
	list, err := s.repo.ListPublished(ctx)
	if err != nil {
		return nil, err
	}
	return s.toCourseDTOs(ctx, userID, list)
}

func (s *Service) ListMyCourses(ctx context.Context, userID string) ([]CourseDTO, error) {
	list, err := s.repo.ListByAuthor(ctx, userID)
	if err != nil {
		return nil, err
	}
	return s.toCourseDTOs(ctx, userID, list)
}

func (s *Service) GetCourseDetail(ctx context.Context, userID, courseID string) (*CourseDetailDTO, error) {
	c, err := s.repo.FindCourse(ctx, courseID)
	if err != nil {
		return nil, err
	}
	if err := s.assertCanView(userID, c); err != nil {
		return nil, err
	}
	nodes, err := s.repo.ListNodesByCourse(ctx, courseID)
	if err != nil {
		return nil, err
	}
	name, avatarURL := s.authorInfo(ctx, c.AuthorUserID)
	dto := toCourseDTO(c, len(nodes), s.coverURL(c.CoverKey), c.AuthorUserID == userID, name, avatarURL)
	return &CourseDetailDTO{Course: dto, Nodes: buildTree(nodes, nil)}, nil
}

func (s *Service) CreateCourse(ctx context.Context, userID string, req CreateCourseReq) (*CourseDTO, error) {
	if ok, err := s.IsTeacher(ctx, userID); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrNotTeacher
	}
	now := time.Now()
	c := &Course{
		ID:           uuid.NewString(),
		Title:        strings.TrimSpace(req.Title),
		Description:  strings.TrimSpace(req.Description),
		AuthorUserID: userID,
		Published:    false,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.repo.InsertCourse(ctx, c); err != nil {
		return nil, err
	}
	dto := toCourseDTO(c, 0, "", true, "", "")
	return &dto, nil
}

func (s *Service) UpdateCourse(ctx context.Context, userID, courseID string, req UpdateCourseReq) (*CourseDTO, error) {
	c, err := s.repo.FindCourse(ctx, courseID)
	if err != nil {
		return nil, err
	}
	if err := s.assertCanEdit(userID, c); err != nil {
		return nil, err
	}
	fields := map[string]any{
		"title":       strings.TrimSpace(req.Title),
		"description": strings.TrimSpace(req.Description),
		"updated_at":  time.Now(),
	}
	if req.CoverFileID != "" {
		fields["cover_key"] = req.CoverFileID
	}
	if req.Published != nil {
		fields["published"] = *req.Published
	}
	if err := s.repo.UpdateCourse(ctx, courseID, fields); err != nil {
		return nil, err
	}
	return s.courseDTOByID(ctx, userID, courseID)
}

func (s *Service) DeleteCourse(ctx context.Context, userID, courseID string) error {
	c, err := s.repo.FindCourse(ctx, courseID)
	if err != nil {
		return err
	}
	if err := s.assertCanEdit(userID, c); err != nil {
		return err
	}
	return s.repo.DeleteCourse(ctx, courseID)
}

// ---- Node ----

func (s *Service) GetNode(ctx context.Context, userID, nodeID string) (*NodeDetailDTO, error) {
	n, err := s.repo.FindNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	c, err := s.repo.FindCourse(ctx, n.CourseID)
	if err != nil {
		return nil, err
	}
	if err := s.assertCanView(userID, c); err != nil {
		return nil, err
	}
	dto := s.nodeDetailDTO(ctx, n, c.AuthorUserID == userID, userID)
	return &dto, nil
}

// nodeDetailDTO 组装节点详情。作者拿到完整 assessment(含答案),学生拿剥除版。
func (s *Service) nodeDetailDTO(ctx context.Context, n *CourseNode, isAuthor bool, userID string) NodeDetailDTO {
	var assessment json.RawMessage
	if isAuthor {
		assessment = rawContent(n.Assessment)
	} else {
		assessment = studentAssessment(n.Assessment)
	}
	passed := false
	if p, err := s.repo.FindProgress(ctx, userID, n.ID); err == nil {
		passed = p.Completed
	}
	return NodeDetailDTO{
		ID:          n.ID,
		CourseID:    n.CourseID,
		ParentID:    n.ParentID,
		Title:       n.Title,
		Kind:        n.Kind,
		Completable: isCompletable(n),
		Passed:      passed,
		IsAuthor:    isAuthor,
		Content:     rawContent(n.Content),
		Assessment:  assessment,
	}
}

func (s *Service) CreateNode(ctx context.Context, userID, courseID string, req CreateNodeReq) (*CourseNodeDTO, error) {
	c, err := s.repo.FindCourse(ctx, courseID)
	if err != nil {
		return nil, err
	}
	if err := s.assertCanEdit(userID, c); err != nil {
		return nil, err
	}
	if req.ParentID != nil {
		parent, err := s.repo.FindNode(ctx, *req.ParentID)
		if err != nil {
			return nil, err
		}
		if parent.CourseID != courseID {
			return nil, ErrInvalidState
		}
		// 只有「章节」能容纳子节点
		if parent.Kind != KindSection {
			return nil, ErrParentNotSection
		}
	}
	kind := req.Kind
	if kind == "" {
		kind = KindTheory
	}
	maxPos, err := s.repo.MaxChildPosition(ctx, courseID, req.ParentID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	n := &CourseNode{
		ID:         uuid.NewString(),
		CourseID:   courseID,
		ParentID:   req.ParentID,
		Title:      strings.TrimSpace(req.Title),
		Kind:       kind,
		Position:   maxPos + 1,
		Content:    "null", // JSON 列不收空串;null 是合法 JSON,且 hasContent/rawContent 视其为空
		Assessment: "null",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.repo.InsertNode(ctx, n); err != nil {
		return nil, err
	}
	s.touchCourse(ctx, courseID)
	dto := toNodeDTO(n)
	return &dto, nil
}

func (s *Service) UpdateNode(ctx context.Context, userID, nodeID string, req UpdateNodeReq) (*NodeDetailDTO, error) {
	n, err := s.repo.FindNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	c, err := s.repo.FindCourse(ctx, n.CourseID)
	if err != nil {
		return nil, err
	}
	if err := s.assertCanEdit(userID, c); err != nil {
		return nil, err
	}
	fields := map[string]any{"updated_at": time.Now()}
	if req.Title != nil {
		fields["title"] = strings.TrimSpace(*req.Title)
	}
	if req.Kind != nil {
		// 不能把「带子节点的章节」改成非 section,否则会出现非 section 却有子节点
		if *req.Kind != KindSection {
			all, err := s.repo.ListNodesByCourse(ctx, n.CourseID)
			if err != nil {
				return nil, err
			}
			for i := range all {
				if all[i].ParentID != nil && *all[i].ParentID == nodeID {
					return nil, ErrParentNotSection
				}
			}
		}
		fields["kind"] = *req.Kind
	}
	if len(req.Content) > 0 {
		if !json.Valid(req.Content) {
			return nil, ErrInvalidState
		}
		fields["content"] = string(req.Content)
	}
	if len(req.Assessment) > 0 {
		if !json.Valid(req.Assessment) {
			return nil, ErrInvalidState
		}
		fields["assessment"] = string(req.Assessment)
	}
	if err := s.repo.UpdateNode(ctx, nodeID, fields); err != nil {
		return nil, err
	}
	s.touchCourse(ctx, n.CourseID)
	updated, err := s.repo.FindNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	dto := s.nodeDetailDTO(ctx, updated, true, userID)
	return &dto, nil
}

func (s *Service) DeleteNode(ctx context.Context, userID, nodeID string) error {
	n, err := s.repo.FindNode(ctx, nodeID)
	if err != nil {
		return err
	}
	c, err := s.repo.FindCourse(ctx, n.CourseID)
	if err != nil {
		return err
	}
	if err := s.assertCanEdit(userID, c); err != nil {
		return err
	}
	all, err := s.repo.ListNodesByCourse(ctx, n.CourseID)
	if err != nil {
		return err
	}
	ids := collectSubtree(all, nodeID)
	if err := s.repo.DeleteNodes(ctx, ids); err != nil {
		return err
	}
	s.touchCourse(ctx, n.CourseID)
	return nil
}

func (s *Service) MoveNode(ctx context.Context, userID, courseID string, req MoveNodeReq) error {
	c, err := s.repo.FindCourse(ctx, courseID)
	if err != nil {
		return err
	}
	if err := s.assertCanEdit(userID, c); err != nil {
		return err
	}
	all, err := s.repo.ListNodesByCourse(ctx, courseID)
	if err != nil {
		return err
	}
	byID := make(map[string]*CourseNode, len(all))
	for i := range all {
		byID[all[i].ID] = &all[i]
	}
	node, ok := byID[req.NodeID]
	if !ok {
		return ErrNotFound
	}
	if req.ParentID != nil {
		parent, ok := byID[*req.ParentID]
		if !ok {
			return ErrNotFound
		}
		// 不能挂到自身或自身子树下,避免成环
		if parent.ID == node.ID || isDescendant(byID, node.ID, parent.ID) {
			return ErrInvalidState
		}
		// 只有「章节」能容纳子节点
		if parent.Kind != KindSection {
			return ErrParentNotSection
		}
	}
	// 目标父级下的兄弟(已按 position 排序),排除 node 自身
	var sibs []string
	for i := range all {
		n := &all[i]
		if n.ID != node.ID && parentEq(n.ParentID, req.ParentID) {
			sibs = append(sibs, n.ID)
		}
	}
	pos := min(max(req.Position, 0), len(sibs))
	ordered := make([]string, 0, len(sibs)+1)
	ordered = append(ordered, sibs[:pos]...)
	ordered = append(ordered, node.ID)
	ordered = append(ordered, sibs[pos:]...)

	if err := s.repo.MoveNode(ctx, node.ID, req.ParentID, ordered); err != nil {
		return err
	}
	s.touchCourse(ctx, courseID)
	return nil
}

// ---- Progress ----

// AssessNode 判 theory/quiz 选择题作答;全对则把该节点置为「已通过」(只升不降)。
func (s *Service) AssessNode(ctx context.Context, userID, nodeID string, req AssessNodeReq) (*AssessResultDTO, error) {
	n, err := s.repo.FindNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	c, err := s.repo.FindCourse(ctx, n.CourseID)
	if err != nil {
		return nil, err
	}
	if err := s.assertCanView(userID, c); err != nil {
		return nil, err
	}
	if !assessmentGradable(n.Kind, n.Assessment) {
		return nil, ErrInvalidState
	}
	var results map[string]bool
	var explanations map[string]string
	var passed bool
	if n.Kind == KindLab || n.Kind == KindWorkshop {
		a := parseAssessment(n.Assessment)
		sol, err := solveFBD(a.FBD)
		if err != nil {
			return nil, ErrInvalidState
		}
		results = map[string]bool{}
		passed = true
		for _, sup := range a.FBD.Supports {
			ok := fbdReactionOK(req.Reactions[sup.ID], sol[sup.ID], a.FBD.Tolerance)
			results[sup.ID] = ok
			if !ok {
				passed = false
			}
		}
	} else {
		results, explanations, passed = gradeMCQ(n.Assessment, req.Answers)
	}
	if passed {
		now := time.Now()
		prog, err := s.repo.FindProgress(ctx, userID, nodeID)
		if errors.Is(err, ErrNotFound) {
			prog = &NodeProgress{
				ID:          uuid.NewString(),
				UserID:      userID,
				NodeID:      nodeID,
				CourseID:    n.CourseID,
				Completed:   true,
				CompletedAt: &now,
			}
		} else if err != nil {
			return nil, err
		} else {
			prog.Completed = true
			if prog.CompletedAt == nil {
				prog.CompletedAt = &now
			}
		}
		if err := s.repo.SaveProgress(ctx, prog); err != nil {
			return nil, err
		}
	}
	return &AssessResultDTO{Results: results, Explanations: explanations, Passed: passed}, nil
}

// FBDSolution 作者出题时预览标准解(各支座反力)。仅作者可调。
func (s *Service) FBDSolution(ctx context.Context, userID, nodeID string) (*FBDSolutionDTO, error) {
	n, err := s.repo.FindNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	c, err := s.repo.FindCourse(ctx, n.CourseID)
	if err != nil {
		return nil, err
	}
	if err := s.assertCanEdit(userID, c); err != nil {
		return nil, err
	}
	a := parseAssessment(n.Assessment)
	if a == nil || a.FBD == nil {
		return nil, ErrInvalidState
	}
	sol, err := solveFBD(a.FBD)
	if err != nil {
		return nil, ErrInvalidState
	}
	return &FBDSolutionDTO{Reactions: sol}, nil
}

func (s *Service) GetCourseProgress(ctx context.Context, userID, courseID string) (*CourseProgressDTO, error) {
	c, err := s.repo.FindCourse(ctx, courseID)
	if err != nil {
		return nil, err
	}
	if err := s.assertCanView(userID, c); err != nil {
		return nil, err
	}
	nodes, err := s.repo.ListNodesByCourse(ctx, courseID)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.ListProgressByCourse(ctx, userID, courseID)
	if err != nil {
		return nil, err
	}
	doneSet := make(map[string]bool, len(rows))
	for _, p := range rows {
		if p.Completed {
			doneSet[p.NodeID] = true
		}
	}
	out := &CourseProgressDTO{CourseID: courseID}
	for i := range nodes {
		if !isCompletable(&nodes[i]) {
			continue
		}
		out.Total++
		done := doneSet[nodes[i].ID]
		if done {
			out.Completed++
		}
		out.Nodes = append(out.Nodes, NodeProgressDTO{NodeID: nodes[i].ID, Completed: done})
	}
	return out, nil
}

// ---- Annotation ----

func (s *Service) ListAnnotations(ctx context.Context, userID, nodeID string) ([]AnnotationDTO, error) {
	n, err := s.repo.FindNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	c, err := s.repo.FindCourse(ctx, n.CourseID)
	if err != nil {
		return nil, err
	}
	if err := s.assertCanView(userID, c); err != nil {
		return nil, err
	}
	rows, err := s.repo.ListAnnotations(ctx, nodeID, userID)
	if err != nil {
		return nil, err
	}
	names := s.nameCache(ctx)
	out := make([]AnnotationDTO, len(rows))
	for i := range rows {
		out[i] = toAnnotationDTO(&rows[i], userID, names(rows[i].UserID))
	}
	return out, nil
}

func (s *Service) CreateAnnotation(ctx context.Context, userID, nodeID string, req CreateAnnotationReq) (*AnnotationDTO, error) {
	n, err := s.repo.FindNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	c, err := s.repo.FindCourse(ctx, n.CourseID)
	if err != nil {
		return nil, err
	}
	if err := s.assertCanView(userID, c); err != nil {
		return nil, err
	}
	now := time.Now()
	a := &Annotation{
		ID:         uuid.NewString(),
		NodeID:     nodeID,
		CourseID:   n.CourseID,
		UserID:     userID,
		Body:       strings.TrimSpace(req.Body),
		Visibility: req.Visibility,
		AnchorFrom: req.AnchorFrom,
		AnchorTo:   req.AnchorTo,
		Quote:      req.Quote,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.repo.InsertAnnotation(ctx, a); err != nil {
		return nil, err
	}
	names := s.nameCache(ctx)
	dto := toAnnotationDTO(a, userID, names(userID))
	return &dto, nil
}

func (s *Service) UpdateAnnotation(ctx context.Context, userID, id string, req UpdateAnnotationReq) (*AnnotationDTO, error) {
	a, err := s.repo.FindAnnotation(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.UserID != userID {
		return nil, ErrForbidden
	}
	fields := map[string]any{"updated_at": time.Now()}
	if req.Body != nil {
		fields["body"] = strings.TrimSpace(*req.Body)
	}
	if req.Visibility != nil {
		fields["visibility"] = *req.Visibility
	}
	if err := s.repo.UpdateAnnotation(ctx, id, fields); err != nil {
		return nil, err
	}
	updated, err := s.repo.FindAnnotation(ctx, id)
	if err != nil {
		return nil, err
	}
	names := s.nameCache(ctx)
	dto := toAnnotationDTO(updated, userID, names(updated.UserID))
	return &dto, nil
}

func (s *Service) DeleteAnnotation(ctx context.Context, userID, id string) error {
	a, err := s.repo.FindAnnotation(ctx, id)
	if err != nil {
		return err
	}
	if a.UserID != userID {
		return ErrForbidden
	}
	return s.repo.DeleteAnnotation(ctx, id)
}

// ---- 媒体上传 / stream-through ----

var allowedMimeKind = map[string]string{
	"image/png":   FileKindImage,
	"image/jpeg":  FileKindImage,
	"image/jpg":   FileKindImage,
	"image/webp":  FileKindImage,
	"image/gif":   FileKindImage,
	"audio/mpeg":  FileKindAudio,
	"audio/mp3":   FileKindAudio,
	"audio/wav":   FileKindAudio,
	"audio/x-wav": FileKindAudio,
	"audio/ogg":   FileKindAudio,
	"audio/aac":   FileKindAudio,
	"video/mp4":   FileKindVideo,
	"video/webm":  FileKindVideo,
	"video/ogg":   FileKindVideo,
}

func (s *Service) UploadMedia(ctx context.Context, ownerID string, files []*multipart.FileHeader) ([]MediaDTO, error) {
	out := make([]MediaDTO, 0, len(files))
	for _, fh := range files {
		if fh.Size > s.cfg.Course.MaxFileSize {
			return nil, ErrFileTooLarge
		}
		mime := resolveMime(fh)
		kind, ok := allowedMimeKind[mime]
		if !ok {
			return nil, ErrFileTypeNotAllowed
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
		key := "courses/" + ownerID + "/" + suffix + ext
		if err := s.oss.Upload(ctx, key, src, mime); err != nil {
			src.Close()
			return nil, err
		}
		src.Close()

		f := CourseFile{
			ID:           uuid.NewString(),
			OwnerUserID:  ownerID,
			OSSKey:       key,
			OriginalName: fh.Filename,
			MimeType:     mime,
			Kind:         kind,
			Size:         fh.Size,
			CreatedAt:    time.Now(),
		}
		if err := s.repo.InsertFile(ctx, &f); err != nil {
			return nil, err
		}
		out = append(out, s.toMediaDTO(&f))
	}
	return out, nil
}

func (s *Service) OpenMedia(ctx context.Context, id string) (*CourseFile, io.ReadCloser, error) {
	f, err := s.repo.FindFile(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	body, err := s.oss.Download(ctx, f.OSSKey)
	if err != nil {
		return nil, nil, err
	}
	return f, body, nil
}

func (s *Service) MediaURL(fileID string) string {
	return s.cfg.App.BackendBaseURL + "/api/course/attachments/" + fileID
}

func (s *Service) coverURL(fileID string) string {
	if fileID == "" {
		return ""
	}
	return s.MediaURL(fileID)
}

func (s *Service) toMediaDTO(f *CourseFile) MediaDTO {
	return MediaDTO{
		ID:           f.ID,
		Kind:         f.Kind,
		MimeType:     f.MimeType,
		OriginalName: f.OriginalName,
		Size:         f.Size,
		URL:          s.MediaURL(f.ID),
	}
}

// ---- 内部 helper ----

// assertCanView 已发布对所有人开放;未发布只有作者本人可看。
func (s *Service) assertCanView(userID string, c *Course) error {
	if c.Published || c.AuthorUserID == userID {
		return nil
	}
	return ErrForbidden
}

// assertCanEdit 只有作者本人可改(作者必为建课的 teacher)。
func (s *Service) assertCanEdit(userID string, c *Course) error {
	if c.AuthorUserID != userID {
		return ErrForbidden
	}
	return nil
}

func (s *Service) touchCourse(ctx context.Context, courseID string) {
	_ = s.repo.UpdateCourse(ctx, courseID, map[string]any{"updated_at": time.Now()})
}

func (s *Service) courseDTOByID(ctx context.Context, userID, courseID string) (*CourseDTO, error) {
	c, err := s.repo.FindCourse(ctx, courseID)
	if err != nil {
		return nil, err
	}
	counts, err := s.repo.CountNodesByCourses(ctx, []string{courseID})
	if err != nil {
		return nil, err
	}
	name, avatarURL := s.authorInfo(ctx, c.AuthorUserID)
	dto := toCourseDTO(c, counts[courseID], s.coverURL(c.CoverKey), c.AuthorUserID == userID, name, avatarURL)
	return &dto, nil
}

func (s *Service) toCourseDTOs(ctx context.Context, userID string, list []Course) ([]CourseDTO, error) {
	ids := make([]string, len(list))
	for i := range list {
		ids[i] = list[i].ID
	}
	counts, err := s.repo.CountNodesByCourses(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]CourseDTO, len(list))
	for i := range list {
		out[i] = toCourseDTO(&list[i], counts[list[i].ID], s.coverURL(list[i].CoverKey), list[i].AuthorUserID == userID, "", "")
	}
	return out, nil
}

// nameCache 返回一个按需查用户名的闭包,带本地缓存(批注作者名展示用)。
func (s *Service) nameCache(ctx context.Context) func(string) string {
	cache := map[string]string{}
	return func(uid string) string {
		if name, ok := cache[uid]; ok {
			return name
		}
		name := ""
		if u, err := s.userRepo.FindByID(ctx, uid); err == nil {
			name = u.Name
		}
		cache[uid] = name
		return name
	}
}

// authorInfo 查课程作者的展示名 + 头像 URL(无头像返回空串),供课程详情展示创建人。
func (s *Service) authorInfo(ctx context.Context, authorUserID string) (name, avatarURL string) {
	u, err := s.userRepo.FindByID(ctx, authorUserID)
	if err != nil {
		return "", ""
	}
	return u.Name, s.userSvc.AvatarURL(authorUserID, u.AvatarKey)
}

func toCourseDTO(c *Course, nodeCount int, coverURL string, isAuthor bool, authorName, authorAvatarURL string) CourseDTO {
	return CourseDTO{
		ID:              c.ID,
		Title:           c.Title,
		Description:     c.Description,
		CoverURL:        coverURL,
		AuthorID:        c.AuthorUserID,
		AuthorName:      authorName,
		AuthorAvatarURL: authorAvatarURL,
		Published:       c.Published,
		IsAuthor:        isAuthor,
		NodeCount:       nodeCount,
		CreatedAt:       c.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       c.UpdatedAt.Format(time.RFC3339),
	}
}

func toNodeDTO(n *CourseNode) CourseNodeDTO {
	return CourseNodeDTO{
		ID:          n.ID,
		ParentID:    n.ParentID,
		Title:       n.Title,
		Kind:        n.Kind,
		Position:    n.Position,
		Completable: isCompletable(n),
		Children:    []CourseNodeDTO{},
	}
}

func toAnnotationDTO(a *Annotation, viewerID, authorName string) AnnotationDTO {
	return AnnotationDTO{
		ID:         a.ID,
		NodeID:     a.NodeID,
		UserID:     a.UserID,
		AuthorName: authorName,
		Body:       a.Body,
		Visibility: a.Visibility,
		AnchorFrom: a.AnchorFrom,
		AnchorTo:   a.AnchorTo,
		Quote:      a.Quote,
		IsMine:     a.UserID == viewerID,
		CreatedAt:  a.CreatedAt.Format(time.RFC3339),
	}
}

// buildTree 从拉平的节点(已按 position 排序)递归组装某父级下的子树。
func buildTree(nodes []CourseNode, parentID *string) []CourseNodeDTO {
	out := []CourseNodeDTO{}
	for i := range nodes {
		if !parentEq(nodes[i].ParentID, parentID) {
			continue
		}
		dto := toNodeDTO(&nodes[i])
		dto.Children = buildTree(nodes, &nodes[i].ID)
		out = append(out, dto)
	}
	return out
}

// collectSubtree 返回 rootID 及其所有后代节点 id。
func collectSubtree(nodes []CourseNode, rootID string) []string {
	childrenOf := map[string][]string{}
	for i := range nodes {
		if nodes[i].ParentID != nil {
			childrenOf[*nodes[i].ParentID] = append(childrenOf[*nodes[i].ParentID], nodes[i].ID)
		}
	}
	var ids []string
	var walk func(id string)
	walk = func(id string) {
		ids = append(ids, id)
		for _, child := range childrenOf[id] {
			walk(child)
		}
	}
	walk(rootID)
	return ids
}

// isDescendant 判断 candidateID 是否在 ancestorID 的子树内(沿父指针上溯)。
func isDescendant(byID map[string]*CourseNode, ancestorID, candidateID string) bool {
	cur, ok := byID[candidateID]
	for ok && cur.ParentID != nil {
		if *cur.ParentID == ancestorID {
			return true
		}
		cur, ok = byID[*cur.ParentID]
	}
	return false
}

func parentEq(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// isCompletable = 该节点是否「可判定/计入进度」(有判定规格)。非 section 且 assessment 可判才算。
func isCompletable(n *CourseNode) bool {
	if n.Kind == KindSection {
		return false
	}
	return assessmentGradable(n.Kind, n.Assessment)
}

func hasContent(raw string) bool {
	t := strings.TrimSpace(raw)
	return t != "" && t != "null" && t != "{}" && t != "[]"
}

// rawContent 把库里 content 透传成合法 JSON(空则给 null)。
func rawContent(raw string) json.RawMessage {
	if !hasContent(raw) {
		return json.RawMessage("null")
	}
	return json.RawMessage(raw)
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
		return mimeFromExt(filepath.Ext(fh.Filename))
	}
	return raw
}

func mimeFromExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".ogg":
		return "audio/ogg"
	case ".aac":
		return "audio/aac"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	default:
		return ""
	}
}
