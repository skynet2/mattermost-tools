# Release Playbook Integration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enable the bot to create and manage Mattermost Playbook runs for production releases, with auto-generated repository tables and progress tracking.

**Architecture:** Add a Playbooks API client to interact with Mattermost Playbooks plugin. Create a release manager that tracks active releases by channel ID. The `create-release` command gathers repo changes, contributors, and PR status, then creates a playbook run with a structured markdown document. Commands like `changes` and `release-prs` update the release document when run in a release channel.

**Tech Stack:** Go, Mattermost Playbooks REST API, existing GitHub client

---

## Task 1: Add Playbooks Types

**Files:**
- Create: `pkg/mattermost/playbooks_types.go`

**Step 1: Create playbook types file**

```go
package mattermost

type PlaybookRun struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Description          string   `json:"description"`
	OwnerUserID          string   `json:"owner_user_id"`
	TeamID               string   `json:"team_id"`
	ChannelID            string   `json:"channel_id"`
	PlaybookID           string   `json:"playbook_id"`
	CurrentStatus        string   `json:"current_status"`
	CreateAt             int64    `json:"create_at"`
	ParticipantIDs       []string `json:"participant_ids"`
}

type CreatePlaybookRunRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	OwnerUserID string `json:"owner_user_id"`
	TeamID      string `json:"team_id"`
	PlaybookID  string `json:"playbook_id"`
}

type PlaybookRunResponse struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
}

type Checklist struct {
	Title string          `json:"title"`
	Items []ChecklistItem `json:"items"`
}

type ChecklistItem struct {
	Title string `json:"title"`
}

type Playbook struct {
	ID                      string      `json:"id"`
	Title                   string      `json:"title"`
	Description             string      `json:"description"`
	TeamID                  string      `json:"team_id"`
	CreatePublicPlaybookRun bool        `json:"create_public_playbook_run"`
	Public                  bool        `json:"public"`
	Checklists              []Checklist `json:"checklists"`
	MemberIDs               []string    `json:"member_ids"`
	InvitedUserIDs          []string    `json:"invited_user_ids"`
	InviteUsersEnabled      bool        `json:"invite_users_enabled"`
}

type CreatePlaybookRequest struct {
	Title                   string      `json:"title"`
	Description             string      `json:"description"`
	TeamID                  string      `json:"team_id"`
	CreatePublicPlaybookRun bool        `json:"create_public_playbook_run"`
	Public                  bool        `json:"public"`
	Checklists              []Checklist `json:"checklists"`
	MemberIDs               []string    `json:"member_ids"`
	InvitedUserIDs          []string    `json:"invited_user_ids"`
	InviteUsersEnabled      bool        `json:"invite_users_enabled"`
}
```

**Step 2: Commit**

```bash
git add pkg/mattermost/playbooks_types.go
git commit -m "feat: add Mattermost Playbooks types"
```

---

## Task 2: Add Playbooks API Client

**Files:**
- Create: `pkg/mattermost/playbooks.go`

**Step 1: Create playbooks client**

```go
package mattermost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type PlaybooksClient struct {
	baseURL    string
	token      string
	httpClient HTTPDoer
}

func NewPlaybooksClient(baseURL, token string) *PlaybooksClient {
	return &PlaybooksClient{
		baseURL:    baseURL,
		token:      token,
		httpClient: &http.Client{},
	}
}

func (c *PlaybooksClient) CreatePlaybook(ctx context.Context, req CreatePlaybookRequest) (*Playbook, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	url := fmt.Sprintf("%s/plugins/playbooks/api/v0/playbooks", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error: %d", resp.StatusCode)
	}

	var playbook Playbook
	if err := json.NewDecoder(resp.Body).Decode(&playbook); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &playbook, nil
}

func (c *PlaybooksClient) CreateRun(ctx context.Context, req CreatePlaybookRunRequest) (*PlaybookRunResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	url := fmt.Sprintf("%s/plugins/playbooks/api/v0/runs", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error: %d", resp.StatusCode)
	}

	var runResp PlaybookRunResponse
	if err := json.NewDecoder(resp.Body).Decode(&runResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &runResp, nil
}

func (c *PlaybooksClient) GetRunByChannelID(ctx context.Context, channelID string) (*PlaybookRun, error) {
	url := fmt.Sprintf("%s/plugins/playbooks/api/v0/runs?channel_id=%s", c.baseURL, channelID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error: %d", resp.StatusCode)
	}

	var result struct {
		Items []PlaybookRun `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if len(result.Items) == 0 {
		return nil, nil
	}

	return &result.Items[0], nil
}

