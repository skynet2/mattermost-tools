package prs

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/user/mattermost-tools/internal/config"
	"github.com/user/mattermost-tools/pkg/github"
	"github.com/user/mattermost-tools/pkg/mattermost"
)

var (
	configFile  string
	webhookURL  string
	ignoreRepos string
	dryRun      bool
)

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prs",
		Short: "Post pending PR review reminders to Mattermost",
		RunE:  runPRs,
	}

	cmd.Flags().StringVarP(&configFile, "config", "c", "config.yaml", "Path to config file")
	cmd.Flags().StringVar(&webhookURL, "webhook-url", "", "Mattermost webhook URL (overrides config)")
	cmd.Flags().StringVar(&ignoreRepos, "ignore-repos", "", "Comma-separated list of repos to ignore (overrides config)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print message to stdout instead of posting")

	return cmd
}

func runPRs(cmd *cobra.Command, args []string) error {
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

	ghClient := github.NewClient(ghToken)

	fmt.Fprintf(os.Stderr, "Fetching repositories for %s...\n", org)
	repos, err := ghClient.ListRepositories(ctx, org)
	if err != nil {
		return fmt.Errorf("listing repositories: %w", err)
	}

	teamMembersCache := make(map[string][]github.User)

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
			if pr.Draft {
				continue
			}

			for _, team := range pr.RequestedTeams {
				members, ok := teamMembersCache[team.Slug]
				if !ok {
					fmt.Fprintf(os.Stderr, "Fetching members for team %s...\n", team.Slug)
					members, err = ghClient.ListTeamMembers(ctx, org, team.Slug)
					if err != nil {
						fmt.Fprintf(os.Stderr, "WARNING: failed to fetch team members for %s: %v\n", team.Slug, err)
						members = []github.User{}
					}
					teamMembersCache[team.Slug] = members
				}
				pr.RequestedReviewers = append(pr.RequestedReviewers, members...)
			}

			openPRs = append(openPRs, pr)
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
