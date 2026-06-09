package play

import (
	"encoding/json"
	"time"
)

type Scenario struct {
	ID        string    `gorm:"primaryKey;type:char(36)"`
	UserID    string    `gorm:"type:char(36);not null;index:idx_user_updated,priority:1"`
	Name      string    `gorm:"type:varchar(200);not null"`
	Structure string    `gorm:"type:json;not null"`
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null;index:idx_user_updated,priority:2,sort:desc"`
}

func (Scenario) TableName() string { return "play_scenarios" }

type ScenarioDTO struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Structure json.RawMessage `json:"structure"`
	CreatedAt string          `json:"created_at"`
	UpdatedAt string          `json:"updated_at"`
}

func toScenarioDTO(s *Scenario) ScenarioDTO {
	return ScenarioDTO{
		ID:        s.ID,
		Name:      s.Name,
		Structure: json.RawMessage(s.Structure),
		CreatedAt: s.CreatedAt.Format(time.RFC3339),
		UpdatedAt: s.UpdatedAt.Format(time.RFC3339),
	}
}

type CreateScenarioReq struct {
	Name      string          `json:"name" binding:"required,min=1,max=200"`
	Structure json.RawMessage `json:"structure" binding:"required"`
}

// UpdateScenarioReq 的 Structure 可选:只改名时不带,重命名保留原结构。
type UpdateScenarioReq struct {
	Name      string          `json:"name" binding:"required,min=1,max=200"`
	Structure json.RawMessage `json:"structure"`
}
