# CI Tracking Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Track GitHub Actions CI status for each repo in a release, showing build status, run numbers, and CHART_VERSION from Helm job logs.

**Architecture:** Add `RepoCIStatus` model linked 1:1 with `ReleaseRepo`. When repo data is gathered, detect merged PRs and find their workflow runs. Background goroutine polls until completion, then extracts CHART_VERSION from Helm job logs.

**Tech Stack:** Go (gorm, zerolog), Vue 3, TypeScript, GitHub Actions API

---

### Task 1: Add RepoCIStatus Model

**Files:**
- Modify: `internal/database/models.go`

**Step 1: Add the RepoCIStatus struct to models.go**

After the `ReleaseHistory` struct, add:

```go
type RepoCIStatus struct {
	ID             uint   `gorm:"primaryKey;autoIncrement"`
	ReleaseRepoID  uint   `gorm:"uniqueIndex;not null"`
	WorkflowRunID  int64
	WorkflowRunNum int
	WorkflowURL    string
	Status         string `gorm:"default:pending"`
	ChartVersion   string
	MergeCommitSHA string
	StartedAt      int64
	CompletedAt    int64
	LastCheckedAt  int64
}
```

**Step 2: Run build to verify**

Run: `go build ./...`
Expected: PASS

---

### Task 2: Add MergeCommitSHA to ReleaseRepo

**Files:**
- Modify: `internal/database/models.go`

**Step 1: Add MergeCommitSHA field to ReleaseRepo struct**

Add after `InfraChanges` field:

```go
MergeCommitSHA string
```

**Step 2: Run build to verify**

Run: `go build ./...`
Expected: PASS

---

### Task 3: Add GitHub Workflow Types

**Files:**
- Modify: `pkg/github/types.go`

**Step 1: Add workflow run types at the end of the file**

```go
type WorkflowRun struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	RunNumber    int       `json:"run_number"`
	Status       string    `json:"status"`
	Conclusion   string    `json:"conclusion"`
	HTMLURL      string    `json:"html_url"`
	HeadSHA      string    `json:"head_sha"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	RunStartedAt time.Time `json:"run_started_at"`
}

type WorkflowRunsResponse struct {
	TotalCount   int           `json:"total_count"`
	WorkflowRuns []WorkflowRun `json:"workflow_runs"`
}

type WorkflowJob struct {
	ID          int64     `json:"id"`
	RunID       int64     `json:"run_id"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Conclusion  string    `json:"conclusion"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
}

type WorkflowJobsResponse struct {
	TotalCount int           `json:"total_count"`
	Jobs       []WorkflowJob `json:"jobs"`
}
```

**Step 2: Run build to verify**

Run: `go build ./...`
Expected: PASS

---

### Task 4: Add GetWorkflowRuns Method

**Files:**
- Modify: `pkg/github/client.go`

**Step 1: Add GetWorkflowRuns method at the end of client.go**

```go
func (c *Client) GetWorkflowRuns(ctx context.Context, owner, repo, branch, headSHA string) (*WorkflowRunsResponse, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/actions/runs?branch=%s&head_sha=%s&per_page=5", c.baseURL, owner, repo, branch, headSHA)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API error: %d", resp.StatusCode)
	}

	var result WorkflowRunsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &result, nil
}
```

**Step 2: Run build to verify**

Run: `go build ./...`
Expected: PASS

---

### Task 5: Add GetWorkflowRunByID Method

**Files:**
- Modify: `pkg/github/client.go`

**Step 1: Add GetWorkflowRunByID method**

```go
func (c *Client) GetWorkflowRunByID(ctx context.Context, owner, repo string, runID int64) (*WorkflowRun, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/actions/runs/%d", c.baseURL, owner, repo, runID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API error: %d", resp.StatusCode)
	}

	var result WorkflowRun
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &result, nil
}
```

**Step 2: Run build to verify**

Run: `go build ./...`
Expected: PASS

---

### Task 6: Add GetWorkflowJobs Method

**Files:**
- Modify: `pkg/github/client.go`

**Step 1: Add GetWorkflowJobs method**

```go
func (c *Client) GetWorkflowJobs(ctx context.Context, owner, repo string, runID int64) (*WorkflowJobsResponse, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/actions/runs/%d/jobs", c.baseURL, owner, repo, runID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API error: %d", resp.StatusCode)
	}

	var result WorkflowJobsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &result, nil
}
```

**Step 2: Run build to verify**

Run: `go build ./...`
Expected: PASS

---

### Task 7: Add GetJobLogs Method

**Files:**
- Modify: `pkg/github/client.go`

**Step 1: Add GetJobLogs method**

```go
func (c *Client) GetJobLogs(ctx context.Context, owner, repo string, jobID int64) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/actions/jobs/%d/logs", c.baseURL, owner, repo, jobID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API error: %d", resp.StatusCode)
	}

	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	return buf.String(), nil
}
```

**Step 2: Run build to verify**

Run: `go build ./...`
Expected: PASS

---

### Task 8: Add CI Service Methods - Create/Get

**Files:**
- Modify: `internal/dashboard/service.go`

**Step 1: Add CreateOrUpdateCIStatus method**

Add after the `GetHistory` method:

```go
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

