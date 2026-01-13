package dashboard_test

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"github.com/user/mattermost-tools/internal/dashboard"
	"github.com/user/mattermost-tools/internal/database"
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
