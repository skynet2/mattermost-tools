# PR Review Reminder Command Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build `mmtools prs <org>` command that fetches open GitHub PRs and posts review reminders to Mattermost.

**Architecture:** Cobra CLI with three packages: `pkg/github` for GitHub API, `pkg/mattermost` for webhook posting, `internal/commands/prs` for the command logic and formatting.

**Tech Stack:** Go 1.21+, Cobra CLI, standard `net/http` for API calls, `encoding/json` for parsing.

---

### Task 1: Project Setup

**Files:**
- Create: `go.mod`
- Create: `Makefile`
- Create: `cmd/mmtools/main.go`
- Create: `internal/commands/cmd.go`

**Step 1: Initialize Go module**

Run:
```bash
cd /Users/iqpirat/sources/github/mattermost-tools
go mod init github.com/user/mattermost-tools
```

**Step 2: Create Makefile**

Create `Makefile`:
```makefile
.PHONY: build test clean generate

BINARY=mmtools
BUILD_DIR=bin

build:
	go build -o $(BUILD_DIR)/$(BINARY) ./cmd/mmtools

test:
	go test -v ./...

clean:
	rm -rf $(BUILD_DIR)

generate:
	go generate ./...
```

**Step 3: Create main.go entry point**

Create `cmd/mmtools/main.go`:
```go
package main

import (
	"os"

	"github.com/user/mattermost-tools/internal/commands"
)

func main() {
	if err := commands.Execute(); err != nil {
		os.Exit(1)
	}
}
```

**Step 4: Create root command**

Create `internal/commands/cmd.go`:
```go
package commands

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "mmtools",
	Short: "Mattermost tools CLI",
}

func Execute() error {
	return rootCmd.Execute()
}
```

**Step 5: Add Cobra dependency and verify build**

Run:
```bash
go get github.com/spf13/cobra
go build ./...
```

Expected: Build succeeds with no errors.

**Step 6: Commit**

```bash
git add -A
git commit -m "feat: project setup with Cobra CLI skeleton"
```

---

### Task 2: GitHub Client - Types

**Files:**
- Create: `pkg/github/types.go`

**Step 1: Create GitHub types**

Create `pkg/github/types.go`:
```go
package github

import "time"

type Repository struct {
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Archived bool   `json:"archived"`
	HTMLURL  string `json:"html_url"`
}

type User struct {
	Login string `json:"login"`
}

type PullRequest struct {
	Number             int       `json:"number"`
	Title              string    `json:"title"`
	HTMLURL            string    `json:"html_url"`
	State              string    `json:"state"`
	Draft              bool      `json:"draft"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	User               User      `json:"user"`
	RequestedReviewers []User    `json:"requested_reviewers"`
}
```

**Step 2: Verify compilation**

Run:
```bash
go build ./...
```

Expected: Build succeeds.

**Step 3: Commit**

```bash
git add pkg/github/types.go
git commit -m "feat: add GitHub API types"
```

---

### Task 3: GitHub Client - Core

**Files:**
- Create: `pkg/github/client.go`
- Create: `pkg/github/client_test.go`

**Step 1: Write test for NewClient**

Create `pkg/github/client_test.go`:
```go
package github_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/mattermost-tools/pkg/github"
)

func TestNewClient_Success(t *testing.T) {
	client := github.NewClient("test-token")

	require.NotNil(t, client)
}

func TestNewClient_EmptyToken(t *testing.T) {
	client := github.NewClient("")

	require.NotNil(t, client)
}
```

**Step 2: Run test to verify it fails**

Run:
```bash
go get github.com/stretchr/testify
go test ./pkg/github/... -v
```

Expected: FAIL - `NewClient` not defined.

**Step 3: Write minimal implementation**

Create `pkg/github/client.go`:
```go
package github

import (
	"net/http"
)

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type Client struct {
	token      string
	httpClient HTTPDoer
	baseURL    string
}

