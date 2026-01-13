# Release Dashboard Design

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a custom release management dashboard to track code changes between branches, ensure Dev and QA sign-off before deployment.

**Architecture:** Go backend with embedded Vue.js SPA, SQLite database with GORM, Keycloak SSO authentication. Single binary deployment.

**Tech Stack:** Go, SQLite, GORM, Vue.js 3, Keycloak OIDC, Mattermost WebSocket bot

---

## Problem Statement

Code that wasn't properly tested on some microservices got deployed to production. Need a system to:
- Track which repos have changes between branches (e.g., `uat` → `master`)
- Require both Dev lead and QA sign-off before deployment
- Show deploy ordering based on service dependencies
- Maintain full release history

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                    mmtools binary                           │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │ HTTP Server │  │ WebSocket   │  │ Vue.js SPA          │  │
│  │ (API + Auth)│  │ Bot Client  │  │ (embedded static)   │  │
│  └──────┬──────┘  └──────┬──────┘  └─────────────────────┘  │
│         │                │                                   │
│         ▼                ▼                                   │
│  ┌─────────────────────────────────────────────────────────┐│
│  │              Release Manager Service                    ││
│  └─────────────────────────────────────────────────────────┘│
│         │                │                                   │
│         ▼                ▼                                   │
│  ┌─────────────┐  ┌─────────────┐                           │
│  │   SQLite    │  │  GitHub API │                           │
│  │   (GORM)    │  │   Client    │                           │
│  └─────────────┘  └─────────────┘                           │
└─────────────────────────────────────────────────────────────┘
```

**Key points:**
- Single binary with embedded Vue.js static files
- SQLite for persistence (no external DB required)
- Keycloak SSO for authentication
- Bot creates releases, dashboard manages them

---

## Database Schema

```sql
-- Releases
CREATE TABLE releases (
    id TEXT PRIMARY KEY,
    source_branch TEXT NOT NULL,
    dest_branch TEXT NOT NULL,
    status TEXT DEFAULT 'pending',  -- pending, approved, deployed
    notes TEXT,
    breaking_changes TEXT,
    created_by TEXT NOT NULL,       -- Mattermost user ID
    channel_id TEXT NOT NULL,       -- For notifications
    mattermost_post_id TEXT,        -- Summary post to update
    dev_approved_by TEXT,
    dev_approved_at INTEGER,
    qa_approved_by TEXT,
    qa_approved_at INTEGER,
    last_refreshed_at INTEGER,
    created_at INTEGER NOT NULL
);

-- Repos in each release
CREATE TABLE release_repos (
    id INTEGER PRIMARY KEY,
    release_id TEXT NOT NULL,
    repo_name TEXT NOT NULL,
    commit_count INTEGER DEFAULT 0,
    contributors TEXT,              -- JSON array
    pr_number INTEGER,
    pr_url TEXT,
    excluded BOOLEAN DEFAULT FALSE,
    depends_on TEXT,                -- JSON array: ["auth-service", "config-svc"]
    FOREIGN KEY (release_id) REFERENCES releases(id)
);
```

---

## API Endpoints

### Authentication
- `GET /auth/login` - Redirect to Keycloak
- `GET /auth/callback` - Handle OIDC callback, set session cookie
- `GET /auth/logout` - Clear session
- `GET /api/me` - Get current user info

### Releases
- `GET /api/releases` - List all releases (with filters: status, branch)
- `GET /api/releases/{id}` - Get release details with repos
- `POST /api/releases` - Create release (called by bot)
- `PATCH /api/releases/{id}` - Update notes, breaking_changes
- `POST /api/releases/{id}/refresh` - Re-fetch from GitHub

### Repos
- `PATCH /api/releases/{id}/repos/{repo_id}` - Update excluded, depends_on

### Approvals
- `POST /api/releases/{id}/approve/dev` - Dev lead approval
- `POST /api/releases/{id}/approve/qa` - QA approval
- `DELETE /api/releases/{id}/approve/dev` - Revoke dev approval
- `DELETE /api/releases/{id}/approve/qa` - Revoke QA approval

---

## Vue.js UI

### Release List Page (`/releases`)

```
┌─────────────────────────────────────────────────────────────┐
│ Releases                                    [+ New Release] │
├─────────────────────────────────────────────────────────────┤
│ Filter: [All ▼] [uat→master ▼]              Search: [____]  │
├─────────────────────────────────────────────────────────────┤
│ Status │ Branches      │ Repos │ Created    │ Approvals     │
│ 🟡     │ uat → master  │ 5     │ 2h ago     │ Dev ☑  QA ☐   │
│ 🟢     │ uat → master  │ 3     │ 1d ago     │ Dev ☑  QA ☑   │
│ 🟡     │ dev → uat     │ 8     │ 2d ago     │ Dev ☐  QA ☐   │
└─────────────────────────────────────────────────────────────┘
```

### Release Detail Page (`/releases/{id}`)

```
┌─────────────────────────────────────────────────────────────────────┐
│ Release: uat → master                    [Refresh from GitHub] ⟳    │
│ Created by @username • 2 hours ago • Last refreshed: 5 min ago      │
├─────────────────────────────────────────────────────────────────────┤
│ Notes:                                                              │
│ ┌─────────────────────────────────────────────────────────────────┐ │
│ │ Deploy auth-service first, wait 5 min before others             │ │
│ └─────────────────────────────────────────────────────────────────┘ │
│                                                              [Edit] │
├─────────────────────────────────────────────────────────────────────┤
│ Breaking Changes:                                                   │
│ ┌─────────────────────────────────────────────────────────────────┐ │
│ │ API v2 endpoints deprecated, clients must migrate to v3         │ │
│ └─────────────────────────────────────────────────────────────────┘ │
│                                                              [Edit] │
├─────────────────────────────────────────────────────────────────────┤
│ Repository     │ Commits │ PR   │ Depends On              │ Excl   │
│ auth-service   │ 5       │ #123 │ [ Select... ▼]          │ ☐      │
│ api-gateway    │ 3       │ #456 │ [auth-service ×] [+▼]   │ ☐      │
│ user-service   │ 2       │ #789 │ [auth-service ×] [+▼]   │ ☐      │
│ ̶n̶o̶t̶i̶f̶i̶c̶a̶t̶i̶o̶n̶-̶s̶v̶c̶ │ ̶1̶       │ ̶-̶    │                         │ ☑      │
├─────────────────────────────────────────────────────────────────────┤
│ Deploy Order: 1) auth-service → 2) api-gateway, user-service        │
├─────────────────────────────────────────────────────────────────────┤
│ Approvals:                                                          │
│   Dev Lead: ☐ Not approved          [Approve as Dev]                │
│   QA:       ☐ Not approved          [Approve as QA]                 │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Mattermost Bot Integration

