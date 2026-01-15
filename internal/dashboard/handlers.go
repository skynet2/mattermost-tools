package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/user/mattermost-tools/internal/database"
	"github.com/user/mattermost-tools/pkg/github"
	"github.com/user/mattermost-tools/pkg/mattermost"
)

type Handlers struct {
	service      *Service
	auth         *Auth
	ghClient     *github.Client
	org          string
	ignoredRepos map[string]struct{}
	mmBot        *mattermost.Bot
	baseURL      string
}

func NewHandlers(service *Service, auth *Auth, ghClient *github.Client, org string, ignoredRepos map[string]struct{}, mmBot *mattermost.Bot, baseURL string) *Handlers {
	return &Handlers{
		service:      service,
		auth:         auth,
		ghClient:     ghClient,
		org:          org,
		ignoredRepos: ignoredRepos,
		mmBot:        mmBot,
		baseURL:      baseURL,
	}
}

func (h *Handlers) ListReleases(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	releases, err := h.service.ListReleases(r.Context(), status)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, releases)
}

func (h *Handlers) CreateRelease(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SourceBranch string `json:"source_branch"`
		DestBranch   string `json:"dest_branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.SourceBranch == "" || req.DestBranch == "" {
		http.Error(w, "source_branch and dest_branch are required", http.StatusBadRequest)
		return
	}

	actor := "system"
	if h.auth != nil {
		if user := h.auth.GetUser(r); user != nil {
			actor = user.Email
		}
	}

	release, err := h.service.CreateRelease(r.Context(), CreateReleaseRequest{
		SourceBranch: req.SourceBranch,
		DestBranch:   req.DestBranch,
		CreatedBy:    actor,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.service.RecordHistory(r.Context(), release.ID, "release_created", actor, map[string]any{
		"source_branch": req.SourceBranch,
		"dest_branch":   req.DestBranch,
	})

	if h.ghClient != nil {
		go func() {
			ctx := context.Background()
			repos, err := h.gatherRepoData(ctx, req.SourceBranch, req.DestBranch)
			if err == nil && len(repos) > 0 {
				h.service.RefreshRepos(ctx, release.ID, repos)
				h.service.RecordHistory(ctx, release.ID, "repos_synced", "system", map[string]any{
					"count": len(repos),
				})
			}
		}()
	}

	respondJSON(w, map[string]any{
		"ID":           release.ID,
		"SourceBranch": release.SourceBranch,
		"DestBranch":   release.DestBranch,
		"Status":       release.Status,
		"CreatedAt":    release.CreatedAt,
		"syncing":      h.ghClient != nil,
	})
}

func (h *Handlers) GetRelease(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/releases/")
	id = strings.Split(id, "/")[0]

	release, err := h.service.GetReleaseWithRepos(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	deployOrder, _ := CalculateDeployOrder(release.Repos)

	type repoWithOrder struct {
		database.ReleaseRepo
		DeployOrder int `json:"DeployOrder"`
	}

	repos := make([]repoWithOrder, len(release.Repos))
	for i, repo := range release.Repos {
		repos[i] = repoWithOrder{
			ReleaseRepo: repo,
			DeployOrder: deployOrder[repo.ID],
		}
	}

	respondJSON(w, map[string]interface{}{
		"release": release.Release,
		"repos":   repos,
		"org":     h.org,
	})
}

func (h *Handlers) UpdateRelease(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/releases/")
	id = strings.Split(id, "/")[0]

	release, err := h.service.GetRelease(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	var req UpdateReleaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.service.UpdateRelease(r.Context(), id, req); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	actor := "system"
	if h.auth != nil {
		if user := h.auth.GetUser(r); user != nil {
			actor = user.Email
		}
	}

	if req.Notes != nil {
		h.service.RecordHistory(r.Context(), id, "notes_updated", actor, map[string]any{
			"old": release.Notes,
			"new": *req.Notes,
		})
	}
	if req.BreakingChanges != nil {
		h.service.RecordHistory(r.Context(), id, "breaking_changes_updated", actor, map[string]any{
			"old": release.BreakingChanges,
			"new": *req.BreakingChanges,
		})
	}

	respondJSON(w, map[string]string{"status": "ok"})
}

func (h *Handlers) UpdateRepo(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	repoIDStr := parts[len(parts)-1]
	releaseID := parts[len(parts)-3]
	repoID, err := strconv.ParseUint(repoIDStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid repo id", http.StatusBadRequest)
		return
	}

	repo, err := h.service.GetRepo(r.Context(), uint(repoID))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	var req struct {
		Excluded  *bool    `json:"excluded"`
		DependsOn []string `json:"depends_on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.service.UpdateRepo(r.Context(), uint(repoID), req.Excluded, req.DependsOn); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	actor := "system"
	if h.auth != nil {
		if user := h.auth.GetUser(r); user != nil {
			actor = user.Email
		}
	}

	if req.Excluded != nil {
		action := "repo_excluded"
		if !*req.Excluded {
			action = "repo_included"
		}
		h.service.RecordHistory(r.Context(), releaseID, action, actor, map[string]any{
			"repo": repo.RepoName,
		})
	}
	if req.DependsOn != nil {
		var oldDeps []string
		if repo.DependsOn != "" {
			json.Unmarshal([]byte(repo.DependsOn), &oldDeps)
		}

		oldSet := make(map[string]bool)
		for _, d := range oldDeps {
			oldSet[d] = true
		}
		newSet := make(map[string]bool)
		for _, d := range req.DependsOn {
			newSet[d] = true
		}

		var added, removed []string
		for _, d := range req.DependsOn {
			if !oldSet[d] {
				added = append(added, d)
			}
		}
		for _, d := range oldDeps {
			if !newSet[d] {
				removed = append(removed, d)
			}
		}

		h.service.RecordHistory(r.Context(), releaseID, "repo_dependencies_updated", actor, map[string]any{
			"repo":    repo.RepoName,
			"old":     oldDeps,
			"new":     req.DependsOn,
			"added":   added,
			"removed": removed,
		})
	}

	respondJSON(w, map[string]string{"status": "ok"})
}

