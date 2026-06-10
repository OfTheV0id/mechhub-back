package play

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	repo *Repo
}

func NewService(repo *Repo) *Service {
	return &Service{repo: repo}
}

func (s *Service) CreateScenario(ctx context.Context, userID, name, structure string) (*Scenario, error) {
	now := time.Now()
	sc := &Scenario{
		ID:        uuid.NewString(),
		UserID:    userID,
		Name:      name,
		Structure: structure,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repo.InsertScenario(ctx, sc); err != nil {
		return nil, err
	}
	return sc, nil
}

func (s *Service) ListScenarios(ctx context.Context, userID string) ([]Scenario, error) {
	return s.repo.ListScenarios(ctx, userID)
}

func (s *Service) GetScenario(ctx context.Context, id, userID string) (*Scenario, error) {
	return s.repo.FindScenario(ctx, id, userID)
}

func (s *Service) UpdateScenario(ctx context.Context, id, userID, name, structure string) (*Scenario, error) {
	if err := s.repo.UpdateScenario(ctx, id, userID, name, structure); err != nil {
		return nil, err
	}
	return s.repo.FindScenario(ctx, id, userID)
}

func (s *Service) DeleteScenario(ctx context.Context, id, userID string) error {
	return s.repo.DeleteScenario(ctx, id, userID)
}