func NewClient(token string) *Client {
	return &Client{
		token:      token,
		httpClient: &http.Client{},
		baseURL:    "https://api.github.com",
	}
}
```

**Step 4: Run test to verify it passes**

Run:
```bash
go test ./pkg/github/... -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add pkg/github/client.go pkg/github/client_test.go
git commit -m "feat: add GitHub client core"
```

---

### Task 4: GitHub Client - List Repositories

**Files:**
- Modify: `pkg/github/client.go`
- Modify: `pkg/github/client_test.go`

**Step 1: Write test for ListRepositories**

Add to `pkg/github/client_test.go`:
```go
func TestClient_ListRepositories_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	responseBody := `[
		{"name": "repo1", "full_name": "org/repo1", "archived": false, "html_url": "https://github.com/org/repo1"},
		{"name": "repo2", "full_name": "org/repo2", "archived": true, "html_url": "https://github.com/org/repo2"}
	]`

	mockHTTP := mocks.NewMockHTTPDoer(ctrl)
	mockHTTP.EXPECT().
		Do(gomock.Any()).
		DoAndReturn(func(req *http.Request) (*http.Response, error) {
			require.Equal(t, "https://api.github.com/orgs/testorg/repos?per_page=100&page=1", req.URL.String())
			require.Equal(t, "Bearer test-token", req.Header.Get("Authorization"))
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(responseBody)),
			}, nil
		})

	client := github.NewClientWithHTTP("test-token", mockHTTP)
	repos, err := client.ListRepositories(context.Background(), "testorg")

	require.NoError(t, err)
	require.Len(t, repos, 2)
	require.Equal(t, "repo1", repos[0].Name)
	require.False(t, repos[0].Archived)
	require.Equal(t, "repo2", repos[1].Name)
	require.True(t, repos[1].Archived)
}

func TestClient_ListRepositories_APIError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHTTP := mocks.NewMockHTTPDoer(ctrl)
	mockHTTP.EXPECT().
		Do(gomock.Any()).
		Return(&http.Response{
			StatusCode: 404,
			Body:       io.NopCloser(strings.NewReader(`{"message": "Not Found"}`)),
		}, nil)

	client := github.NewClientWithHTTP("test-token", mockHTTP)
	repos, err := client.ListRepositories(context.Background(), "nonexistent")

	require.Error(t, err)
	require.Contains(t, err.Error(), "404")
	require.Nil(t, repos)
}
```

Update imports at top of test file:
```go
package github_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	"github.com/user/mattermost-tools/pkg/github"
	"github.com/user/mattermost-tools/pkg/github/mocks"
)
```

**Step 2: Generate mock**

Add to `pkg/github/client.go` after HTTPDoer interface:
```go
//go:generate mockgen -destination=mocks/http_doer_mock.go -package=mocks github.com/user/mattermost-tools/pkg/github HTTPDoer
```

Run:
```bash
go get github.com/golang/mock/gomock
go install github.com/golang/mock/mockgen@latest
mkdir -p pkg/github/mocks
go generate ./pkg/github/...
```

**Step 3: Run test to verify it fails**

Run:
```bash
go test ./pkg/github/... -v
```

Expected: FAIL - `ListRepositories` not defined.

**Step 4: Write implementation**

Add to `pkg/github/client.go`:
```go
import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

func NewClientWithHTTP(token string, httpClient HTTPDoer) *Client {
	return &Client{
		token:      token,
		httpClient: httpClient,
		baseURL:    "https://api.github.com",
	}
}

