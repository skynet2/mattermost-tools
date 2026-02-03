package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/user/mattermost-tools/internal/database"
)

var (
	ErrGitHubNotConfigured = errors.New("GitHub username not configured")
	ErrNotContributor      = errors.New("not a contributor")
	ErrAlreadyConfirmed    = errors.New("already confirmed")
	ErrRepoNotFound        = errors.New("repo not found")
)

type Service struct {
	db                   *gorm.DB
	onFullApprovalNotify func(release *database.Release)
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

func (s *Service) SetFullApprovalCallback(fn func(release *database.Release)) {
	s.onFullApprovalNotify = fn
}

type CreateReleaseRequest struct {
	SourceBranch string
	DestBranch   string
	CreatedBy    string
	ChannelID    string
}

type ReleaseWithRepos struct {
	database.Release
	Repos []database.ReleaseRepo
}

type RepoData struct {
	RepoName       string
	CommitCount    int
	Additions      int
	Deletions      int
	Contributors   []string
	PRNumber       int
	PRURL          string
	PRMerged       bool
	Summary        string
	IsBreaking     bool
	InfraChanges   []string
	MergeCommitSHA string
	HeadSHA        string
}

func (s *Service) CreateRelease(ctx context.Context, req CreateReleaseRequest) (*database.Release, error) {
	release := &database.Release{
		ID:           uuid.New().String(),
		SourceBranch: req.SourceBranch,
		DestBranch:   req.DestBranch,
		Status:       "pending",
		CreatedBy:    req.CreatedBy,
		ChannelID:    req.ChannelID,
		CreatedAt:    time.Now().Unix(),
	}

	if err := s.db.WithContext(ctx).Create(release).Error; err != nil {
		return nil, fmt.Errorf("creating release: %w", err)
	}

	return release, nil
}

func (s *Service) GetRelease(ctx context.Context, id string) (*database.Release, error) {
	var release database.Release
	if err := s.db.WithContext(ctx).First(&release, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("getting release: %w", err)
	}
	return &release, nil
}

func (s *Service) ListReleases(ctx context.Context, status string) ([]database.Release, error) {
	var releases []database.Release
	query := s.db.WithContext(ctx).Order("created_at DESC")
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if err := query.Find(&releases).Error; err != nil {
		return nil, fmt.Errorf("listing releases: %w", err)
	}
	return releases, nil
}

func (s *Service) AddRepos(ctx context.Context, releaseID string, repos []RepoData) error {
	for _, r := range repos {
		repo := database.ReleaseRepo{
			ReleaseID:      releaseID,
			RepoName:       r.RepoName,
			CommitCount:    r.CommitCount,
			Additions:      r.Additions,
			Deletions:      r.Deletions,
			PRNumber:       r.PRNumber,
			PRURL:          r.PRURL,
			PRMerged:       r.PRMerged,
			Summary:        r.Summary,
			IsBreaking:     r.IsBreaking,
			MergeCommitSHA: r.MergeCommitSHA,
			HeadSHA:        r.HeadSHA,
		}
		if err := repo.SetContributors(r.Contributors); err != nil {
			return fmt.Errorf("setting contributors: %w", err)
		}
		if err := repo.SetInfraChanges(r.InfraChanges); err != nil {
			return fmt.Errorf("setting infra changes: %w", err)
		}
		if err := s.db.WithContext(ctx).Create(&repo).Error; err != nil {
			return fmt.Errorf("adding repo %s: %w", r.RepoName, err)
		}
	}
	return nil
}

func (s *Service) GetReleaseWithRepos(ctx context.Context, id string) (*ReleaseWithRepos, error) {
	release, err := s.GetRelease(ctx, id)
	if err != nil {
		return nil, err
	}

	var repos []database.ReleaseRepo
	if err := s.db.WithContext(ctx).Where("release_id = ?", id).Find(&repos).Error; err != nil {
		return nil, fmt.Errorf("getting release repos: %w", err)
	}

	return &ReleaseWithRepos{
		Release: *release,
		Repos:   repos,
	}, nil
}

type UpdateReleaseRequest struct {
	Notes           *string `json:"notes"`
	BreakingChanges *string `json:"breaking_changes"`
}

func (s *Service) UpdateRelease(ctx context.Context, id string, req UpdateReleaseRequest) error {
	updates := make(map[string]interface{})
	if req.Notes != nil {
		updates["notes"] = *req.Notes
	}
	if req.BreakingChanges != nil {
		updates["breaking_changes"] = *req.BreakingChanges
	}
	if len(updates) == 0 {
		return nil
	}
	if err := s.db.WithContext(ctx).Model(&database.Release{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("updating release: %w", err)
	}
	return nil
}

func (s *Service) UpdateRepo(ctx context.Context, repoID uint, excluded *bool, dependsOn []string) error {
	updates := make(map[string]interface{})
	if excluded != nil {
		updates["excluded"] = *excluded
	}
	if dependsOn != nil {
		data, err := json.Marshal(dependsOn)
		if err != nil {
			return fmt.Errorf("marshaling depends_on: %w", err)
		}
		updates["depends_on"] = string(data)
	}
	if len(updates) == 0 {
		return nil
	}
	if err := s.db.WithContext(ctx).Model(&database.ReleaseRepo{}).Where("id = ?", repoID).Updates(updates).Error; err != nil {
		return fmt.Errorf("updating repo: %w", err)
	}
	return nil
}

func (s *Service) ApproveRelease(ctx context.Context, id, approvalType, userID string) error {
	now := time.Now().Unix()
	updates := make(map[string]interface{})

	switch approvalType {
	case "dev":
		updates["dev_approved_by"] = userID
		updates["dev_approved_at"] = now
	case "qa":
		updates["qa_approved_by"] = userID
		updates["qa_approved_at"] = now
	default:
		return fmt.Errorf("invalid approval type: %s", approvalType)
	}

	if err := s.db.WithContext(ctx).Model(&database.Release{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("approving release: %w", err)
	}

	release, err := s.GetRelease(ctx, id)
	if err != nil {
		return fmt.Errorf("checking approval status: %w", err)
	}

	if release.DevApprovedBy != "" && release.QAApprovedBy != "" {
		if err := s.db.WithContext(ctx).Model(&database.Release{}).Where("id = ?", id).Update("status", "approved").Error; err != nil {
			return fmt.Errorf("updating status to approved: %w", err)
		}
		release.Status = "approved"
		if s.onFullApprovalNotify != nil {
			s.onFullApprovalNotify(release)
		}
	}

	return nil
}

func (s *Service) RevokeApproval(ctx context.Context, id, approvalType string) error {
	updates := make(map[string]interface{})

	switch approvalType {
	case "dev":
		updates["dev_approved_by"] = ""
		updates["dev_approved_at"] = 0
	case "qa":
		updates["qa_approved_by"] = ""
		updates["qa_approved_at"] = 0
	default:
		return fmt.Errorf("invalid approval type: %s", approvalType)
	}

	updates["status"] = "pending"

	if err := s.db.WithContext(ctx).Model(&database.Release{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("revoking approval: %w", err)
	}
	return nil
}

func (s *Service) RefreshRelease(ctx context.Context, id string) error {
	now := time.Now().Unix()
	if err := s.db.WithContext(ctx).Model(&database.Release{}).Where("id = ?", id).Update("last_refreshed_at", now).Error; err != nil {
		return fmt.Errorf("refreshing release: %w", err)
	}
	return nil
}

func (s *Service) SetMattermostPostID(ctx context.Context, id, postID string) error {
	return s.db.WithContext(ctx).Model(&database.Release{}).Where("id = ?", id).Update("mattermost_post_id", postID).Error
}

func (s *Service) RefreshRepos(ctx context.Context, releaseID string, repos []RepoData) error {
	var existingRepos []database.ReleaseRepo
	if err := s.db.WithContext(ctx).Where("release_id = ?", releaseID).Find(&existingRepos).Error; err != nil {
		return fmt.Errorf("fetching existing repos: %w", err)
	}

	existingByName := make(map[string]*database.ReleaseRepo)
	for i := range existingRepos {
		existingByName[existingRepos[i].RepoName] = &existingRepos[i]
	}

	incomingNames := make(map[string]bool)
	for _, r := range repos {
		incomingNames[r.RepoName] = true

		existing := existingByName[r.RepoName]

		summary := r.Summary
		isBreaking := r.IsBreaking
		if existing != nil && existing.HeadSHA != "" && existing.HeadSHA == r.HeadSHA && existing.Summary != "" {
			summary = existing.Summary
			isBreaking = existing.IsBreaking
		}

		contributorsJSON, _ := json.Marshal(r.Contributors)
		infraChangesJSON, _ := json.Marshal(r.InfraChanges)

		if existing != nil {
			updates := map[string]interface{}{
				"commit_count":     r.CommitCount,
				"additions":        r.Additions,
				"deletions":        r.Deletions,
				"pr_number":        r.PRNumber,
				"pr_url":           r.PRURL,
				"pr_merged":        r.PRMerged,
				"summary":          summary,
				"is_breaking":      isBreaking,
				"merge_commit_sha": r.MergeCommitSHA,
				"head_sha":         r.HeadSHA,
				"contributors":     string(contributorsJSON),
				"infra_changes":    string(infraChangesJSON),
			}
			if err := s.db.WithContext(ctx).Model(&database.ReleaseRepo{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
				return fmt.Errorf("updating repo %s: %w", r.RepoName, err)
			}
		} else {
			repo := database.ReleaseRepo{
				ReleaseID:      releaseID,
				RepoName:       r.RepoName,
				CommitCount:    r.CommitCount,
				Additions:      r.Additions,
				Deletions:      r.Deletions,
				PRNumber:       r.PRNumber,
				PRURL:          r.PRURL,
				PRMerged:       r.PRMerged,
				Summary:        summary,
				IsBreaking:     isBreaking,
				MergeCommitSHA: r.MergeCommitSHA,
				HeadSHA:        r.HeadSHA,
				Contributors:   string(contributorsJSON),
				InfraChanges:   string(infraChangesJSON),
			}
			if err := s.db.WithContext(ctx).Create(&repo).Error; err != nil {
				return fmt.Errorf("creating repo %s: %w", r.RepoName, err)
			}
		}
	}

	for _, existing := range existingRepos {
		if !incomingNames[existing.RepoName] {
			if err := s.db.WithContext(ctx).Where("release_repo_id = ?", existing.ID).Delete(&database.RepoCIStatus{}).Error; err != nil {
				return fmt.Errorf("deleting CI status for removed repo %s: %w", existing.RepoName, err)
			}
			if err := s.db.WithContext(ctx).Delete(&existing).Error; err != nil {
				return fmt.Errorf("deleting removed repo %s: %w", existing.RepoName, err)
			}
		}
	}

	now := time.Now().Unix()
	if err := s.db.WithContext(ctx).Model(&database.Release{}).Where("id = ?", releaseID).Update("last_refreshed_at", now).Error; err != nil {
		return fmt.Errorf("updating refresh time: %w", err)
	}

	return nil
}

func (s *Service) DeclineRelease(ctx context.Context, id, userID string) error {
	updates := map[string]interface{}{
		"status":          "declined",
		"declined_by":     userID,
		"declined_at":     time.Now().Unix(),
		"dev_approved_by": "",
		"dev_approved_at": 0,
		"qa_approved_by":  "",
		"qa_approved_at":  0,
	}

	if err := s.db.WithContext(ctx).Model(&database.Release{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("declining release: %w", err)
	}
	return nil
}

func (s *Service) GetUserByEmail(ctx context.Context, email string) (*database.User, error) {
	var user database.User
	if err := s.db.WithContext(ctx).Where("email = ?", email).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("getting user by email: %w", err)
	}
	return &user, nil
}

func (s *Service) CreateOrUpdateUser(ctx context.Context, email, githubUser, mattermostUser string) (*database.User, error) {
	var user database.User
	err := s.db.WithContext(ctx).Where("email = ?", email).First(&user).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("finding user: %w", err)
	}

	now := time.Now().Unix()

	if err == gorm.ErrRecordNotFound {
		user = database.User{
			Email:          email,
			GitHubUser:     githubUser,
			MattermostUser: mattermostUser,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := s.db.WithContext(ctx).Create(&user).Error; err != nil {
			return nil, fmt.Errorf("creating user: %w", err)
		}
		return &user, nil
	}

	updates := map[string]interface{}{
		"updated_at": now,
	}
	if githubUser != "" {
		updates["git_hub_user"] = githubUser
	}
	if mattermostUser != "" {
		updates["mattermost_user"] = mattermostUser
	}

	if err := s.db.WithContext(ctx).Model(&user).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("updating user: %w", err)
	}

	if githubUser != "" {
		user.GitHubUser = githubUser
	}
	if mattermostUser != "" {
		user.MattermostUser = mattermostUser
	}
	user.UpdatedAt = now

	return &user, nil
}

func (s *Service) GetRepo(ctx context.Context, repoID uint) (*database.ReleaseRepo, error) {
	var repo database.ReleaseRepo
	if err := s.db.WithContext(ctx).First(&repo, "id = ?", repoID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRepoNotFound
		}
		return nil, fmt.Errorf("getting repo: %w", err)
	}
	return &repo, nil
}

func (s *Service) ConfirmRepo(ctx context.Context, repoID uint, githubUser string) error {
	repo, err := s.GetRepo(ctx, repoID)
	if err != nil {
		return err
	}

	contributors, err := repo.GetContributors()
	if err != nil {
		return fmt.Errorf("getting contributors: %w", err)
	}

	isContributor := false
	for _, c := range contributors {
		if c == githubUser {
			isContributor = true
			break
		}
	}
	if !isContributor {
		return ErrNotContributor
	}

	confirmedBy, err := repo.GetConfirmedBy()
	if err != nil {
		return fmt.Errorf("getting confirmed_by: %w", err)
	}

	for _, c := range confirmedBy {
		if c == githubUser {
			return ErrAlreadyConfirmed
		}
	}

	confirmedBy = append(confirmedBy, githubUser)
	if err := repo.SetConfirmedBy(confirmedBy); err != nil {
		return fmt.Errorf("setting confirmed_by: %w", err)
	}

	now := time.Now().Unix()
	if err := s.db.WithContext(ctx).Model(&database.ReleaseRepo{}).Where("id = ?", repoID).Updates(map[string]interface{}{
		"confirmed_by": repo.ConfirmedBy,
		"confirmed_at": now,
	}).Error; err != nil {
		return fmt.Errorf("updating repo confirmation: %w", err)
	}

	return nil
}

func (s *Service) UnconfirmRepo(ctx context.Context, repoID uint, githubUser string) error {
	repo, err := s.GetRepo(ctx, repoID)
	if err != nil {
		return err
	}

	confirmedBy, err := repo.GetConfirmedBy()
	if err != nil {
		return fmt.Errorf("getting confirmed_by: %w", err)
	}

	newConfirmedBy := make([]string, 0, len(confirmedBy))
	for _, c := range confirmedBy {
		if c != githubUser {
			newConfirmedBy = append(newConfirmedBy, c)
		}
	}

	if err := repo.SetConfirmedBy(newConfirmedBy); err != nil {
		return fmt.Errorf("setting confirmed_by: %w", err)
	}

	var confirmedAt int64
	if len(newConfirmedBy) > 0 {
		confirmedAt = time.Now().Unix()
	}

	if err := s.db.WithContext(ctx).Model(&database.ReleaseRepo{}).Where("id = ?", repoID).Updates(map[string]interface{}{
		"confirmed_by": repo.ConfirmedBy,
		"confirmed_at": confirmedAt,
	}).Error; err != nil {
		return fmt.Errorf("updating repo confirmation: %w", err)
	}

	return nil
}

func IsRepoConfirmed(repo *database.ReleaseRepo) bool {
	contributors, err := repo.GetContributors()
	if err != nil || len(contributors) == 0 {
		return false
	}

	confirmedBy, err := repo.GetConfirmedBy()
	if err != nil {
		return false
	}

	return len(confirmedBy) > len(contributors)/2
}

type PendingAction struct {
	GitHubUser     string
	MattermostUser string
	ActionType     string
	RepoName       string
}

func (s *Service) GetPendingActions(ctx context.Context, releaseWithRepos *ReleaseWithRepos) []PendingAction {
	var actions []PendingAction

	githubToMattermost := make(map[string]string)
	var users []database.User
	s.db.WithContext(ctx).Find(&users)
	for _, u := range users {
		if u.GitHubUser != "" {
			githubToMattermost[u.GitHubUser] = u.MattermostUser
		}
	}

	for _, repo := range releaseWithRepos.Repos {
		if repo.Excluded {
			continue
		}
		if IsRepoConfirmed(&repo) {
			continue
		}

		contributors, err := repo.GetContributors()
		if err != nil {
			continue
		}
		confirmedBy, err := repo.GetConfirmedBy()
		if err != nil {
			confirmedBy = []string{}
		}

		confirmedSet := make(map[string]struct{})
		for _, c := range confirmedBy {
			confirmedSet[c] = struct{}{}
		}

		for _, contributor := range contributors {
			if _, confirmed := confirmedSet[contributor]; !confirmed {
				actions = append(actions, PendingAction{
					GitHubUser:     contributor,
					MattermostUser: githubToMattermost[contributor],
					ActionType:     "confirm_repo",
					RepoName:       repo.RepoName,
				})
			}
		}
	}

	return actions
}

func (s *Service) RecordHistory(ctx context.Context, releaseID, action, actor string, details map[string]any) error {
	var detailsJSON string
	if details != nil {
		data, err := json.Marshal(details)
		if err != nil {
			return fmt.Errorf("marshaling history details: %w", err)
		}
		detailsJSON = string(data)
	}

	history := database.ReleaseHistory{
		ReleaseID: releaseID,
		Action:    action,
		Actor:     actor,
		Details:   detailsJSON,
		CreatedAt: time.Now().Unix(),
	}

	if err := s.db.WithContext(ctx).Create(&history).Error; err != nil {
		return fmt.Errorf("creating history entry: %w", err)
	}

	return nil
}

func (s *Service) GetHistory(ctx context.Context, releaseID string) ([]database.ReleaseHistory, error) {
	var history []database.ReleaseHistory
	err := s.db.WithContext(ctx).
		Where("release_id = ?", releaseID).
		Order("created_at DESC").
		Find(&history).Error
	if err != nil {
		return nil, fmt.Errorf("fetching history: %w", err)
	}
	return history, nil
}

func (s *Service) CreateOrUpdateCIStatus(ctx context.Context, status *database.RepoCIStatus) error {
	var existing database.RepoCIStatus
	err := s.db.WithContext(ctx).Where("release_repo_id = ?", status.ReleaseRepoID).First(&existing).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return fmt.Errorf("checking existing CI status: %w", err)
	}

	if err == gorm.ErrRecordNotFound {
		if err := s.db.WithContext(ctx).Create(status).Error; err != nil {
			return fmt.Errorf("creating CI status: %w", err)
		}
		return nil
	}

	status.ID = existing.ID
	if err := s.db.WithContext(ctx).Save(status).Error; err != nil {
		return fmt.Errorf("updating CI status: %w", err)
	}
	return nil
}

func (s *Service) GetCIStatusByRepoID(ctx context.Context, releaseRepoID uint) (*database.RepoCIStatus, error) {
	var status database.RepoCIStatus
	err := s.db.WithContext(ctx).Where("release_repo_id = ?", releaseRepoID).First(&status).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("getting CI status: %w", err)
	}
	return &status, nil
}

func (s *Service) GetIncompleteCIStatuses(ctx context.Context) ([]database.RepoCIStatus, error) {
	var statuses []database.RepoCIStatus
	err := s.db.WithContext(ctx).
		Joins("INNER JOIN release_repos ON release_repos.id = repo_ci_statuses.release_repo_id").
		Where("repo_ci_statuses.status NOT IN ?", []string{"success", "failure", "cancelled", "skipped"}).
		Find(&statuses).Error
	if err != nil {
		return nil, fmt.Errorf("fetching incomplete CI statuses: %w", err)
	}
	return statuses, nil
}

func (s *Service) GetCIStatusesForRelease(ctx context.Context, releaseID string) ([]database.RepoCIStatus, error) {
	var repos []database.ReleaseRepo
	if err := s.db.WithContext(ctx).Where("release_id = ?", releaseID).Find(&repos).Error; err != nil {
		return nil, fmt.Errorf("getting release repos: %w", err)
	}

	if len(repos) == 0 {
		return nil, nil
	}

	repoIDs := make([]uint, len(repos))
	for i, r := range repos {
		repoIDs[i] = r.ID
	}

	var statuses []database.RepoCIStatus
	err := s.db.WithContext(ctx).Where("release_repo_id IN ?", repoIDs).Find(&statuses).Error
	if err != nil {
		return nil, fmt.Errorf("fetching CI statuses: %w", err)
	}
	return statuses, nil
}

func (s *Service) GetReposByReleaseID(ctx context.Context, releaseID string) ([]database.ReleaseRepo, error) {
	var repos []database.ReleaseRepo
	if err := s.db.WithContext(ctx).Where("release_id = ?", releaseID).Find(&repos).Error; err != nil {
		return nil, fmt.Errorf("getting release repos: %w", err)
	}
	return repos, nil
}

func (s *Service) CreateOrUpdateDeploymentStatus(ctx context.Context, status *database.RepoDeploymentStatus) error {
	var existing database.RepoDeploymentStatus
	err := s.db.WithContext(ctx).
		Where("release_repo_id = ? AND environment = ?", status.ReleaseRepoID, status.Environment).
		First(&existing).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return fmt.Errorf("checking existing deployment status: %w", err)
	}

	if err == gorm.ErrRecordNotFound {
		if err := s.db.WithContext(ctx).Create(status).Error; err != nil {
			return fmt.Errorf("creating deployment status: %w", err)
		}
		return nil
	}

	status.ID = existing.ID
	if err := s.db.WithContext(ctx).Save(status).Error; err != nil {
		return fmt.Errorf("updating deployment status: %w", err)
	}
	return nil
}

