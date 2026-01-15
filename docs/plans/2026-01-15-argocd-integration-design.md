# ArgoCD Integration Design

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Track deployment status in ArgoCD across qa/uat/prod environments, showing version comparison, sync status, health, and rollout progress.

**Architecture:** Hybrid polling approach - poll ArgoCD only after CI succeeds. ArgoCD client with Cloudflare auth headers fetches app status per environment. Results cached and exposed via API for frontend display.

**Tech Stack:** Go (ArgoCD client, tracker), PostgreSQL (deployment status), Vue.js (frontend display)

---

## Architecture Overview

**Data Flow:**
```
CI Success → ArgoCD Tracker starts polling → Fetches app status per environment
                                          → Stores in DB (RepoDeploymentStatus)
                                          → Frontend displays via API
```

**Key Components:**
1. **Config** - YAML file with ArgoCD URLs, Cloudflare credentials, app naming rules
2. **ArgoCD Client** - HTTP client with Cloudflare auth headers, fetches app details
3. **ArgoCD Tracker** - Background service (like CI Tracker), polls when CI succeeds
4. **Database** - New `RepoDeploymentStatus` table per repo per environment
5. **API/Frontend** - Endpoint returns deployment status, UI shows comparison grid

**Matching Logic:**
- Default: `{repo-name}-{env-suffix}` → ArgoCD app name
- Override: Config map for repos with custom app names

---

## Config Structure

```yaml
argocd:
  poll_interval: 30s      # how often to check when CI succeeded
  cache_ttl: 10s          # cache duration for API responses
  environments:
    qa:
      url: "https://argocd-qa.example.com"
      cf_client_id: "xxx"
      cf_client_secret: "xxx"
      app_suffix: "-qa"
    uat:
      url: "https://argocd-uat.example.com"
      cf_client_id: "xxx"
      cf_client_secret: "xxx"
      app_suffix: "-uat"
    prod:
      url: "https://argocd.example.com"
      cf_client_id: "xxx"
      cf_client_secret: "xxx"
      app_suffix: "-master"
  overrides:
    special-repo: "custom-app-name"  # optional per-repo override
```

---

## Database Model

**New Table: `RepoDeploymentStatus`**

```go
type RepoDeploymentStatus struct {
    ID              uint   `gorm:"primaryKey;autoIncrement"`
    ReleaseRepoID   uint   `gorm:"index;not null"`
    Environment     string `gorm:"index;not null"`  // qa, uat, prod
    AppName         string                          // argocd app name
    CurrentVersion  string                          // deployed chart version
    ExpectedVersion string                          // from CI status
    SyncStatus      string                          // Synced, OutOfSync, Unknown
    HealthStatus    string                          // Healthy, Degraded, Progressing, Missing
    RolloutStatus   string                          // json: {"replicas":3,"ready":2,"updated":3}
    LastCheckedAt   int64
}
```

**Unique Constraint:** One record per `ReleaseRepoID + Environment` combination.

---

## ArgoCD Client

**Package:** `pkg/argocd/client.go`

```go
type Client struct {
    httpClient     *http.Client
    baseURL        string
    cfClientID     string
    cfClientSecret string
}

type AppStatus struct {
    Name            string
    SyncStatus      string  // Synced, OutOfSync, Unknown
    HealthStatus    string  // Healthy, Degraded, Progressing, Missing
    CurrentVersion  string  // from status.summary.images or helm values
    Replicas        int
    ReadyReplicas   int
    UpdatedReplicas int
}
```

**Key Methods:**
- `GetApplication(ctx, appName)` - Fetches single app status
- `ListApplications(ctx)` - Lists all apps (for discovery)

**HTTP with Cloudflare Headers:**
```go
func (c *Client) doRequest(ctx context.Context, path string) (*http.Response, error) {
    req, _ := http.NewRequestWithContext(ctx, "GET", c.baseURL+path, nil)
    req.Header.Set("CF-Access-Client-Id", c.cfClientID)
    req.Header.Set("CF-Access-Client-Secret", c.cfClientSecret)
    return c.httpClient.Do(req)
}
```

**ArgoCD API Endpoints:**
- `GET /api/v1/applications/{name}` - Get single app
- `GET /api/v1/applications` - List all apps

---

## ArgoCD Tracker

**Package:** `internal/dashboard/argocd_tracker.go`

```go
type ArgoCDTracker struct {
    service    *Service
    clients    map[string]*argocd.Client  // env -> client
    config     *ArgoCDConfig
    interval   time.Duration
    cacheTTL   time.Duration
    cache      map[string]*deploymentCache  // releaseID -> cached statuses
    cacheMu    sync.RWMutex
    stopCh     chan struct{}
    wg         sync.WaitGroup
}
```

**Hybrid Polling Logic:**
- Background ticker runs every `poll_interval`
- Only checks repos where CI status = success
- Skips repos where deployment already matches expected version

**App Name Resolution:**
```go
func (t *ArgoCDTracker) resolveAppName(repoName, env string) string {
    if override, ok := t.config.Overrides[repoName]; ok {
        return override + t.config.Environments[env].AppSuffix
    }
    return repoName + t.config.Environments[env].AppSuffix
}
```

---

## API Endpoint

**Endpoint:** `GET /api/releases/{id}/deployment-status`

**Response:**
```json
{
  "statuses": [
    {
      "repo_id": 123,
      "repo_name": "bloody-accounting-master",
      "environments": {
        "qa": {
          "app_name": "bloody-accounting-master-qa",
          "current_version": "1.1.939",
          "expected_version": "1.1.940",
          "sync_status": "OutOfSync",
          "health_status": "Healthy",
          "rollout": {"replicas": 3, "ready": 3, "updated": 3},
          "last_checked_at": 1705123456
        },
        "uat": { ... },
        "prod": { ... }
      }
    }
  ],
  "any_pending": true
}
```

---

## Frontend Display

**Table Display (new columns or separate section):**

| Repo | CI Status | QA | UAT | Prod |
|------|-----------|----|----|------|
| bloody-accounting | ✅ v1.1.940 | ✅ 1.1.940 | 🔄 1.1.939→1.1.940 | ⏳ 1.1.938 |

**Status Icons:**
- ✅ Green - Version matches, synced, healthy
- 🔄 Yellow - Rollout in progress (sync ongoing)
- ⚠️ Orange - Version mismatch or OutOfSync
- ❌ Red - Degraded or failed
- ⏳ Gray - Waiting (not deployed yet)

**Hover/Click:** Shows full status including replica counts and health details.

---

## Implementation Tasks

### Task 1: Add Config Structure
- Add ArgoCD config to existing config system
- Parse YAML with environment definitions and overrides

### Task 2: Create Database Model
- Add `RepoDeploymentStatus` model
- Run migration
- Add service methods for CRUD operations

### Task 3: Create ArgoCD Client Package
- New `pkg/argocd/` package
- HTTP client with Cloudflare auth headers
- Parse ArgoCD API responses

### Task 4: Create ArgoCD Tracker
- Background polling service
- Hybrid logic (poll only after CI success)
- Cache with mutex for thread safety

### Task 5: Add API Endpoint
- Handler for `/api/releases/{id}/deployment-status`
- Aggregate data from tracker cache

### Task 6: Frontend Integration
- Add deployment status columns to repos table
- Status badges with version comparison
- Polling/refresh for active deployments