func (s *Service) GetCIStatusByRepoID(ctx context.Context, repoID uint) (*database.RepoCIStatus, error) {
	var status database.RepoCIStatus
	err := s.db.WithContext(ctx).Where("release_repo_id = ?", repoID).First(&status).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("getting CI status: %w", err)
	}
	return &status, nil
}
```

**Step 2: Run build to verify**

Run: `go build ./...`
Expected: PASS

---

### Task 9: Add CI Service Methods - List Incomplete

**Files:**
- Modify: `internal/dashboard/service.go`

**Step 1: Add GetIncompleteCIStatuses method**

```go
func (s *Service) GetIncompleteCIStatuses(ctx context.Context) ([]database.RepoCIStatus, error) {
	var statuses []database.RepoCIStatus
	err := s.db.WithContext(ctx).
		Where("status IN ?", []string{"pending", "in_progress", "queued"}).
		Find(&statuses).Error
	if err != nil {
		return nil, fmt.Errorf("listing incomplete CI statuses: %w", err)
	}
	return statuses, nil
}

func (s *Service) GetCIStatusesForRelease(ctx context.Context, releaseID string) ([]database.RepoCIStatus, error) {
	var statuses []database.RepoCIStatus
	err := s.db.WithContext(ctx).
		Joins("JOIN release_repos ON release_repos.id = repo_ci_statuses.release_repo_id").
		Where("release_repos.release_id = ?", releaseID).
		Find(&statuses).Error
	if err != nil {
		return nil, fmt.Errorf("listing CI statuses for release: %w", err)
	}
	return statuses, nil
}
```

**Step 2: Run build to verify**

Run: `go build ./...`
Expected: PASS

---

### Task 10: Create CI Tracker Module

**Files:**
- Create: `internal/dashboard/ci_tracker.go`

**Step 1: Create the ci_tracker.go file**

```go
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

type CITracker struct {
	service  *Service
	ghClient *github.Client
	org      string
	interval time.Duration
	timeout  time.Duration
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

func NewCITracker(service *Service, ghClient *github.Client, org string) *CITracker {
	return &CITracker{
		service:  service,
		ghClient: ghClient,
		org:      org,
		interval: 30 * time.Second,
		timeout:  24 * time.Hour,
		stopCh:   make(chan struct{}),
	}
}

func (t *CITracker) Start() {
	t.wg.Add(1)
	go t.pollLoop()
	logger.Info().Msg("CI tracker started")
}

func (t *CITracker) Stop() {
	close(t.stopCh)
	t.wg.Wait()
	logger.Info().Msg("CI tracker stopped")
}

func (t *CITracker) pollLoop() {
	defer t.wg.Done()

	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			t.pollIncomplete()
		}
	}
}

func (t *CITracker) pollIncomplete() {
	ctx := context.Background()

	statuses, err := t.service.GetIncompleteCIStatuses(ctx)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to get incomplete CI statuses")
		return
	}

	for _, status := range statuses {
		if err := t.updateStatus(ctx, &status); err != nil {
			logger.Error().Err(err).Uint("repo_id", status.ReleaseRepoID).Msg("Failed to update CI status")
		}
	}
}

func (t *CITracker) updateStatus(ctx context.Context, status *database.RepoCIStatus) error {
	repo, err := t.service.GetRepo(ctx, status.ReleaseRepoID)
	if err != nil {
		return err
	}

	if status.WorkflowRunID == 0 {
		return t.findWorkflowRun(ctx, status, repo)
	}

	return t.checkWorkflowRun(ctx, status, repo)
}