func (c *Client) ListRepositories(ctx context.Context, org string) ([]Repository, error) {
	var allRepos []Repository
	page := 1

	for {
		url := fmt.Sprintf("%s/orgs/%s/repos?per_page=100&page=%d", c.baseURL, org, page)
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

		var repos []Repository
		if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
			return nil, fmt.Errorf("decoding response: %w", err)
		}

		if len(repos) == 0 {
			break
		}

		allRepos = append(allRepos, repos...)

		if len(repos) < 100 {
			break
		}
		page++
	}

	return allRepos, nil
}
```

**Step 5: Run test to verify it passes**

Run:
```bash
go test ./pkg/github/... -v
```

Expected: PASS.

**Step 6: Commit**

```bash
git add -A
git commit -m "feat: add ListRepositories to GitHub client"
```

---

### Task 5: GitHub Client - List Pull Requests

**Files:**
- Modify: `pkg/github/client.go`
- Modify: `pkg/github/client_test.go`

**Step 1: Write test for ListPullRequests**

Add to `pkg/github/client_test.go`:
```go
func TestClient_ListPullRequests_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	responseBody := `[
		{
			"number": 123,
			"title": "feat: add feature",
			"html_url": "https://github.com/org/repo/pull/123",
			"state": "open",
			"draft": false,
			"created_at": "2025-01-01T10:00:00Z",
			"updated_at": "2025-01-10T10:00:00Z",
			"user": {"login": "author1"},
			"requested_reviewers": [{"login": "reviewer1"}, {"login": "reviewer2"}]
		},
		{
			"number": 124,
			"title": "fix: bug fix",
			"html_url": "https://github.com/org/repo/pull/124",
			"state": "open",
			"draft": true,
			"created_at": "2025-01-05T10:00:00Z",
			"updated_at": "2025-01-08T10:00:00Z",
			"user": {"login": "author2"},
			"requested_reviewers": []
		}
	]`

	mockHTTP := mocks.NewMockHTTPDoer(ctrl)
	mockHTTP.EXPECT().
		Do(gomock.Any()).
		DoAndReturn(func(req *http.Request) (*http.Response, error) {
			require.Equal(t, "https://api.github.com/repos/org/repo/pulls?state=open&per_page=100&page=1", req.URL.String())
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(responseBody)),
			}, nil
		})

	client := github.NewClientWithHTTP("test-token", mockHTTP)
	prs, err := client.ListPullRequests(context.Background(), "org", "repo")

	require.NoError(t, err)
	require.Len(t, prs, 2)
	require.Equal(t, 123, prs[0].Number)
	require.Equal(t, "feat: add feature", prs[0].Title)
	require.False(t, prs[0].Draft)
	require.Len(t, prs[0].RequestedReviewers, 2)
	require.True(t, prs[1].Draft)
}
```

**Step 2: Run test to verify it fails**

Run:
```bash
go test ./pkg/github/... -v
```

Expected: FAIL - `ListPullRequests` not defined.

**Step 3: Write implementation**

Add to `pkg/github/client.go`:
```go
func (c *Client) ListPullRequests(ctx context.Context, owner, repo string) ([]PullRequest, error) {
	var allPRs []PullRequest
	page := 1

	for {
		url := fmt.Sprintf("%s/repos/%s/%s/pulls?state=open&per_page=100&page=%d", c.baseURL, owner, repo, page)
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

		var prs []PullRequest
		if err := json.NewDecoder(resp.Body).Decode(&prs); err != nil {
			return nil, fmt.Errorf("decoding response: %w", err)
		}

		if len(prs) == 0 {
			break
		}

		allPRs = append(allPRs, prs...)

		if len(prs) < 100 {
			break
		}
		page++
	}

	return allPRs, nil
}
```

**Step 4: Run test to verify it passes**

Run:
```bash
go test ./pkg/github/... -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add ListPullRequests to GitHub client"
```

---

### Task 6: Mattermost Webhook Client

**Files:**
- Create: `pkg/mattermost/webhook.go`
- Create: `pkg/mattermost/webhook_test.go`

**Step 1: Write test for webhook posting**

Create `pkg/mattermost/webhook_test.go`:
```go
package mattermost_test

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	"github.com/user/mattermost-tools/pkg/mattermost"
	"github.com/user/mattermost-tools/pkg/mattermost/mocks"
)

func TestWebhook_Post_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHTTP := mocks.NewMockHTTPDoer(ctrl)
	mockHTTP.EXPECT().
		Do(gomock.Any()).
		DoAndReturn(func(req *http.Request) (*http.Response, error) {
			require.Equal(t, "https://mattermost.example.com/hooks/xxx", req.URL.String())
			require.Equal(t, "application/json", req.Header.Get("Content-Type"))

			body, _ := io.ReadAll(req.Body)
			require.Contains(t, string(body), "Test message")

			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(nil),
			}, nil
		})

	webhook := mattermost.NewWebhookWithHTTP("https://mattermost.example.com/hooks/xxx", mockHTTP)
	err := webhook.Post(context.Background(), "Test message")

	require.NoError(t, err)
}

