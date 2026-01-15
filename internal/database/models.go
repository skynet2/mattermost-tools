package database

import (
	"encoding/json"
	"fmt"
)

type Release struct {
	ID               string `gorm:"primaryKey"`
	SourceBranch     string `gorm:"not null"`
	DestBranch       string `gorm:"not null"`
	Status           string `gorm:"default:pending"`
	Notes            string
	BreakingChanges  string
	CreatedBy        string `gorm:"not null"`
	ChannelID        string `gorm:"not null"`
	MattermostPostID string
	DevApprovedBy    string
	DevApprovedAt    int64
	QAApprovedBy     string
	QAApprovedAt     int64
	DeclinedBy       string
	DeclinedAt       int64
	LastRefreshedAt  int64
	CreatedAt        int64 `gorm:"not null"`
}

type ReleaseRepo struct {
	ID             uint   `gorm:"primaryKey;autoIncrement"`
	ReleaseID      string `gorm:"not null;index"`
	RepoName       string `gorm:"not null"`
	CommitCount    int    `gorm:"default:0"`
	Additions      int    `gorm:"default:0"`
	Deletions      int    `gorm:"default:0"`
	Contributors   string
	PRNumber       int
	PRURL          string
	PRMerged       bool `gorm:"default:false"`
	Excluded       bool `gorm:"default:false"`
	DependsOn      string
	Summary        string
	IsBreaking     bool `gorm:"default:false"`
	ConfirmedBy    string
	ConfirmedAt    int64
	InfraChanges   string
	MergeCommitSHA string
	HeadSHA        string
}

func (r *ReleaseRepo) GetContributors() ([]string, error) {
	if r.Contributors == "" {
		return nil, nil
	}
	var contributors []string
	if err := json.Unmarshal([]byte(r.Contributors), &contributors); err != nil {
		return nil, fmt.Errorf("unmarshaling contributors: %w", err)
	}
	return contributors, nil
}

func (r *ReleaseRepo) SetContributors(contributors []string) error {
	data, err := json.Marshal(contributors)
	if err != nil {
		return fmt.Errorf("marshaling contributors: %w", err)
	}
	r.Contributors = string(data)
	return nil
}

func (r *ReleaseRepo) GetDependsOn() ([]string, error) {
	if r.DependsOn == "" {
		return nil, nil
	}
	var deps []string
	if err := json.Unmarshal([]byte(r.DependsOn), &deps); err != nil {
		return nil, fmt.Errorf("unmarshaling depends_on: %w", err)
	}
	return deps, nil
}

func (r *ReleaseRepo) SetDependsOn(deps []string) error {
	data, err := json.Marshal(deps)
	if err != nil {
		return fmt.Errorf("marshaling depends_on: %w", err)
	}
	r.DependsOn = string(data)
	return nil
}

func (r *ReleaseRepo) GetConfirmedBy() ([]string, error) {
	if r.ConfirmedBy == "" {
		return nil, nil
	}
	var confirmedBy []string
	if err := json.Unmarshal([]byte(r.ConfirmedBy), &confirmedBy); err != nil {
		return nil, fmt.Errorf("unmarshaling confirmed_by: %w", err)
	}
	return confirmedBy, nil
}

func (r *ReleaseRepo) SetConfirmedBy(confirmedBy []string) error {
	data, err := json.Marshal(confirmedBy)
	if err != nil {
		return fmt.Errorf("marshaling confirmed_by: %w", err)
	}
	r.ConfirmedBy = string(data)
	return nil
}

func (r *ReleaseRepo) GetInfraChanges() ([]string, error) {
	if r.InfraChanges == "" {
		return nil, nil
	}
	var infraChanges []string
	if err := json.Unmarshal([]byte(r.InfraChanges), &infraChanges); err != nil {
		return nil, fmt.Errorf("unmarshaling infra_changes: %w", err)
	}
	return infraChanges, nil
}

func (r *ReleaseRepo) SetInfraChanges(infraChanges []string) error {
	data, err := json.Marshal(infraChanges)
	if err != nil {
		return fmt.Errorf("marshaling infra_changes: %w", err)
	}
	r.InfraChanges = string(data)
	return nil
}

type User struct {
	ID             uint   `gorm:"primaryKey;autoIncrement"`
	Email          string `gorm:"uniqueIndex"`
	MattermostUser string `gorm:"index"`
	GitHubUser     string `gorm:"index"`
	CreatedAt      int64
	UpdatedAt      int64
}

type ReleaseHistory struct {
	ID        uint   `gorm:"primaryKey;autoIncrement"`
	ReleaseID string `gorm:"not null;index"`
	Action    string `gorm:"not null"`
	Actor     string
	Details   string
	CreatedAt int64 `gorm:"not null;index"`
}

type RepoCIStatus struct {
	ID             uint   `gorm:"primaryKey;autoIncrement"`
	ReleaseRepoID  uint   `gorm:"uniqueIndex;not null"`
	WorkflowRunID  int64
	WorkflowRunNum int
	WorkflowURL    string
	Status         string `gorm:"default:pending"`
	ChartName      string
	ChartVersion   string
	MergeCommitSHA string
	StartedAt      int64
	CompletedAt    int64
	LastCheckedAt  int64
}
