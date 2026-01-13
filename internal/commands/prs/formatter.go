package prs

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/user/mattermost-tools/internal/mappings"
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
			if !isBot(pr.User.Login) {
				if _, ok := mappings.MattermostFromGitHub(pr.User.Login); !ok {
					unmappedSet[pr.User.Login] = struct{}{}
				}
			}

			staleDuration := now.Sub(pr.UpdatedAt)
			emoji := stalenessEmoji(staleDuration)

			sb.WriteString(fmt.Sprintf("%s [#%d](%s) %s _(%s)_\n",
				emoji, pr.Number, pr.HTMLURL, pr.Title, pr.User.Login))

			stale := formatDuration(staleDuration)
			age := formatDuration(now.Sub(pr.CreatedAt))

			var waitingOn string
			if len(pr.RequestedReviewers) == 0 {
				waitingOn = "No reviewers assigned"
			} else {
				var reviewers []string
				for _, r := range pr.RequestedReviewers {
					if isBot(r.Login) {
						continue
					}
					if mm, ok := mappings.MattermostFromGitHub(r.Login); ok {
						reviewers = append(reviewers, "@"+mm)
					} else {
						reviewers = append(reviewers, r.Login)
						unmappedSet[r.Login] = struct{}{}
					}
				}
				if len(reviewers) == 0 {
					waitingOn = "No reviewers assigned"
				} else {
					waitingOn = "Waiting on " + strings.Join(reviewers, ", ")
				}
			}

			sb.WriteString(fmt.Sprintf("   %s stale · %s old · %s\n\n", stale, age, waitingOn))
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

func isBot(login string) bool {
	return strings.HasSuffix(login, "[bot]")
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

func stalenessEmoji(d time.Duration) string {
	days := int(d.Hours() / 24)
	switch {
	case days < 1:
		return "🟢"
	case days < 3:
		return "🟡"
	case days < 7:
		return "🟠"
	default:
		return "🔴"
	}
}
