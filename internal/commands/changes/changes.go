package changes

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/user/mattermost-tools/internal/config"
	"github.com/user/mattermost-tools/pkg/github"
	"github.com/user/mattermost-tools/pkg/mattermost"
)

var (
	configFile  string
	webhookURL  string
	ignoreRepos string
	repos       string
	dryRun      bool
	withDiff    bool
	concurrency int
)

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "changes <source-branch> <dest-branch>",
		Short: "Summarize undeployed changes between branches using AI",
		Long: `Compare two branches across all repositories and generate AI summaries of changes.

Example:
  mmtools changes uat master        # Compare uat branch to master
  mmtools changes develop main      # Compare develop to main`,
		Args: cobra.ExactArgs(2),
		RunE: runChanges,
	}

	cmd.Flags().StringVarP(&configFile, "config", "c", "config.yaml", "Path to config file")
	cmd.Flags().StringVar(&webhookURL, "webhook-url", "", "Mattermost webhook URL (overrides config)")
	cmd.Flags().StringVar(&ignoreRepos, "ignore-repos", "", "Comma-separated list of repos to ignore (overrides config)")
	cmd.Flags().StringVar(&repos, "repos", "", "Comma-separated list of specific repos to check (if set, only these repos are checked)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print message to stdout instead of posting")
	cmd.Flags().BoolVar(&withDiff, "with-diff", false, "Include actual diff content for AI analysis (more detailed but slower)")
	cmd.Flags().IntVar(&concurrency, "concurrency", 4, "Number of repos to process in parallel")

	return cmd
}

type RepoChanges struct {
	Repo      github.Repository
	Compare   *github.CompareResult
	Summary   string
	IsBreaking bool
}

func runChanges(cmd *cobra.Command, args []string) error {
	sourceBranch := args[0]
	destBranch := args[1]
	ctx := context.Background()

	cfg, err := config.Load(configFile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("loading config: %w", err)
	}
	if cfg == nil {
		cfg = &config.Config{}
	}

	org := cfg.Org
	if org == "" {
		return fmt.Errorf("org is required in config")
	}

	ghToken := os.Getenv("GITHUB_TOKEN")
	if ghToken == "" {
		ghToken = cfg.GitHubToken
	}
	if ghToken == "" {
		return fmt.Errorf("GITHUB_TOKEN environment variable is required")
	}

	webhook := webhookURL
	if webhook == "" {
		webhook = os.Getenv("MATTERMOST_WEBHOOK_URL")
	}
	if webhook == "" {
		webhook = cfg.PRs.WebhookURL
	}
	if webhook == "" && !dryRun {
		return fmt.Errorf("webhook URL required: set MATTERMOST_WEBHOOK_URL or use --webhook-url")
	}

	ignoredRepos := make(map[string]struct{})
	for _, r := range cfg.IgnoreRepos {
		ignoredRepos[r] = struct{}{}
	}
	if ignoreRepos != "" {
		for _, r := range strings.Split(ignoreRepos, ",") {
			ignoredRepos[strings.TrimSpace(r)] = struct{}{}
		}
	}

	specificRepos := make(map[string]struct{})
	if repos != "" {
		for _, r := range strings.Split(repos, ",") {
			specificRepos[strings.TrimSpace(r)] = struct{}{}
		}
	}

	ghClient := github.NewClient(ghToken)

	var repoList []github.Repository
	if len(specificRepos) > 0 {
		fmt.Fprintf(os.Stderr, "Checking %d specific repositories...\n", len(specificRepos))
		for repoName := range specificRepos {
			repoList = append(repoList, github.Repository{
				Name:     repoName,
				FullName: org + "/" + repoName,
				HTMLURL:  fmt.Sprintf("https://github.com/%s/%s", org, repoName),
			})
		}
	} else {
		fmt.Fprintf(os.Stderr, "Fetching repositories for %s...\n", org)
		var err error
		repoList, err = ghClient.ListRepositories(ctx, org)
		if err != nil {
			return fmt.Errorf("listing repositories: %w", err)
		}
	}

	var filteredRepos []github.Repository
	for _, repo := range repoList {
		if repo.Archived {
			continue
		}
		if _, ignored := ignoredRepos[repo.Name]; ignored {
			continue
		}
		filteredRepos = append(filteredRepos, repo)
	}

	var (
		reposWithChanges []RepoChanges
		mu               sync.Mutex
		wg               sync.WaitGroup
		sem              = make(chan struct{}, concurrency)
	)

	for _, repo := range filteredRepos {
		wg.Add(1)
		go func(repo github.Repository) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			fmt.Fprintf(os.Stderr, "Comparing %s...%s for %s...\n", destBranch, sourceBranch, repo.Name)
			compare, err := ghClient.CompareBranches(ctx, org, repo.Name, destBranch, sourceBranch)
			if err != nil {
				fmt.Fprintf(os.Stderr, "WARNING: failed to compare branches for %s: %v\n", repo.Name, err)
				return
			}

			if compare == nil || compare.TotalCommits == 0 {
				return
			}

			fmt.Fprintf(os.Stderr, "Found %d commits in %s, generating AI summary...\n", compare.TotalCommits, repo.Name)

			var diff string
			if withDiff {
				fmt.Fprintf(os.Stderr, "Fetching diff for %s...\n", repo.Name)
				diff, err = ghClient.GetDiff(ctx, org, repo.Name, destBranch, sourceBranch)
				if err != nil {
					fmt.Fprintf(os.Stderr, "WARNING: failed to fetch diff for %s: %v\n", repo.Name, err)
				}
			}

			summary, isBreaking, err := generateSummary(repo.Name, compare, diff)
			if err != nil {
				fmt.Fprintf(os.Stderr, "WARNING: failed to generate summary for %s: %v\n", repo.Name, err)
				summary = fmt.Sprintf("%d commits (AI summary unavailable)", compare.TotalCommits)
			}

			mu.Lock()
			reposWithChanges = append(reposWithChanges, RepoChanges{
				Repo:       repo,
				Compare:    compare,
				Summary:    summary,
				IsBreaking: isBreaking,
			})
			mu.Unlock()
		}(repo)
	}

	wg.Wait()

	if len(reposWithChanges) == 0 {
		fmt.Println("No changes found between branches.")
		return nil
	}

	message := formatMessage(reposWithChanges, sourceBranch, destBranch)

	if dryRun {
		fmt.Println(message)
		return nil
	}

	webhookClient := mattermost.NewWebhook(webhook)
	if err := webhookClient.Post(ctx, message); err != nil {
		return fmt.Errorf("posting to Mattermost: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Posted changes summary to Mattermost.\n")
	return nil
}