func TestWebhook_Post_Error(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHTTP := mocks.NewMockHTTPDoer(ctrl)
	mockHTTP.EXPECT().
		Do(gomock.Any()).
		Return(&http.Response{
			StatusCode: 500,
			Body:       io.NopCloser(nil),
		}, nil)

	webhook := mattermost.NewWebhookWithHTTP("https://mattermost.example.com/hooks/xxx", mockHTTP)
	err := webhook.Post(context.Background(), "Test message")

	require.Error(t, err)
	require.Contains(t, err.Error(), "500")
}
```

**Step 2: Generate mock**

Create `pkg/mattermost/webhook.go` with generate directive:
```go
package mattermost

import "net/http"

//go:generate mockgen -destination=mocks/http_doer_mock.go -package=mocks github.com/user/mattermost-tools/pkg/mattermost HTTPDoer

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}
```

Run:
```bash
mkdir -p pkg/mattermost/mocks
go generate ./pkg/mattermost/...
```

**Step 3: Run test to verify it fails**

Run:
```bash
go test ./pkg/mattermost/... -v
```

Expected: FAIL - `NewWebhookWithHTTP` not defined.

**Step 4: Write implementation**

Update `pkg/mattermost/webhook.go`:
```go
package mattermost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

//go:generate mockgen -destination=mocks/http_doer_mock.go -package=mocks github.com/user/mattermost-tools/pkg/mattermost HTTPDoer

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type Webhook struct {
	url        string
	httpClient HTTPDoer
}

func NewWebhook(url string) *Webhook {
	return &Webhook{
		url:        url,
		httpClient: &http.Client{},
	}
}

func NewWebhookWithHTTP(url string, httpClient HTTPDoer) *Webhook {
	return &Webhook{
		url:        url,
		httpClient: httpClient,
	}
}

type webhookPayload struct {
	Text string `json:"text"`
}

func (w *Webhook) Post(ctx context.Context, message string) error {
	payload := webhookPayload{Text: message}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook error: %d", resp.StatusCode)
	}

	return nil
}
```

**Step 5: Run test to verify it passes**

Run:
```bash
go test ./pkg/mattermost/... -v
```

Expected: PASS.

**Step 6: Commit**

```bash
git add -A
git commit -m "feat: add Mattermost webhook client"
```

---

### Task 7: User Mappings

**Files:**
- Create: `internal/commands/prs/mappings.go`

**Step 1: Create mappings file**

Create `internal/commands/prs/mappings.go`:
```go
package prs

// GitHubToMattermost maps GitHub usernames to Mattermost usernames.
// Mattermost usernames will be prefixed with @ for mentions.
//
// To add a mapping:
//  1. Find the GitHub username (e.g., "john-doe")
//  2. Find their Mattermost username (e.g., "john.doe")
//  3. Add entry: "john-doe": "john.doe",
//
// Unmapped users will appear without @ mention and be listed in warnings.
var GitHubToMattermost = map[string]string{
	// "github-username": "mattermost-username",
	// "john-doe":        "john.doe",
	// "jane-smith":      "jane.smith",
}

// MapUser returns the Mattermost username for a GitHub user.
// Returns the mapped username and true if found, or empty string and false if not.
func MapUser(githubUsername string) (string, bool) {
	mm, ok := GitHubToMattermost[githubUsername]
	return mm, ok
}
```

**Step 2: Verify compilation**

Run:
```bash
go build ./...
```

Expected: Build succeeds.

**Step 3: Commit**

```bash
git add internal/commands/prs/mappings.go
git commit -m "feat: add GitHub to Mattermost user mappings"
```

---

### Task 8: Message Formatter

**Files:**
- Create: `internal/commands/prs/formatter.go`
- Create: `internal/commands/prs/formatter_test.go`

**Step 1: Create formatter types**

Create `internal/commands/prs/formatter.go`:
```go
package prs

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/user/mattermost-tools/pkg/github"
)

type RepoPRs struct {
	Repo github.Repository
	PRs  []github.PullRequest
}