### Bot Commands

| Command | Action |
|---------|--------|
| `@pusheen create-release uat master` | Creates release, posts link to dashboard |
| `@pusheen releases` | Lists active releases with links |
| `@pusheen help` | Shows available commands |

### Message Format (on create)

```
## Release: `uat` → `master`
**Repositories:** 5 | **Commits:** 23
[View Dashboard](https://your-domain/releases/abc123)
```

### Notification on Full Approval

```
✅ **Release Ready to Deploy**
`uat` → `master` | 5 repositories
Approved by: @dev-lead (Dev), @qa-lead (QA)
[View Details](https://your-domain/releases/abc123)
```

### Flow

1. User: `@pusheen create-release uat master`
2. Bot compares branches via GitHub API
3. Bot creates release in SQLite
4. Bot posts summary + link to channel
5. User clicks link, manages release in dashboard
6. On full approval: Bot posts "Ready to Deploy" to channel

---

## Authentication (Keycloak SSO)

- OIDC flow with Keycloak
- Session cookie after successful auth
- API endpoints protected by middleware
- User info (name, email) from Keycloak token

---

## Deploy Order Calculation

Dependencies are per-release. Each repo can depend on other repos in the same release.

**Algorithm (topological sort):**
1. Repos with no dependencies = order 1
2. Repos depending only on order-1 repos = order 2
3. Continue until all repos assigned
4. Circular dependencies = error shown in UI

**Example:**
- `auth-service` depends on nothing → order 1
- `api-gateway` depends on `auth-service` → order 2
- `user-service` depends on `auth-service` → order 2
- `notification-svc` depends on `user-service` → order 3

---

## File Structure

```
mmtools/
├── cmd/mmtools/main.go
├── internal/
│   ├── dashboard/
│   │   ├── server.go          # HTTP server, routes
│   │   ├── handlers.go        # API handlers
│   │   ├── auth.go            # Keycloak OIDC
│   │   └── middleware.go      # Auth middleware
│   ├── database/
│   │   ├── sqlite.go          # GORM setup
│   │   ├── models.go          # Release, ReleaseRepo
│   │   └── migrations.go      # Auto-migrate
│   └── release/
│       ├── service.go         # Business logic
│       └── github.go          # GitHub integration
├── web/                       # Vue.js source
│   ├── src/
│   │   ├── App.vue
│   │   ├── views/
│   │   │   ├── ReleaseList.vue
│   │   │   └── ReleaseDetail.vue
│   │   └── components/
│   └── dist/                  # Built files (embedded)
└── docs/plans/
```

---

## Configuration

```yaml
serve:
  port: 8080

  # Existing Mattermost bot settings
  mattermost_url: "https://mattermost.example.com"
  mattermost_token: "bot-token"

  # Dashboard settings
  dashboard:
    enabled: true
    base_url: "https://releases.example.com"  # For links in Mattermost
    sqlite_path: "./releases.db"

    # Keycloak OIDC
    keycloak:
      issuer: "https://keycloak.example.com/realms/myrealm"
      client_id: "mmtools-dashboard"
      client_secret: "secret"
      redirect_url: "https://releases.example.com/auth/callback"
```
