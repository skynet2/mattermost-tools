package dashboard

import (
	"context"
	"sync"
	"time"

	"github.com/user/mattermost-tools/internal/database"
	"github.com/user/mattermost-tools/internal/logger"
	"github.com/user/mattermost-tools/pkg/github"
)

type CITracker struct {
	service  *Service
	ghClient *github.Client
	org      string
	interval time.Duration
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

func NewCITracker(service *Service, ghClient *github.Client, org string, interval time.Duration) *CITracker {
	if interval == 0 {
		interval = 30 * time.Second
	}
	return &CITracker{
		service:  service,
		ghClient: ghClient,
		org:      org,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

func (t *CITracker) Start() {
	t.wg.Add(1)
	go t.run()
}

func (t *CITracker) Stop() {
	close(t.stopCh)
	t.wg.Wait()
}

func (t *CITracker) run() {
	defer t.wg.Done()
	log := logger.Get()

	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	log.Info().Dur("interval", t.interval).Msg("CI tracker started")

	for {
		select {
		case <-t.stopCh:
			log.Info().Msg("CI tracker stopped")
			return
		case <-ticker.C:
			t.checkIncompleteStatuses()
		}
	}
}

func (t *CITracker) checkIncompleteStatuses() {
	log := logger.Get()
	ctx := context.Background()

	statuses, err := t.service.GetIncompleteCIStatuses(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get incomplete CI statuses")
		return
	}

	if len(statuses) == 0 {
		return
	}

	log.Debug().Int("count", len(statuses)).Msg("Checking incomplete CI statuses")

	for _, status := range statuses {
		t.updateCIStatus(ctx, &status)
	}
}

func (t *CITracker) updateCIStatus(ctx context.Context, status *database.RepoCIStatus) {
	log := logger.Get()

	repo, err := t.service.GetRepo(ctx, status.ReleaseRepoID)
	if err != nil {
		log.Error().Err(err).Uint("repo_id", status.ReleaseRepoID).Msg("Failed to get repo")
		return
	}

	if status.WorkflowRunID == 0 {
		t.findWorkflowRun(ctx, status, repo)
		return
	}

	run, err := t.ghClient.GetWorkflowRunByID(ctx, t.org, repo.RepoName, status.WorkflowRunID)
	if err != nil {
		log.Error().Err(err).Int64("run_id", status.WorkflowRunID).Msg("Failed to get workflow run")
		return
	}

	if run == nil {
		log.Warn().Int64("run_id", status.WorkflowRunID).Msg("Workflow run not found")
		return
	}

	status.Status = mapWorkflowStatus(run.Status, run.Conclusion)
	status.LastCheckedAt = time.Now().Unix()

	if run.Status == "completed" {
		status.CompletedAt = run.UpdatedAt.Unix()
	}

	if err := t.service.CreateOrUpdateCIStatus(ctx, status); err != nil {
		log.Error().Err(err).Uint("repo_id", status.ReleaseRepoID).Msg("Failed to update CI status")
	}
}

func (t *CITracker) findWorkflowRun(ctx context.Context, status *database.RepoCIStatus, repo *database.ReleaseRepo) {
	log := logger.Get()

	if repo.MergeCommitSHA == "" {
		log.Debug().Str("repo", repo.RepoName).Msg("No merge commit SHA for repo")
		return
	}

	runs, err := t.ghClient.GetWorkflowRuns(ctx, t.org, repo.RepoName, repo.MergeCommitSHA)
	if err != nil {
		log.Error().Err(err).Str("repo", repo.RepoName).Msg("Failed to get workflow runs")
		return
	}

	if runs == nil || len(runs.WorkflowRuns) == 0 {
		log.Debug().Str("repo", repo.RepoName).Str("sha", repo.MergeCommitSHA).Msg("No workflow runs found")
		return
	}

	run := runs.WorkflowRuns[0]
	status.WorkflowRunID = run.ID
	status.WorkflowRunNum = run.RunNumber
	status.WorkflowURL = run.HTMLURL
	status.Status = mapWorkflowStatus(run.Status, run.Conclusion)
	status.MergeCommitSHA = run.HeadSHA
	status.StartedAt = run.RunStartedAt.Unix()
	status.LastCheckedAt = time.Now().Unix()

	if run.Status == "completed" {
		status.CompletedAt = run.UpdatedAt.Unix()
	}

	if err := t.service.CreateOrUpdateCIStatus(ctx, status); err != nil {
		log.Error().Err(err).Str("repo", repo.RepoName).Msg("Failed to save CI status")
	}

	log.Info().
		Str("repo", repo.RepoName).
		Int64("run_id", run.ID).
		Str("status", status.Status).
		Msg("Found workflow run")
}

func (t *CITracker) InitCITracking(ctx context.Context, releaseID string) error {
	log := logger.Get()

	repos, err := t.service.GetReposByReleaseID(ctx, releaseID)
	if err != nil {
		return err
	}

	for _, repo := range repos {
		if repo.Excluded {
			continue
		}

		status := &database.RepoCIStatus{
			ReleaseRepoID:  repo.ID,
			Status:         "pending",
			MergeCommitSHA: repo.MergeCommitSHA,
			LastCheckedAt:  time.Now().Unix(),
		}

		if err := t.service.CreateOrUpdateCIStatus(ctx, status); err != nil {
			log.Error().Err(err).Str("repo", repo.RepoName).Msg("Failed to create CI status")
			continue
		}

		if repo.MergeCommitSHA != "" {
			t.findWorkflowRun(ctx, status, &repo)
		}
	}

	return nil
}

func mapWorkflowStatus(status, conclusion string) string {
	switch status {
	case "queued", "waiting":
		return "queued"
	case "in_progress":
		return "in_progress"
	case "completed":
		switch conclusion {
		case "success":
			return "success"
		case "failure":
			return "failure"
		case "cancelled":
			return "cancelled"
		case "skipped":
			return "skipped"
		default:
			return conclusion
		}
	default:
		return status
	}
}
