package assignment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"mechhub-back/internal/class"
	"mechhub-back/internal/config"
	"mechhub-back/internal/realtime"
	"mechhub-back/internal/reference"
	"mechhub-back/internal/solochat"
	"mechhub-back/internal/storage"
	"mechhub-back/internal/user"
)

var (
	ErrForbidden        = errors.New("assignment: forbidden")
	ErrNotTeacher       = errors.New("assignment: not a teacher")
	ErrClosed           = errors.New("assignment: closed")
	ErrAlreadySubmitted = errors.New("assignment: already submitted")
	ErrBadInput         = errors.New("assignment: bad input")
)

var allowedFileMime = map[string]string{
	"image/png":       "image",
	"image/jpeg":      "image",
	"image/webp":      "image",
	"application/pdf": "document",
}

type Service struct {
	repo        *Repo
	classRepo   *class.Repo
	userRepo    *user.Repo
	oss         *storage.OSS
	hub         *realtime.Hub
	solochatSvc *solochat.Service
	cfg         *config.Config
}

func NewService(repo *Repo, classRepo *class.Repo, userRepo *user.Repo, oss *storage.OSS, hub *realtime.Hub, solochatSvc *solochat.Service, cfg *config.Config) *Service {
	return &Service{repo: repo, classRepo: classRepo, userRepo: userRepo, oss: oss, hub: hub, solochatSvc: solochatSvc, cfg: cfg}
}

// emitClass 推送作业失效给全班(创建/编辑/删除作业)。
func (s *Service) emitClass(classID, reason, assignmentID string) {
	go s.hub.BroadcastToClass(classID, realtime.AssignmentInvalidate{
		Type:         realtime.FrameAssignmentInvalidate,
		ClassID:      classID,
		Reason:       reason,
		AssignmentID: assignmentID,
	})
}

// emitUser 推送给指定用户(提交→教师、批改→学生)。
func (s *Service) emitUser(userID, classID, reason, assignmentID, submissionID string) {
	go s.hub.SendToUsers([]string{userID}, realtime.AssignmentInvalidate{
		Type:         realtime.FrameAssignmentInvalidate,
		ClassID:      classID,
		Reason:       reason,
		AssignmentID: assignmentID,
		SubmissionID: submissionID,
	})
}

// recordsForSubmission 取一份提交已物化的批改/聊天记录富引用(供师生同款预览)。
func (s *Service) recordsForSubmission(ctx context.Context, submissionID string) ([]reference.Reference, error) {
	rows, err := s.repo.ListSubmissionRecords(ctx, submissionID)
	if err != nil {
		return nil, err
	}
	out := make([]reference.Reference, 0, len(rows))
	for i := range rows {
		if rows[i].Reference == "" {
			continue
		}
		var ref reference.Reference
		if json.Unmarshal([]byte(rows[i].Reference), &ref) != nil {
			continue
		}
		out = append(out, ref)
	}
	return out, nil
}

// materializeRecords 把学生选的 SoloChat 会话快照成自包含记录:校验归属(以学生身份读会话)→
// 收集图片 → 复制进 assignment_files(scope=answer)→ reference 包构建快照。返回记录行 +
// 回滚函数;任一步失败先回滚已复制的文件(OSS 对象 + 行),不留半成品。
func (s *Service) materializeRecords(ctx context.Context, submissionID, studentID, classID string, inputs []SubmissionRecordInput) ([]SubmissionRecord, func(), error) {
	var allRowIDs, allKeys []string
	rollback := func() {
		for _, id := range allRowIDs {
			_ = s.repo.DeleteFile(context.Background(), id)
		}
		for _, k := range allKeys {
			_ = s.oss.Delete(context.Background(), k)
		}
	}

	now := time.Now()
	out := make([]SubmissionRecord, 0, len(inputs))
	for i, in := range inputs {
		msgs, err := s.solochatSvc.ListMessages(ctx, in.SoloChatConvID, studentID)
		if err != nil {
			rollback()
			return nil, nil, err
		}

		ref := reference.Reference{Type: in.Kind, SourceChatID: in.SoloChatConvID, SourceTitle: in.Title}

		// grading:抽取该会话的 AI 批改结果(grade_with_ocr)。若学生把一条本无批改结果的
		// 会话选成了「批改记录」(选择器列全部会话,无法预先区分),退化为对话记录(thread)
		// 快照 —— 保住选择、不让整单提交失败;会话里若含批改,thread 预览里照样能展开。
		g, hasGrade := reference.FirstGrading(msgs)
		if in.Kind == reference.TypeGrading && hasGrade {
			copied, rowIDs, keys, err := s.copySolochatFiles(ctx, studentID, classID, reference.GradingFileIDs(g))
			allRowIDs = append(allRowIDs, rowIDs...)
			allKeys = append(allKeys, keys...)
			if err != nil {
				rollback()
				return nil, nil, err
			}
			ref.Type = reference.TypeGrading
			ref.Grading = reference.BuildGrading(g, copied)
		} else {
			var fileIDs []string
			for _, m := range msgs {
				fileIDs = append(fileIDs, reference.ThreadFileIDs(m)...)
			}
			copied, rowIDs, keys, err := s.copySolochatFiles(ctx, studentID, classID, fileIDs)
			allRowIDs = append(allRowIDs, rowIDs...)
			allKeys = append(allKeys, keys...)
			if err != nil {
				rollback()
				return nil, nil, err
			}
			ref.Type = reference.TypeThread
			ref.Segments = reference.BuildThread(msgs, copied)
		}

		refJSON, err := json.Marshal(&ref)
		if err != nil {
			rollback()
			return nil, nil, err
		}
		out = append(out, SubmissionRecord{
			ID:           uuid.NewString(),
			SubmissionID: submissionID,
			Kind:         ref.Type,
			SourceChatID: in.SoloChatConvID,
			Title:        in.Title,
			Reference:    string(refJSON),
			Position:     i,
			CreatedAt:    now,
		})
	}
	return out, rollback, nil
}