func (c *PlaybooksClient) AddParticipants(ctx context.Context, runID string, userIDs []string) error {
	for _, userID := range userIDs {
		body, _ := json.Marshal(map[string]string{"user_id": userID})
		url := fmt.Sprintf("%s/plugins/playbooks/api/v0/runs/%s/participants", c.baseURL, runID)
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("creating request: %w", err)
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+c.token)

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			return fmt.Errorf("executing request: %w", err)
		}
		resp.Body.Close()
	}
	return nil
}
```

**Step 2: Commit**

```bash
git add pkg/mattermost/playbooks.go
git commit -m "feat: add Mattermost Playbooks API client"
```

---

## Task 3: Add User Lookup to Mattermost Client

**Files:**
- Modify: `pkg/mattermost/bot.go`

**Step 1: Add GetUserByUsername method to Bot**

Add to `pkg/mattermost/bot.go`:

```go
type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

func (b *Bot) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	url := fmt.Sprintf("%s/api/v4/users/username/%s", b.baseURL, username)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+b.token)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error: %d", resp.StatusCode)
	}

	var user User
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &user, nil
}

func (b *Bot) GetMe(ctx context.Context) (*User, error) {
	url := fmt.Sprintf("%s/api/v4/users/me", b.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+b.token)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error: %d", resp.StatusCode)
	}

	var user User
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &user, nil
}
```

**Step 2: Add encoding/json import if not present**

**Step 3: Commit**

```bash
git add pkg/mattermost/bot.go
git commit -m "feat: add user lookup methods to Mattermost Bot client"
```

---

## Task 4: Add Release Configuration

**Files:**
- Modify: `internal/config/config.go`
- Modify: `config.yaml.example`

**Step 1: Update config struct**

Add to `ServeConfig` in `internal/config/config.go`:

```go
type ServeConfig struct {
	Port               int                 `yaml:"port"`
	MattermostURL      string              `yaml:"mattermost_url"`
	MattermostToken    string              `yaml:"mattermost_token"`
	AllowedTokens      []string            `yaml:"allowed_tokens"`
	CommandPermissions map[string][]string `yaml:"command_permissions"`
	Release            ReleaseConfig       `yaml:"release"`
}

type ReleaseConfig struct {
	TeamID          string   `yaml:"team_id"`
	PlaybookID      string   `yaml:"playbook_id"`
	DefaultReviewers []string `yaml:"default_reviewers"`
	DefaultQA        []string `yaml:"default_qa"`
}
```

**Step 2: Update config.yaml.example**

Add to config.yaml.example:

```yaml
  # Release playbook settings
  release:
    # Mattermost Team ID (find in System Console or via API)
    team_id: "your-team-id"
    # Playbook ID to use as template (create one manually first)
    playbook_id: "your-playbook-id"
    # Default reviewers to invite (Mattermost usernames)
    default_reviewers:
      - reviewer1
      - reviewer2
    # Default QA to invite (Mattermost usernames)
    default_qa:
      - qa1
```

**Step 3: Commit**

```bash
git add internal/config/config.go config.yaml.example
git commit -m "feat: add release configuration options"
```

---

## Task 5: Create Release Manager

**Files:**
- Create: `pkg/release/manager.go`
- Create: `pkg/release/types.go`

**Step 1: Create release types**

Create `pkg/release/types.go`:

```go
package release

type RepoStatus struct {
	Name         string
	Commits      int
	Contributors []string
	PRURL        string
	PRNumber     int
	HasPR        bool
	DevApproved  bool
	QAApproved   bool
}

type Release struct {
	ID           string
	ChannelID    string
	SourceBranch string
	DestBranch   string
	Repos        []RepoStatus
	CreatedBy    string
	CreatedAt    int64
}
```

**Step 2: Create release manager**

Create `pkg/release/manager.go`:

```go
package release

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/user/mattermost-tools/internal/mappings"
	"github.com/user/mattermost-tools/pkg/github"
	"github.com/user/mattermost-tools/pkg/mattermost"
)

