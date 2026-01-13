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