// copySolochatFiles 把一组 solochat 文件(本人拥有)复制成作业附件(scope=answer)。
// 返回 solochat file id → 复制后附件 的映射,以及新建的文件行 id 与 OSS key(失败时供回滚)。
func (s *Service) copySolochatFiles(ctx context.Context, studentID, classID string, fileIDs []string) (map[string]reference.CopiedFile, []string, []string, error) {
	seen := make(map[string]struct{}, len(fileIDs))
	uniq := make([]string, 0, len(fileIDs))
	for _, id := range fileIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	copied := make(map[string]reference.CopiedFile, len(uniq))
	var rowIDs, keys []string
	if len(uniq) == 0 {
		return copied, rowIDs, keys, nil
	}

	files, err := s.solochatSvc.FindFiles(ctx, uniq, studentID)
	if err != nil {
		return copied, rowIDs, keys, err
	}
	if len(files) != len(uniq) {
		return copied, rowIDs, keys, ErrBadInput
	}

	now := time.Now()
	for i := range files {
		f := files[i]
		suffix, err := randomHex(8)
		if err != nil {
			return copied, rowIDs, keys, err
		}
		destKey := "assignment/" + classID + "/" + ScopeAnswer + "/" + studentID + "/" + suffix + filepath.Ext(f.OriginalName)
		if err := s.oss.Copy(ctx, f.OSSKey, destKey); err != nil {
			return copied, rowIDs, keys, err
		}
		keys = append(keys, destKey)
		af := &AssignmentFile{
			ID:           uuid.NewString(),
			ClassID:      classID,
			Scope:        ScopeAnswer,
			OwnerUserID:  studentID,
			OSSKey:       destKey,
			OriginalName: f.OriginalName,
			MimeType:     f.MimeType,
			Kind:         f.Kind,
			Size:         f.Size,
			CreatedAt:    now,
		}
		if err := s.repo.InsertFile(ctx, af); err != nil {
			return copied, rowIDs, keys, err
		}
		rowIDs = append(rowIDs, af.ID)
		copied[f.ID] = reference.CopiedFile{
			ID:           af.ID,
			OriginalName: af.OriginalName,
			MimeType:     af.MimeType,
			URL:          s.fileURL(af.ID),
		}
	}
	return copied, rowIDs, keys, nil
}

// ============ 访问控制 ============

// memberClass 校验用户是该班成员,返回班级行(含 OwnerUserID)。非成员 → ErrForbidden。
func (s *Service) memberClass(ctx context.Context, classID, userID string) (*class.Class, error) {
	row, err := s.classRepo.GetForUser(ctx, classID, userID)
	if err != nil {
		if errors.Is(err, class.ErrNotFound) {
			return nil, ErrForbidden
		}
		return nil, err
	}
	return row, nil
}

func isTeacher(cls *class.Class, userID string) bool {
	return cls.OwnerUserID == userID
}

// ============ Hub 总览 ============

