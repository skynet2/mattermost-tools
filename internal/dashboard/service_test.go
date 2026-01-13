package dashboard_test

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/user/mattermost-tools/internal/dashboard"
	"github.com/user/mattermost-tools/internal/database"
)

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&database.Release{}, &database.ReleaseRepo{}))
	return db
}

func TestService_GetRelease_Success(t *testing.T) {
	type tc struct {
		name         string
		sourceBranch string
		destBranch   string
		createdBy    string
		channelID    string
	}

	cases := []tc{
		{
			name:         "get existing release",
			sourceBranch: "uat",
			destBranch:   "master",
			createdBy:    "user123",
			channelID:    "channel456",
		},
		{
			name:         "get another release",
			sourceBranch: "develop",
			destBranch:   "staging",
			createdBy:    "admin",
			channelID:    "release-channel",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := setupTestDB(t)
			svc := dashboard.NewService(db)

			created, err := svc.CreateRelease(context.Background(), dashboard.CreateReleaseRequest{
				SourceBranch: c.sourceBranch,
				DestBranch:   c.destBranch,
				CreatedBy:    c.createdBy,
				ChannelID:    c.channelID,
			})
			require.NoError(t, err)

			release, err := svc.GetRelease(context.Background(), created.ID)

			require.NoError(t, err)
			require.Equal(t, created.ID, release.ID)
			require.Equal(t, c.sourceBranch, release.SourceBranch)
			require.Equal(t, c.destBranch, release.DestBranch)
		})
	}
}

func TestService_ListReleases_Success(t *testing.T) {
	type tc struct {
		name           string
		statusFilter   string
		releaseStatus  string
		expectedCount  int
	}

	cases := []tc{
		{
			name:          "list all releases",
			statusFilter:  "",
			releaseStatus: "pending",
			expectedCount: 1,
		},
		{
			name:          "filter by pending status",
			statusFilter:  "pending",
			releaseStatus: "pending",
			expectedCount: 1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := setupTestDB(t)
			svc := dashboard.NewService(db)

			_, err := svc.CreateRelease(context.Background(), dashboard.CreateReleaseRequest{
				SourceBranch: "uat",
				DestBranch:   "master",
				CreatedBy:    "user123",
				ChannelID:    "channel456",
			})
			require.NoError(t, err)

			releases, err := svc.ListReleases(context.Background(), c.statusFilter)

			require.NoError(t, err)
			require.Len(t, releases, c.expectedCount)
		})
	}
}

func TestService_ListReleases_FilterNoMatch(t *testing.T) {
	db := setupTestDB(t)
	svc := dashboard.NewService(db)

	_, err := svc.CreateRelease(context.Background(), dashboard.CreateReleaseRequest{
		SourceBranch: "uat",
		DestBranch:   "master",
		CreatedBy:    "user123",
		ChannelID:    "channel456",
	})
	require.NoError(t, err)

	releases, err := svc.ListReleases(context.Background(), "completed")

	require.NoError(t, err)
	require.Len(t, releases, 0)
}

func TestService_AddRepos_Success(t *testing.T) {
	type tc struct {
		name  string
		repos []dashboard.RepoData
	}

	cases := []tc{
		{
			name: "add single repo",
			repos: []dashboard.RepoData{
				{RepoName: "auth-service", CommitCount: 5, Contributors: []string{"alice", "bob"}},
			},
		},
		{
			name: "add multiple repos",
			repos: []dashboard.RepoData{
				{RepoName: "auth-service", CommitCount: 5, Contributors: []string{"alice", "bob"}},
				{RepoName: "api-gateway", CommitCount: 3, Contributors: []string{"charlie"}},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := setupTestDB(t)
			svc := dashboard.NewService(db)

			release, err := svc.CreateRelease(context.Background(), dashboard.CreateReleaseRequest{
				SourceBranch: "uat",
				DestBranch:   "master",
				CreatedBy:    "user123",
				ChannelID:    "channel456",
			})
			require.NoError(t, err)

			err = svc.AddRepos(context.Background(), release.ID, c.repos)

			require.NoError(t, err)

			releaseWithRepos, err := svc.GetReleaseWithRepos(context.Background(), release.ID)
			require.NoError(t, err)
			require.Len(t, releaseWithRepos.Repos, len(c.repos))
		})
	}
}

