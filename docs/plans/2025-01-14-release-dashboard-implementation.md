# Release Dashboard Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a release management dashboard with SQLite persistence, Vue.js UI, Keycloak SSO, and Mattermost bot integration.

**Architecture:** Extend existing `mmtools` binary with embedded Vue.js SPA, SQLite database via GORM, Keycloak OIDC auth. Bot creates releases, dashboard manages approvals.

**Tech Stack:** Go, SQLite, GORM, Vue.js 3, Keycloak OIDC, gorilla/sessions, embed package

---

## Task 1: Database Models and GORM Setup

**Files:**
- Create: `internal/database/sqlite.go`
- Create: `internal/database/models.go`

**Step 1: Create database package with SQLite connection**

```go
// internal/database/sqlite.go
package database

import (
	"fmt"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func NewSQLiteDB(path string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database: %w", err)
	}

	if err := db.AutoMigrate(&Release{}, &ReleaseRepo{}); err != nil {
		return nil, fmt.Errorf("auto migrating: %w", err)
	}

	return db, nil
}
```

**Step 2: Create GORM models**

```go
// internal/database/models.go
package database

import (
	"encoding/json"
)

type Release struct {
	ID                string `gorm:"primaryKey"`
	SourceBranch      string `gorm:"not null"`
	DestBranch        string `gorm:"not null"`
	Status            string `gorm:"default:pending"`
	Notes             string
	BreakingChanges   string
	CreatedBy         string `gorm:"not null"`
	ChannelID         string `gorm:"not null"`
	MattermostPostID  string
	DevApprovedBy     string
	DevApprovedAt     int64
	QAApprovedBy      string
	QAApprovedAt      int64
	LastRefreshedAt   int64
	CreatedAt         int64 `gorm:"not null"`
}

type ReleaseRepo struct {
	ID           uint   `gorm:"primaryKey;autoIncrement"`
	ReleaseID    string `gorm:"not null;index"`
	RepoName     string `gorm:"not null"`
	CommitCount  int    `gorm:"default:0"`
	Contributors string
	PRNumber     int
	PRURL        string
	Excluded     bool   `gorm:"default:false"`
	DependsOn    string
}

func (r *ReleaseRepo) GetContributors() []string {
	if r.Contributors == "" {
		return nil
	}
	var contributors []string
	json.Unmarshal([]byte(r.Contributors), &contributors)
	return contributors
}

func (r *ReleaseRepo) SetContributors(contributors []string) {
	data, _ := json.Marshal(contributors)
	r.Contributors = string(data)
}

func (r *ReleaseRepo) GetDependsOn() []string {
	if r.DependsOn == "" {
		return nil
	}
	var deps []string
	json.Unmarshal([]byte(r.DependsOn), &deps)
	return deps
}

func (r *ReleaseRepo) SetDependsOn(deps []string) {
	data, _ := json.Marshal(deps)
	r.DependsOn = string(data)
}
```

**Step 3: Run to verify it compiles**

```bash
go build ./...
```
Expected: Build succeeds

**Step 4: Commit**

```bash
git add internal/database/
git commit -m "feat(database): add SQLite setup and GORM models for releases"
```

---

## Task 2: Dashboard Config and Initialization

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/commands/serve/serve.go`

**Step 1: Add dashboard config to config.go**

Add after `ReleaseConfig` struct:

```go
type DashboardConfig struct {
	Enabled    bool           `yaml:"enabled"`
	BaseURL    string         `yaml:"base_url"`
	SQLitePath string         `yaml:"sqlite_path"`
	Keycloak   KeycloakConfig `yaml:"keycloak"`
}

type KeycloakConfig struct {
	Issuer       string `yaml:"issuer"`
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	RedirectURL  string `yaml:"redirect_url"`
}
```

Add `Dashboard DashboardConfig` field to `ServeConfig` struct.

**Step 2: Initialize SQLite in serve.go**

Add import and initialization in `runServe` function after config loading:

```go
var db *gorm.DB
if cfg.Serve.Dashboard.Enabled {
	sqlitePath := cfg.Serve.Dashboard.SQLitePath
	if sqlitePath == "" {
		sqlitePath = "./releases.db"
	}
	var err error
	db, err = database.NewSQLiteDB(sqlitePath)
	if err != nil {
		return fmt.Errorf("initializing database: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Dashboard database: %s\n", sqlitePath)
}
```

**Step 3: Run to verify it compiles**

```bash
go build ./...
```
Expected: Build succeeds

**Step 4: Commit**

```bash
git add internal/config/config.go internal/commands/serve/serve.go
git commit -m "feat(config): add dashboard configuration with Keycloak settings"
```

---

## Task 3: Release Service Layer

**Files:**
- Create: `internal/dashboard/service.go`
- Create: `internal/dashboard/service_test.go`

**Step 1: Write failing test for CreateRelease**

```go
// internal/dashboard/service_test.go
package dashboard_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/mattermost-tools/internal/dashboard"
	"github.com/user/mattermost-tools/internal/database"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&database.Release{}, &database.ReleaseRepo{}))
	return db
}

func TestService_CreateRelease_Success(t *testing.T) {
	db := setupTestDB(t)
	svc := dashboard.NewService(db)

	release, err := svc.CreateRelease(context.Background(), dashboard.CreateReleaseRequest{
		SourceBranch: "uat",
		DestBranch:   "master",
		CreatedBy:    "user123",
		ChannelID:    "channel456",
	})

	require.NoError(t, err)
	require.NotEmpty(t, release.ID)
	require.Equal(t, "uat", release.SourceBranch)
	require.Equal(t, "master", release.DestBranch)
	require.Equal(t, "pending", release.Status)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/dashboard/... -v
```
Expected: FAIL - package does not exist

**Step 3: Write minimal implementation**

```go
// internal/dashboard/service.go
package dashboard

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/user/mattermost-tools/internal/database"
	"gorm.io/gorm"
)

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

type CreateReleaseRequest struct {
	SourceBranch string
	DestBranch   string
	CreatedBy    string
	ChannelID    string
}

