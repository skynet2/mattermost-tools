package dashboard

import (
	"context"
	"sync"
	"time"

	"github.com/user/mattermost-tools/internal/config"
	"github.com/user/mattermost-tools/internal/database"
	"github.com/user/mattermost-tools/internal/logger"
	"github.com/user/mattermost-tools/pkg/argocd"
)

type deploymentCache struct {
	statuses  []database.RepoDeploymentStatus
	fetchedAt time.Time
}

type ArgoCDTracker struct {
	service   *Service
	clients   map[string]*argocd.Client
	config    *config.ArgoCDConfig
	interval  time.Duration
	cacheTTL  time.Duration
	cache     map[string]*deploymentCache
	cacheMu   sync.RWMutex
	fetchLock map[string]*sync.Mutex
	fetchMu   sync.Mutex
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

func NewArgoCDTracker(service *Service, cfg *config.ArgoCDConfig) *ArgoCDTracker {
	interval := cfg.PollInterval
	if interval == 0 {
		interval = 30 * time.Second
	}

	cacheTTL := cfg.CacheTTL
	if cacheTTL == 0 {
		cacheTTL = 10 * time.Second
	}

	clients := make(map[string]*argocd.Client)
	for envName, envCfg := range cfg.Environments {
		clients[envName] = argocd.NewClient(envCfg.URL, envCfg.CFClientID, envCfg.CFClientSecret)
	}

	return &ArgoCDTracker{
		service:   service,
		clients:   clients,
		config:    cfg,
		interval:  interval,
		cacheTTL:  cacheTTL,
		cache:     make(map[string]*deploymentCache),
		fetchLock: make(map[string]*sync.Mutex),
		stopCh:    make(chan struct{}),
	}
}

func (t *ArgoCDTracker) Start() {
	t.wg.Add(1)
	go t.run()
}

func (t *ArgoCDTracker) Stop() {
	close(t.stopCh)
	t.wg.Wait()
}

func (t *ArgoCDTracker) run() {
	defer t.wg.Done()
	log := logger.Get()

	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	log.Info().Dur("interval", t.interval).Int("environments", len(t.clients)).Msg("ArgoCD tracker started")

	for {
		select {
		case <-t.stopCh:
			log.Info().Msg("ArgoCD tracker stopped")
			return
		case <-ticker.C:
			t.checkPendingDeployments()
		}
	}
}

func (t *ArgoCDTracker) checkPendingDeployments() {
	log := logger.Get()
	ctx := context.Background()

	repos, err := t.getReposWithSuccessfulCI(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get repos with successful CI")
		return
	}

	if len(repos) == 0 {
		return
	}

	log.Debug().Int("count", len(repos)).Msg("Checking deployment statuses for repos with successful CI")

	for i := range repos {
		t.updateDeploymentStatus(ctx, &repos[i])
	}

	t.cacheMu.Lock()
	t.cache = make(map[string]*deploymentCache)
	t.cacheMu.Unlock()
}

func (t *ArgoCDTracker) getReposWithSuccessfulCI(ctx context.Context) ([]database.ReleaseRepo, error) {
	var repos []database.ReleaseRepo
	err := t.service.db.WithContext(ctx).
		Joins("INNER JOIN repo_ci_statuses ON repo_ci_statuses.release_repo_id = release_repos.id").
		Where("repo_ci_statuses.status = ?", "success").
		Where("repo_ci_statuses.chart_version != ''").
		Where("release_repos.excluded = ?", false).
		Find(&repos).Error
	if err != nil {
		return nil, err
	}
	return repos, nil
}

func (t *ArgoCDTracker) updateDeploymentStatus(ctx context.Context, repo *database.ReleaseRepo) {
	log := logger.Get()

	ciStatus, err := t.service.GetCIStatusByRepoID(ctx, repo.ID)
	if err != nil {
		log.Error().Err(err).Uint("repo_id", repo.ID).Msg("Failed to get CI status")
		return
	}
	if ciStatus == nil || ciStatus.ChartVersion == "" {
		log.Debug().Uint("repo_id", repo.ID).Msg("No chart version available for repo")
		return
	}

	expectedVersion := ciStatus.ChartVersion
	allMatch := true

	for envName, client := range t.clients {
		appName := t.resolveAppName(repo.RepoName, envName)

		appStatus, err := client.GetApplication(ctx, appName)
		if err != nil {
			log.Error().Err(err).Str("app", appName).Str("env", envName).Msg("Failed to get ArgoCD application")
			continue
		}

		status := &database.RepoDeploymentStatus{
			ReleaseRepoID:   repo.ID,
			Environment:     envName,
			AppName:         appName,
			ExpectedVersion: expectedVersion,
			LastCheckedAt:   time.Now().Unix(),
		}

		if appStatus == nil {
			status.RolloutStatus = "not_found"
			allMatch = false
		} else {
			status.CurrentVersion = appStatus.CurrentVersion
			status.SyncStatus = appStatus.SyncStatus
			status.HealthStatus = appStatus.HealthStatus
			status.RolloutStatus = t.determineRolloutStatus(appStatus, expectedVersion)

			if status.RolloutStatus != "deployed" {
				allMatch = false
			}
		}

		if err := t.service.CreateOrUpdateDeploymentStatus(ctx, status); err != nil {
			log.Error().Err(err).Str("app", appName).Str("env", envName).Msg("Failed to save deployment status")
		}
	}

	if allMatch {
		log.Debug().Str("repo", repo.RepoName).Str("version", expectedVersion).Msg("All environments deployed to expected version")
	}
}

func (t *ArgoCDTracker) resolveAppName(repoName, env string) string {
	if t.config.Overrides != nil {
		overrideKey := repoName + "-" + env
		if override, ok := t.config.Overrides[overrideKey]; ok {
			return override
		}
		if override, ok := t.config.Overrides[repoName]; ok {
			return override
		}
	}

	envCfg, ok := t.config.Environments[env]
	if !ok {
		return repoName
	}

	if envCfg.AppSuffix != "" {
		return repoName + envCfg.AppSuffix
	}

	return repoName
}

func (t *ArgoCDTracker) determineRolloutStatus(appStatus *argocd.AppStatus, expectedVersion string) string {
	if appStatus.CurrentVersion != expectedVersion {
		return "pending"
	}

	if appStatus.SyncStatus != "Synced" {
		return "syncing"
	}

	if appStatus.HealthStatus != "Healthy" {
		return "unhealthy"
	}

	return "deployed"
}

func (t *ArgoCDTracker) getFetchLock(releaseID string) *sync.Mutex {
	t.fetchMu.Lock()
	defer t.fetchMu.Unlock()
	if _, ok := t.fetchLock[releaseID]; !ok {
		t.fetchLock[releaseID] = &sync.Mutex{}
	}
	return t.fetchLock[releaseID]
}

func (t *ArgoCDTracker) GetCachedDeploymentStatuses(ctx context.Context, releaseID string) ([]database.RepoDeploymentStatus, bool) {
	t.cacheMu.RLock()
	cached, exists := t.cache[releaseID]
	if exists && time.Since(cached.fetchedAt) < t.cacheTTL {
		statuses := cached.statuses
		t.cacheMu.RUnlock()
		anyPending := t.checkAnyPending(statuses)
		return statuses, anyPending
	}
	t.cacheMu.RUnlock()

	lock := t.getFetchLock(releaseID)
	lock.Lock()
	defer lock.Unlock()

	t.cacheMu.RLock()
	cached, exists = t.cache[releaseID]
	if exists && time.Since(cached.fetchedAt) < t.cacheTTL {
		statuses := cached.statuses
		t.cacheMu.RUnlock()
		anyPending := t.checkAnyPending(statuses)
		return statuses, anyPending
	}
	t.cacheMu.RUnlock()

	statuses, err := t.service.GetDeploymentStatusesForRelease(ctx, releaseID)
	if err != nil {
		log := logger.Get()
		log.Error().Err(err).Str("release_id", releaseID).Msg("Failed to get deployment statuses")
		return nil, false
	}

	t.cacheMu.Lock()
	t.cache[releaseID] = &deploymentCache{
		statuses:  statuses,
		fetchedAt: time.Now(),
	}
	t.cacheMu.Unlock()

	anyPending := t.checkAnyPending(statuses)
	return statuses, anyPending
}

func (t *ArgoCDTracker) checkAnyPending(statuses []database.RepoDeploymentStatus) bool {
	for _, s := range statuses {
		if s.RolloutStatus == "pending" || s.RolloutStatus == "syncing" || s.RolloutStatus == "unhealthy" || s.RolloutStatus == "not_found" {
			return true
		}
	}
	return false
}

func (t *ArgoCDTracker) InvalidateCache(releaseID string) {
	t.cacheMu.Lock()
	delete(t.cache, releaseID)
	t.cacheMu.Unlock()
}

func (t *ArgoCDTracker) InitDeploymentTracking(ctx context.Context, releaseID string) error {
	log := logger.Get()

	repos, err := t.service.GetReposByReleaseID(ctx, releaseID)
	if err != nil {
		log.Error().Err(err).Str("release_id", releaseID).Msg("Failed to get repos for deployment tracking")
		return err
	}

	log.Info().Str("release_id", releaseID).Int("repo_count", len(repos)).Msg("Initializing deployment tracking")

	for _, repo := range repos {
		if repo.Excluded {
			log.Debug().Str("repo", repo.RepoName).Msg("Skipping excluded repo for deployment tracking")
			continue
		}

		ciStatus, err := t.service.GetCIStatusByRepoID(ctx, repo.ID)
		if err != nil {
			log.Error().Err(err).Str("repo", repo.RepoName).Msg("Failed to get CI status")
			continue
		}
		if ciStatus == nil || ciStatus.Status != "success" || ciStatus.ChartVersion == "" {
			log.Debug().Str("repo", repo.RepoName).Msg("Skipping repo - CI not successful or no chart version")
			continue
		}

		for envName := range t.clients {
			appName := t.resolveAppName(repo.RepoName, envName)

			status := &database.RepoDeploymentStatus{
				ReleaseRepoID:   repo.ID,
				Environment:     envName,
				AppName:         appName,
				ExpectedVersion: ciStatus.ChartVersion,
				RolloutStatus:   "pending",
				LastCheckedAt:   time.Now().Unix(),
			}

			if err := t.service.CreateOrUpdateDeploymentStatus(ctx, status); err != nil {
				log.Error().Err(err).Str("repo", repo.RepoName).Str("env", envName).Msg("Failed to create deployment status")
				continue
			}

			log.Info().
				Str("repo", repo.RepoName).
				Str("env", envName).
				Str("app", appName).
				Str("expected_version", ciStatus.ChartVersion).
				Msg("Created deployment status")
		}
	}

	return nil
}
