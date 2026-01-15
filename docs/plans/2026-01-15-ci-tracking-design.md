# CI Tracking Feature Design

## Overview

Track GitHub Actions CI status for each repo in a release. After PR merge, Shipyard detects the workflow run triggered by the merge commit, polls until completion, and extracts CHART_VERSION from Helm job logs when available.

**Purpose:** QA engineers can see if a release was deployed, the CI status, and the deployed artifact version.

## Data Model

New `RepoCIStatus` table, linked 1:1 with `ReleaseRepo`:

```go
type RepoCIStatus struct {
    ID              uint   `gorm:"primaryKey;autoIncrement"`
    ReleaseRepoID   uint   `gorm:"uniqueIndex;not null"` // FK to ReleaseRepo
    WorkflowRunID   int64  // GitHub workflow run ID
    WorkflowRunNum  int    // Run number (#937)
    WorkflowURL     string // Link to the run
    Status          string // pending, in_progress, success, failure, cancelled, no_workflow, timeout
    ChartVersion    string // Extracted from Helm job logs (optional)
    MergeCommitSHA  string // The commit that triggered the run
    StartedAt       int64
    CompletedAt     int64
    LastCheckedAt   int64
}
```

Also add `MergeCommitSHA` column to `ReleaseRepo`.

## CI Tracking Flow

1. **Trigger:** When Shipyard fetches repo data (release creation/refresh), detect merged PRs
2. **Find Run:** Call `GET /repos/{owner}/{repo}/actions/runs?branch={dest_branch}&head_sha={merge_commit_sha}`
3. **Poll:** Background goroutine polls every 30s until status is `success`, `failure`, or `cancelled`
4. **Extract Version:** On completion, fetch Helm job logs and parse `CHART_VERSION=X.X.X`

## API Endpoints

### New: GET /api/releases/{id}/ci-status

Returns CI status for all repos:

```json
{
  "statuses": [
    {
      "repo_name": "mattermost-server",
      "status": "success",
      "run_number": 937,
      "run_url": "https://github.com/...",
      "chart_version": "1.1.937",
      "started_at": 1704000000,
      "completed_at": 1704000120
    }
  ],
  "any_in_progress": false
}
```

### Modified: GET /api/releases/{id}

Include CI summary:

```json
{
  "release": { ... },
  "repos": [ ... ],
  "ci_summary": {
    "total": 4,
    "success": 2,
    "failed": 1,
    "in_progress": 1,
    "pending": 0
  }
}
```

## UI Display

### Repo Row (compact)

```
[mattermost-server]  15 commits  [CI: ✓ #937 → v1.1.937]
[mattermost-webapp]  8 commits   [CI: ⏳ #412 in progress]
[mattermost-plugin]  3 commits   [CI: ✗ #156 failed]
[some-other-repo]    2 commits   [CI: — no workflow]
```

- Status icons: ✓ green, ✗ red, ⏳ yellow, ○ gray
- Run number links to GitHub
- CHART_VERSION shown when available

### Deployments Section

Detailed table view:

| Repo | CI Status | Run | Chart Version | Started | Completed |
|------|-----------|-----|---------------|---------|-----------|
| mattermost-server | ✓ Success | #937 | 1.1.937 | 2m ago | 1m ago |

Auto-refreshes while any CI is in progress.

## Error Handling

- **Rate limits:** Back off to 5-minute polling, log warnings
- **Workflow not found:** After 3 checks, mark as `no_workflow`
- **Logs unavailable:** Leave `chart_version` empty, still show status
- **Stale releases:** Stop polling after 24 hours, mark as `timeout`

## Configuration

- `CI_POLL_INTERVAL` - polling interval (default 30s)
- `CI_POLL_TIMEOUT` - stop polling after duration (default 24h)

## Implementation Notes

- Background polling goroutine starts with serve command
- Reuse existing authenticated GitHub client
- Record `ci_started`, `ci_completed`, `ci_failed` in ReleaseHistory
- Only track workflow from `.github/workflows/general.yaml`