func (t *CITracker) findWorkflowRun(ctx context.Context, status *database.RepoCIStatus, repo *database.ReleaseRepo) error {
	if status.MergeCommitSHA == "" {
		return nil
	}

	release, err := t.service.GetRelease(ctx, repo.ReleaseID)
	if err != nil {
		return err
	}

	runs, err := t.ghClient.GetWorkflowRuns(ctx, t.org, repo.RepoName, release.DestBranch, status.MergeCommitSHA)
	if err != nil {
		return err
	}

	if runs.TotalCount == 0 {
		status.LastCheckedAt = time.Now().Unix()
		checkCount := (time.Now().Unix() - status.LastCheckedAt) / int64(t.interval.Seconds())
		if checkCount > 3 {
			status.Status = "no_workflow"
		}
		return t.service.CreateOrUpdateCIStatus(ctx, status)
	}

	run := runs.WorkflowRuns[0]
	status.WorkflowRunID = run.ID
	status.WorkflowRunNum = run.RunNumber
	status.WorkflowURL = run.HTMLURL
	status.Status = mapGitHubStatus(run.Status, run.Conclusion)
	status.StartedAt = run.RunStartedAt.Unix()
	status.LastCheckedAt = time.Now().Unix()

	if run.Conclusion != "" {
		status.CompletedAt = run.UpdatedAt.Unix()
	}

	return t.service.CreateOrUpdateCIStatus(ctx, status)
}

func (t *CITracker) checkWorkflowRun(ctx context.Context, status *database.RepoCIStatus, repo *database.ReleaseRepo) error {
	run, err := t.ghClient.GetWorkflowRunByID(ctx, t.org, repo.RepoName, status.WorkflowRunID)
	if err != nil {
		return err
	}

	if run == nil {
		status.Status = "not_found"
		return t.service.CreateOrUpdateCIStatus(ctx, status)
	}

	status.Status = mapGitHubStatus(run.Status, run.Conclusion)
	status.LastCheckedAt = time.Now().Unix()

	if run.Conclusion != "" {
		status.CompletedAt = run.UpdatedAt.Unix()
		if run.Conclusion == "success" {
			chartVersion := t.extractChartVersion(ctx, repo.RepoName, status.WorkflowRunID)
			if chartVersion != "" {
				status.ChartVersion = chartVersion
			}
		}
	}

	return t.service.CreateOrUpdateCIStatus(ctx, status)
}

func (t *CITracker) extractChartVersion(ctx context.Context, repoName string, runID int64) string {
	jobs, err := t.ghClient.GetWorkflowJobs(ctx, t.org, repoName, runID)
	if err != nil {
		logger.Warn().Err(err).Msg("Failed to get workflow jobs")
		return ""
	}

	var helmJobID int64
	for _, job := range jobs.Jobs {
		if strings.Contains(strings.ToLower(job.Name), "helm") {
			helmJobID = job.ID
			break
		}
	}

	if helmJobID == 0 {
		return ""
	}

	logs, err := t.ghClient.GetJobLogs(ctx, t.org, repoName, helmJobID)
	if err != nil {
		logger.Warn().Err(err).Msg("Failed to get job logs")
		return ""
	}

	re := regexp.MustCompile(`CHART_VERSION=(\d+\.\d+\.\d+)`)
	matches := re.FindStringSubmatch(logs)
	if len(matches) > 1 {
		return matches[1]
	}

	return ""
}

