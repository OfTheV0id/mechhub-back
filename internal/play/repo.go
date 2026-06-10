package play

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

var ErrNotFound = errors.New("play: not found")

type Repo struct {
	db *gorm.DB
}

func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

func (r *Repo) InsertScenario(ctx context.Context, s *Scenario) error {
	return r.db.WithContext(ctx).Create(s).Error
}

func (r *Repo) ListScenarios(ctx context.Context, userID string) ([]Scenario, error) {
	var list []Scenario
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("updated_at DESC").
		Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *Repo) FindScenario(ctx context.Context, id, userID string) (*Scenario, error) {
	var s Scenario
	err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *Repo) UpdateScenario(ctx context.Context, id, userID, name, structure string) error {
	fields := map[string]any{"name": name}
	if structure != "" {
		fields["structure"] = structure
	}
	res := r.db.WithContext(ctx).Model(&Scenario{}).
		Where("id = ? AND user_id = ?", id, userID).
		Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repo) DeleteScenario(ctx context.Context, id, userID string) error {
	res := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", id, userID).
		Delete(&Scenario{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