type FormatResult struct {
	Message       string
	UnmappedUsers []string
}
```

**Step 2: Write test for formatter**

Create `internal/commands/prs/formatter_test.go`:
```go
package prs_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/user/mattermost-tools/internal/commands/prs"
	"github.com/user/mattermost-tools/pkg/github"
)

func TestFormatMessage_Success(t *testing.T) {
	now := time.Date(2025, 1, 13, 12, 0, 0, 0, time.UTC)

	repoPRs := []prs.RepoPRs{
		{
			Repo: github.Repository{
				Name:     "repo1",
				FullName: "org/repo1",
				HTMLURL:  "https://github.com/org/repo1",
			},
			PRs: []github.PullRequest{
				{
					Number:    123,
					Title:     "feat: add feature",
					HTMLURL:   "https://github.com/org/repo1/pull/123",
					CreatedAt: now.AddDate(0, -2, 0),
					UpdatedAt: now.AddDate(0, 0, -5),
					User:      github.User{Login: "author1"},
					RequestedReviewers: []github.User{
						{Login: "reviewer1"},
					},
				},
			},
		},
	}

	result := prs.FormatMessage(repoPRs, now)

	require.Contains(t, result.Message, "#### Pending review on [org/repo1]")
	require.Contains(t, result.Message, "[#123]")
	require.Contains(t, result.Message, "feat: add feature")
	require.Contains(t, result.Message, "_(author1)_")
	require.Contains(t, result.Message, "5 days stale")
	require.Contains(t, result.Message, "2 months old")
}

func TestFormatMessage_NoReviewers(t *testing.T) {
	now := time.Date(2025, 1, 13, 12, 0, 0, 0, time.UTC)

	repoPRs := []prs.RepoPRs{
		{
			Repo: github.Repository{
				Name:     "repo1",
				FullName: "org/repo1",
				HTMLURL:  "https://github.com/org/repo1",
			},
			PRs: []github.PullRequest{
				{
					Number:             123,
					Title:              "feat: no reviewers",
					HTMLURL:            "https://github.com/org/repo1/pull/123",
					CreatedAt:          now.AddDate(0, 0, -3),
					UpdatedAt:          now.AddDate(0, 0, -1),
					User:               github.User{Login: "author1"},
					RequestedReviewers: []github.User{},
				},
			},
		},
	}

	result := prs.FormatMessage(repoPRs, now)

	require.Contains(t, result.Message, "No reviewers assigned")
}