func mapGitHubStatus(status, conclusion string) string {
	if conclusion != "" {
		switch conclusion {
		case "success":
			return "success"
		case "failure":
			return "failure"
		case "cancelled":
			return "cancelled"
		default:
			return conclusion
		}
	}

	switch status {
	case "queued":
		return "queued"
	case "in_progress":
		return "in_progress"
	case "waiting":
		return "pending"
	default:
		return "pending"
	}
}
```

**Step 2: Run build to verify**

Run: `go build ./...`
Expected: PASS

---

### Task 11: Add InitCITracking Method to Handlers

**Files:**
- Modify: `internal/dashboard/handlers.go`

**Step 1: Add method to initialize CI tracking for a repo**

Add after `gatherRepoData` method:

```go
func (h *Handlers) initCITracking(ctx context.Context, releaseID string, repos []database.ReleaseRepo) {
	for _, repo := range repos {
		if repo.PRNumber == 0 || repo.MergeCommitSHA == "" {
			continue
		}

		status := &database.RepoCIStatus{
			ReleaseRepoID:  repo.ID,
			Status:         "pending",
			MergeCommitSHA: repo.MergeCommitSHA,
			LastCheckedAt:  time.Now().Unix(),
		}

		if err := h.service.CreateOrUpdateCIStatus(ctx, status); err != nil {
			logger.Error().Err(err).Str("repo", repo.RepoName).Msg("Failed to init CI tracking")
		}
	}
}
```

**Step 2: Add import for time package if not present**

Verify `"time"` is in imports.

**Step 3: Run build to verify**

Run: `go build ./...`
Expected: PASS

---

### Task 12: Update gatherRepoData to Capture MergeCommitSHA

**Files:**
- Modify: `internal/dashboard/handlers.go`

**Step 1: Update RepoData struct in service.go to include MergeCommitSHA**

In `internal/dashboard/service.go`, add to `RepoData` struct:

```go
MergeCommitSHA string
```

**Step 2: Update gatherRepoData in handlers.go to capture merge commit SHA**

In the `gatherRepoData` method, after the `pr, _ := h.ghClient.FindPullRequest(...)` line, add logic to get merge commit if PR is merged. For now, we'll use the head SHA from the compare result as a proxy:

Replace the section that builds `data`:

```go
			data := RepoData{
				RepoName:       repo.Name,
				CommitCount:    compare.TotalCommits,
				Additions:      additions,
				Deletions:      deletions,
				Contributors:   contributors,
				Summary:        summary,
				IsBreaking:     isBreaking,
				InfraChanges:   infraChanges,
				MergeCommitSHA: compare.Commits[len(compare.Commits)-1].SHA,
			}
