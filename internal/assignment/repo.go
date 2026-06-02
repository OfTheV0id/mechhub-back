package assignment

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

var ErrNotFound = errors.New("assignment: not found")

type Repo struct {
	db *gorm.DB
}

func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

// ============ 作业 ============

// CreateAssignment 事务里写作业 + 题目。
func (r *Repo) CreateAssignment(ctx context.Context, a *Assignment, qs []Question) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(a).Error; err != nil {
			return err
		}
		if len(qs) > 0 {
			return tx.Create(&qs).Error
		}
		return nil
	})
}

func (r *Repo) GetAssignment(ctx context.Context, id string) (*Assignment, error) {
	var a Assignment
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *Repo) ListByClass(ctx context.Context, classID string) ([]Assignment, error) {
	var rows []Assignment
	err := r.db.WithContext(ctx).
		Where("class_id = ?", classID).
		Order("due_at DESC, created_at DESC").
		Find(&rows).Error
	return rows, err
}

func (r *Repo) UpdateAssignment(ctx context.Context, id string, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&Assignment{}).Where("id = ?", id).Updates(updates).Error
}

// ReplaceQuestions 删旧题写新题(编辑作业整组替换),事务保证原子。
func (r *Repo) ReplaceQuestions(ctx context.Context, assignmentID string, qs []Question) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("assignment_id = ?", assignmentID).Delete(&Question{}).Error; err != nil {
			return err
		}
		if len(qs) > 0 {
			return tx.Create(&qs).Error
		}
		return nil
	})
}

// DeleteAssignment 联带删题目 / 提交 / 作答。
func (r *Repo) DeleteAssignment(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var subIDs []string
		if err := tx.Model(&Submission{}).Where("assignment_id = ?", id).Pluck("id", &subIDs).Error; err != nil {
			return err
		}
		if len(subIDs) > 0 {
			if err := tx.Where("submission_id IN ?", subIDs).Delete(&Answer{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("assignment_id = ?", id).Delete(&Submission{}).Error; err != nil {
			return err
		}
		if err := tx.Where("assignment_id = ?", id).Delete(&Question{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&Assignment{}).Error
	})
}

// ============ 题目 ============

func (r *Repo) ListQuestions(ctx context.Context, assignmentID string) ([]Question, error) {
	var rows []Question
	err := r.db.WithContext(ctx).
		Where("assignment_id = ?", assignmentID).
		Order("position ASC").
		Find(&rows).Error
	return rows, err
}

// ============ 提交 ============

func (r *Repo) GetSubmission(ctx context.Context, id string) (*Submission, error) {
	var s Submission
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *Repo) FindSubmission(ctx context.Context, assignmentID, studentID string) (*Submission, error) {
	var s Submission
	err := r.db.WithContext(ctx).
		Where("assignment_id = ? AND student_id = ?", assignmentID, studentID).
		First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *Repo) UpsertSubmission(ctx context.Context, s *Submission, answers []Answer) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(s).Error; err != nil {
			return err
		}
		if err := tx.Where("submission_id = ?", s.ID).Delete(&Answer{}).Error; err != nil {
			return err
		}
		if len(answers) > 0 {
			return tx.Create(&answers).Error
		}
		return nil
	})
}

// ListSubmissionsByAssignment 取该作业的全部提交(看板/统计共用)。
func (r *Repo) ListSubmissionsByAssignment(ctx context.Context, assignmentID string) ([]Submission, error) {
	var rows []Submission
	err := r.db.WithContext(ctx).Where("assignment_id = ?", assignmentID).Find(&rows).Error
	return rows, err
}

// ListSubmissionsForStudent 取某学生在一组作业里的提交,用于列表/侧栏批量带 my 摘要。
func (r *Repo) ListSubmissionsForStudent(ctx context.Context, assignmentIDs []string, studentID string) ([]Submission, error) {
	if len(assignmentIDs) == 0 {
		return nil, nil
	}
	var rows []Submission
	err := r.db.WithContext(ctx).
		Where("assignment_id IN ? AND student_id = ?", assignmentIDs, studentID).
		Find(&rows).Error
	return rows, err
}

func (r *Repo) ListAnswers(ctx context.Context, submissionID string) ([]Answer, error) {
	var rows []Answer
	err := r.db.WithContext(ctx).Where("submission_id = ?", submissionID).Find(&rows).Error
	return rows, err
}

// GradeSubmission 事务写每题分数/评语/批注 + 更新提交总分/状态。
func (r *Repo) GradeSubmission(ctx context.Context, submissionID string, updates map[string]any, perAnswer map[string]map[string]any) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for qid, patch := range perAnswer {
			if err := tx.Model(&Answer{}).
				Where("submission_id = ? AND question_id = ?", submissionID, qid).
				Updates(patch).Error; err != nil {
				return err
			}
		}
		if len(updates) > 0 {
			return tx.Model(&Submission{}).Where("id = ?", submissionID).Updates(updates).Error
		}
		return nil
	})
}

// ============ 文件 ============

func (r *Repo) InsertFile(ctx context.Context, f *AssignmentFile) error {
	return r.db.WithContext(ctx).Create(f).Error
}

func (r *Repo) FindFile(ctx context.Context, id string) (*AssignmentFile, error) {
	var f AssignmentFile
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&f).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}