func (s *Service) Hub(ctx context.Context, userID string) (*HubDTO, error) {
	u, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	isT := u.Role == user.UserRoleTeacher

	classes, err := s.classRepo.ListForUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	out := &HubDTO{IsTeacher: isT, Classes: make([]ClassSummaryDTO, 0, len(classes))}
	var nextDue *time.Time
	var gradedScores []float64
	heatCounts := map[string]int{}

	for i := range classes {
		cls := &classes[i]
		owner := cls.OwnerUserID == userID

		assignments, err := s.repo.ListByClass(ctx, cls.ID)
		if err != nil {
			return nil, err
		}
		members, err := s.classRepo.ListMembers(ctx, cls.ID, cls.OwnerUserID)
		if err != nil {
			return nil, err
		}
		studentCount := 0
		for j := range members {
			if members[j].UserRole == user.UserRoleStudent {
				studentCount++
			}
		}

		aids := make([]string, len(assignments))
		for j := range assignments {
			aids[j] = assignments[j].ID
		}
		mySubs, err := s.repo.ListSubmissionsForStudent(ctx, aids, userID)
		if err != nil {
			return nil, err
		}
		myByA := indexSubsByAssignment(mySubs)

		sum := ClassSummaryDTO{
			ID:        cls.ID,
			Name:      cls.Name,
			AvatarURL: s.classAvatarURL(cls.ID, cls.AvatarKey),
			Students:  studentCount,
			Count:     len(assignments),
		}
		for j := range assignments {
			a := &assignments[j]
			bumpHeat(heatCounts, a.DueAt)
			if a.Status == StatusOpen {
				sum.Open++
				if sum.NextTitle == "" {
					sum.NextTitle = a.Title
				}
				out.OpenCount++
			}
			if owner {
				allSubs, err := s.repo.ListSubmissionsByAssignment(ctx, a.ID)
				if err != nil {
					return nil, err
				}
				for k := range allSubs {
					if allSubs[k].SubmittedAt != nil {
						bumpHeat(heatCounts, *allSubs[k].SubmittedAt)
					}
				}
				submitted, graded, _ := computeStats(allSubs)
				pending := submitted - graded
				if pending > 0 {
					sum.ToGrade += pending
					out.ToGrade += pending
				}
			} else {
				sub := myByA[a.ID]
				st := subStatus(sub)
				if a.Status == StatusOpen && (st == SubTodo || st == SubDoing) {
					sum.MyTodo++
					out.MyTodo++
				}
				if a.Status == StatusOpen && nextDueLess(nextDue, a.DueAt) {
					d := a.DueAt
					nextDue = &d
				}
				if sub != nil && sub.Status == SubGraded && sub.TotalScore != nil {
					gradedScores = append(gradedScores, *sub.TotalScore)
				}
				if sub != nil && sub.SubmittedAt != nil {
					bumpHeat(heatCounts, *sub.SubmittedAt)
				}
				qc := s.questionCount(ctx, a.ID)
				out.MyAssignments = append(out.MyAssignments, HubAssignmentDTO{
					ID:             a.ID,
					ClassID:        cls.ID,
					ClassName:      cls.Name,
					ClassAvatarURL: s.classAvatarURL(cls.ID, cls.AvatarKey),
					Title:          a.Title,
					DueAt:          a.DueAt.Format(time.RFC3339),
					Status:         a.Status,
					MyStatus:       st,
					MyDone:         countAnsweredFallback(sub, qc),
					MyTotal:        qc,
					MyScore:        subScore(sub),
				})
			}
		}
		out.Classes = append(out.Classes, sum)
	}

	out.ClassCount = len(classes)
	if nextDue != nil {
		out.NextDue = nextDue.Format(time.RFC3339)
	}
	if len(gradedScores) > 0 {
		out.MonthAvg = mean(gradedScores)
	}
	out.Heat = buildHeatSeries(heatCounts)
	return out, nil
}

// ============ 列表 / 详情 ============

func (s *Service) ListByClass(ctx context.Context, classID, userID string) ([]AssignmentDTO, error) {
	cls, err := s.memberClass(ctx, classID, userID)
	if err != nil {
		return nil, err
	}
	assignments, err := s.repo.ListByClass(ctx, classID)
	if err != nil {
		return nil, err
	}
	owner := isTeacher(cls, userID)

	aids := make([]string, len(assignments))
	for i := range assignments {
		aids[i] = assignments[i].ID
	}
	mySubs, err := s.repo.ListSubmissionsForStudent(ctx, aids, userID)
	if err != nil {
		return nil, err
	}
	myByA := indexSubsByAssignment(mySubs)

	studentTotal := 0
	if owner {
		studentTotal, err = s.studentCount(ctx, cls)
		if err != nil {
			return nil, err
		}
	}

	out := make([]AssignmentDTO, 0, len(assignments))
	for i := range assignments {
		a := &assignments[i]
		qs, err := s.repo.ListQuestions(ctx, a.ID)
		if err != nil {
			return nil, err
		}
		dto := s.toAssignmentDTO(a, qs)
		if owner {
			subs, err := s.repo.ListSubmissionsByAssignment(ctx, a.ID)
			if err != nil {
				return nil, err
			}
			s.fillTeacherStats(&dto, subs, studentTotal)
		} else {
			s.fillMy(&dto, myByA[a.ID], len(qs))
		}
		out = append(out, dto)
	}
	return out, nil
}