type Manager struct {
	ghClient        *github.Client
	mmBot           *mattermost.Bot
	playbooksClient *mattermost.PlaybooksClient
	org             string
	ignoredRepos    map[string]struct{}
	releases        map[string]*Release // channelID -> Release
	mu              sync.RWMutex
}

func NewManager(ghClient *github.Client, mmBot *mattermost.Bot, playbooksClient *mattermost.PlaybooksClient, org string, ignoredRepos map[string]struct{}) *Manager {
	return &Manager{
		ghClient:        ghClient,
		mmBot:           mmBot,
		playbooksClient: playbooksClient,
		org:             org,
		ignoredRepos:    ignoredRepos,
		releases:        make(map[string]*Release),
	}
}

func (m *Manager) CreateRelease(ctx context.Context, teamID, playbookID, ownerUserID, sourceBranch, destBranch string, defaultReviewers, defaultQA []string) (*Release, error) {
	repos, err := m.gatherRepoStatuses(ctx, sourceBranch, destBranch)
	if err != nil {
		return nil, fmt.Errorf("gathering repo statuses: %w", err)
	}

	if len(repos) == 0 {
		return nil, fmt.Errorf("no changes found between %s and %s", sourceBranch, destBranch)
	}

	releaseName := fmt.Sprintf("Release: %s → %s (%s)", sourceBranch, destBranch, time.Now().Format("2006-01-02 15:04"))

	runResp, err := m.playbooksClient.CreateRun(ctx, mattermost.CreatePlaybookRunRequest{
		Name:        releaseName,
		Description: fmt.Sprintf("Production release from %s to %s", sourceBranch, destBranch),
		OwnerUserID: ownerUserID,
		TeamID:      teamID,
		PlaybookID:  playbookID,
	})
	if err != nil {
		return nil, fmt.Errorf("creating playbook run: %w", err)
	}

	release := &Release{
		ID:           runResp.ID,
		ChannelID:    runResp.ChannelID,
		SourceBranch: sourceBranch,
		DestBranch:   destBranch,
		Repos:        repos,
		CreatedBy:    ownerUserID,
		CreatedAt:    time.Now().Unix(),
	}

	m.mu.Lock()
	m.releases[runResp.ChannelID] = release
	m.mu.Unlock()

	// Invite contributors and default reviewers
	userIDs := m.collectUserIDs(ctx, repos, defaultReviewers, defaultQA)
	if len(userIDs) > 0 {
		m.playbooksClient.AddParticipants(ctx, runResp.ID, userIDs)
	}

	// Post the release summary
	summary := m.formatReleaseSummary(release)
	m.mmBot.PostMessage(ctx, runResp.ChannelID, summary)

	return release, nil
}

func (m *Manager) gatherRepoStatuses(ctx context.Context, sourceBranch, destBranch string) ([]RepoStatus, error) {
	repos, err := m.ghClient.ListRepositories(ctx, m.org)
	if err != nil {
		return nil, err
	}

	var (
		results []RepoStatus
		mu      sync.Mutex
		wg      sync.WaitGroup
		sem     = make(chan struct{}, 4)
	)

	for _, repo := range repos {
		if repo.Archived {
			continue
		}
		if _, ignored := m.ignoredRepos[repo.Name]; ignored {
			continue
		}

		wg.Add(1)
		go func(repo github.Repository) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			compare, err := m.ghClient.CompareBranches(ctx, m.org, repo.Name, destBranch, sourceBranch)
			if err != nil || compare == nil || compare.TotalCommits == 0 || len(compare.Files) == 0 {
				return
			}

			contributors := m.extractContributors(compare.Commits)
			pr, _ := m.ghClient.FindPullRequest(ctx, m.org, repo.Name, sourceBranch, destBranch)

			status := RepoStatus{
				Name:         repo.Name,
				Commits:      compare.TotalCommits,
				Contributors: contributors,
			}

			if pr != nil {
				status.HasPR = true
				status.PRURL = pr.HTMLURL
				status.PRNumber = pr.Number
			}

			mu.Lock()
			results = append(results, status)
			mu.Unlock()
		}(repo)
	}

	wg.Wait()
	return results, nil
}