func (s *Service) CreateRelease(ctx context.Context, req CreateReleaseRequest) (*database.Release, error) {
	release := &database.Release{
		ID:           uuid.New().String(),
		SourceBranch: req.SourceBranch,
		DestBranch:   req.DestBranch,
		Status:       "pending",
		CreatedBy:    req.CreatedBy,
		ChannelID:    req.ChannelID,
		CreatedAt:    time.Now().Unix(),
	}

	if err := s.db.WithContext(ctx).Create(release).Error; err != nil {
		return nil, err
	}

	return release, nil
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/dashboard/... -v
```
Expected: PASS

**Step 5: Commit**

```bash
git add internal/dashboard/
git commit -m "feat(dashboard): add service layer with CreateRelease"
```

---

## Task 4: Service Methods - GetRelease, ListReleases, AddRepos

**Files:**
- Modify: `internal/dashboard/service.go`
- Modify: `internal/dashboard/service_test.go`

**Step 1: Write failing tests**

```go
func TestService_GetRelease_Success(t *testing.T) {
	db := setupTestDB(t)
	svc := dashboard.NewService(db)

	created, err := svc.CreateRelease(context.Background(), dashboard.CreateReleaseRequest{
		SourceBranch: "uat",
		DestBranch:   "master",
		CreatedBy:    "user123",
		ChannelID:    "channel456",
	})
	require.NoError(t, err)

	release, err := svc.GetRelease(context.Background(), created.ID)

	require.NoError(t, err)
	require.Equal(t, created.ID, release.ID)
}

func TestService_ListReleases_Success(t *testing.T) {
	db := setupTestDB(t)
	svc := dashboard.NewService(db)

	_, err := svc.CreateRelease(context.Background(), dashboard.CreateReleaseRequest{
		SourceBranch: "uat",
		DestBranch:   "master",
		CreatedBy:    "user123",
		ChannelID:    "channel456",
	})
	require.NoError(t, err)

	releases, err := svc.ListReleases(context.Background(), "")

	require.NoError(t, err)
	require.Len(t, releases, 1)
}

func TestService_AddRepos_Success(t *testing.T) {
	db := setupTestDB(t)
	svc := dashboard.NewService(db)

	release, err := svc.CreateRelease(context.Background(), dashboard.CreateReleaseRequest{
		SourceBranch: "uat",
		DestBranch:   "master",
		CreatedBy:    "user123",
		ChannelID:    "channel456",
	})
	require.NoError(t, err)

	repos := []dashboard.RepoData{
		{RepoName: "auth-service", CommitCount: 5, Contributors: []string{"alice", "bob"}},
		{RepoName: "api-gateway", CommitCount: 3, Contributors: []string{"charlie"}},
	}
	err = svc.AddRepos(context.Background(), release.ID, repos)

	require.NoError(t, err)

	releaseWithRepos, err := svc.GetReleaseWithRepos(context.Background(), release.ID)
	require.NoError(t, err)
	require.Len(t, releaseWithRepos.Repos, 2)
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/dashboard/... -v
```
Expected: FAIL

**Step 3: Implement methods**

```go
type ReleaseWithRepos struct {
	database.Release
	Repos []database.ReleaseRepo
}

type RepoData struct {
	RepoName     string
	CommitCount  int
	Contributors []string
	PRNumber     int
	PRURL        string
}

func (s *Service) GetRelease(ctx context.Context, id string) (*database.Release, error) {
	var release database.Release
	if err := s.db.WithContext(ctx).First(&release, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &release, nil
}

func (s *Service) ListReleases(ctx context.Context, status string) ([]database.Release, error) {
	var releases []database.Release
	query := s.db.WithContext(ctx).Order("created_at DESC")
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if err := query.Find(&releases).Error; err != nil {
		return nil, err
	}
	return releases, nil
}

func (s *Service) AddRepos(ctx context.Context, releaseID string, repos []RepoData) error {
	for _, r := range repos {
		repo := database.ReleaseRepo{
			ReleaseID:   releaseID,
			RepoName:    r.RepoName,
			CommitCount: r.CommitCount,
			PRNumber:    r.PRNumber,
			PRURL:       r.PRURL,
		}
		repo.SetContributors(r.Contributors)
		if err := s.db.WithContext(ctx).Create(&repo).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) GetReleaseWithRepos(ctx context.Context, id string) (*ReleaseWithRepos, error) {
	release, err := s.GetRelease(ctx, id)
	if err != nil {
		return nil, err
	}

	var repos []database.ReleaseRepo
	if err := s.db.WithContext(ctx).Where("release_id = ?", id).Find(&repos).Error; err != nil {
		return nil, err
	}

	return &ReleaseWithRepos{
		Release: *release,
		Repos:   repos,
	}, nil
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/dashboard/... -v
```
Expected: PASS

**Step 5: Commit**

```bash
git add internal/dashboard/
git commit -m "feat(dashboard): add GetRelease, ListReleases, AddRepos methods"
```

---

## Task 5: Service Methods - UpdateRelease, UpdateRepo, Approvals

**Files:**
- Modify: `internal/dashboard/service.go`
- Modify: `internal/dashboard/service_test.go`

**Step 1: Write failing tests**

```go
func TestService_UpdateRelease_Success(t *testing.T) {
	db := setupTestDB(t)
	svc := dashboard.NewService(db)

	release, _ := svc.CreateRelease(context.Background(), dashboard.CreateReleaseRequest{
		SourceBranch: "uat",
		DestBranch:   "master",
		CreatedBy:    "user123",
		ChannelID:    "channel456",
	})

	err := svc.UpdateRelease(context.Background(), release.ID, dashboard.UpdateReleaseRequest{
		Notes:           strPtr("Deploy auth-service first"),
		BreakingChanges: strPtr("API v2 deprecated"),
	})

	require.NoError(t, err)

	updated, _ := svc.GetRelease(context.Background(), release.ID)
	require.Equal(t, "Deploy auth-service first", updated.Notes)
	require.Equal(t, "API v2 deprecated", updated.BreakingChanges)
}

func strPtr(s string) *string { return &s }

func TestService_ApproveRelease_Dev_Success(t *testing.T) {
	db := setupTestDB(t)
	svc := dashboard.NewService(db)

	release, _ := svc.CreateRelease(context.Background(), dashboard.CreateReleaseRequest{
		SourceBranch: "uat",
		DestBranch:   "master",
		CreatedBy:    "user123",
		ChannelID:    "channel456",
	})

	err := svc.ApproveRelease(context.Background(), release.ID, "dev", "devlead123")

	require.NoError(t, err)

	updated, _ := svc.GetRelease(context.Background(), release.ID)
	require.Equal(t, "devlead123", updated.DevApprovedBy)
	require.NotZero(t, updated.DevApprovedAt)
}

func TestService_ApproveRelease_FullApproval_Success(t *testing.T) {
	db := setupTestDB(t)
	svc := dashboard.NewService(db)

	release, _ := svc.CreateRelease(context.Background(), dashboard.CreateReleaseRequest{
		SourceBranch: "uat",
		DestBranch:   "master",
		CreatedBy:    "user123",
		ChannelID:    "channel456",
	})

	_ = svc.ApproveRelease(context.Background(), release.ID, "dev", "devlead")
	_ = svc.ApproveRelease(context.Background(), release.ID, "qa", "qalead")

	updated, _ := svc.GetRelease(context.Background(), release.ID)
	require.Equal(t, "approved", updated.Status)
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/dashboard/... -v
```
Expected: FAIL

**Step 3: Implement methods**

```go
type UpdateReleaseRequest struct {
	Notes           *string
	BreakingChanges *string
}

func (s *Service) UpdateRelease(ctx context.Context, id string, req UpdateReleaseRequest) error {
	updates := make(map[string]interface{})
	if req.Notes != nil {
		updates["notes"] = *req.Notes
	}
	if req.BreakingChanges != nil {
		updates["breaking_changes"] = *req.BreakingChanges
	}
	if len(updates) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Model(&database.Release{}).Where("id = ?", id).Updates(updates).Error
}

func (s *Service) UpdateRepo(ctx context.Context, repoID uint, excluded *bool, dependsOn []string) error {
	updates := make(map[string]interface{})
	if excluded != nil {
		updates["excluded"] = *excluded
	}
	if dependsOn != nil {
		data, _ := json.Marshal(dependsOn)
		updates["depends_on"] = string(data)
	}
	return s.db.WithContext(ctx).Model(&database.ReleaseRepo{}).Where("id = ?", repoID).Updates(updates).Error
}

func (s *Service) ApproveRelease(ctx context.Context, id, approvalType, userID string) error {
	now := time.Now().Unix()
	updates := make(map[string]interface{})

	switch approvalType {
	case "dev":
		updates["dev_approved_by"] = userID
		updates["dev_approved_at"] = now
	case "qa":
		updates["qa_approved_by"] = userID
		updates["qa_approved_at"] = now
	default:
		return fmt.Errorf("invalid approval type: %s", approvalType)
	}

	if err := s.db.WithContext(ctx).Model(&database.Release{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return err
	}

	release, err := s.GetRelease(ctx, id)
	if err != nil {
		return err
	}

	if release.DevApprovedBy != "" && release.QAApprovedBy != "" {
		return s.db.WithContext(ctx).Model(&database.Release{}).Where("id = ?", id).Update("status", "approved").Error
	}

	return nil
}

func (s *Service) RevokeApproval(ctx context.Context, id, approvalType string) error {
	updates := make(map[string]interface{})

	switch approvalType {
	case "dev":
		updates["dev_approved_by"] = ""
		updates["dev_approved_at"] = 0
	case "qa":
		updates["qa_approved_by"] = ""
		updates["qa_approved_at"] = 0
	default:
		return fmt.Errorf("invalid approval type: %s", approvalType)
	}

	updates["status"] = "pending"

	return s.db.WithContext(ctx).Model(&database.Release{}).Where("id = ?", id).Updates(updates).Error
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/dashboard/... -v
```
Expected: PASS

**Step 5: Commit**

```bash
git add internal/dashboard/
git commit -m "feat(dashboard): add UpdateRelease, UpdateRepo, approval methods"
```

---

## Task 6: Deploy Order Calculation (Topological Sort)

**Files:**
- Create: `internal/dashboard/deploy_order.go`
- Create: `internal/dashboard/deploy_order_test.go`

**Step 1: Write failing tests**

```go
// internal/dashboard/deploy_order_test.go
package dashboard_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/mattermost-tools/internal/dashboard"
	"github.com/user/mattermost-tools/internal/database"
)

func TestCalculateDeployOrder_NoDeps(t *testing.T) {
	repos := []database.ReleaseRepo{
		{ID: 1, RepoName: "auth-service"},
		{ID: 2, RepoName: "api-gateway"},
	}

	order, err := dashboard.CalculateDeployOrder(repos)

	require.NoError(t, err)
	require.Equal(t, 1, order[1])
	require.Equal(t, 1, order[2])
}

func TestCalculateDeployOrder_WithDeps(t *testing.T) {
	repos := []database.ReleaseRepo{
		{ID: 1, RepoName: "auth-service"},
		{ID: 2, RepoName: "api-gateway", DependsOn: `["auth-service"]`},
		{ID: 3, RepoName: "notification-svc", DependsOn: `["api-gateway"]`},
	}

	order, err := dashboard.CalculateDeployOrder(repos)

	require.NoError(t, err)
	require.Equal(t, 1, order[1])
	require.Equal(t, 2, order[2])
	require.Equal(t, 3, order[3])
}

func TestCalculateDeployOrder_CircularDeps(t *testing.T) {
	repos := []database.ReleaseRepo{
		{ID: 1, RepoName: "service-a", DependsOn: `["service-b"]`},
		{ID: 2, RepoName: "service-b", DependsOn: `["service-a"]`},
	}

	_, err := dashboard.CalculateDeployOrder(repos)

	require.Error(t, err)
	require.Contains(t, err.Error(), "circular")
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/dashboard/... -v -run TestCalculateDeployOrder
```
Expected: FAIL

**Step 3: Implement topological sort**

```go
// internal/dashboard/deploy_order.go
package dashboard

import (
	"fmt"

	"github.com/user/mattermost-tools/internal/database"
)

func CalculateDeployOrder(repos []database.ReleaseRepo) (map[uint]int, error) {
	nameToID := make(map[string]uint)
	for _, r := range repos {
		nameToID[r.RepoName] = r.ID
	}

	deps := make(map[uint][]uint)
	for _, r := range repos {
		depNames := r.GetDependsOn()
		for _, depName := range depNames {
			if depID, ok := nameToID[depName]; ok {
				deps[r.ID] = append(deps[r.ID], depID)
			}
		}
	}

	order := make(map[uint]int)
	visited := make(map[uint]bool)
	inStack := make(map[uint]bool)

	var visit func(id uint) (int, error)
	visit = func(id uint) (int, error) {
		if inStack[id] {
			return 0, fmt.Errorf("circular dependency detected")
		}
		if visited[id] {
			return order[id], nil
		}

		inStack[id] = true

		maxDepOrder := 0
		for _, depID := range deps[id] {
			depOrder, err := visit(depID)
			if err != nil {
				return 0, err
			}
			if depOrder > maxDepOrder {
				maxDepOrder = depOrder
			}
		}

		inStack[id] = false
		visited[id] = true
		order[id] = maxDepOrder + 1

		return order[id], nil
	}

	for _, r := range repos {
		if _, err := visit(r.ID); err != nil {
			return nil, err
		}
	}

	return order, nil
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/dashboard/... -v -run TestCalculateDeployOrder
```
Expected: PASS

**Step 5: Commit**

```bash
git add internal/dashboard/deploy_order.go internal/dashboard/deploy_order_test.go
git commit -m "feat(dashboard): add deploy order calculation with topological sort"
```

---

## Task 7: Keycloak OIDC Authentication

**Files:**
- Create: `internal/dashboard/auth.go`

**Step 1: Implement OIDC auth handlers**

```go
// internal/dashboard/auth.go
package dashboard

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gorilla/sessions"
	"golang.org/x/oauth2"
)

type AuthConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

type Auth struct {
	provider     *oidc.Provider
	oauth2Config *oauth2.Config
	verifier     *oidc.IDTokenVerifier
	store        *sessions.CookieStore
}

type UserInfo struct {
	ID       string `json:"sub"`
	Username string `json:"preferred_username"`
	Email    string `json:"email"`
	Name     string `json:"name"`
}

func NewAuth(ctx context.Context, cfg AuthConfig, sessionSecret []byte) (*Auth, error) {
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("creating oidc provider: %w", err)
	}

	oauth2Config := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})

	return &Auth{
		provider:     provider,
		oauth2Config: oauth2Config,
		verifier:     verifier,
		store:        sessions.NewCookieStore(sessionSecret),
	}, nil
}

func (a *Auth) HandleLogin(w http.ResponseWriter, r *http.Request) {
	state := generateState()
	session, _ := a.store.Get(r, "auth-session")
	session.Values["state"] = state
	session.Save(r, w)

	http.Redirect(w, r, a.oauth2Config.AuthCodeURL(state), http.StatusFound)
}

func (a *Auth) HandleCallback(w http.ResponseWriter, r *http.Request) {
	session, _ := a.store.Get(r, "auth-session")
	savedState, ok := session.Values["state"].(string)
	if !ok || savedState != r.URL.Query().Get("state") {
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	token, err := a.oauth2Config.Exchange(r.Context(), code)
	if err != nil {
		http.Error(w, "Failed to exchange token", http.StatusInternalServerError)
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "No id_token in response", http.StatusInternalServerError)
		return
	}

	idToken, err := a.verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		http.Error(w, "Failed to verify token", http.StatusInternalServerError)
		return
	}

	var claims UserInfo
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "Failed to parse claims", http.StatusInternalServerError)
		return
	}

	session.Values["user"] = claims
	session.Values["state"] = ""
	session.Save(r, w)

	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *Auth) HandleLogout(w http.ResponseWriter, r *http.Request) {
	session, _ := a.store.Get(r, "auth-session")
	session.Values["user"] = nil
	session.Options.MaxAge = -1
	session.Save(r, w)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *Auth) HandleMe(w http.ResponseWriter, r *http.Request) {
	session, _ := a.store.Get(r, "auth-session")
	user, ok := session.Values["user"].(UserInfo)
	if !ok {
		http.Error(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func (a *Auth) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := a.store.Get(r, "auth-session")
		if _, ok := session.Values["user"].(UserInfo); !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (a *Auth) GetUser(r *http.Request) *UserInfo {
	session, _ := a.store.Get(r, "auth-session")
	user, ok := session.Values["user"].(UserInfo)
	if !ok {
		return nil
	}
	return &user
}

func generateState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

func init() {
	gob.Register(UserInfo{})
}
```

**Step 2: Add missing import**

Add at top of file:
```go
import "encoding/gob"
```

**Step 3: Run to verify it compiles**

```bash
go build ./...
```
Expected: Build succeeds (may need to run `go mod tidy` first)

**Step 4: Commit**

```bash
go mod tidy
git add internal/dashboard/auth.go go.mod go.sum
git commit -m "feat(dashboard): add Keycloak OIDC authentication"
```

---

## Task 8: HTTP API Handlers

**Files:**
- Create: `internal/dashboard/handlers.go`

**Step 1: Implement API handlers**

```go
// internal/dashboard/handlers.go
package dashboard

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/user/mattermost-tools/internal/database"
)

type Handlers struct {
	service *Service
	auth    *Auth
}

func NewHandlers(service *Service, auth *Auth) *Handlers {
	return &Handlers{service: service, auth: auth}
}

func (h *Handlers) ListReleases(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	releases, err := h.service.ListReleases(r.Context(), status)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, releases)
}

func (h *Handlers) GetRelease(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/releases/")
	id = strings.Split(id, "/")[0]

	release, err := h.service.GetReleaseWithRepos(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	deployOrder, _ := CalculateDeployOrder(release.Repos)

	type repoWithOrder struct {
		database.ReleaseRepo
		DeployOrder int `json:"deploy_order"`
	}

	repos := make([]repoWithOrder, len(release.Repos))
	for i, repo := range release.Repos {
		repos[i] = repoWithOrder{
			ReleaseRepo: repo,
			DeployOrder: deployOrder[repo.ID],
		}
	}

	respondJSON(w, map[string]interface{}{
		"release": release.Release,
		"repos":   repos,
	})
}

func (h *Handlers) UpdateRelease(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/releases/")
	id = strings.Split(id, "/")[0]

	var req UpdateReleaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.service.UpdateRelease(r.Context(), id, req); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]string{"status": "ok"})
}

func (h *Handlers) UpdateRepo(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	repoIDStr := parts[len(parts)-1]
	repoID, err := strconv.ParseUint(repoIDStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid repo id", http.StatusBadRequest)
		return
	}

	var req struct {
		Excluded  *bool    `json:"excluded"`
		DependsOn []string `json:"depends_on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.service.UpdateRepo(r.Context(), uint(repoID), req.Excluded, req.DependsOn); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]string{"status": "ok"})
}

func (h *Handlers) ApproveRelease(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	approvalType := parts[len(parts)-1]
	releaseID := parts[len(parts)-3]

	user := h.auth.GetUser(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := h.service.ApproveRelease(r.Context(), releaseID, approvalType, user.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]string{"status": "ok"})
}

func (h *Handlers) RevokeApproval(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	approvalType := parts[len(parts)-1]
	releaseID := parts[len(parts)-3]

	if err := h.service.RevokeApproval(r.Context(), releaseID, approvalType); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]string{"status": "ok"})
}

func (h *Handlers) RefreshRelease(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	releaseID := parts[len(parts)-2]

	if err := h.service.RefreshRelease(r.Context(), releaseID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]string{"status": "ok"})
}

func respondJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}
```

**Step 2: Add RefreshRelease to service (stub for now)**

Add to `service.go`:
```go
func (s *Service) RefreshRelease(ctx context.Context, id string) error {
	now := time.Now().Unix()
	return s.db.WithContext(ctx).Model(&database.Release{}).Where("id = ?", id).Update("last_refreshed_at", now).Error
}
```

**Step 3: Run to verify it compiles**

```bash
go build ./...
```
Expected: Build succeeds

**Step 4: Commit**

```bash
git add internal/dashboard/handlers.go internal/dashboard/service.go
git commit -m "feat(dashboard): add HTTP API handlers"
```

---

## Task 9: Dashboard HTTP Server Setup

**Files:**
- Create: `internal/dashboard/server.go`

**Step 1: Implement server with routes**

```go
// internal/dashboard/server.go
package dashboard

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"gorm.io/gorm"
)

type Server struct {
	db       *gorm.DB
	service  *Service
	auth     *Auth
	handlers *Handlers
	mux      *http.ServeMux
}

type ServerConfig struct {
	DB            *gorm.DB
	AuthConfig    AuthConfig
	SessionSecret []byte
}

func NewServer(ctx context.Context, cfg ServerConfig) (*Server, error) {
	service := NewService(cfg.DB)

	var auth *Auth
	var err error
	if cfg.AuthConfig.Issuer != "" {
		auth, err = NewAuth(ctx, cfg.AuthConfig, cfg.SessionSecret)
		if err != nil {
			return nil, err
		}
	}

	handlers := NewHandlers(service, auth)

	s := &Server{
		db:       cfg.DB,
		service:  service,
		auth:     auth,
		handlers: handlers,
		mux:      http.NewServeMux(),
	}

	s.setupRoutes()
	return s, nil
}

func (s *Server) setupRoutes() {
	if s.auth != nil {
		s.mux.HandleFunc("/auth/login", s.auth.HandleLogin)
		s.mux.HandleFunc("/auth/callback", s.auth.HandleCallback)
		s.mux.HandleFunc("/auth/logout", s.auth.HandleLogout)
		s.mux.HandleFunc("/api/me", s.auth.HandleMe)
	}

	s.mux.HandleFunc("/api/releases", s.handleReleases)
	s.mux.HandleFunc("/api/releases/", s.handleRelease)
}

func (s *Server) handleReleases(w http.ResponseWriter, r *http.Request) {
	if s.auth != nil {
		s.auth.RequireAuth(s.handlers.ListReleases)(w, r)
	} else {
		s.handlers.ListReleases(w, r)
	}
}

func (s *Server) handleRelease(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/releases/")
	parts := strings.Split(path, "/")

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.handlers.GetRelease(w, r)
		case http.MethodPatch:
			if len(parts) > 1 && parts[1] == "repos" {
				s.handlers.UpdateRepo(w, r)
			} else {
				s.handlers.UpdateRelease(w, r)
			}
		case http.MethodPost:
			if len(parts) > 1 && parts[1] == "approve" {
				s.handlers.ApproveRelease(w, r)
			} else if len(parts) > 1 && parts[1] == "refresh" {
				s.handlers.RefreshRelease(w, r)
			}
		case http.MethodDelete:
			if len(parts) > 1 && parts[1] == "approve" {
				s.handlers.RevokeApproval(w, r)
			}
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}

	if s.auth != nil {
		s.auth.RequireAuth(handler)(w, r)
	} else {
		handler(w, r)
	}
}

func (s *Server) Service() *Service {
	return s.service
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) ServeStaticFiles(staticFS embed.FS, subdir string) error {
	subFS, err := fs.Sub(staticFS, subdir)
	if err != nil {
		return err
	}

	fileServer := http.FileServer(http.FS(subFS))
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/") {
			return
		}
		if r.URL.Path != "/" && !strings.Contains(r.URL.Path, ".") {
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})

	return nil
}
```

**Step 2: Run to verify it compiles**

```bash
go build ./...
```
Expected: Build succeeds

**Step 3: Commit**

```bash
git add internal/dashboard/server.go
git commit -m "feat(dashboard): add HTTP server with routing"
```

---

## Task 10: Integrate Dashboard Server into serve.go

**Files:**
- Modify: `internal/commands/serve/serve.go`

**Step 1: Add dashboard server initialization and routes**

Add imports:
```go
"github.com/user/mattermost-tools/internal/dashboard"
"github.com/user/mattermost-tools/internal/database"
```

Add after database initialization in `runServe`:
```go
var dashboardServer *dashboard.Server
if cfg.Serve.Dashboard.Enabled && db != nil {
	sessionSecret := []byte(cfg.Serve.MattermostToken)
	if len(sessionSecret) < 32 {
		sessionSecret = append(sessionSecret, make([]byte, 32-len(sessionSecret))...)
	}

	dashboardServer, err = dashboard.NewServer(context.Background(), dashboard.ServerConfig{
		DB: db,
		AuthConfig: dashboard.AuthConfig{
			Issuer:       cfg.Serve.Dashboard.Keycloak.Issuer,
			ClientID:     cfg.Serve.Dashboard.Keycloak.ClientID,
			ClientSecret: cfg.Serve.Dashboard.Keycloak.ClientSecret,
			RedirectURL:  cfg.Serve.Dashboard.Keycloak.RedirectURL,
		},
		SessionSecret: sessionSecret,
	})
	if err != nil {
		return fmt.Errorf("initializing dashboard server: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Dashboard enabled at %s\n", cfg.Serve.Dashboard.BaseURL)
}
```

Mount dashboard routes on mux (before server start):
```go
if dashboardServer != nil {
	mux.Handle("/api/", dashboardServer.Handler())
	mux.Handle("/auth/", dashboardServer.Handler())
}
```

**Step 2: Run to verify it compiles**

```bash
go build ./...
```
Expected: Build succeeds

**Step 3: Commit**

```bash
git add internal/commands/serve/serve.go
git commit -m "feat(serve): integrate dashboard server into serve command"
```

---

## Task 11: Vue.js Project Setup

**Files:**
- Create: `web/` directory with Vue.js project

**Step 1: Create Vue.js project**

```bash
cd /Users/iqpirat/sources/github/mattermost-tools
npm create vue@latest web -- --typescript --router --pinia
cd web
npm install
npm install axios
```

**Step 2: Verify it builds**

```bash
cd web && npm run build
```
Expected: Build succeeds, creates `dist/` folder

**Step 3: Commit**

```bash
cd /Users/iqpirat/sources/github/mattermost-tools
git add web/
git commit -m "feat(web): initialize Vue.js project with TypeScript and Router"
```

---

## Task 12: Vue.js API Client

**Files:**
- Create: `web/src/api/client.ts`
- Create: `web/src/api/types.ts`

**Step 1: Create types**

```typescript
// web/src/api/types.ts
export interface Release {
  ID: string
  SourceBranch: string
  DestBranch: string
  Status: string
  Notes: string
  BreakingChanges: string
  CreatedBy: string
  ChannelID: string
  DevApprovedBy: string
  DevApprovedAt: number
  QAApprovedBy: string
  QAApprovedAt: number
  LastRefreshedAt: number
  CreatedAt: number
}

export interface ReleaseRepo {
  ID: number
  ReleaseID: string
  RepoName: string
  CommitCount: number
  Contributors: string
  PRNumber: number
  PRURL: string
  Excluded: boolean
  DependsOn: string
  DeployOrder: number
}

export interface ReleaseWithRepos {
  release: Release
  repos: ReleaseRepo[]
}

export interface UserInfo {
  sub: string
  preferred_username: string
  email: string
  name: string
}
```

**Step 2: Create API client**

```typescript
// web/src/api/client.ts
import axios from 'axios'
import type { Release, ReleaseWithRepos, UserInfo } from './types'

const api = axios.create({
  baseURL: '/api',
  withCredentials: true
})

export const releaseApi = {
  list: async (status?: string): Promise<Release[]> => {
    const params = status ? { status } : {}
    const { data } = await api.get('/releases', { params })
    return data
  },

  get: async (id: string): Promise<ReleaseWithRepos> => {
    const { data } = await api.get(`/releases/${id}`)
    return data
  },

  update: async (id: string, updates: { notes?: string; breaking_changes?: string }) => {
    await api.patch(`/releases/${id}`, updates)
  },

  updateRepo: async (releaseId: string, repoId: number, updates: { excluded?: boolean; depends_on?: string[] }) => {
    await api.patch(`/releases/${releaseId}/repos/${repoId}`, updates)
  },

  approve: async (id: string, type: 'dev' | 'qa') => {
    await api.post(`/releases/${id}/approve/${type}`)
  },

  revokeApproval: async (id: string, type: 'dev' | 'qa') => {
    await api.delete(`/releases/${id}/approve/${type}`)
  },

  refresh: async (id: string) => {
    await api.post(`/releases/${id}/refresh`)
  }
}

export const authApi = {
  me: async (): Promise<UserInfo | null> => {
    try {
      const { data } = await api.get('/me')
      return data
    } catch {
      return null
    }
  },

  login: () => {
    window.location.href = '/auth/login'
  },

  logout: () => {
    window.location.href = '/auth/logout'
  }
}
```

**Step 3: Commit**

```bash
git add web/src/api/
git commit -m "feat(web): add API client and types"
```

---

## Task 13: Vue.js Release List View

**Files:**
- Modify: `web/src/views/HomeView.vue` → rename to `ReleaseListView.vue`
- Modify: `web/src/router/index.ts`

**Step 1: Create ReleaseListView**

```vue
<!-- web/src/views/ReleaseListView.vue -->
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { releaseApi } from '@/api/client'
import type { Release } from '@/api/types'

const releases = ref<Release[]>([])
const loading = ref(true)
const statusFilter = ref('')

async function loadReleases() {
  loading.value = true
  try {
    releases.value = await releaseApi.list(statusFilter.value || undefined)
  } finally {
    loading.value = false
  }
}

function formatDate(timestamp: number) {
  return new Date(timestamp * 1000).toLocaleString()
}

function statusEmoji(status: string) {
  switch (status) {
    case 'approved': return '🟢'
    case 'deployed': return '🔵'
    default: return '🟡'
  }
}

onMounted(loadReleases)
</script>

<template>
  <div class="release-list">
    <header>
      <h1>Releases</h1>
      <select v-model="statusFilter" @change="loadReleases">
        <option value="">All</option>
        <option value="pending">Pending</option>
        <option value="approved">Approved</option>
        <option value="deployed">Deployed</option>
      </select>
    </header>

    <div v-if="loading" class="loading">Loading...</div>

    <table v-else>
      <thead>
        <tr>
          <th>Status</th>
          <th>Branches</th>
          <th>Created</th>
          <th>Dev</th>
          <th>QA</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="release in releases" :key="release.ID">
          <td>{{ statusEmoji(release.Status) }}</td>
          <td>
            <router-link :to="`/releases/${release.ID}`">
              {{ release.SourceBranch }} → {{ release.DestBranch }}
            </router-link>
          </td>
          <td>{{ formatDate(release.CreatedAt) }}</td>
          <td>{{ release.DevApprovedBy ? '☑' : '☐' }}</td>
          <td>{{ release.QAApprovedBy ? '☑' : '☐' }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.release-list { padding: 20px; }
header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
table { width: 100%; border-collapse: collapse; }
th, td { padding: 10px; text-align: left; border-bottom: 1px solid #ddd; }
.loading { text-align: center; padding: 40px; }
a { color: #007bff; text-decoration: none; }
a:hover { text-decoration: underline; }
</style>
```

**Step 2: Update router**

```typescript
// web/src/router/index.ts
import { createRouter, createWebHistory } from 'vue-router'
import ReleaseListView from '../views/ReleaseListView.vue'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/',
      redirect: '/releases'
    },
    {
      path: '/releases',
      name: 'releases',
      component: ReleaseListView
    },
    {
      path: '/releases/:id',
      name: 'release-detail',
      component: () => import('../views/ReleaseDetailView.vue')
    }
  ]
})

export default router
```

**Step 3: Commit**

```bash
git add web/src/views/ web/src/router/
git commit -m "feat(web): add release list view"
```

---

## Task 14: Vue.js Release Detail View

**Files:**
- Create: `web/src/views/ReleaseDetailView.vue`

**Step 1: Create ReleaseDetailView**

```vue
<!-- web/src/views/ReleaseDetailView.vue -->
<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { useRoute } from 'vue-router'
import { releaseApi } from '@/api/client'
import type { Release, ReleaseRepo } from '@/api/types'

const route = useRoute()
const release = ref<Release | null>(null)
const repos = ref<ReleaseRepo[]>([])
const loading = ref(true)
const refreshing = ref(false)
const editingNotes = ref(false)
const editingBreaking = ref(false)
const notesText = ref('')
const breakingText = ref('')

const releaseId = computed(() => route.params.id as string)

const sortedRepos = computed(() => {
  return [...repos.value].sort((a, b) => a.DeployOrder - b.DeployOrder)
})

async function loadRelease() {
  loading.value = true
  try {
    const data = await releaseApi.get(releaseId.value)
    release.value = data.release
    repos.value = data.repos
    notesText.value = data.release.Notes || ''
    breakingText.value = data.release.BreakingChanges || ''
  } finally {
    loading.value = false
  }
}

async function refresh() {
  refreshing.value = true
  try {
    await releaseApi.refresh(releaseId.value)
    await loadRelease()
  } finally {
    refreshing.value = false
  }
}

async function saveNotes() {
  await releaseApi.update(releaseId.value, { notes: notesText.value })
  if (release.value) release.value.Notes = notesText.value
  editingNotes.value = false
}

async function saveBreaking() {
  await releaseApi.update(releaseId.value, { breaking_changes: breakingText.value })
  if (release.value) release.value.BreakingChanges = breakingText.value
  editingBreaking.value = false
}

async function toggleExcluded(repo: ReleaseRepo) {
  await releaseApi.updateRepo(releaseId.value, repo.ID, { excluded: !repo.Excluded })
  repo.Excluded = !repo.Excluded
}

async function updateDependsOn(repo: ReleaseRepo, deps: string[]) {
  await releaseApi.updateRepo(releaseId.value, repo.ID, { depends_on: deps })
  await loadRelease()
}

async function approve(type: 'dev' | 'qa') {
  await releaseApi.approve(releaseId.value, type)
  await loadRelease()
}

async function revoke(type: 'dev' | 'qa') {
  await releaseApi.revokeApproval(releaseId.value, type)
  await loadRelease()
}

function formatDate(timestamp: number) {
  if (!timestamp) return 'Not approved'
  return new Date(timestamp * 1000).toLocaleString()
}

function getContributors(repo: ReleaseRepo): string[] {
  if (!repo.Contributors) return []
  try {
    return JSON.parse(repo.Contributors)
  } catch {
    return []
  }
}

function getDependsOn(repo: ReleaseRepo): string[] {
  if (!repo.DependsOn) return []
  try {
    return JSON.parse(repo.DependsOn)
  } catch {
    return []
  }
}

function getOtherRepoNames(currentRepo: ReleaseRepo): string[] {
  return repos.value
    .filter(r => r.ID !== currentRepo.ID && !r.Excluded)
    .map(r => r.RepoName)
}

onMounted(loadRelease)
</script>

<template>
  <div class="release-detail" v-if="!loading && release">
    <header>
      <div>
        <h1>Release: {{ release.SourceBranch }} → {{ release.DestBranch }}</h1>
        <p class="meta">
          Created {{ formatDate(release.CreatedAt) }}
          <span v-if="release.LastRefreshedAt"> • Last refreshed {{ formatDate(release.LastRefreshedAt) }}</span>
        </p>
      </div>
      <div class="actions">
        <button @click="refresh" :disabled="refreshing">
          {{ refreshing ? 'Refreshing...' : '⟳ Refresh from GitHub' }}
        </button>
        <router-link to="/releases">← Back</router-link>
      </div>
    </header>

    <section class="notes-section">
      <h2>Notes <button @click="editingNotes = !editingNotes">{{ editingNotes ? 'Cancel' : 'Edit' }}</button></h2>
      <div v-if="editingNotes">
        <textarea v-model="notesText" rows="4"></textarea>
        <button @click="saveNotes">Save</button>
      </div>
      <p v-else>{{ release.Notes || 'No notes' }}</p>
    </section>

    <section class="breaking-section">
      <h2>Breaking Changes <button @click="editingBreaking = !editingBreaking">{{ editingBreaking ? 'Cancel' : 'Edit' }}</button></h2>
      <div v-if="editingBreaking">
        <textarea v-model="breakingText" rows="4"></textarea>
        <button @click="saveBreaking">Save</button>
      </div>
      <p v-else>{{ release.BreakingChanges || 'None' }}</p>
    </section>

    <section class="repos-section">
      <h2>Repositories</h2>
      <table>
        <thead>
          <tr>
            <th>Order</th>
            <th>Repository</th>
            <th>Commits</th>
            <th>PR</th>
            <th>Depends On</th>
            <th>Excluded</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="repo in sortedRepos" :key="repo.ID" :class="{ excluded: repo.Excluded }">
            <td>{{ repo.DeployOrder }}</td>
            <td>{{ repo.RepoName }}</td>
            <td>{{ repo.CommitCount }}</td>
            <td>
              <a v-if="repo.PRURL" :href="repo.PRURL" target="_blank">#{{ repo.PRNumber }}</a>
              <span v-else>-</span>
            </td>
            <td>
              <select multiple @change="e => updateDependsOn(repo, Array.from((e.target as HTMLSelectElement).selectedOptions, o => o.value))">
                <option v-for="name in getOtherRepoNames(repo)" :key="name" :value="name" :selected="getDependsOn(repo).includes(name)">
                  {{ name }}
                </option>
              </select>
            </td>
            <td>
              <input type="checkbox" :checked="repo.Excluded" @change="toggleExcluded(repo)" />
            </td>
          </tr>
        </tbody>
      </table>
    </section>

    <section class="approvals-section">
      <h2>Approvals</h2>
      <div class="approval-row">
        <span>Dev Lead:</span>
        <span v-if="release.DevApprovedBy">
          ☑ {{ release.DevApprovedBy }} ({{ formatDate(release.DevApprovedAt) }})
          <button @click="revoke('dev')">Revoke</button>
        </span>
        <span v-else>
          ☐ Not approved
          <button @click="approve('dev')">Approve as Dev</button>
        </span>
      </div>
      <div class="approval-row">
        <span>QA:</span>
        <span v-if="release.QAApprovedBy">
          ☑ {{ release.QAApprovedBy }} ({{ formatDate(release.QAApprovedAt) }})
          <button @click="revoke('qa')">Revoke</button>
        </span>
        <span v-else>
          ☐ Not approved
          <button @click="approve('qa')">Approve as QA</button>
        </span>
      </div>
      <div v-if="release.Status === 'approved'" class="approved-banner">
        ✅ Release is fully approved and ready to deploy!
      </div>
    </section>
  </div>
  <div v-else class="loading">Loading...</div>
</template>

<style scoped>
.release-detail { padding: 20px; max-width: 1200px; margin: 0 auto; }
header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 20px; }
.meta { color: #666; font-size: 0.9em; }
.actions { display: flex; gap: 10px; align-items: center; }
section { margin-bottom: 30px; }
h2 { display: flex; align-items: center; gap: 10px; }
h2 button { font-size: 0.8em; }
textarea { width: 100%; padding: 10px; }
table { width: 100%; border-collapse: collapse; }
th, td { padding: 10px; text-align: left; border-bottom: 1px solid #ddd; }
.excluded { opacity: 0.5; text-decoration: line-through; }
.approval-row { margin: 10px 0; display: flex; gap: 10px; align-items: center; }
.approved-banner { background: #d4edda; padding: 15px; border-radius: 5px; margin-top: 20px; }
.loading { text-align: center; padding: 40px; }
select[multiple] { min-height: 60px; }
</style>
```

**Step 2: Verify it compiles**

```bash
cd web && npm run build
```
Expected: Build succeeds

**Step 3: Commit**

```bash
git add web/src/views/ReleaseDetailView.vue
git commit -m "feat(web): add release detail view with approvals and dependencies"
```

---

## Task 15: Embed Vue.js in Go Binary

**Files:**
- Create: `web/embed.go`
- Modify: `internal/commands/serve/serve.go`

**Step 1: Create embed file**

```go
// web/embed.go
package web

import "embed"

//go:embed dist/*
var StaticFiles embed.FS
```

**Step 2: Build Vue.js for production**

```bash
cd web && npm run build
```

**Step 3: Update serve.go to serve static files**

Add import:
```go
"github.com/user/mattermost-tools/web"
```

Add after dashboard server initialization:
```go
if dashboardServer != nil {
	if err := dashboardServer.ServeStaticFiles(web.StaticFiles, "dist"); err != nil {
		return fmt.Errorf("setting up static files: %w", err)
	}
}
```

Update mux to handle static files (replace the existing mux.Handle lines for dashboard):
```go
if dashboardServer != nil {
	mux.Handle("/", dashboardServer.Handler())
}
```

**Step 4: Verify it compiles**

```bash
go build ./...
```
Expected: Build succeeds

**Step 5: Commit**

```bash
git add web/embed.go internal/commands/serve/serve.go
git commit -m "feat(serve): embed Vue.js static files in Go binary"
```

---

## Task 16: Update Bot to Use Dashboard Service

**Files:**
- Modify: `internal/commands/serve/serve.go`

**Step 1: Update processCreateReleaseAsync to use dashboard service**

Replace `processCreateReleaseAsync` function:
```go
func processCreateReleaseAsync(dashboardSvc *dashboard.Service, ghClient *github.Client, org string, ignoredRepos map[string]struct{}, mmBot *mattermost.Bot, baseURL, channelID, threadID, userName, sourceBranch, destBranch string) {
	ctx := context.Background()

	ownerUser, err := mmBot.GetUserByUsername(ctx, userName)
	if err != nil || ownerUser == nil {
		mmBot.PostMessageInThread(ctx, channelID, threadID, fmt.Sprintf("Failed to find user @%s\n\n_Requested by @%s_", userName, userName))
		return
	}

	release, err := dashboardSvc.CreateRelease(ctx, dashboard.CreateReleaseRequest{
		SourceBranch: sourceBranch,
		DestBranch:   destBranch,
		CreatedBy:    ownerUser.ID,
		ChannelID:    channelID,
	})
	if err != nil {
		mmBot.PostMessageInThread(ctx, channelID, threadID, fmt.Sprintf("Failed to create release: %v\n\n_Requested by @%s_", err, userName))
		return
	}

	repos, err := gatherRepoData(ctx, ghClient, org, ignoredRepos, sourceBranch, destBranch)
	if err != nil {
		mmBot.PostMessageInThread(ctx, channelID, threadID, fmt.Sprintf("Failed to gather repos: %v\n\n_Requested by @%s_", err, userName))
		return
	}

	if err := dashboardSvc.AddRepos(ctx, release.ID, repos); err != nil {
		mmBot.PostMessageInThread(ctx, channelID, threadID, fmt.Sprintf("Failed to add repos: %v\n\n_Requested by @%s_", err, userName))
		return
	}

	releaseURL := fmt.Sprintf("%s/releases/%s", baseURL, release.ID)
	message := fmt.Sprintf("## Release: `%s` → `%s`\n**Repositories:** %d\n[View Dashboard](%s)\n\n_Requested by @%s_",
		sourceBranch, destBranch, len(repos), releaseURL, userName)

	postID, err := mmBot.PostMessageWithID(ctx, channelID, message)
	if err == nil {
		dashboardSvc.SetMattermostPostID(ctx, release.ID, postID)
	}
}
```

**Step 2: Add helper function for gathering repo data**

```go
func gatherRepoData(ctx context.Context, ghClient *github.Client, org string, ignoredRepos map[string]struct{}, sourceBranch, destBranch string) ([]dashboard.RepoData, error) {
	repos, err := ghClient.ListRepositories(ctx, org)
	if err != nil {
		return nil, err
	}

	var results []dashboard.RepoData
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)

	for _, repo := range repos {
		if repo.Archived {
			continue
		}
		if _, ignored := ignoredRepos[repo.Name]; ignored {
			continue
		}

		wg.Add(1)
		go func(repo github.Repository) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			compare, err := ghClient.CompareBranches(ctx, org, repo.Name, destBranch, sourceBranch)
			if err != nil || compare == nil || compare.TotalCommits == 0 {
				return
			}

			var contributors []string
			seen := make(map[string]struct{})
			for _, c := range compare.Commits {
				if c.Author.Login != "" {
					if _, ok := seen[c.Author.Login]; !ok {
						seen[c.Author.Login] = struct{}{}
						contributors = append(contributors, c.Author.Login)
					}
				}
			}

			pr, _ := ghClient.FindPullRequest(ctx, org, repo.Name, sourceBranch, destBranch)

			data := dashboard.RepoData{
				RepoName:     repo.Name,
				CommitCount:  compare.TotalCommits,
				Contributors: contributors,
			}
			if pr != nil {
				data.PRNumber = pr.Number
				data.PRURL = pr.HTMLURL
			}

			mu.Lock()
			results = append(results, data)
			mu.Unlock()
		}(repo)
	}

	wg.Wait()
	return results, nil
}
```

**Step 3: Add SetMattermostPostID to service**

Add to `internal/dashboard/service.go`:
```go
func (s *Service) SetMattermostPostID(ctx context.Context, id, postID string) error {
	return s.db.WithContext(ctx).Model(&database.Release{}).Where("id = ?", id).Update("mattermost_post_id", postID).Error
}
```

**Step 4: Update the command handler call site**

Update the `create-release` case in `handleWebSocketMessage` to pass dashboardServer.Service() instead of releaseManager.

**Step 5: Verify it compiles**

```bash
go build ./...
```
Expected: Build succeeds

**Step 6: Commit**

```bash
git add internal/commands/serve/serve.go internal/dashboard/service.go
git commit -m "feat(bot): update create-release to use dashboard service"
```

---

## Task 17: Mattermost Notification on Full Approval

**Files:**
- Modify: `internal/dashboard/service.go`

**Step 1: Add notification callback to service**

Add to Service struct and constructor:
```go
type Service struct {
	db                   *gorm.DB
	onFullApprovalNotify func(release *database.Release)
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

func (s *Service) SetFullApprovalCallback(fn func(release *database.Release)) {
	s.onFullApprovalNotify = fn
}
```

Update `ApproveRelease` to call callback:
```go
func (s *Service) ApproveRelease(ctx context.Context, id, approvalType, userID string) error {
	// ... existing code ...

	if release.DevApprovedBy != "" && release.QAApprovedBy != "" {
		if err := s.db.WithContext(ctx).Model(&database.Release{}).Where("id = ?", id).Update("status", "approved").Error; err != nil {
			return err
		}
		release.Status = "approved"
		if s.onFullApprovalNotify != nil {
			s.onFullApprovalNotify(release)
		}
	}

	return nil
}
```

**Step 2: Wire up notification in serve.go**

Add after dashboard server initialization:
```go
if dashboardServer != nil && mmBot != nil {
	dashboardServer.Service().SetFullApprovalCallback(func(release *database.Release) {
		message := fmt.Sprintf("✅ **Release Ready to Deploy**\n`%s` → `%s`\nApproved by: Dev (%s), QA (%s)\n[View Details](%s/releases/%s)",
			release.SourceBranch, release.DestBranch,
			release.DevApprovedBy, release.QAApprovedBy,
			cfg.Serve.Dashboard.BaseURL, release.ID)
		mmBot.PostMessage(context.Background(), release.ChannelID, message)
	})
}
```

**Step 3: Verify it compiles**

```bash
go build ./...
```
Expected: Build succeeds

**Step 4: Commit**

```bash
git add internal/dashboard/service.go internal/commands/serve/serve.go
git commit -m "feat(dashboard): add Mattermost notification on full approval"
```

---

## Task 18: Integration Test - End to End

**Files:**
- Create: `internal/dashboard/integration_test.go`

**Step 1: Write integration test**

```go
// internal/dashboard/integration_test.go
package dashboard_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/mattermost-tools/internal/dashboard"
	"github.com/user/mattermost-tools/internal/database"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestIntegration_FullReleaseWorkflow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&database.Release{}, &database.ReleaseRepo{}))

	svc := dashboard.NewService(db)
	ctx := context.Background()

	notified := false
	svc.SetFullApprovalCallback(func(r *database.Release) {
		notified = true
	})

	release, err := svc.CreateRelease(ctx, dashboard.CreateReleaseRequest{
		SourceBranch: "uat",
		DestBranch:   "master",
		CreatedBy:    "user123",
		ChannelID:    "channel456",
	})
	require.NoError(t, err)

	repos := []dashboard.RepoData{
		{RepoName: "auth-service", CommitCount: 5},
		{RepoName: "api-gateway", CommitCount: 3},
	}
	require.NoError(t, svc.AddRepos(ctx, release.ID, repos))

	require.NoError(t, svc.UpdateRepo(ctx, 2, nil, []string{"auth-service"}))

	releaseData, err := svc.GetReleaseWithRepos(ctx, release.ID)
	require.NoError(t, err)
	require.Len(t, releaseData.Repos, 2)

	order, err := dashboard.CalculateDeployOrder(releaseData.Repos)
	require.NoError(t, err)
	require.Equal(t, 1, order[1])
	require.Equal(t, 2, order[2])

	require.NoError(t, svc.ApproveRelease(ctx, release.ID, "dev", "devlead"))
	require.False(t, notified)

	require.NoError(t, svc.ApproveRelease(ctx, release.ID, "qa", "qalead"))
	require.True(t, notified)

	finalRelease, err := svc.GetRelease(ctx, release.ID)
	require.NoError(t, err)
	require.Equal(t, "approved", finalRelease.Status)
}
```

**Step 2: Run test**

```bash
go test ./internal/dashboard/... -v -run TestIntegration
```
Expected: PASS

**Step 3: Commit**

```bash
git add internal/dashboard/integration_test.go
git commit -m "test(dashboard): add integration test for full release workflow"
```

---

## Task 19: Update config.yaml.example

**Files:**
- Modify: `config.yaml.example`

**Step 1: Add dashboard configuration section**

```yaml
  # Dashboard settings (optional - for web UI)
  dashboard:
    enabled: true
    base_url: "https://releases.example.com"
    sqlite_path: "./releases.db"

    # Keycloak OIDC settings
    keycloak:
      issuer: "https://keycloak.example.com/realms/myrealm"
      client_id: "mmtools-dashboard"
      client_secret: "your-client-secret"
      redirect_url: "https://releases.example.com/auth/callback"
```

**Step 2: Commit**

```bash
git add config.yaml.example
git commit -m "docs: add dashboard configuration to config example"
```

---

## Task 20: Final Build and Verification

**Step 1: Build Vue.js production**

```bash
cd web && npm run build
```

**Step 2: Build Go binary**

```bash
go build ./cmd/mmtools
```

**Step 3: Run linter**

```bash
make lint
```

**Step 4: Run all tests**

```bash
go test ./... -v
```

**Step 5: Final commit**

```bash
git add -A
git commit -m "chore: final build verification"
```

---

## Summary

This plan implements the release dashboard with:
1. SQLite database with GORM models
2. Service layer with full CRUD operations
3. Deploy order calculation via topological sort
4. Keycloak OIDC authentication
5. HTTP API handlers
6. Vue.js SPA with list and detail views
7. Embedded static files in Go binary
8. Bot integration for create-release command
9. Mattermost notification on full approval
10. Comprehensive tests