func TestService_GetReleaseWithRepos_Success(t *testing.T) {
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
		{RepoName: "auth-service", CommitCount: 5, Contributors: []string{"alice", "bob"}, PRNumber: 123, PRURL: "https://github.com/org/auth-service/pull/123"},
	}
	err = svc.AddRepos(context.Background(), release.ID, repos)
	require.NoError(t, err)

	releaseWithRepos, err := svc.GetReleaseWithRepos(context.Background(), release.ID)

	require.NoError(t, err)
	require.Equal(t, release.ID, releaseWithRepos.ID)
	require.Len(t, releaseWithRepos.Repos, 1)
	require.Equal(t, "auth-service", releaseWithRepos.Repos[0].RepoName)
	require.Equal(t, 5, releaseWithRepos.Repos[0].CommitCount)
	require.Equal(t, 123, releaseWithRepos.Repos[0].PRNumber)
	require.Equal(t, "https://github.com/org/auth-service/pull/123", releaseWithRepos.Repos[0].PRURL)
}

func TestService_CreateRelease_Success(t *testing.T) {
	type tc struct {
		name         string
		sourceBranch string
		destBranch   string
		createdBy    string
		channelID    string
	}

	cases := []tc{
		{
			name:         "create uat to master release",
			sourceBranch: "uat",
			destBranch:   "master",
			createdBy:    "user123",
			channelID:    "channel456",
		},
		{
			name:         "create develop to staging release",
			sourceBranch: "develop",
			destBranch:   "staging",
			createdBy:    "admin",
			channelID:    "release-channel",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := setupTestDB(t)
			svc := dashboard.NewService(db)

			release, err := svc.CreateRelease(context.Background(), dashboard.CreateReleaseRequest{
				SourceBranch: c.sourceBranch,
				DestBranch:   c.destBranch,
				CreatedBy:    c.createdBy,
				ChannelID:    c.channelID,
			})

			require.NoError(t, err)
			require.NotEmpty(t, release.ID)
			require.Equal(t, c.sourceBranch, release.SourceBranch)
			require.Equal(t, c.destBranch, release.DestBranch)
			require.Equal(t, "pending", release.Status)
			require.Equal(t, c.createdBy, release.CreatedBy)
			require.Equal(t, c.channelID, release.ChannelID)
			require.NotZero(t, release.CreatedAt)
		})
	}
}

func strPtr(s string) *string { return &s }

func boolPtr(b bool) *bool { return &b }

func TestService_UpdateRelease_Success(t *testing.T) {
	type tc struct {
		name            string
		notes           *string
		breakingChanges *string
		expectedNotes   string
		expectedBreak   string
	}

	cases := []tc{
		{
			name:            "update notes only",
			notes:           strPtr("Deploy auth-service first"),
			breakingChanges: nil,
			expectedNotes:   "Deploy auth-service first",
			expectedBreak:   "",
		},
		{
			name:            "update breaking changes only",
			notes:           nil,
			breakingChanges: strPtr("API v2 deprecated"),
			expectedNotes:   "",
			expectedBreak:   "API v2 deprecated",
		},
		{
			name:            "update both fields",
			notes:           strPtr("Deploy auth-service first"),
			breakingChanges: strPtr("API v2 deprecated"),
			expectedNotes:   "Deploy auth-service first",
			expectedBreak:   "API v2 deprecated",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := setupTestDB(t)
			svc := dashboard.NewService(db)

			release, err := svc.CreateRelease(context.Background(), dashboard.CreateReleaseRequest{
				SourceBranch: "uat",
				DestBranch:   "master",
				CreatedBy:    "user123",
				ChannelID:    "channel456",
			})
			require.NoError(t, err)

			err = svc.UpdateRelease(context.Background(), release.ID, dashboard.UpdateReleaseRequest{
				Notes:           c.notes,
				BreakingChanges: c.breakingChanges,
			})

			require.NoError(t, err)

			updated, err := svc.GetRelease(context.Background(), release.ID)
			require.NoError(t, err)
			require.Equal(t, c.expectedNotes, updated.Notes)
			require.Equal(t, c.expectedBreak, updated.BreakingChanges)
		})
	}
}