func (m *Manager) extractContributors(commits []github.Commit) []string {
	seen := make(map[string]struct{})
	var contributors []string

	for _, c := range commits {
		if c.Author.Login == "" {
			continue
		}
		if _, ok := seen[c.Author.Login]; ok {
			continue
		}
		seen[c.Author.Login] = struct{}{}
		contributors = append(contributors, c.Author.Login)
	}

	return contributors
}

func (m *Manager) collectUserIDs(ctx context.Context, repos []RepoStatus, defaultReviewers, defaultQA []string) []string {
	usernames := make(map[string]struct{})

	// Add contributors (mapped to Mattermost)
	for _, repo := range repos {
		for _, ghUser := range repo.Contributors {
			if mmUser, ok := mappings.MattermostFromGitHub(ghUser); ok {
				usernames[mmUser] = struct{}{}
			}
		}
	}

	// Add default reviewers and QA
	for _, u := range defaultReviewers {
		usernames[u] = struct{}{}
	}
	for _, u := range defaultQA {
		usernames[u] = struct{}{}
	}

	var userIDs []string
	for username := range usernames {
		user, err := m.mmBot.GetUserByUsername(ctx, username)
		if err == nil && user != nil {
			userIDs = append(userIDs, user.ID)
		}
	}

	return userIDs
}

func (m *Manager) formatReleaseSummary(release *Release) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Release: `%s` → `%s`\n\n", release.SourceBranch, release.DestBranch))
	sb.WriteString(fmt.Sprintf("**Repositories with changes:** %d\n\n", len(release.Repos)))

	sb.WriteString("### Repository Review Status\n\n")
	sb.WriteString("| Repository | Commits | Contributors | Release PR | Dev | QA |\n")
	sb.WriteString("|------------|---------|--------------|------------|-----|----|\n")

	for _, repo := range release.Repos {
		contributors := m.formatContributors(repo.Contributors)
		prStatus := "❌ Missing"
		if repo.HasPR {
			prStatus = fmt.Sprintf("[#%d](%s)", repo.PRNumber, repo.PRURL)
		}
		devStatus := "⬜"
		qaStatus := "⬜"

		sb.WriteString(fmt.Sprintf("| %s | %d | %s | %s | %s | %s |\n",
			repo.Name, repo.Commits, contributors, prStatus, devStatus, qaStatus))
	}

	sb.WriteString("\n### Legend\n")
	sb.WriteString("- ⬜ Pending review\n")
	sb.WriteString("- ✅ Approved\n")
	sb.WriteString("- ❌ Missing/Rejected\n")

	var missingPRs []string
	for _, repo := range release.Repos {
		if !repo.HasPR {
			missingPRs = append(missingPRs, repo.Name)
		}
	}

	if len(missingPRs) > 0 {
		sb.WriteString("\n### Action Required\n")
		sb.WriteString(fmt.Sprintf("**%d repositories need release PRs:**\n", len(missingPRs)))
		for _, name := range missingPRs {
			sb.WriteString(fmt.Sprintf("- %s\n", name))
		}
	}

	sb.WriteString("\n---\n")
	sb.WriteString("**Commands:** `@pusheen refresh` to update this table\n")

	return sb.String()
}

func (m *Manager) formatContributors(ghUsers []string) string {
	var mmUsers []string
	for _, gh := range ghUsers {
		if mm, ok := mappings.MattermostFromGitHub(gh); ok {
			mmUsers = append(mmUsers, "@"+mm)
		} else {
			mmUsers = append(mmUsers, gh)
		}
	}
	if len(mmUsers) > 3 {
		return fmt.Sprintf("%s +%d", strings.Join(mmUsers[:3], ", "), len(mmUsers)-3)
	}
	return strings.Join(mmUsers, ", ")
}

func (m *Manager) GetReleaseByChannel(channelID string) *Release {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.releases[channelID]
}

