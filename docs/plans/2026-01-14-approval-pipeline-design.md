# Approval Pipeline Design

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement a staged approval pipeline where contributors confirm their repos before QA and Dev Lead approvals.

**Architecture:** Three-stage pipeline with soft enforcement (warnings, not blocks). User identity mapping across GitHub/Mattermost/Email systems.

**Tech Stack:** Go/GORM backend, Vue.js frontend, SQLite database

---

## Pipeline Stages

1. **Dev Confirmations** - Contributors confirm their repos have correct dependencies/ordering
2. **QA Approval** - QA signs off on the release
3. **Dev Lead Approval** - Final approval

Soft ordering: warnings shown if approving out of order, but actions are allowed.

## Data Model

### New `User` Table

```go
type User struct {
    ID             uint   `gorm:"primaryKey;autoIncrement"`
    Email          string `gorm:"uniqueIndex"`
    MattermostUser string `gorm:"index"`
    GitHubUser     string `gorm:"index"`
    CreatedAt      int64
    UpdatedAt      int64
}
```

### Updated `ReleaseRepo` Table

Add fields:
```go
ConfirmedBy string // JSON array of GitHub usernames who confirmed
ConfirmedAt int64  // Timestamp of last confirmation
```

## Confirmation Rules

- Only listed contributors (matched via GitHub username) can confirm a repo
- **Majority rule**: More than half of contributors must confirm
  - 3 contributors → need 2 confirmations
  - 2 contributors → need 2 confirmations
  - 1 contributor → need 1 confirmation
- Release is "all confirmed" when every non-excluded repo has majority confirmation

## User Mapping

- Self-service: first time user tries to confirm, prompt for GitHub username
- Mapping stored in `User` table, looked up by email (from OIDC login)
- Once mapped, no further prompts needed

## Refresh Behavior

- New repos added by refresh → need fresh confirmations
- Existing repos (matched by RepoName) → keep their confirmations
- Deleted repos → removed from release

## API Endpoints

### Confirmation

```
POST   /api/releases/{id}/repos/{repoId}/confirm  - Add confirmation
DELETE /api/releases/{id}/repos/{repoId}/confirm  - Remove confirmation
```

**Validation:**
1. Get user's GitHub username from `User` table
2. If not mapped → return 400 "GitHub username not configured"
3. Get repo's contributors list
4. If user's GitHub not in contributors → return 403 "Not a contributor"
5. If already confirmed → return 400 "Already confirmed"
6. Add username to `ConfirmedBy` array, save

### User Mapping

```
GET /api/users/me/github  - Get current user's GitHub mapping
PUT /api/users/me/github  - Set current user's GitHub mapping
```

### Pipeline Warnings

QA/Dev Lead approval endpoints return warnings (not errors) if out of order:
- QA approving before all repos confirmed → `warning: "X repos not yet confirmed"`
- Dev Lead approving before QA → `warning: "QA has not approved yet"`

## UI Changes

### Repo Table - New "Confirmed" Column

Shows:
- Progress: "2/3" with checkmarks (✓✓○)
- "✓ Confirmed" when majority reached
- Hover shows names of who confirmed
- "Confirm" button if user is eligible (contributor + not yet confirmed)

### Pipeline Progress Bar

Above approvals section:
```
[✓ Dev Confirmations (8/8)] → [○ QA Approval] → [○ Dev Lead]
```

Shows current stage, warnings if out of order.

### GitHub Username Modal

First-time confirmation triggers modal:
- "Link your GitHub account"
- Text input for GitHub username
- Saved to User table, used for all future confirmations

## Implementation Tasks

1. Create `User` table and migration
2. Add `ConfirmedBy`, `ConfirmedAt` fields to `ReleaseRepo`
3. Create user mapping service and endpoints
4. Create confirmation endpoints with validation
5. Update refresh logic to preserve confirmations for existing repos
6. Add GitHub username modal component
7. Add confirmation column to repo table
8. Add pipeline progress bar component
9. Add warnings to QA/Dev Lead approval flow
10. Update TypeScript types