func TestService_UpdateRelease_NoUpdates_Success(t *testing.T) {
	db := setupTestDB(t)
	svc := dashboard.NewService(db)

	release, err := svc.CreateRelease(context.Background(), dashboard.CreateReleaseRequest{
		SourceBranch: "uat",
		DestBranch:   "master",
		CreatedBy:    "user123",
		ChannelID:    "channel456",
	})
	require.NoError(t, err)

	err = svc.UpdateRelease(context.Background(), release.ID, dashboard.UpdateReleaseRequest{})

	require.NoError(t, err)
}

func TestService_UpdateRepo_Success(t *testing.T) {
	type tc struct {
		name             string
		excluded         *bool
		dependsOn        []string
		expectedExcluded bool
		expectedDeps     []string
	}

	cases := []tc{
		{
			name:             "update excluded only",
			excluded:         boolPtr(true),
			dependsOn:        nil,
			expectedExcluded: true,
			expectedDeps:     nil,
		},
		{
			name:             "update depends_on only",
			excluded:         nil,
			dependsOn:        []string{"auth-service", "api-gateway"},
			expectedExcluded: false,
			expectedDeps:     []string{"auth-service", "api-gateway"},
		},
		{
			name:             "update both fields",
			excluded:         boolPtr(true),
			dependsOn:        []string{"core-service"},
			expectedExcluded: true,
			expectedDeps:     []string{"core-service"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
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
				{RepoName: "test-repo", CommitCount: 5, Contributors: []string{"alice"}},
			}
			err = svc.AddRepos(context.Background(), release.ID, repos)
			require.NoError(t, err)

			releaseWithRepos, err := svc.GetReleaseWithRepos(context.Background(), release.ID)
			require.NoError(t, err)
			repoID := releaseWithRepos.Repos[0].ID

			err = svc.UpdateRepo(context.Background(), repoID, c.excluded, c.dependsOn)

			require.NoError(t, err)

			updated, err := svc.GetReleaseWithRepos(context.Background(), release.ID)
			require.NoError(t, err)
			require.Equal(t, c.expectedExcluded, updated.Repos[0].Excluded)
			deps, err := updated.Repos[0].GetDependsOn()
			require.NoError(t, err)
			require.Equal(t, c.expectedDeps, deps)
		})
	}
}

func TestService_ApproveRelease_Dev_Success(t *testing.T) {
	db := setupTestDB(t)
	svc := dashboard.NewService(db)

	release, err := svc.CreateRelease(context.Background(), dashboard.CreateReleaseRequest{
		SourceBranch: "uat",
		DestBranch:   "master",
		CreatedBy:    "user123",
		ChannelID:    "channel456",
	})
	require.NoError(t, err)

	err = svc.ApproveRelease(context.Background(), release.ID, "dev", "devlead123")

	require.NoError(t, err)

	updated, err := svc.GetRelease(context.Background(), release.ID)
	require.NoError(t, err)
	require.Equal(t, "devlead123", updated.DevApprovedBy)
	require.NotZero(t, updated.DevApprovedAt)
	require.Equal(t, "pending", updated.Status)
}

func TestService_ApproveRelease_QA_Success(t *testing.T) {
	db := setupTestDB(t)
	svc := dashboard.NewService(db)

	release, err := svc.CreateRelease(context.Background(), dashboard.CreateReleaseRequest{
		SourceBranch: "uat",
		DestBranch:   "master",
		CreatedBy:    "user123",
		ChannelID:    "channel456",
	})
	require.NoError(t, err)

	err = svc.ApproveRelease(context.Background(), release.ID, "qa", "qalead123")

	require.NoError(t, err)

	updated, err := svc.GetRelease(context.Background(), release.ID)
	require.NoError(t, err)
	require.Equal(t, "qalead123", updated.QAApprovedBy)
	require.NotZero(t, updated.QAApprovedAt)
	require.Equal(t, "pending", updated.Status)
}

