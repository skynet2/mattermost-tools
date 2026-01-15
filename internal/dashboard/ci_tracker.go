package dashboard

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/user/mattermost-tools/internal/database"
	"github.com/user/mattermost-tools/internal/logger"
	"github.com/user/mattermost-tools/pkg/github"
)

var chartVersionRegex = regexp.MustCompile(`CHART_VERSION=(\d+\.\d+\.\d+)`)
var chartInfoRegex = regexp.MustCompile(`Chart:\s+(\S+)\s+v?(\d+\.\d+\.\d+)`)

type ciCache struct {
	statuses  []database.RepoCIStatus
	fetchedAt time.Time
	fetching  bool
}

type CITracker struct {
	service   *Service
	ghClient  *github.Client
	org       string
	interval  time.Duration
	cacheTTL  time.Duration
	stopCh    chan struct{}
	wg        sync.WaitGroup
	cache     map[string]*ciCache
	cacheMu   sync.RWMutex
	fetchLock map[string]*sync.Mutex
	fetchMu   sync.Mutex
}

func NewCITracker(service *Service, ghClient *github.Client, org string, interval time.Duration) *CITracker {
	if interval == 0 {
		interval = 30 * time.Second
	}
	return &CITracker{
		service:   service,
		ghClient:  ghClient,
		org:       org,
		interval:  interval,
		cacheTTL:  5 * time.Second,
		stopCh:    make(chan struct{}),
		cache:     make(map[string]*ciCache),
		fetchLock: make(map[string]*sync.Mutex),
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

	t.cacheMu.Lock()
	t.cache = make(map[string]*ciCache)
	t.cacheMu.Unlock()
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
		if status.ChartVersion == "" {
			status.ChartName, status.ChartVersion = t.extractChartInfo(ctx, repo.RepoName, status.WorkflowRunID)
		}
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

	var run *github.WorkflowRun
	for i := range runs.WorkflowRuns {
		r := &runs.WorkflowRuns[i]
		if strings.HasSuffix(r.Path, "general.yaml") || strings.HasSuffix(r.Path, "general.yml") {
			run = r
			break
		}
	}

	if run == nil {
		run = &runs.WorkflowRuns[0]
		log.Debug().Str("repo", repo.RepoName).Str("path", run.Path).Msg("No general.yaml workflow found, using first workflow")
	}

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
		Str("workflow_path", run.Path).
		Str("status", status.Status).
		Msg("Found workflow run")
}

func (t *CITracker) InitCITracking(ctx context.Context, releaseID string) error {
	log := logger.Get()

	repos, err := t.service.GetReposByReleaseID(ctx, releaseID)
	if err != nil {
		log.Error().Err(err).Str("release_id", releaseID).Msg("Failed to get repos for CI tracking")
		return err
	}

	log.Info().Str("release_id", releaseID).Int("repo_count", len(repos)).Msg("Initializing CI tracking")

	for _, repo := range repos {
		if repo.Excluded {
			log.Debug().Str("repo", repo.RepoName).Msg("Skipping excluded repo")
			continue
		}

		log.Info().
			Str("repo", repo.RepoName).
			Uint("repo_id", repo.ID).
			Str("merge_commit_sha", repo.MergeCommitSHA).
			Int("pr_number", repo.PRNumber).
			Msg("Creating CI status for repo")

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
		} else {
			log.Warn().Str("repo", repo.RepoName).Msg("No merge commit SHA - PR may not be merged yet")
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

func (t *CITracker) getFetchLock(releaseID string) *sync.Mutex {
	t.fetchMu.Lock()
	defer t.fetchMu.Unlock()
	if _, ok := t.fetchLock[releaseID]; !ok {
		t.fetchLock[releaseID] = &sync.Mutex{}
	}
	return t.fetchLock[releaseID]
}

func (t *CITracker) GetCachedCIStatuses(ctx context.Context, releaseID string) ([]database.RepoCIStatus, bool) {
	t.cacheMu.RLock()
	cached, exists := t.cache[releaseID]
	if exists && time.Since(cached.fetchedAt) < t.cacheTTL {
		statuses := cached.statuses
		t.cacheMu.RUnlock()
		anyInProgress := false
		for _, s := range statuses {
			if s.Status == "pending" || s.Status == "queued" || s.Status == "in_progress" {
				anyInProgress = true
				break
			}
		}
		return statuses, anyInProgress
	}
	t.cacheMu.RUnlock()

	lock := t.getFetchLock(releaseID)
	lock.Lock()
	defer lock.Unlock()

	t.cacheMu.RLock()
	cached, exists = t.cache[releaseID]
	if exists && time.Since(cached.fetchedAt) < t.cacheTTL {
		statuses := cached.statuses
		t.cacheMu.RUnlock()
		anyInProgress := false
		for _, s := range statuses {
			if s.Status == "pending" || s.Status == "queued" || s.Status == "in_progress" {
				anyInProgress = true
				break
			}
		}
		return statuses, anyInProgress
	}
	t.cacheMu.RUnlock()

	statuses, err := t.service.GetCIStatusesForRelease(ctx, releaseID)
	if err != nil {
		log := logger.Get()
		log.Error().Err(err).Str("release_id", releaseID).Msg("Failed to get CI statuses")
		return nil, false
	}

	t.cacheMu.Lock()
	t.cache[releaseID] = &ciCache{
		statuses:  statuses,
		fetchedAt: time.Now(),
	}
	t.cacheMu.Unlock()

	anyInProgress := false
	for _, s := range statuses {
		if s.Status == "pending" || s.Status == "queued" || s.Status == "in_progress" {
			anyInProgress = true
			break
		}
	}
	return statuses, anyInProgress
}

func (t *CITracker) InvalidateCache(releaseID string) {
	t.cacheMu.Lock()
	delete(t.cache, releaseID)
	t.cacheMu.Unlock()
}

func (t *CITracker) extractChartInfo(ctx context.Context, repoName string, runID int64) (chartName, chartVersion string) {
	log := logger.Get()

	jobs, err := t.ghClient.GetWorkflowJobs(ctx, t.org, repoName, runID)
	if err != nil {
		log.Error().Err(err).Str("repo", repoName).Int64("run_id", runID).Msg("Failed to get workflow jobs")
		return "", ""
	}

	if jobs == nil || len(jobs.Jobs) == 0 {
		log.Debug().Str("repo", repoName).Int64("run_id", runID).Msg("No jobs found for workflow run")
		return "", ""
	}

	for _, job := range jobs.Jobs {
		if !strings.Contains(strings.ToLower(job.Name), "helm") &&
			!strings.Contains(strings.ToLower(job.Name), "build") &&
			!strings.Contains(strings.ToLower(job.Name), "release") {
			continue
		}

		logs, err := t.ghClient.GetJobLogs(ctx, t.org, repoName, job.ID)
		if err != nil {
			log.Debug().Err(err).Str("repo", repoName).Int64("job_id", job.ID).Msg("Failed to get job logs")
			continue
		}

		matches := chartInfoRegex.FindStringSubmatch(logs)
		if len(matches) > 2 {
			log.Info().Str("repo", repoName).Str("chart_name", matches[1]).Str("chart_version", matches[2]).Msg("Found chart info")
			return matches[1], matches[2]
		}

		matches = chartVersionRegex.FindStringSubmatch(logs)
		if len(matches) > 1 {
			log.Info().Str("repo", repoName).Str("chart_version", matches[1]).Msg("Found chart version (no name)")
			return "", matches[1]
		}
	}

	log.Debug().Str("repo", repoName).Int64("run_id", runID).Msg("Chart info not found in job logs")
	return "", ""
}

func (t *CITracker) RefreshChartInfo(ctx context.Context, releaseRepoID uint) (chartName, chartVersion string, err error) {
	log := logger.Get()

	status, err := t.service.GetCIStatusByRepoID(ctx, releaseRepoID)
	if err != nil {
		return "", "", err
	}
	if status == nil {
		return "", "", nil
	}
	if status.WorkflowRunID == 0 {
		return "", "", nil
	}

	repo, err := t.service.GetRepo(ctx, releaseRepoID)
	if err != nil {
		return "", "", err
	}

	chartName, chartVersion = t.extractChartInfo(ctx, repo.RepoName, status.WorkflowRunID)
	if chartVersion != "" {
		status.ChartName = chartName
		status.ChartVersion = chartVersion
		if err := t.service.CreateOrUpdateCIStatus(ctx, status); err != nil {
			log.Error().Err(err).Uint("repo_id", releaseRepoID).Msg("Failed to update chart info")
			return "", "", err
		}
	}

	return chartName, chartVersion, nil
}