func TestFormatMessage_UnmappedUsers(t *testing.T) {
	now := time.Date(2025, 1, 13, 12, 0, 0, 0, time.UTC)

	repoPRs := []prs.RepoPRs{
		{
			Repo: github.Repository{
				Name:     "repo1",
				FullName: "org/repo1",
				HTMLURL:  "https://github.com/org/repo1",
			},
			PRs: []github.PullRequest{
				{
					Number:    123,
					Title:     "feat: test",
					HTMLURL:   "https://github.com/org/repo1/pull/123",
					CreatedAt: now.AddDate(0, 0, -3),
					UpdatedAt: now.AddDate(0, 0, -1),
					User:      github.User{Login: "author1"},
					RequestedReviewers: []github.User{
						{Login: "unmapped-user"},
					},
				},
			},
		},
	}

	result := prs.FormatMessage(repoPRs, now)

	require.Contains(t, result.UnmappedUsers, "unmapped-user")
	require.Contains(t, result.Message, ":warning: **Unmapped GitHub users:**")
}
```

**Step 3: Run test to verify it fails**

Run:
```bash
go test ./internal/commands/prs/... -v
```

Expected: FAIL - `FormatMessage` not defined.

**Step 4: Write implementation**

Add to `internal/commands/prs/formatter.go`:
```go
func FormatMessage(repoPRs []RepoPRs, now time.Time) FormatResult {
	var sb strings.Builder
	unmappedSet := make(map[string]struct{})

	sort.Slice(repoPRs, func(i, j int) bool {
		return repoPRs[i].Repo.Name < repoPRs[j].Repo.Name
	})

	for i, rp := range repoPRs {
		if i > 0 {
			sb.WriteString("\n---\n\n")
		}

		sb.WriteString(fmt.Sprintf("#### Pending review on [%s](%s)\n\n",
			rp.Repo.FullName, rp.Repo.HTMLURL))

		sort.Slice(rp.PRs, func(i, j int) bool {
			staleI := now.Sub(rp.PRs[i].UpdatedAt)
			staleJ := now.Sub(rp.PRs[j].UpdatedAt)
			return staleI > staleJ
		})

		for _, pr := range rp.PRs {
			sb.WriteString(fmt.Sprintf("[#%d](%s) %s _(%s)_\n",
				pr.Number, pr.HTMLURL, pr.Title, pr.User.Login))

			stale := formatDuration(now.Sub(pr.UpdatedAt))
			age := formatDuration(now.Sub(pr.CreatedAt))

			var waitingOn string
			if len(pr.RequestedReviewers) == 0 {
				waitingOn = "No reviewers assigned"
			} else {
				var reviewers []string
				for _, r := range pr.RequestedReviewers {
					if mm, ok := MapUser(r.Login); ok {
						reviewers = append(reviewers, "@"+mm)
					} else {
						reviewers = append(reviewers, r.Login)
						unmappedSet[r.Login] = struct{}{}
					}
				}
				waitingOn = "Waiting on " + strings.Join(reviewers, ", ")
			}

			sb.WriteString(fmt.Sprintf("%s stale · %s old · %s\n\n", stale, age, waitingOn))
		}
	}

	var unmappedUsers []string
	for u := range unmappedSet {
		unmappedUsers = append(unmappedUsers, u)
	}
	sort.Strings(unmappedUsers)

	if len(unmappedUsers) > 0 {
		sb.WriteString("---\n\n")
		sb.WriteString(fmt.Sprintf(":warning: **Unmapped GitHub users:** %s\n",
			strings.Join(unmappedUsers, ", ")))
	}

	return FormatResult{
		Message:       sb.String(),
		UnmappedUsers: unmappedUsers,
	}
}

func formatDuration(d time.Duration) string {
	days := int(d.Hours() / 24)

	if days >= 60 {
		months := days / 30
		return fmt.Sprintf("%d months", months)
	}
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}
```

**Step 5: Run test to verify it passes**

Run:
```bash
go test ./internal/commands/prs/... -v
```

Expected: PASS.

**Step 6: Commit**

```bash
git add -A
git commit -m "feat: add PR message formatter"
```

---

### Task 9: PRS Command Implementation

**Files:**
- Create: `internal/commands/prs/prs.go`
- Modify: `internal/commands/cmd.go`

**Step 1: Create prs command**

Create `internal/commands/prs/prs.go`:
```go
package prs

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/user/mattermost-tools/pkg/github"
	"github.com/user/mattermost-tools/pkg/mattermost"
)

var (
	webhookURL  string
	ignoreRepos string
	dryRun      bool
)

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prs <org>",
		Short: "Post pending PR review reminders to Mattermost",
		Args:  cobra.ExactArgs(1),
		RunE:  runPRs,
	}

	cmd.Flags().StringVar(&webhookURL, "webhook-url", "", "Mattermost webhook URL (overrides MATTERMOST_WEBHOOK_URL)")
	cmd.Flags().StringVar(&ignoreRepos, "ignore-repos", "", "Comma-separated list of repos to ignore")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print message to stdout instead of posting")

	return cmd
}