func generateSummary(repoName string, compare *github.CompareResult, diff string) (string, bool, error) {
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

	if diff != "" {
		maxDiffLen := 50000
		if len(diff) > maxDiffLen {
			diff = diff[:maxDiffLen] + "\n... (diff truncated)"
		}
		commitInfo.WriteString("\nDiff:\n")
		commitInfo.WriteString(diff)
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
		return "", false, fmt.Errorf("claude command failed: %w (stderr: %s)", err, stderr.String())
	}

	summary := strings.TrimSpace(stdout.String())
	isBreaking := strings.HasPrefix(strings.ToUpper(summary), "BREAKING:")

	if isBreaking {
		summary = strings.TrimPrefix(summary, "BREAKING:")
		summary = strings.TrimPrefix(summary, "breaking:")
		summary = strings.TrimSpace(summary)
	}

	return summary, isBreaking, nil
}

func formatMessage(changes []RepoChanges, source, dest string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("### 📦 Undeployed Changes: `%s` → `%s`\n\n", source, dest))
	sb.WriteString(fmt.Sprintf("Found changes in **%d** repositories:\n\n", len(changes)))

	for _, rc := range changes {
		var emoji string
		if rc.IsBreaking {
			emoji = "🚨"
		} else if rc.Compare.TotalCommits > 10 {
			emoji = "📚"
		} else if rc.Compare.TotalCommits > 5 {
			emoji = "📝"
		} else {
			emoji = "📄"
		}

		sb.WriteString(fmt.Sprintf("**%s [%s](%s)** (%d commits)\n",
			emoji, rc.Repo.Name, rc.Repo.HTMLURL, rc.Compare.TotalCommits))
		sb.WriteString(fmt.Sprintf("%s\n\n", rc.Summary))
	}

	return sb.String()
}
