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
	releases        map[string]*Release
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
		return nil, fmt.Errorf("no repositories with changes found between %s and %s", sourceBranch, destBranch)
	}

	releaseName := fmt.Sprintf("Release %s -> %s (%s)", sourceBranch, destBranch, time.Now().Format("2006-01-02 15:04"))

	runResp, err := m.playbooksClient.CreateRun(ctx, mattermost.CreatePlaybookRunRequest{
		Name:        releaseName,
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

	participantIDs := m.collectUserIDs(ctx, repos, defaultReviewers, defaultQA)
	if len(participantIDs) > 0 {
		if err := m.playbooksClient.AddParticipants(ctx, runResp.ID, participantIDs); err != nil {
			fmt.Printf("warning: failed to add participants: %v\n", err)
		}
	}

	summary := m.FormatReleaseSummary(release)
	postID, err := m.mmBot.PostMessageWithID(ctx, runResp.ChannelID, summary)
	if err != nil {
		fmt.Printf("warning: failed to post summary: %v\n", err)
	} else {
		release.SummaryPostID = postID
	}

	m.mu.Lock()
	m.releases[runResp.ChannelID] = release
	m.mu.Unlock()

	return release, nil
}

func (m *Manager) gatherRepoStatuses(ctx context.Context, sourceBranch, destBranch string) ([]RepoStatus, error) {
	repos, err := m.ghClient.ListRepositories(ctx, m.org)
	if err != nil {
		return nil, fmt.Errorf("listing repositories: %w", err)
	}

	var filteredRepos []github.Repository
	for _, repo := range repos {
		if repo.Archived {
			continue
		}
		if _, ignored := m.ignoredRepos[repo.Name]; ignored {
			continue
		}
		filteredRepos = append(filteredRepos, repo)
	}

	var (
		results []RepoStatus
		mu      sync.Mutex
		wg      sync.WaitGroup
		sem     = make(chan struct{}, 4)
	)

	for _, repo := range filteredRepos {
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
				status.PRNumber = pr.Number
				status.PRURL = pr.HTMLURL
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
	ghUsers := make(map[string]struct{})

	for _, repo := range repos {
		for _, contrib := range repo.Contributors {
			ghUsers[contrib] = struct{}{}
		}
	}

	for _, reviewer := range defaultReviewers {
		ghUsers[reviewer] = struct{}{}
	}
	for _, qa := range defaultQA {
		ghUsers[qa] = struct{}{}
	}

	var userIDs []string
	seen := make(map[string]struct{})

	for ghUser := range ghUsers {
		mmUsername, ok := mappings.MattermostFromGitHub(ghUser)
		if !ok {
			continue
		}

		user, err := m.mmBot.GetUserByUsername(ctx, mmUsername)
		if err != nil || user == nil {
			continue
		}

		if _, exists := seen[user.ID]; exists {
			continue
		}
		seen[user.ID] = struct{}{}
		userIDs = append(userIDs, user.ID)
	}

	return userIDs
}

func (m *Manager) FormatReleaseSummary(release *Release) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Release: `%s` -> `%s`\n\n", release.SourceBranch, release.DestBranch))
	sb.WriteString(fmt.Sprintf("**Repositories with changes:** %d\n\n", len(release.Repos)))

	sb.WriteString("| Repository | Commits | Contributors | PR | Dev | QA |\n")
	sb.WriteString("|------------|---------|--------------|----|----|----|\n")

	var missingPRs []RepoStatus
	for _, repo := range release.Repos {
		contributors := m.formatContributors(repo.Contributors)

		prStatus := "-"
		if repo.HasPR {
			prStatus = fmt.Sprintf("[#%d](%s)", repo.PRNumber, repo.PRURL)
		} else {
			missingPRs = append(missingPRs, repo)
		}

		devCheck := "[ ]"
		if repo.DevApproved {
			devCheck = "[x]"
		}

		qaCheck := "[ ]"
		if repo.QAApproved {
			qaCheck = "[x]"
		}

		sb.WriteString(fmt.Sprintf("| %s | %d | %s | %s | %s | %s |\n",
			repo.Name, repo.Commits, contributors, prStatus, devCheck, qaCheck))
	}

	if len(missingPRs) > 0 {
		sb.WriteString(fmt.Sprintf("\n### Missing PRs (%d)\n", len(missingPRs)))
		for _, repo := range missingPRs {
			sb.WriteString(fmt.Sprintf("- **%s** (%d commits)\n", repo.Name, repo.Commits))
		}
	}

	return sb.String()
}

func (m *Manager) formatContributors(ghUsers []string) string {
	if len(ghUsers) == 0 {
		return "-"
	}

	var formatted []string
	for i, ghUser := range ghUsers {
		if i >= 3 {
			formatted = append(formatted, fmt.Sprintf("+%d more", len(ghUsers)-3))
			break
		}
		mmUsername, ok := mappings.MattermostFromGitHub(ghUser)
		if ok {
			formatted = append(formatted, "@"+mmUsername)
		} else {
			formatted = append(formatted, ghUser)
		}
	}

	return strings.Join(formatted, ", ")
}

func (m *Manager) GetReleaseByChannel(channelID string) *Release {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.releases[channelID]
}

func (m *Manager) RefreshRelease(ctx context.Context, channelID string) (*Release, error) {
	m.mu.RLock()
	existing := m.releases[channelID]
	m.mu.RUnlock()

	if existing == nil {
		return nil, fmt.Errorf("no release found for channel %s", channelID)
	}

	repos, err := m.gatherRepoStatuses(ctx, existing.SourceBranch, existing.DestBranch)
	if err != nil {
		return nil, fmt.Errorf("gathering repo statuses: %w", err)
	}

	m.mu.Lock()
	existing.Repos = repos
	m.mu.Unlock()

	if existing.SummaryPostID != "" {
		summary := m.FormatReleaseSummary(existing)
		if err := m.mmBot.UpdatePost(ctx, existing.SummaryPostID, summary); err != nil {
			return nil, fmt.Errorf("updating summary post: %w", err)
		}
	}

	return existing, nil
}