func (s *Service) GetDetail(ctx context.Context, assignmentID, userID string) (*AssignmentDetailDTO, error) {
	a, err := s.repo.GetAssignment(ctx, assignmentID)
	if err != nil {
		return nil, err
	}
	cls, err := s.memberClass(ctx, a.ClassID, userID)
	if err != nil {
		return nil, err
	}
	qs, err := s.repo.ListQuestions(ctx, assignmentID)
	if err != nil {
		return nil, err
	}
	dto := s.toAssignmentDTO(a, qs)
	detail := &AssignmentDetailDTO{
		Assignment: dto,
		Questions:  s.toQuestionDTOs(qs),
	}

	if isTeacher(cls, userID) {
		subs, err := s.repo.ListSubmissionsByAssignment(ctx, assignmentID)
		if err != nil {
			return nil, err
		}
		total, err := s.studentCount(ctx, cls)
		if err != nil {
			return nil, err
		}
		s.fillTeacherStats(&detail.Assignment, subs, total)
	} else {
		sub, err := s.repo.FindSubmission(ctx, assignmentID, userID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		s.fillMy(&detail.Assignment, sub, len(qs))
		if sub != nil {
			sd, err := s.toSubmissionDTO(ctx, sub)
			if err != nil {
				return nil, err
			}
			detail.My = sd
		}
	}
	return detail, nil
}

// ============ 创建 / 编辑 / 删除(教师)============

func (s *Service) Create(ctx context.Context, classID, userID string, req CreateAssignmentReq) (*AssignmentDTO, error) {
	cls, err := s.memberClass(ctx, classID, userID)
	if err != nil {
		return nil, err
	}
	if !isTeacher(cls, userID) {
		return nil, ErrNotTeacher
	}
	dueAt, err := time.Parse(time.RFC3339, req.DueAt)
	if err != nil {
		return nil, ErrBadInput
	}

	now := time.Now()
	a := &Assignment{
		ID:          uuid.NewString(),
		ClassID:     classID,
		Title:       strings.TrimSpace(req.Title),
		Description: strings.TrimSpace(req.Description),
		Status:      StatusOpen,
		AssignedAt:  now,
		DueAt:       dueAt,
		CreatedBy:   userID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	qs := s.buildQuestions(a.ID, req.Questions)
	if err := s.repo.CreateAssignment(ctx, a, qs); err != nil {
		return nil, err
	}
	s.emitClass(classID, realtime.ReasonAssignmentCreated, a.ID)
	dto := s.toAssignmentDTO(a, qs)
	return &dto, nil
}

func (s *Service) Update(ctx context.Context, assignmentID, userID string, req UpdateAssignmentReq) (*AssignmentDTO, error) {
	a, err := s.repo.GetAssignment(ctx, assignmentID)
	if err != nil {
		return nil, err
	}
	cls, err := s.memberClass(ctx, a.ClassID, userID)
	if err != nil {
		return nil, err
	}
	if !isTeacher(cls, userID) {
		return nil, ErrNotTeacher
	}

	updates := map[string]any{"updated_at": time.Now()}
	if req.Title != nil {
		updates["title"] = strings.TrimSpace(*req.Title)
	}
	if req.Description != nil {
		updates["description"] = strings.TrimSpace(*req.Description)
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if req.DueAt != nil {
		dueAt, err := time.Parse(time.RFC3339, *req.DueAt)
		if err != nil {
			return nil, ErrBadInput
		}
		updates["due_at"] = dueAt
	}
	if err := s.repo.UpdateAssignment(ctx, assignmentID, updates); err != nil {
		return nil, err
	}
	if req.Questions != nil {
		qs := s.buildQuestions(assignmentID, req.Questions)
		if err := s.repo.ReplaceQuestions(ctx, assignmentID, qs); err != nil {
			return nil, err
		}
	}

	s.emitClass(a.ClassID, realtime.ReasonAssignmentUpdated, assignmentID)

	updated, err := s.repo.GetAssignment(ctx, assignmentID)
	if err != nil {
		return nil, err
	}
	qs, err := s.repo.ListQuestions(ctx, assignmentID)
	if err != nil {
		return nil, err
	}
	dto := s.toAssignmentDTO(updated, qs)
	return &dto, nil
}

func (s *Service) Delete(ctx context.Context, assignmentID, userID string) error {
	a, err := s.repo.GetAssignment(ctx, assignmentID)
	if err != nil {
		return err
	}
	cls, err := s.memberClass(ctx, a.ClassID, userID)
	if err != nil {
		return err
	}
	if !isTeacher(cls, userID) {
		return ErrNotTeacher
	}
	if err := s.repo.DeleteAssignment(ctx, assignmentID); err != nil {
		return err
	}
	s.emitClass(a.ClassID, realtime.ReasonAssignmentDeleted, assignmentID)
	return nil
}

// ============ 看板(教师)============

func (s *Service) Roster(ctx context.Context, assignmentID, userID string) ([]RosterEntryDTO, error) {
	a, err := s.repo.GetAssignment(ctx, assignmentID)
	if err != nil {
		return nil, err
	}
	cls, err := s.memberClass(ctx, a.ClassID, userID)
	if err != nil {
		return nil, err
	}
	if !isTeacher(cls, userID) {
		return nil, ErrForbidden
	}

	members, err := s.classRepo.ListMembers(ctx, a.ClassID, cls.OwnerUserID)
	if err != nil {
		return nil, err
	}
	subs, err := s.repo.ListSubmissionsByAssignment(ctx, assignmentID)
	if err != nil {
		return nil, err
	}
	subByStudent := make(map[string]*Submission, len(subs))
	for i := range subs {
		subByStudent[subs[i].StudentID] = &subs[i]
	}

	out := make([]RosterEntryDTO, 0)
	for i := range members {
		m := &members[i]
		if m.UserRole != user.UserRoleStudent {
			continue
		}
		entry := RosterEntryDTO{
			Student: StudentLite{
				ID:        m.UserID,
				Name:      m.UserName,
				AvatarURL: s.userAvatarURL(m.UserID, m.AvatarKey),
			},
			State: "missing",
		}
		if sub := subByStudent[m.UserID]; sub != nil {
			entry.SubmissionID = sub.ID
			entry.State = rosterState(sub)
			entry.Score = sub.TotalScore
			if sub.SubmittedAt != nil {
				entry.SubmittedAt = sub.SubmittedAt.Format(time.RFC3339)
			}
		}
		out = append(out, entry)
	}
	return out, nil
}

// ============ 学生提交 ============

func (s *Service) GetMySubmission(ctx context.Context, assignmentID, userID string) (*SubmissionDTO, error) {
	a, err := s.repo.GetAssignment(ctx, assignmentID)
	if err != nil {
		return nil, err
	}
	if _, err := s.memberClass(ctx, a.ClassID, userID); err != nil {
		return nil, err
	}
	sub, err := s.repo.FindSubmission(ctx, assignmentID, userID)
	if errors.Is(err, ErrNotFound) {
		return &SubmissionDTO{AssignmentID: assignmentID, StudentID: userID, Status: SubTodo, Answers: []AnswerDTO{}}, nil
	}
	if err != nil {
		return nil, err
	}
	return s.toSubmissionDTO(ctx, sub)
}

func (s *Service) SaveSubmission(ctx context.Context, assignmentID, userID string, req SaveSubmissionReq) (*SubmissionDTO, error) {
	a, err := s.repo.GetAssignment(ctx, assignmentID)
	if err != nil {
		return nil, err
	}
	cls, err := s.memberClass(ctx, a.ClassID, userID)
	if err != nil {
		return nil, err
	}
	if isTeacher(cls, userID) {
		return nil, ErrForbidden
	}
	if a.Status == StatusClosed {
		return nil, ErrClosed
	}

	now := time.Now()
	sub, err := s.repo.FindSubmission(ctx, assignmentID, userID)
	if errors.Is(err, ErrNotFound) {
		sub = &Submission{
			ID:           uuid.NewString(),
			AssignmentID: assignmentID,
			StudentID:    userID,
			CreatedAt:    now,
		}
	} else if err != nil {
		return nil, err
	} else if sub.Status == SubSubmitted || sub.Status == SubLate || sub.Status == SubGraded {
		// 已提交即锁定:再写会清掉教师批改痕迹。
		return nil, ErrAlreadySubmitted
	}

	sub.Source = normalizeSource(req.Source)
	sub.SoloChatConvID = req.SoloChatConvID
	sub.SoloChatTitle = req.SoloChatTitle
	sub.UpdatedAt = now
	if req.Submit {
		sub.SubmittedAt = &now
		if now.After(a.DueAt) {
			sub.Status = SubLate
		} else {
			sub.Status = SubSubmitted
		}
	} else if sub.Status != SubSubmitted && sub.Status != SubGraded && sub.Status != SubLate {
		sub.Status = SubDoing
	}

	answers := make([]Answer, 0, len(req.Answers))
	for _, in := range req.Answers {
		answers = append(answers, Answer{
			ID:           uuid.NewString(),
			SubmissionID: sub.ID,
			QuestionID:   in.QuestionID,
			Choice:       in.Choice,
			Text:         in.Text,
			ImageKeys:    marshalJSON(in.ImageKeys),
			Annotations:  "[]",
			Highlights:   "[]",
		})
	}
	// 正式提交时物化附加记录(快照 + 拷贝图片);草稿不带记录。
	var records []SubmissionRecord
	var rollbackRecords func()
	if req.Submit && len(req.Records) > 0 {
		recs, rb, err := s.materializeRecords(ctx, sub.ID, userID, a.ClassID, req.Records)
		if err != nil {
			return nil, err
		}
		records, rollbackRecords = recs, rb
	}

	if err := s.repo.UpsertSubmission(ctx, sub, answers); err != nil {
		if rollbackRecords != nil {
			rollbackRecords()
		}
		return nil, err
	}
	if req.Submit {
		if err := s.repo.ReplaceSubmissionRecords(ctx, sub.ID, records); err != nil {
			if rollbackRecords != nil {
				rollbackRecords()
			}
			return nil, err
		}
		s.emitUser(cls.OwnerUserID, a.ClassID, realtime.ReasonSubmissionCreated, assignmentID, sub.ID)
	}
	return s.toSubmissionDTO(ctx, sub)
}

// ============ 教师批阅 ============

func (s *Service) GetGradeView(ctx context.Context, submissionID, userID string) (*GradeViewDTO, error) {
	sub, err := s.repo.GetSubmission(ctx, submissionID)
	if err != nil {
		return nil, err
	}
	a, err := s.repo.GetAssignment(ctx, sub.AssignmentID)
	if err != nil {
		return nil, err
	}
	cls, err := s.memberClass(ctx, a.ClassID, userID)
	if err != nil {
		return nil, err
	}
	if !isTeacher(cls, userID) {
		return nil, ErrForbidden
	}

	qs, err := s.repo.ListQuestions(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	sd, err := s.toSubmissionDTO(ctx, sub)
	if err != nil {
		return nil, err
	}
	stu, err := s.userRepo.FindByID(ctx, sub.StudentID)
	if err != nil {
		return nil, err
	}

	// 已提交学生序列(可翻页)
	allSubs, err := s.repo.ListSubmissionsByAssignment(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	roster := make([]GradeRosterLite, 0, len(allSubs))
	for i := range allSubs {
		st := &allSubs[i]
		if rosterState(st) == "missing" {
			continue
		}
		su, err := s.userRepo.FindByID(ctx, st.StudentID)
		if err != nil {
			continue
		}
		roster = append(roster, GradeRosterLite{
			SubmissionID: st.ID,
			Name:         su.Name,
			AvatarURL:    s.userAvatarURL(su.ID, su.AvatarKey),
		})
	}

	return &GradeViewDTO{
		Assignment: s.toAssignmentDTO(a, qs),
		Questions:  s.toQuestionDTOs(qs),
		Student:    StudentLite{ID: stu.ID, Name: stu.Name, AvatarURL: s.userAvatarURL(stu.ID, stu.AvatarKey)},
		Submission: *sd,
		Roster:     roster,
	}, nil
}

func (s *Service) Grade(ctx context.Context, submissionID, userID string, req GradeReq) (*SubmissionDTO, error) {
	sub, err := s.repo.GetSubmission(ctx, submissionID)
	if err != nil {
		return nil, err
	}
	a, err := s.repo.GetAssignment(ctx, sub.AssignmentID)
	if err != nil {
		return nil, err
	}
	cls, err := s.memberClass(ctx, a.ClassID, userID)
	if err != nil {
		return nil, err
	}
	if !isTeacher(cls, userID) {
		return nil, ErrNotTeacher
	}

	perAnswer := make(map[string]map[string]any, len(req.Answers))
	var total float64
	hasScore := false
	for _, in := range req.Answers {
		patch := map[string]any{
			"comment":     in.Comment,
			"annotations": marshalJSON(in.Annotations),
			"highlights":  marshalJSON(in.Highlights),
		}
		if in.Score != nil {
			patch["score"] = *in.Score
			total += *in.Score
			hasScore = true
		} else {
			patch["score"] = nil
		}
		perAnswer[in.QuestionID] = patch
	}

	now := time.Now()
	updates := map[string]any{"updated_at": now}
	if hasScore {
		updates["total_score"] = total
	}
	if req.Finalize {
		updates["status"] = SubGraded
		updates["graded_at"] = now
	}
	if err := s.repo.GradeSubmission(ctx, submissionID, updates, perAnswer); err != nil {
		return nil, err
	}
	if req.Finalize {
		s.emitUser(sub.StudentID, a.ClassID, realtime.ReasonGraded, a.ID, submissionID)
	}

	updated, err := s.repo.GetSubmission(ctx, submissionID)
	if err != nil {
		return nil, err
	}
	return s.toSubmissionDTO(ctx, updated)
}

// ============ 附件 ============

// UploadFiles 班级级上传:教师上传记为题目媒体(question,全班可读),
// 学生上传记为图片作答(answer,仅本人+教师可读)。
func (s *Service) UploadFiles(ctx context.Context, classID, userID string, files []*multipart.FileHeader) ([]AssignmentFileDTO, error) {
	cls, err := s.memberClass(ctx, classID, userID)
	if err != nil {
		return nil, err
	}
	scope := ScopeAnswer
	if isTeacher(cls, userID) {
		scope = ScopeQuestion
	}

	out := make([]AssignmentFileDTO, 0, len(files))
	for _, fh := range files {
		mime := fh.Header.Get("Content-Type")
		kind, ok := allowedFileMime[mime]
		if !ok {
			return nil, ErrBadInput
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
		key := "assignment/" + classID + "/" + scope + "/" + userID + "/" + suffix + ext
		if err := s.oss.Upload(ctx, key, src, mime); err != nil {
			src.Close()
			return nil, err
		}
		src.Close()

		f := &AssignmentFile{
			ID:           uuid.NewString(),
			ClassID:      classID,
			Scope:        scope,
			OwnerUserID:  userID,
			OSSKey:       key,
			OriginalName: fh.Filename,
			MimeType:     mime,
			Kind:         kind,
			Size:         fh.Size,
			CreatedAt:    time.Now(),
		}
		if err := s.repo.InsertFile(ctx, f); err != nil {
			return nil, err
		}
		out = append(out, s.toFileDTO(f))
	}
	return out, nil
}

func (s *Service) OpenFile(ctx context.Context, fileID, userID string) (*AssignmentFile, io.ReadCloser, error) {
	f, err := s.repo.FindFile(ctx, fileID)
	if err != nil {
		return nil, nil, err
	}
	cls, err := s.memberClass(ctx, f.ClassID, userID)
	if err != nil {
		return nil, nil, err
	}
	// 题目媒体:全班可读;图片作答:仅 owner 本人或该班教师。
	if f.Scope == ScopeAnswer && f.OwnerUserID != userID && !isTeacher(cls, userID) {
		return nil, nil, ErrForbidden
	}
	body, err := s.oss.Download(ctx, f.OSSKey)
	if err != nil {
		return nil, nil, err
	}
	return f, body, nil
}

// ============ DTO 映射 ============

func (s *Service) toAssignmentDTO(a *Assignment, qs []Question) AssignmentDTO {
	points := 0
	for i := range qs {
		points += qs[i].Points
	}
	return AssignmentDTO{
		ID:            a.ID,
		ClassID:       a.ClassID,
		Title:         a.Title,
		Description:   a.Description,
		Status:        a.Status,
		AssignedAt:    a.AssignedAt.Format(time.RFC3339),
		DueAt:         a.DueAt.Format(time.RFC3339),
		CreatedBy:     a.CreatedBy,
		QuestionCount: len(qs),
		Points:        points,
		CreatedAt:     a.CreatedAt.Format(time.RFC3339),
	}
}

func (s *Service) toQuestionDTOs(qs []Question) []QuestionDTO {
	out := make([]QuestionDTO, 0, len(qs))
	for i := range qs {
		q := &qs[i]
		out = append(out, QuestionDTO{
			ID:       q.ID,
			Type:     q.Type,
			Prompt:   q.Prompt,
			Points:   q.Points,
			Options:  parseOptions(q.Options),
			Answer:   q.Answer,
			Media:    s.toMediaDTOs(parseMedia(q.Media)),
			Position: q.Position,
		})
	}
	return out
}

func (s *Service) toSubmissionDTO(ctx context.Context, sub *Submission) (*SubmissionDTO, error) {
	answers, err := s.repo.ListAnswers(ctx, sub.ID)
	if err != nil {
		return nil, err
	}
	dto := &SubmissionDTO{
		ID:             sub.ID,
		AssignmentID:   sub.AssignmentID,
		StudentID:      sub.StudentID,
		Status:         sub.Status,
		Source:         sub.Source,
		SoloChatConvID: sub.SoloChatConvID,
		SoloChatTitle:  sub.SoloChatTitle,
		TotalScore:     sub.TotalScore,
		Answers:        make([]AnswerDTO, 0, len(answers)),
	}
	if sub.SubmittedAt != nil {
		dto.SubmittedAt = sub.SubmittedAt.Format(time.RFC3339)
	}
	if sub.GradedAt != nil {
		dto.GradedAt = sub.GradedAt.Format(time.RFC3339)
	}
	for i := range answers {
		ans := &answers[i]
		dto.Answers = append(dto.Answers, AnswerDTO{
			QuestionID:  ans.QuestionID,
			Choice:      ans.Choice,
			Text:        ans.Text,
			ImageKeys:   parseStrings(ans.ImageKeys),
			ImageURLs:   s.imageURLs(ans.ImageKeys),
			Score:       ans.Score,
			Comment:     ans.Comment,
			Annotations: parseAnnotations(ans.Annotations),
			Highlights:  parseHighlights(ans.Highlights),
		})
	}
	recs, err := s.recordsForSubmission(ctx, sub.ID)
	if err != nil {
		return nil, err
	}
	dto.Records = recs
	return dto, nil
}

func (s *Service) toMediaDTOs(media []Media) []MediaDTO {
	out := make([]MediaDTO, 0, len(media))
	for i := range media {
		out = append(out, MediaDTO{
			ID:   media[i].ID,
			Name: media[i].Name,
			Kind: media[i].Kind,
			URL:  s.fileURL(media[i].ID),
		})
	}
	return out
}

func (s *Service) toFileDTO(f *AssignmentFile) AssignmentFileDTO {
	return AssignmentFileDTO{
		ID:   f.ID,
		URL:  s.fileURL(f.ID),
		Name: f.OriginalName,
		Kind: f.Kind,
		Size: f.Size,
	}
}

// ============ 统计填充 ============

func (s *Service) fillTeacherStats(dto *AssignmentDTO, subs []Submission, total int) {
	submitted, graded, avg := computeStats(subs)
	dto.Submitted = submitted
	dto.Graded = graded
	dto.Total = total
	dto.Avg = avg
}

func (s *Service) fillMy(dto *AssignmentDTO, sub *Submission, qCount int) {
	my := &MySubmissionLite{Status: subStatus(sub), Total: qCount}
	if sub != nil {
		my.Done = countAnsweredFallback(sub, qCount)
		my.Score = subScore(sub)
	}
	dto.My = my
}

// ============ helpers ============

func (s *Service) buildQuestions(assignmentID string, inputs []QuestionInput) []Question {
	qs := make([]Question, 0, len(inputs))
	for i, in := range inputs {
		qs = append(qs, Question{
			ID:           uuid.NewString(),
			AssignmentID: assignmentID,
			Position:     i,
			Type:         in.Type,
			Prompt:       strings.TrimSpace(in.Prompt),
			Points:       FixedQuestionPoints,
			Options:      marshalJSON(in.Options),
			Answer:       in.Answer,
			Media:        marshalJSON(in.Media),
		})
	}
	return qs
}

func (s *Service) questionCount(ctx context.Context, assignmentID string) int {
	qs, err := s.repo.ListQuestions(ctx, assignmentID)
	if err != nil {
		return 0
	}
	return len(qs)
}

func (s *Service) studentCount(ctx context.Context, cls *class.Class) (int, error) {
	members, err := s.classRepo.ListMembers(ctx, cls.ID, cls.OwnerUserID)
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range members {
		if members[i].UserRole == user.UserRoleStudent {
			n++
		}
	}
	return n, nil
}

func (s *Service) classAvatarURL(classID, key string) string {
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

func (s *Service) fileURL(fileID string) string {
	return s.cfg.App.BackendBaseURL + "/api/assignment/files/" + fileID
}

func (s *Service) imageURLs(raw string) []string {
	ids := parseStrings(raw)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.fileURL(id))
	}
	return out
}

// ---- 纯函数 ----

func computeStats(subs []Submission) (submitted, graded int, avg *float64) {
	var scores []float64
	for i := range subs {
		switch subs[i].Status {
		case SubSubmitted, SubLate, SubGraded:
			submitted++
		}
		if subs[i].Status == SubGraded {
			graded++
			if subs[i].TotalScore != nil {
				scores = append(scores, *subs[i].TotalScore)
			}
		}
	}
	return submitted, graded, mean(scores)
}

func rosterState(sub *Submission) string {
	switch sub.Status {
	case SubGraded:
		return "graded"
	case SubSubmitted:
		return "submitted"
	case SubLate:
		return "late"
	default:
		return "missing"
	}
}

func subStatus(sub *Submission) string {
	if sub == nil {
		return SubTodo
	}
	return sub.Status
}

func subScore(sub *Submission) *float64 {
	if sub == nil || sub.Status != SubGraded {
		return nil
	}
	return sub.TotalScore
}

// countAnsweredFallback 已提交/已批 → 视为全部完成;否则 0(草稿暂不细算每题)。
func countAnsweredFallback(sub *Submission, qCount int) int {
	if sub == nil {
		return 0
	}
	switch sub.Status {
	case SubSubmitted, SubLate, SubGraded:
		return qCount
	default:
		return 0
	}
}

func indexSubsByAssignment(subs []Submission) map[string]*Submission {
	m := make(map[string]*Submission, len(subs))
	for i := range subs {
		m[subs[i].AssignmentID] = &subs[i]
	}
	return m
}

func nextDueLess(cur *time.Time, candidate time.Time) bool {
	return cur == nil || candidate.Before(*cur)
}

func normalizeSource(src string) string {
	switch src {
	case SourceSoloChat, SourceUpload:
		return src
	default:
		return SourceDirect
	}
}

func mean(xs []float64) *float64 {
	if len(xs) == 0 {
		return nil
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	v := sum / float64(len(xs))
	// 保留一位小数
	v = float64(int(v*10+0.5)) / 10
	return &v
}

// marshalJSON 把切片序列化进 json 列;nil 切片 → "[]" 而非 "null",
// 避免空字符串/null 写进 MySQL json 列时报「invalid JSON」。
func marshalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	s := string(b)
	if s == "null" {
		return "[]"
	}
	return s
}

func parseOptions(raw string) []Option {
	var out []Option
	if raw == "" {
		return []Option{}
	}
	_ = json.Unmarshal([]byte(raw), &out)
	if out == nil {
		return []Option{}
	}
	return out
}

func parseMedia(raw string) []Media {
	var out []Media
	if raw == "" {
		return []Media{}
	}
	_ = json.Unmarshal([]byte(raw), &out)
	if out == nil {
		return []Media{}
	}
	return out
}

func parseAnnotations(raw string) []Annotation {
	var out []Annotation
	if raw == "" {
		return []Annotation{}
	}
	_ = json.Unmarshal([]byte(raw), &out)
	if out == nil {
		return []Annotation{}
	}
	return out
}

func parseStrings(raw string) []string {
	var out []string
	if raw == "" {
		return []string{}
	}
	_ = json.Unmarshal([]byte(raw), &out)
	if out == nil {
		return []string{}
	}
	return out
}

func parseHighlights(raw string) []HighlightRange {
	var out []HighlightRange
	if raw == "" {
		return []HighlightRange{}
	}
	_ = json.Unmarshal([]byte(raw), &out)
	if out == nil {
		return []HighlightRange{}
	}
	return out
}

// ---- 热力图聚合 ----

const heatDays = 18 * 7

func heatLevel(count int) int {
	switch {
	case count <= 0:
		return 0
	case count == 1:
		return 1
	case count == 2:
		return 2
	case count <= 4:
		return 3
	default:
		return 4
	}
}

func bumpHeat(counts map[string]int, t time.Time) {
	counts[t.Format("2006-01-02")]++
}

// buildHeatSeries 从今天往前 heatDays 天,按日产出有序热力序列。
func buildHeatSeries(counts map[string]int) []HeatCell {
	out := make([]HeatCell, 0, heatDays)
	today := time.Now()
	for i := heatDays - 1; i >= 0; i-- {
		d := today.AddDate(0, 0, -i).Format("2006-01-02")
		out = append(out, HeatCell{Date: d, Level: heatLevel(counts[d])})
	}
	return out
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