func (s *Service) GetDeploymentStatusesForRelease(ctx context.Context, releaseID string) ([]database.RepoDeploymentStatus, error) {
	var repos []database.ReleaseRepo
	if err := s.db.WithContext(ctx).Where("release_id = ?", releaseID).Find(&repos).Error; err != nil {
		return nil, fmt.Errorf("getting release repos: %w", err)
	}

	if len(repos) == 0 {
		return nil, nil
	}

	repoIDs := make([]uint, len(repos))
	for i, r := range repos {
		repoIDs[i] = r.ID
	}

	var statuses []database.RepoDeploymentStatus
	err := s.db.WithContext(ctx).Where("release_repo_id IN ?", repoIDs).Find(&statuses).Error
	if err != nil {
		return nil, fmt.Errorf("fetching deployment statuses: %w", err)
	}
	return statuses, nil
}

func (s *Service) GetDeploymentStatusByRepoIDAndEnv(ctx context.Context, releaseRepoID uint, env string) (*database.RepoDeploymentStatus, error) {
	var status database.RepoDeploymentStatus
	err := s.db.WithContext(ctx).
		Where("release_repo_id = ? AND environment = ?", releaseRepoID, env).
		First(&status).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("getting deployment status: %w", err)
	}
	return &status, nil
}

func (s *Service) GetReposWithSuccessfulCI(ctx context.Context) ([]database.ReleaseRepo, error) {
	var repos []database.ReleaseRepo
	err := s.db.WithContext(ctx).
		Joins("INNER JOIN repo_ci_statuses ON repo_ci_statuses.release_repo_id = release_repos.id").
		Where("repo_ci_statuses.status = ?", "success").
		Where("repo_ci_statuses.chart_version != ''").
		Where("release_repos.excluded = ?", false).
		Find(&repos).Error
	if err != nil {
		return nil, fmt.Errorf("getting repos with successful CI: %w", err)
	}
	return repos, nil
}
