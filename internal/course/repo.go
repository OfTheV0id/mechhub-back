package course

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

var ErrNotFound = errors.New("course: not found")

type Repo struct {
	db *gorm.DB
}

func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

// ---- Course ----

func (r *Repo) InsertCourse(ctx context.Context, c *Course) error {
	return r.db.WithContext(ctx).Create(c).Error
}

func (r *Repo) FindCourse(ctx context.Context, id string) (*Course, error) {
	var c Course
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ListPublished 全部已发布课程(学堂首页扁平列表)。
func (r *Repo) ListPublished(ctx context.Context) ([]Course, error) {
	var list []Course
	if err := r.db.WithContext(ctx).
		Where("published = ?", true).
		Order("position ASC, updated_at DESC").
		Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *Repo) ListByAuthor(ctx context.Context, authorID string) ([]Course, error) {
	var list []Course
	if err := r.db.WithContext(ctx).
		Where("author_user_id = ?", authorID).
		Order("updated_at DESC").
		Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *Repo) UpdateCourse(ctx context.Context, id string, fields map[string]any) error {
	res := r.db.WithContext(ctx).Model(&Course{}).Where("id = ?", id).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repo) DeleteCourse(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("course_id = ?", id).Delete(&Annotation{}).Error; err != nil {
			return err
		}
		if err := tx.Where("course_id = ?", id).Delete(&NodeProgress{}).Error; err != nil {
			return err
		}
		if err := tx.Where("course_id = ?", id).Delete(&CourseNode{}).Error; err != nil {
			return err
		}
		res := tx.Where("id = ?", id).Delete(&Course{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// CountNodesByCourses 一次查多门课的节点数,返回 courseID → count。
func (r *Repo) CountNodesByCourses(ctx context.Context, courseIDs []string) (map[string]int, error) {
	out := make(map[string]int, len(courseIDs))
	if len(courseIDs) == 0 {
		return out, nil
	}
	type row struct {
		CourseID string
		N        int
	}
	var rows []row
	if err := r.db.WithContext(ctx).Model(&CourseNode{}).
		Select("course_id, COUNT(*) AS n").
		Where("course_id IN ?", courseIDs).
		Group("course_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, x := range rows {
		out[x.CourseID] = x.N
	}
	return out, nil
}

// ---- CourseNode ----

func (r *Repo) InsertNode(ctx context.Context, n *CourseNode) error {
	return r.db.WithContext(ctx).Create(n).Error
}

func (r *Repo) FindNode(ctx context.Context, id string) (*CourseNode, error) {
	var n CourseNode
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&n).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// ListNodesByCourse 拉平课程下全部节点(按 position),树在内存里组装。
func (r *Repo) ListNodesByCourse(ctx context.Context, courseID string) ([]CourseNode, error) {
	var list []CourseNode
	if err := r.db.WithContext(ctx).
		Where("course_id = ?", courseID).
		Order("position ASC, created_at ASC").
		Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *Repo) UpdateNode(ctx context.Context, id string, fields map[string]any) error {
	res := r.db.WithContext(ctx).Model(&CourseNode{}).Where("id = ?", id).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteNodes 删一批节点(连同其下批注/进度),用于删子树。
func (r *Repo) DeleteNodes(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("node_id IN ?", ids).Delete(&Annotation{}).Error; err != nil {
			return err
		}
		if err := tx.Where("node_id IN ?", ids).Delete(&NodeProgress{}).Error; err != nil {
			return err
		}
		return tx.Where("id IN ?", ids).Delete(&CourseNode{}).Error
	})
}

// MoveNode 在事务里把 nodeID 挪到 parentID 下,并按 orderedSiblingIDs 重写该父级 position。
func (r *Repo) MoveNode(ctx context.Context, nodeID string, parentID *string, orderedSiblingIDs []string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&CourseNode{}).Where("id = ?", nodeID).
			Update("parent_id", parentID).Error; err != nil {
			return err
		}
		for i, id := range orderedSiblingIDs {
			if err := tx.Model(&CourseNode{}).Where("id = ?", id).
				Update("position", i).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Repo) MaxChildPosition(ctx context.Context, courseID string, parentID *string) (int, error) {
	var max *int
	q := r.db.WithContext(ctx).Model(&CourseNode{}).Where("course_id = ?", courseID)
	if parentID == nil {
		q = q.Where("parent_id IS NULL")
	} else {
		q = q.Where("parent_id = ?", *parentID)
	}
	if err := q.Select("MAX(position)").Scan(&max).Error; err != nil {
		return 0, err
	}
	if max == nil {
		return -1, nil
	}
	return *max, nil
}

// ---- CourseFile ----

func (r *Repo) InsertFile(ctx context.Context, f *CourseFile) error {
	return r.db.WithContext(ctx).Create(f).Error
}

func (r *Repo) FindFile(ctx context.Context, id string) (*CourseFile, error) {
	var f CourseFile
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&f).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// ---- NodeProgress ----

func (r *Repo) FindProgress(ctx context.Context, userID, nodeID string) (*NodeProgress, error) {
	var p NodeProgress
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND node_id = ?", userID, nodeID).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repo) ListProgressByCourse(ctx context.Context, userID, courseID string) ([]NodeProgress, error) {
	var list []NodeProgress
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND course_id = ?", userID, courseID).
		Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *Repo) SaveProgress(ctx context.Context, p *NodeProgress) error {
	p.UpdatedAt = time.Now()
	if p.Detail == "" {
		p.Detail = "null" // JSON 列不收空串
	}
	return r.db.WithContext(ctx).Save(p).Error
}

// ---- Annotation ----

func (r *Repo) InsertAnnotation(ctx context.Context, a *Annotation) error {
	return r.db.WithContext(ctx).Create(a).Error
}

func (r *Repo) FindAnnotation(ctx context.Context, id string) (*Annotation, error) {
	var a Annotation
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ListAnnotations 某节点下:公开的 + 自己的私有。
func (r *Repo) ListAnnotations(ctx context.Context, nodeID, userID string) ([]Annotation, error) {
	var list []Annotation
	if err := r.db.WithContext(ctx).
		Where("node_id = ? AND (visibility = ? OR user_id = ?)", nodeID, VisibilityPublic, userID).
		Order("created_at ASC").
		Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *Repo) UpdateAnnotation(ctx context.Context, id string, fields map[string]any) error {
	res := r.db.WithContext(ctx).Model(&Annotation{}).Where("id = ?", id).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repo) DeleteAnnotation(ctx context.Context, id string) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&Annotation{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
