# PR Review Reminder Command Design

## Overview

CLI command to fetch open GitHub PRs from an organization and post review reminders to Mattermost.

## Command Usage

```
mmtools prs <org> [options]

Arguments:
  org            GitHub organization name (required)

Options:
  --webhook-url  Mattermost webhook (overrides MATTERMOST_WEBHOOK_URL env)
  --ignore-repos Comma-separated repos to skip (e.g., "archived-repo,docs")
  --dry-run      Print message to stdout, don't post to Mattermost

Environment:
  GITHUB_TOKEN           Required - GitHub PAT with repo read access
  MATTERMOST_WEBHOOK_URL Webhook URL (can be overridden by flag)

Examples:
  mmtools prs FlyrInc --dry-run
  mmtools prs FlyrInc --ignore-repos=archived-repo,internal-docs
  mmtools prs FlyrInc --webhook-url=https://mattermost.example.com/hooks/xxx
```

## Project Structure

```
cmd/mmtools/main.go
internal/commands/
    cmd.go                 # Root command, global flags
    prs/
        prs.go             # Command implementation
        mappings.go        # GitHub → Mattermost user map
        formatter.go       # Message formatting
pkg/
    github/
        client.go          # GitHub API client
        types.go           # PR, Review, etc. structs
    mattermost/
        webhook.go         # Webhook posting
```

## Data Flow

1. List repos in org: `GET /orgs/{org}/repos?type=all&per_page=100`
2. Filter: skip ignored repos, skip archived repos
3. For each repo, fetch open PRs (non-draft): `GET /repos/{owner}/{repo}/pulls?state=open&per_page=100`
4. For each PR, extract: title, number, URL, author, created date, updated date, requested reviewers
5. Group PRs by repo, format message, post to webhook

## Message Format

```markdown
#### Pending review on [FlyrInc/ooms-legacytranslator](https://github.com/FlyrInc/ooms-legacytranslator)

[#1541](https://github.com/FlyrInc/ooms-legacytranslator/pull/1541) fix: LT-1510 adds tkne sending after revalidation. _(menes-turgut)_
5 days stale · 2 months old · Waiting on @john.doe

[#1744](https://github.com/FlyrInc/ooms-legacytranslator/pull/1744) feat: LT-1156 Truncate long names in PNR messages _(maksymilian-lewicki)_
7 days stale · 14 days old · Waiting on jane.smith

---

#### Pending review on [FlyrInc/another-repo](https://github.com/FlyrInc/another-repo)

[#42](https://github.com/FlyrInc/another-repo/pull/42) fix: something important _(some-dev)_
1 day stale · 3 days old · Waiting on @bob.wilson

---

:warning: **Unmapped GitHub users:** jane.smith, some-dev
```

## User Mappings

`internal/commands/prs/mappings.go` contains a map of GitHub usernames to Mattermost usernames:

```go
var GitHubToMattermost = map[string]string{
    // "github-username": "mattermost-username",
}
```

- Mapped users: displayed as `@mattermost-username`
- Unmapped users: displayed as plain `github-username`
- Unmapped users listed in warning at bottom of message and printed to stderr

## Calculations

- **Age**: time since PR creation (`created_at`)
- **Stale**: time since last activity (`updated_at` - includes comments, reviews, commits)
- **Waiting on**: users in `requested_reviewers` who haven't approved

## Edge Cases

| Scenario | Behavior |
|----------|----------|
| Repo has no open PRs | Skip repo (don't show in message) |
| Org has no PRs | Print "No pending PRs found", exit 0 |
| PR has no reviewers | Show "No reviewers assigned" |
| Missing GITHUB_TOKEN | Error with clear message |
| Missing webhook URL | Error unless --dry-run |
| GitHub rate limit | Error with reset time |

## Sorting

- Repos: alphabetically by name
- PRs within repo: by staleness (most stale first)

## Pagination

Both repo listing and PR listing will paginate through all results (GitHub default is 30, max 100 per page).