func TestService_ApproveRelease_FullApproval_Success(t *testing.T) {
	db := setupTestDB(t)
	svc := dashboard.NewService(db)

	release, err := svc.CreateRelease(context.Background(), dashboard.CreateReleaseRequest{
		SourceBranch: "uat",
		DestBranch:   "master",
		CreatedBy:    "user123",
		ChannelID:    "channel456",
	})
	require.NoError(t, err)

	err = svc.ApproveRelease(context.Background(), release.ID, "dev", "devlead")
	require.NoError(t, err)

	err = svc.ApproveRelease(context.Background(), release.ID, "qa", "qalead")
	require.NoError(t, err)

	updated, err := svc.GetRelease(context.Background(), release.ID)
	require.NoError(t, err)
	require.Equal(t, "approved", updated.Status)
}

func TestService_ApproveRelease_InvalidType_Failure(t *testing.T) {
	db := setupTestDB(t)
	svc := dashboard.NewService(db)

	release, err := svc.CreateRelease(context.Background(), dashboard.CreateReleaseRequest{
		SourceBranch: "uat",
		DestBranch:   "master",
		CreatedBy:    "user123",
		ChannelID:    "channel456",
	})
	require.NoError(t, err)

	err = svc.ApproveRelease(context.Background(), release.ID, "invalid", "user123")

	require.Error(t, err)
	require.ErrorContains(t, err, "invalid approval type")
}

func TestService_RevokeApproval_Dev_Success(t *testing.T) {
	db := setupTestDB(t)
	svc := dashboard.NewService(db)

	release, err := svc.CreateRelease(context.Background(), dashboard.CreateReleaseRequest{
		SourceBranch: "uat",
		DestBranch:   "master",
		CreatedBy:    "user123",
		ChannelID:    "channel456",
	})
	require.NoError(t, err)

	err = svc.ApproveRelease(context.Background(), release.ID, "dev", "devlead")
	require.NoError(t, err)
	err = svc.ApproveRelease(context.Background(), release.ID, "qa", "qalead")
	require.NoError(t, err)

	err = svc.RevokeApproval(context.Background(), release.ID, "dev")

	require.NoError(t, err)

	updated, err := svc.GetRelease(context.Background(), release.ID)
	require.NoError(t, err)
	require.Equal(t, "", updated.DevApprovedBy)
	require.Zero(t, updated.DevApprovedAt)
	require.Equal(t, "pending", updated.Status)
}

func TestService_RevokeApproval_QA_Success(t *testing.T) {
	db := setupTestDB(t)
	svc := dashboard.NewService(db)

	release, err := svc.CreateRelease(context.Background(), dashboard.CreateReleaseRequest{
		SourceBranch: "uat",
		DestBranch:   "master",
		CreatedBy:    "user123",
		ChannelID:    "channel456",
	})
	require.NoError(t, err)

	err = svc.ApproveRelease(context.Background(), release.ID, "dev", "devlead")
	require.NoError(t, err)
	err = svc.ApproveRelease(context.Background(), release.ID, "qa", "qalead")
	require.NoError(t, err)

	err = svc.RevokeApproval(context.Background(), release.ID, "qa")

	require.NoError(t, err)

	updated, err := svc.GetRelease(context.Background(), release.ID)
	require.NoError(t, err)
	require.Equal(t, "", updated.QAApprovedBy)
	require.Zero(t, updated.QAApprovedAt)
	require.Equal(t, "pending", updated.Status)
}

func TestService_RevokeApproval_InvalidType_Failure(t *testing.T) {
	db := setupTestDB(t)
	svc := dashboard.NewService(db)

	release, err := svc.CreateRelease(context.Background(), dashboard.CreateReleaseRequest{
		SourceBranch: "uat",
		DestBranch:   "master",
		CreatedBy:    "user123",
		ChannelID:    "channel456",
	})
	require.NoError(t, err)

	err = svc.RevokeApproval(context.Background(), release.ID, "invalid")

	require.Error(t, err)
	require.ErrorContains(t, err, "invalid approval type")
}