func runPRs(cmd *cobra.Command, args []string) error {
	org := args[0]
	ctx := context.Background()

	ghToken := os.Getenv("GITHUB_TOKEN")
	if ghToken == "" {
		return fmt.Errorf("GITHUB_TOKEN environment variable is required")
	}

	webhook := webhookURL
	if webhook == "" {
		webhook = os.Getenv("MATTERMOST_WEBHOOK_URL")
	}
	if webhook == "" && !dryRun {
		return fmt.Errorf("webhook URL required: set MATTERMOST_WEBHOOK_URL or use --webhook-url")
	}

	ignoredRepos := make(map[string]struct{})
	if ignoreRepos != "" {
		for _, r := range strings.Split(ignoreRepos, ",") {
			ignoredRepos[strings.TrimSpace(r)] = struct{}{}
		}
	}

	ghClient := github.NewClient(ghToken)

	fmt.Fprintf(os.Stderr, "Fetching repositories for %s...\n", org)
	repos, err := ghClient.ListRepositories(ctx, org)
	if err != nil {
		return fmt.Errorf("listing repositories: %w", err)
	}

	var repoPRs []RepoPRs
	for _, repo := range repos {
		if repo.Archived {
			continue
		}
		if _, ignored := ignoredRepos[repo.Name]; ignored {
			continue
		}

		fmt.Fprintf(os.Stderr, "Fetching PRs for %s...\n", repo.Name)
		prs, err := ghClient.ListPullRequests(ctx, org, repo.Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: failed to fetch PRs for %s: %v\n", repo.Name, err)
			continue
		}

		var openPRs []github.PullRequest
		for _, pr := range prs {
			if !pr.Draft {
				openPRs = append(openPRs, pr)
			}
		}

		if len(openPRs) > 0 {
			repoPRs = append(repoPRs, RepoPRs{
				Repo: repo,
				PRs:  openPRs,
			})
		}
	}

	if len(repoPRs) == 0 {
		fmt.Println("No pending PRs found.")
		return nil
	}

	result := FormatMessage(repoPRs, time.Now())

	if len(result.UnmappedUsers) > 0 {
		fmt.Fprintf(os.Stderr, "WARNING: unmapped GitHub users: %s\n",
			strings.Join(result.UnmappedUsers, ", "))
	}

	if dryRun {
		fmt.Println(result.Message)
		return nil
	}

	webhookClient := mattermost.NewWebhook(webhook)
	if err := webhookClient.Post(ctx, result.Message); err != nil {
		return fmt.Errorf("posting to Mattermost: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Posted PR reminder to Mattermost.\n")
	return nil
}
```

**Step 2: Register command in cmd.go**

Update `internal/commands/cmd.go`:
```go
package commands

import (
	"github.com/spf13/cobra"
	"github.com/user/mattermost-tools/internal/commands/prs"
)

var rootCmd = &cobra.Command{
	Use:   "mmtools",
	Short: "Mattermost tools CLI",
}

func init() {
	rootCmd.AddCommand(prs.NewCommand())
}

func Execute() error {
	return rootCmd.Execute()
}
```

**Step 3: Verify build**

Run:
```bash
go build ./...
```

Expected: Build succeeds.

**Step 4: Commit**

```bash
git add -A
git commit -m "feat: add prs command"
```

---

### Task 10: Integration Test & Documentation

**Files:**
- Update: `.claude/CLAUDE.md`

**Step 1: Manual integration test**

Run (with real token):
```bash
export GITHUB_TOKEN=your_token_here
go run ./cmd/mmtools prs YourOrg --dry-run
```

Expected: Formatted PR list printed to stdout.

**Step 2: Update CLAUDE.md with prs command**

Update `.claude/CLAUDE.md` Architecture section:
```markdown
## Architecture

CLI tool using Cobra for command structure. Three layers:

```
cmd/mmtools/main.go            # Entry point
internal/commands/             # Cobra commands (private)
    cmd.go                     # Root command, global flags
    prs/                       # PR review reminders
        prs.go                 # Command implementation
        mappings.go            # GitHub → Mattermost user map
        formatter.go           # Message formatting
    completion/                # Shell completion generator
pkg/                           # Reusable API clients
    github/                    # GitHub REST API client
    mattermost/                # Mattermost webhook client
```
```

Update Environment Variables section:
```markdown
## Environment Variables

| Variable | Command | Description |
|----------|---------|-------------|
| `GITHUB_TOKEN` | prs | GitHub Personal Access Token |
| `MATTERMOST_WEBHOOK_URL` | prs | Mattermost incoming webhook URL |
```

**Step 3: Commit**

```bash
git add -A
git commit -m "docs: update CLAUDE.md with prs command"
```

---

## Summary

10 tasks total:
1. Project setup (go.mod, Makefile, main.go, root command)
2. GitHub types
3. GitHub client core
4. GitHub ListRepositories
5. GitHub ListPullRequests
6. Mattermost webhook client
7. User mappings
8. Message formatter
9. PRS command implementation
10. Integration test & documentation