func (m *Manager) RefreshRelease(ctx context.Context, channelID string) (*Release, error) {
	m.mu.RLock()
	release := m.releases[channelID]
	m.mu.RUnlock()

	if release == nil {
		return nil, fmt.Errorf("no active release in this channel")
	}

	repos, err := m.gatherRepoStatuses(ctx, release.SourceBranch, release.DestBranch)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	release.Repos = repos
	m.mu.Unlock()

	return release, nil
}
```

**Step 3: Commit**

```bash
git add pkg/release/types.go pkg/release/manager.go
git commit -m "feat: add release manager for playbook integration"
```

---

## Task 6: Integrate Release Commands into serve.go

**Files:**
- Modify: `internal/commands/serve/serve.go`

**Step 1: Add imports and initialize release manager**

Add imports:
```go
import (
	// ... existing imports ...
	"github.com/user/mattermost-tools/pkg/release"
)
```

**Step 2: Initialize PlaybooksClient and ReleaseManager in runServe**

After initializing mmBot, add:

```go
var playbooksClient *mattermost.PlaybooksClient
var releaseManager *release.Manager

if cfg.Serve.MattermostURL != "" && cfg.Serve.MattermostToken != "" {
	mmBot = mattermost.NewBot(cfg.Serve.MattermostURL, cfg.Serve.MattermostToken)
	playbooksClient = mattermost.NewPlaybooksClient(cfg.Serve.MattermostURL, cfg.Serve.MattermostToken)
	releaseManager = release.NewManager(ghClient, mmBot, playbooksClient, org, ignoredRepos)
	// ... rest of wsClient setup
}
```

**Step 3: Update handleWebSocketMessage signature**

Pass releaseManager and cfg.Serve.Release to handleWebSocketMessage.

**Step 4: Add create-release command to switch statement**

Add to the switch statement in handleWebSocketMessage:

```go
case "create-release", "new-release":
	if len(args) != 2 {
		mmBot.PostMessageInThread(ctx, post.ChannelID, threadID, fmt.Sprintf("Usage: `@pusheen create-release <source-branch> <dest-branch>`\nExample: `@pusheen create-release uat master`\n\n_Requested by @%s_", post.Username))
		return
	}
	if releaseManager == nil {
		mmBot.PostMessageInThread(ctx, post.ChannelID, threadID, fmt.Sprintf("Release management not configured.\n\n_Requested by @%s_", post.Username))
		return
	}
	mmBot.PostMessageInThread(ctx, post.ChannelID, threadID, fmt.Sprintf("⏳ Creating release from `%s` to `%s`...\n\n_Requested by @%s_", args[0], args[1], post.Username))
	go processCreateReleaseAsync(releaseManager, mmBot, cfg.Serve.Release, post.ChannelID, threadID, post.Username, args[0], args[1])

case "refresh":
	if releaseManager == nil {
		mmBot.PostMessageInThread(ctx, post.ChannelID, threadID, fmt.Sprintf("Release management not configured.\n\n_Requested by @%s_", post.Username))
		return
	}
	rel := releaseManager.GetReleaseByChannel(post.ChannelID)
	if rel == nil {
		mmBot.PostMessageInThread(ctx, post.ChannelID, threadID, fmt.Sprintf("No active release in this channel.\n\n_Requested by @%s_", post.Username))
		return
	}
	mmBot.PostMessageInThread(ctx, post.ChannelID, threadID, fmt.Sprintf("⏳ Refreshing release status...\n\n_Requested by @%s_", post.Username))
	go processRefreshReleaseAsync(releaseManager, mmBot, post.ChannelID, threadID, post.Username)
```

**Step 5: Add async processing functions**

```go
func processCreateReleaseAsync(releaseManager *release.Manager, mmBot *mattermost.Bot, releaseCfg config.ReleaseConfig, channelID, threadID, userName, sourceBranch, destBranch string) {
	ctx := context.Background()

	me, err := mmBot.GetMe(ctx)
	if err != nil {
		mmBot.PostMessageInThread(ctx, channelID, threadID, fmt.Sprintf("❌ Failed to get bot user: %v\n\n_Requested by @%s_", err, userName))
		return
	}

	ownerUser, err := mmBot.GetUserByUsername(ctx, userName)
	if err != nil || ownerUser == nil {
		mmBot.PostMessageInThread(ctx, channelID, threadID, fmt.Sprintf("❌ Failed to find user @%s\n\n_Requested by @%s_", userName, userName))
		return
	}

	ownerUserID := ownerUser.ID
	if ownerUserID == "" {
		ownerUserID = me.ID
	}

	rel, err := releaseManager.CreateRelease(ctx, releaseCfg.TeamID, releaseCfg.PlaybookID, ownerUserID, sourceBranch, destBranch, releaseCfg.DefaultReviewers, releaseCfg.DefaultQA)
	if err != nil {
		mmBot.PostMessageInThread(ctx, channelID, threadID, fmt.Sprintf("❌ Failed to create release: %v\n\n_Requested by @%s_", err, userName))
		return
	}

	mmBot.PostMessageInThread(ctx, channelID, threadID, fmt.Sprintf("✅ Release created! Channel: ~release-%s\n\n_Requested by @%s_", rel.ID[:8], userName))
}