```

Note: Add bounds check for commits slice.

**Step 3: Run build to verify**

Run: `go build ./...`
Expected: PASS

---

### Task 13: Update RefreshRepos to Preserve and Init CI Status

**Files:**
- Modify: `internal/dashboard/service.go`

**Step 1: Update RefreshRepos to save MergeCommitSHA**

In the `RefreshRepos` method, when creating `database.ReleaseRepo`, add:

```go
MergeCommitSHA: r.MergeCommitSHA,
```

**Step 2: Run build to verify**

Run: `go build ./...`
Expected: PASS

---

### Task 14: Add GetCIStatus API Handler

**Files:**
- Modify: `internal/dashboard/handlers.go`

**Step 1: Add GetCIStatus handler method**

```go
func (h *Handlers) GetCIStatus(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/releases/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	releaseID := parts[0]

	statuses, err := h.service.GetCIStatusesForRelease(r.Context(), releaseID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	repoIDToStatus := make(map[uint]database.RepoCIStatus)
	for _, s := range statuses {
		repoIDToStatus[s.ReleaseRepoID] = s
	}

	releaseWithRepos, err := h.service.GetReleaseWithRepos(r.Context(), releaseID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	type ciStatusResponse struct {
		RepoName     string `json:"repo_name"`
		RepoID       uint   `json:"repo_id"`
		Status       string `json:"status"`
		RunNumber    int    `json:"run_number"`
		RunURL       string `json:"run_url"`
		ChartVersion string `json:"chart_version"`
		StartedAt    int64  `json:"started_at"`
		CompletedAt  int64  `json:"completed_at"`
	}

	var response []ciStatusResponse
	anyInProgress := false

	for _, repo := range releaseWithRepos.Repos {
		status, exists := repoIDToStatus[repo.ID]
		resp := ciStatusResponse{
			RepoName: repo.RepoName,
			RepoID:   repo.ID,
		}
		if exists {
			resp.Status = status.Status
			resp.RunNumber = status.WorkflowRunNum
			resp.RunURL = status.WorkflowURL
			resp.ChartVersion = status.ChartVersion
			resp.StartedAt = status.StartedAt
			resp.CompletedAt = status.CompletedAt
			if status.Status == "pending" || status.Status == "in_progress" || status.Status == "queued" {
				anyInProgress = true
			}
		}
		response = append(response, resp)
	}

	respondJSON(w, map[string]interface{}{
		"statuses":        response,
		"any_in_progress": anyInProgress,
	})
}
```

**Step 2: Run build to verify**

Run: `go build ./...`
Expected: PASS

---

### Task 15: Register CI Status Route

**Files:**
- Modify: `internal/dashboard/server.go`

**Step 1: Read server.go to find route registration pattern**

**Step 2: Add CI status route**

Add in the routes setup section:

```go
mux.HandleFunc("/api/releases/", func(w http.ResponseWriter, r *http.Request) {
	// existing routing logic...
	// Add case for ci-status:
	if strings.HasSuffix(r.URL.Path, "/ci-status") && r.Method == http.MethodGet {
		handlers.GetCIStatus(w, r)
		return
	}
})
```

**Step 3: Run build to verify**

Run: `go build ./...`
Expected: PASS

---

### Task 16: Add CI Summary to GetRelease Response

**Files:**
- Modify: `internal/dashboard/handlers.go`

**Step 1: Update GetRelease handler to include CI summary**

In the `GetRelease` method, after getting repos, add:

```go
	ciStatuses, _ := h.service.GetCIStatusesForRelease(r.Context(), id)
	ciSummary := map[string]int{
		"total":       len(ciStatuses),
		"success":     0,
		"failed":      0,
		"in_progress": 0,
		"pending":     0,
	}
	for _, s := range ciStatuses {
		switch s.Status {
		case "success":
			ciSummary["success"]++
		case "failure", "cancelled":
			ciSummary["failed"]++
		case "in_progress", "queued":
			ciSummary["in_progress"]++
		default:
			ciSummary["pending"]++
		}
	}
```

Then add to the response:

```go
	respondJSON(w, map[string]interface{}{
		"release":    release.Release,
		"repos":      repos,
		"org":        h.org,
		"ci_summary": ciSummary,
	})
```

**Step 2: Run build to verify**

Run: `go build ./...`
Expected: PASS

---

### Task 17: Start CI Tracker in Serve Command

**Files:**
- Modify: `internal/commands/serve/serve.go`

**Step 1: Read serve.go to understand startup flow**

**Step 2: Add CI tracker initialization and start**

After creating handlers, add:

```go
var ciTracker *dashboard.CITracker
if ghClient != nil {
	ciTracker = dashboard.NewCITracker(svc, ghClient, cfg.GitHubOrg)
	ciTracker.Start()
}
```

**Step 3: Add graceful shutdown**

In the shutdown logic, add:

```go
if ciTracker != nil {
	ciTracker.Stop()
}
```

**Step 4: Run build to verify**

Run: `go build ./...`
Expected: PASS

---

### Task 18: Add Frontend CI Status Types

**Files:**
- Modify: `web/src/api/types.ts`

**Step 1: Add CI status types**

```typescript
export interface CIStatus {
  repo_name: string
  repo_id: number
  status: string
  run_number: number
  run_url: string
  chart_version: string
  started_at: number
  completed_at: number
}

export interface CIStatusResponse {
  statuses: CIStatus[]
  any_in_progress: boolean
}

export interface CISummary {
  total: number
  success: number
  failed: number
  in_progress: number
  pending: number
}
```

**Step 2: Update ReleaseWithRepos to include ci_summary**

```typescript
export interface ReleaseWithRepos {
  release: Release
  repos: ReleaseRepo[]
  org: string
  ci_summary?: CISummary
}
```

---

### Task 19: Add Frontend API Client Methods

**Files:**
- Modify: `web/src/api/client.ts`

**Step 1: Add getCIStatus method to releaseApi**

```typescript
async getCIStatus(id: string): Promise<CIStatusResponse> {
  const res = await fetch(`/api/releases/${id}/ci-status`)
  if (!res.ok) throw new Error('Failed to fetch CI status')
  return res.json()
}
```

**Step 2: Add import for CIStatusResponse type**

---

### Task 20: Add CI Status Badge Component

**Files:**
- Modify: `web/src/views/ReleaseDetailView.vue`

**Step 1: Add CI status ref and fetch function**

In the script section, add:

```typescript
const ciStatuses = ref<Map<number, CIStatus>>(new Map())

async function loadCIStatus() {
  try {
    const response = await releaseApi.getCIStatus(releaseId)
    const statusMap = new Map<number, CIStatus>()
    for (const s of response.statuses) {
      statusMap.set(s.repo_id, s)
    }
    ciStatuses.value = statusMap

    if (response.any_in_progress) {
      setTimeout(loadCIStatus, 10000)
    }
  } catch (error) {
    console.error('Failed to load CI status:', error)
  }
}
```

**Step 2: Call loadCIStatus in onMounted**

**Step 3: Add helper function for CI status display**

```typescript
function getCIStatusBadge(repoId: number) {
  const status = ciStatuses.value.get(repoId)
  if (!status || !status.status) return null
  return status
}
```

**Step 4: Add CI status badge in repo row template**

In the template where repo info is displayed, add a CI status badge showing:
- Status icon (✓ green, ✗ red, ⏳ yellow)
- Run number as link
- Chart version if available

---

### Task 21: Add Deployments Section to UI

**Files:**
- Modify: `web/src/views/ReleaseDetailView.vue`

**Step 1: Add a "Deployments" collapsible section**

Add a new section after the repos list:

```vue
<div class="mt-8">
  <h3 class="text-lg font-medium text-gray-900 mb-4">CI/CD Status</h3>
  <table class="min-w-full divide-y divide-gray-200">
    <thead>
      <tr>
        <th class="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Repo</th>
        <th class="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Status</th>
        <th class="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Run</th>
        <th class="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Chart Version</th>
        <th class="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Completed</th>
      </tr>
    </thead>
    <tbody class="divide-y divide-gray-200">
      <tr v-for="repo in repos" :key="repo.ID">
        <td class="px-4 py-2 text-sm text-gray-900">{{ repo.RepoName }}</td>
        <td class="px-4 py-2">
          <span :class="getCIStatusClass(repo.ID)">
            {{ getCIStatusText(repo.ID) }}
          </span>
        </td>
        <td class="px-4 py-2 text-sm">
          <a v-if="getCIStatusBadge(repo.ID)?.run_url"
             :href="getCIStatusBadge(repo.ID)?.run_url"
             target="_blank"
             class="text-indigo-600 hover:text-indigo-800">
            #{{ getCIStatusBadge(repo.ID)?.run_number }}
          </a>
          <span v-else class="text-gray-400">—</span>
        </td>
        <td class="px-4 py-2 text-sm text-gray-900">
          {{ getCIStatusBadge(repo.ID)?.chart_version || '—' }}
        </td>
        <td class="px-4 py-2 text-sm text-gray-500">
          {{ formatTime(getCIStatusBadge(repo.ID)?.completed_at) }}
        </td>
      </tr>
    </tbody>
  </table>
</div>
```

**Step 2: Add helper functions for status display**

```typescript
function getCIStatusClass(repoId: number): string {
  const status = ciStatuses.value.get(repoId)
  if (!status) return 'text-gray-400'
  switch (status.status) {
    case 'success': return 'text-green-600 font-medium'
    case 'failure': case 'cancelled': return 'text-red-600 font-medium'
    case 'in_progress': case 'queued': return 'text-yellow-600 font-medium'
    default: return 'text-gray-400'
  }
}

function getCIStatusText(repoId: number): string {
  const status = ciStatuses.value.get(repoId)
  if (!status || !status.status) return '—'
  switch (status.status) {
    case 'success': return '✓ Success'
    case 'failure': return '✗ Failed'
    case 'cancelled': return '✗ Cancelled'
    case 'in_progress': return '⏳ Running'
    case 'queued': return '○ Queued'
    case 'pending': return '○ Pending'
    case 'no_workflow': return '— No workflow'
    default: return status.status
  }
}

function formatTime(timestamp?: number): string {
  if (!timestamp) return '—'
  return new Date(timestamp * 1000).toLocaleString()
}
```

---

### Task 22: Run Database Migration

**Step 1: The migration happens automatically with gorm AutoMigrate**

Verify that sqlite.go includes AutoMigrate for the new model. If needed, add:

```go
db.AutoMigrate(&RepoCIStatus{})
```

**Step 2: Run the application to trigger migration**

Run: `go run ./cmd/mmtools serve`
Expected: Application starts and creates new table

---

### Task 23: Integration Test

**Step 1: Build the project**

Run: `go build ./...`
Expected: PASS

**Step 2: Start the frontend dev server**

Run: `cd web && npm run dev`

**Step 3: Manual testing checklist**

- [ ] Create a new release
- [ ] Verify CI status section appears (initially empty or pending)
- [ ] If repos have merged PRs, verify CI tracking starts
- [ ] Check CI status auto-refreshes
- [ ] Verify chart version appears for repos with Helm jobs

---
