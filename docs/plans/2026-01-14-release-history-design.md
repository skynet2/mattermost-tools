# Release History Feature Design

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Track and display all changes to releases in a comprehensive audit log.

**Architecture:** New ReleaseHistory table stores events, existing handlers call RecordHistory service method, dedicated UI section displays timeline.

**Tech Stack:** Go/GORM backend, Vue 3 frontend, SQLite database

---

## Data Model

```go
type ReleaseHistory struct {
    ID        uint   `gorm:"primaryKey;autoIncrement"`
    ReleaseID string `gorm:"not null;index"`
    Action    string `gorm:"not null"`
    Actor     string
    Details   string // JSON
    CreatedAt int64  `gorm:"not null;index"`
}
```

**Action types:**
- `release_created` - Initial creation
- `release_refreshed` - Sync from GitHub
- `release_declined` - Release declined
- `approval_added` / `approval_revoked` - Dev/QA approvals
- `notes_updated` / `breaking_changes_updated` - Text field edits
- `repo_excluded` / `repo_included` - Repo toggle
- `repo_confirmed` - Dev confirmation
- `repo_dependencies_updated` - Dependencies changed
- `participants_poked` - Poke sent

## Backend

**Service methods:**
- `RecordHistory(ctx, releaseID, action, actor string, details map[string]any) error`
- `GetHistory(ctx, releaseID string) ([]ReleaseHistory, error)`

**API endpoint:**
- `GET /api/releases/{id}/history` - Returns history entries sorted newest first

**Integration points:**
All existing handlers call RecordHistory with appropriate action type.
Actor comes from auth context; system actions use "system".

## Frontend UI

Collapsible section at bottom of release detail page:
- Collapsed by default
- Shows timestamp, actor, human-readable action
- Color-coded icons per action type
- Refresh button for history only

## Files to Modify

- `internal/database/models.go` - Add ReleaseHistory model
- `internal/dashboard/service.go` - Add RecordHistory, GetHistory
- `internal/dashboard/handlers.go` - Add GetHistory, update all handlers
- `internal/dashboard/server.go` - Add history route
- `web/src/api/types.ts` - Add HistoryEntry type
- `web/src/api/client.ts` - Add getHistory method
- `web/src/views/ReleaseDetailView.vue` - Add history section