func (h *Handlers) ApproveRelease(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	approvalType := parts[len(parts)-1]
	releaseID := parts[len(parts)-3]

	user := h.auth.GetUser(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := h.service.ApproveRelease(r.Context(), releaseID, approvalType, user.Username); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.service.RecordHistory(r.Context(), releaseID, "approval_added", user.Email, map[string]any{
		"type": approvalType,
	})

	respondJSON(w, map[string]string{"status": "ok"})
}

func (h *Handlers) RevokeApproval(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	approvalType := parts[len(parts)-1]
	releaseID := parts[len(parts)-3]

	if err := h.service.RevokeApproval(r.Context(), releaseID, approvalType); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	actor := "system"
	if h.auth != nil {
		if user := h.auth.GetUser(r); user != nil {
			actor = user.Email
		}
	}

	h.service.RecordHistory(r.Context(), releaseID, "approval_revoked", actor, map[string]any{
		"type": approvalType,
	})

	respondJSON(w, map[string]string{"status": "ok"})
}

func (h *Handlers) RefreshRelease(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	releaseID := parts[len(parts)-2]

	if h.ghClient == nil {
		http.Error(w, "GitHub client not configured", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()

	release, err := h.service.GetRelease(ctx, releaseID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	repos, err := h.gatherRepoData(ctx, release.SourceBranch, release.DestBranch)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := h.service.RefreshRepos(ctx, releaseID, repos); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	actor := "system"
	if h.auth != nil {
		if user := h.auth.GetUser(r); user != nil {
			actor = user.Email
		}
	}

	h.service.RecordHistory(ctx, releaseID, "release_refreshed", actor, nil)

	respondJSON(w, map[string]string{"status": "ok"})
}

func (h *Handlers) DeclineRelease(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	releaseID := parts[len(parts)-2]

	user := h.auth.GetUser(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := h.service.DeclineRelease(r.Context(), releaseID, user.Username); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.service.RecordHistory(r.Context(), releaseID, "release_declined", user.Email, nil)

	respondJSON(w, map[string]string{"status": "ok"})
}

func (h *Handlers) gatherRepoData(ctx context.Context, sourceBranch, destBranch string) ([]RepoData, error) {
	repos, err := h.ghClient.ListRepositories(ctx, h.org)
	if err != nil {
		return nil, err
	}

	var results []RepoData
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)

	for _, repo := range repos {
		if repo.Archived {
			continue
		}
		if _, ignored := h.ignoredRepos[repo.Name]; ignored {
			continue
		}

		wg.Add(1)
		go func(repo github.Repository) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			compare, err := h.ghClient.CompareBranches(ctx, h.org, repo.Name, destBranch, sourceBranch)
			if err != nil || compare == nil || compare.TotalCommits == 0 || len(compare.Files) == 0 {
				return
			}

			var contributors []string
			seen := make(map[string]struct{})
			for _, c := range compare.Commits {
				if c.Author.Login != "" {
					if _, ok := seen[c.Author.Login]; !ok {
						seen[c.Author.Login] = struct{}{}
						contributors = append(contributors, c.Author.Login)
					}
				}
			}

			var additions, deletions int
			for _, f := range compare.Files {
				additions += f.Additions
				deletions += f.Deletions
			}

			infraChanges := detectInfraChanges(compare.Files)

			pr, _ := h.ghClient.FindPullRequest(ctx, h.org, repo.Name, sourceBranch, destBranch)

			summary, isBreaking := generateChangeSummary(repo.Name, compare)

			data := RepoData{
				RepoName:     repo.Name,
				CommitCount:  compare.TotalCommits,
				Additions:    additions,
				Deletions:    deletions,
				Contributors: contributors,
				Summary:      summary,
				IsBreaking:   isBreaking,
				InfraChanges: infraChanges,
			}
			if pr != nil {
				data.PRNumber = pr.Number
				data.PRURL = pr.HTMLURL
			}

			mu.Lock()
			results = append(results, data)
			mu.Unlock()
		}(repo)
	}

	wg.Wait()
	return results, nil
}

func respondJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func detectInfraChanges(files []github.FileChange) []string {
	infraTypes := make(map[string]struct{})

	for _, f := range files {
		path := strings.ToLower(f.Filename)

		if strings.HasSuffix(path, ".tf") || strings.HasSuffix(path, ".tfvars") ||
			strings.Contains(path, "terraform/") || strings.Contains(path, ".terraform/") {
			infraTypes["terraform"] = struct{}{}
		}

		if strings.Contains(path, "helm/") || strings.Contains(path, "charts/") ||
			strings.HasSuffix(path, "/chart.yaml") || strings.HasSuffix(path, "/values.yaml") ||
			strings.Contains(path, "/templates/") && (strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml")) {
			infraTypes["helm"] = struct{}{}
		}

		if strings.HasSuffix(path, "dockerfile") || strings.HasPrefix(path, "dockerfile") ||
			strings.Contains(path, "/dockerfile") {
			infraTypes["docker"] = struct{}{}
		}

		if strings.Contains(path, ".github/workflows/") || strings.Contains(path, ".gitlab-ci") ||
			strings.HasSuffix(path, "jenkinsfile") {
			infraTypes["ci/cd"] = struct{}{}
		}

		if strings.Contains(path, "k8s/") || strings.Contains(path, "kubernetes/") ||
			strings.Contains(path, "kustomize/") {
			infraTypes["kubernetes"] = struct{}{}
		}
	}

	result := make([]string, 0, len(infraTypes))
	for t := range infraTypes {
		result = append(result, t)
	}
	return result
}

func (h *Handlers) GetMyGitHub(w http.ResponseWriter, r *http.Request) {
	user := h.auth.GetUser(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	dbUser, err := h.service.GetUserByEmail(r.Context(), user.Email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var githubUser *string
	if dbUser != nil && dbUser.GitHubUser != "" {
		githubUser = &dbUser.GitHubUser
	}

	respondJSON(w, map[string]*string{"github_user": githubUser})
}

func (h *Handlers) SetMyGitHub(w http.ResponseWriter, r *http.Request) {
	user := h.auth.GetUser(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		GitHubUser string `json:"github_user"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	dbUser, err := h.service.CreateOrUpdateUser(r.Context(), user.Email, req.GitHubUser, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]string{"github_user": dbUser.GitHubUser})
}

func (h *Handlers) GetMyProfile(w http.ResponseWriter, r *http.Request) {
	user := h.auth.GetUser(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	dbUser, err := h.service.GetUserByEmail(r.Context(), user.Email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	profile := map[string]string{
		"email":           user.Email,
		"github_user":     "",
		"mattermost_user": "",
	}
	if dbUser != nil {
		profile["github_user"] = dbUser.GitHubUser
		profile["mattermost_user"] = dbUser.MattermostUser
	}

	respondJSON(w, profile)
}

func (h *Handlers) UpdateMyProfile(w http.ResponseWriter, r *http.Request) {
	user := h.auth.GetUser(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		GitHubUser     string `json:"github_user"`
		MattermostUser string `json:"mattermost_user"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	dbUser, err := h.service.CreateOrUpdateUser(r.Context(), user.Email, req.GitHubUser, req.MattermostUser)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]string{
		"email":           user.Email,
		"github_user":     dbUser.GitHubUser,
		"mattermost_user": dbUser.MattermostUser,
	})
}

func (h *Handlers) PokeParticipants(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	releaseID := parts[len(parts)-2]

	if h.mmBot == nil {
		http.Error(w, "Mattermost bot not configured", http.StatusServiceUnavailable)
		return
	}

	ctx := r.Context()

	releaseWithRepos, err := h.service.GetReleaseWithRepos(ctx, releaseID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	pendingActions := h.service.GetPendingActions(ctx, releaseWithRepos)
	if len(pendingActions) == 0 {
		respondJSON(w, map[string]interface{}{
			"status":  "ok",
			"message": "No pending actions",
			"poked":   0,
		})
		return
	}

	releaseURL := fmt.Sprintf("%s/releases/%s", h.baseURL, releaseID)
	message := h.buildPokeMessage(releaseWithRepos.Release, pendingActions, releaseURL)

	if err := h.mmBot.PostMessage(ctx, releaseWithRepos.Release.ChannelID, message); err != nil {
		http.Error(w, fmt.Sprintf("Failed to send message: %v", err), http.StatusInternalServerError)
		return
	}

	actor := "system"
	if h.auth != nil {
		if user := h.auth.GetUser(r); user != nil {
			actor = user.Email
		}
	}

	h.service.RecordHistory(ctx, releaseID, "participants_poked", actor, map[string]any{
		"count": len(pendingActions),
	})

	respondJSON(w, map[string]interface{}{
		"status":  "ok",
		"message": "Poked participants",
		"poked":   len(pendingActions),
	})
}

func (h *Handlers) buildPokeMessage(release database.Release, pendingActions []PendingAction, releaseURL string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("### 📢 Reminder: Release `%s` → `%s` needs your attention!\n\n", release.SourceBranch, release.DestBranch))

	repoConfirmations := make(map[string][]string)
	var qaNeeded, devNeeded []string

	for _, action := range pendingActions {
		mention := action.MattermostUser
		if mention != "" {
			mention = "@" + mention
		} else {
			mention = action.GitHubUser + " (no MM)"
		}

		switch action.ActionType {
		case "confirm_repo":
			repoConfirmations[action.RepoName] = append(repoConfirmations[action.RepoName], mention)
		case "qa_approval":
			qaNeeded = append(qaNeeded, mention)
		case "dev_approval":
			devNeeded = append(devNeeded, mention)
		}
	}

	if len(repoConfirmations) > 0 {
		sb.WriteString("**Repo confirmations needed:**\n")
		for repo, users := range repoConfirmations {
			sb.WriteString(fmt.Sprintf("- `%s`: %s\n", repo, strings.Join(users, ", ")))
		}
		sb.WriteString("\n")
	}

	if len(qaNeeded) > 0 {
		sb.WriteString(fmt.Sprintf("**QA approval needed:** %s\n\n", strings.Join(qaNeeded, ", ")))
	}

	if len(devNeeded) > 0 {
		sb.WriteString(fmt.Sprintf("**Dev approval needed:** %s\n\n", strings.Join(devNeeded, ", ")))
	}

	sb.WriteString(fmt.Sprintf("[View Release](%s)", releaseURL))

	return sb.String()
}

func generateChangeSummary(repoName string, compare *github.CompareResult) (string, bool) {
	var commitInfo strings.Builder
	commitInfo.WriteString(fmt.Sprintf("Repository: %s\n", repoName))
	commitInfo.WriteString(fmt.Sprintf("Total commits: %d\n\n", compare.TotalCommits))

	commitInfo.WriteString("Commits:\n")
	for _, c := range compare.Commits {
		msg := strings.Split(c.Commit.Message, "\n")[0]
		commitInfo.WriteString(fmt.Sprintf("- %s: %s\n", c.SHA[:7], msg))
	}

	commitInfo.WriteString("\nFiles changed:\n")
	for _, f := range compare.Files {
		commitInfo.WriteString(fmt.Sprintf("- %s (%s, +%d/-%d)\n", f.Filename, f.Status, f.Additions, f.Deletions))
	}

	prompt := fmt.Sprintf(`Analyze these git changes and provide a brief summary (2-3 sentences max).
Focus on: what features/fixes are included, any breaking changes or important notes.
If there are breaking changes, database migrations, API changes, or security updates, start your response with "BREAKING:" followed by the summary.
Otherwise just provide the summary directly.

%s`, commitInfo.String())

	cmd := exec.Command("claude", "-p", prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Sprintf("%d commits (AI summary unavailable)", compare.TotalCommits), false
	}

	summary := strings.TrimSpace(stdout.String())
	isBreaking := strings.HasPrefix(strings.ToUpper(summary), "BREAKING:")

	if isBreaking {
		summary = strings.TrimPrefix(summary, "BREAKING:")
		summary = strings.TrimPrefix(summary, "breaking:")
		summary = strings.TrimSpace(summary)
	}

	return summary, isBreaking
}

func (h *Handlers) ConfirmRepo(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	repoIDStr := parts[len(parts)-2]
	releaseID := parts[len(parts)-4]
	repoID, err := strconv.ParseUint(repoIDStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid repo id", http.StatusBadRequest)
		return
	}

	user := h.auth.GetUser(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	dbUser, err := h.service.GetUserByEmail(r.Context(), user.Email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if dbUser == nil || dbUser.GitHubUser == "" {
		http.Error(w, ErrGitHubNotConfigured.Error(), http.StatusBadRequest)
		return
	}

	repo, err := h.service.GetRepo(r.Context(), uint(repoID))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	if err := h.service.ConfirmRepo(r.Context(), uint(repoID), dbUser.GitHubUser); err != nil {
		if errors.Is(err, ErrRepoNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if errors.Is(err, ErrNotContributor) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		if errors.Is(err, ErrAlreadyConfirmed) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.service.RecordHistory(r.Context(), releaseID, "repo_confirmed", user.Email, map[string]any{
		"repo":   repo.RepoName,
		"github": dbUser.GitHubUser,
	})

	respondJSON(w, map[string]string{"status": "ok"})
}

func (h *Handlers) UnconfirmRepo(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	repoIDStr := parts[len(parts)-2]
	releaseID := parts[len(parts)-4]
	repoID, err := strconv.ParseUint(repoIDStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid repo id", http.StatusBadRequest)
		return
	}

	user := h.auth.GetUser(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	dbUser, err := h.service.GetUserByEmail(r.Context(), user.Email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if dbUser == nil || dbUser.GitHubUser == "" {
		http.Error(w, ErrGitHubNotConfigured.Error(), http.StatusBadRequest)
		return
	}

	repo, err := h.service.GetRepo(r.Context(), uint(repoID))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	if err := h.service.UnconfirmRepo(r.Context(), uint(repoID), dbUser.GitHubUser); err != nil {
		if errors.Is(err, ErrRepoNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.service.RecordHistory(r.Context(), releaseID, "repo_unconfirmed", user.Email, map[string]any{
		"repo":   repo.RepoName,
		"github": dbUser.GitHubUser,
	})

	respondJSON(w, map[string]string{"status": "ok"})
}

func (h *Handlers) GetHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/releases/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	releaseID := parts[0]

	history, err := h.service.GetHistory(r.Context(), releaseID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, history)
}