func processRefreshReleaseAsync(releaseManager *release.Manager, mmBot *mattermost.Bot, channelID, threadID, userName string) {
	ctx := context.Background()

	rel, err := releaseManager.RefreshRelease(ctx, channelID)
	if err != nil {
		mmBot.PostMessageInThread(ctx, channelID, threadID, fmt.Sprintf("❌ Failed to refresh: %v\n\n_Requested by @%s_", err, userName))
		return
	}

	summary := releaseManager.FormatReleaseSummary(rel)
	mmBot.PostMessage(ctx, channelID, fmt.Sprintf("%s\n\n_Refreshed by @%s_", summary, userName))
}
```

**Step 6: Update botHelpText**

Add to botHelpText:

```go
• **create-release <source> <dest>** - Create a release playbook run
  Example: ` + "`@pusheen create-release uat master`" + `

• **refresh** - Refresh release status (in release channel)
```

**Step 7: Commit**

```bash
git add internal/commands/serve/serve.go
git commit -m "feat: integrate release commands into bot"
```

---

## Task 7: Export FormatReleaseSummary Method

**Files:**
- Modify: `pkg/release/manager.go`

**Step 1: Rename formatReleaseSummary to FormatReleaseSummary (exported)**

Change `func (m *Manager) formatReleaseSummary` to `func (m *Manager) FormatReleaseSummary`

**Step 2: Commit**

```bash
git add pkg/release/manager.go
git commit -m "fix: export FormatReleaseSummary method"
```

---

## Task 8: Build and Test

**Step 1: Run go build**

```bash
go build ./...
```

Expected: No errors

**Step 2: Run tests**

```bash
go test ./...
```

Expected: All tests pass

**Step 3: Manual testing checklist**

1. Create a Playbook template manually in Mattermost with basic checklists
2. Get the team_id and playbook_id, add to config.yaml
3. Run the server with `--debug`
4. Test `@pusheen create-release uat master`
5. Verify playbook run is created
6. Verify summary table is posted
7. Test `@pusheen refresh` in the release channel

---

## Task 9: Add Release Channel Detection to Changes Command

**Files:**
- Modify: `internal/commands/serve/serve.go`

**Step 1: Update processChangesAsync to check for active release**

At the end of processChangesAsync, after posting the changes summary, add:

```go
// Update release if this channel has one
if releaseManager != nil {
	if rel := releaseManager.GetReleaseByChannel(channelID); rel != nil {
		releaseManager.RefreshRelease(ctx, channelID)
	}
}
```

Note: This requires passing releaseManager to processChangesAsync.

**Step 2: Commit**

```bash
git add internal/commands/serve/serve.go
git commit -m "feat: auto-update release when changes command runs in release channel"
```

---

## Summary

After completing all tasks, you will have:

1. **Playbooks API Client** - Create playbooks and runs, add participants
2. **Release Manager** - Tracks releases, gathers repo data, formats summaries
3. **Bot Commands:**
   - `@pusheen create-release <source> <dest>` - Creates a new release playbook
   - `@pusheen refresh` - Updates release table in release channel
4. **Auto-updates** - Changes command updates release if run in release channel
5. **Configuration** - team_id, playbook_id, default reviewers/QA in config.yaml

**Configuration required before use:**
1. Create a Playbook template in Mattermost UI with checklists
2. Add team_id, playbook_id to config.yaml
3. Optionally configure default_reviewers and default_qa
