package dashboard_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/user/mattermost-tools/internal/dashboard"
	"github.com/user/mattermost-tools/internal/database"
)

func TestCalculateDeployOrder_NoDeps_Success(t *testing.T) {
	type tc struct {
		name  string
		repos []database.ReleaseRepo
		want  map[uint]int
	}

	cases := []tc{
		{
			name: "single repo without deps",
			repos: []database.ReleaseRepo{
				{ID: 1, RepoName: "auth-service"},
			},
			want: map[uint]int{1: 1},
		},
		{
			name: "multiple repos without deps",
			repos: []database.ReleaseRepo{
				{ID: 1, RepoName: "auth-service"},
				{ID: 2, RepoName: "api-gateway"},
			},
			want: map[uint]int{1: 1, 2: 1},
		},
		{
			name:  "empty repos list",
			repos: []database.ReleaseRepo{},
			want:  map[uint]int{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			order, err := dashboard.CalculateDeployOrder(c.repos)

			require.NoError(t, err)
			require.Equal(t, c.want, order)
		})
	}
}

func TestCalculateDeployOrder_WithDeps_Success(t *testing.T) {
	type tc struct {
		name  string
		repos []database.ReleaseRepo
		want  map[uint]int
	}

	cases := []tc{
		{
			name: "linear dependency chain",
			repos: []database.ReleaseRepo{
				{ID: 1, RepoName: "auth-service"},
				{ID: 2, RepoName: "api-gateway", DependsOn: `["auth-service"]`},
				{ID: 3, RepoName: "notification-svc", DependsOn: `["api-gateway"]`},
			},
			want: map[uint]int{1: 1, 2: 2, 3: 3},
		},
		{
			name: "diamond dependency",
			repos: []database.ReleaseRepo{
				{ID: 1, RepoName: "core"},
				{ID: 2, RepoName: "service-a", DependsOn: `["core"]`},
				{ID: 3, RepoName: "service-b", DependsOn: `["core"]`},
				{ID: 4, RepoName: "aggregator", DependsOn: `["service-a", "service-b"]`},
			},
			want: map[uint]int{1: 1, 2: 2, 3: 2, 4: 3},
		},
		{
			name: "dependency on non-existent repo is ignored",
			repos: []database.ReleaseRepo{
				{ID: 1, RepoName: "auth-service"},
				{ID: 2, RepoName: "api-gateway", DependsOn: `["unknown-service"]`},
			},
			want: map[uint]int{1: 1, 2: 1},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			order, err := dashboard.CalculateDeployOrder(c.repos)

			require.NoError(t, err)
			require.Equal(t, c.want, order)
		})
	}
}

func TestCalculateDeployOrder_CircularDeps_Failure(t *testing.T) {
	type tc struct {
		name  string
		repos []database.ReleaseRepo
	}

	cases := []tc{
		{
			name: "direct circular dependency",
			repos: []database.ReleaseRepo{
				{ID: 1, RepoName: "service-a", DependsOn: `["service-b"]`},
				{ID: 2, RepoName: "service-b", DependsOn: `["service-a"]`},
			},
		},
		{
			name: "self dependency",
			repos: []database.ReleaseRepo{
				{ID: 1, RepoName: "service-a", DependsOn: `["service-a"]`},
			},
		},
		{
			name: "transitive circular dependency",
			repos: []database.ReleaseRepo{
				{ID: 1, RepoName: "service-a", DependsOn: `["service-c"]`},
				{ID: 2, RepoName: "service-b", DependsOn: `["service-a"]`},
				{ID: 3, RepoName: "service-c", DependsOn: `["service-b"]`},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := dashboard.CalculateDeployOrder(c.repos)

			require.Error(t, err)
			require.Contains(t, err.Error(), "circular")
		})
	}
}

func TestCalculateDeployOrder_InvalidJSON_Failure(t *testing.T) {
	type tc struct {
		name  string
		repos []database.ReleaseRepo
	}

	cases := []tc{
		{
			name: "malformed JSON in DependsOn",
			repos: []database.ReleaseRepo{
				{ID: 1, RepoName: "service-a", DependsOn: `not-valid-json`},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := dashboard.CalculateDeployOrder(c.repos)

			require.Error(t, err)
			require.Contains(t, err.Error(), "unmarshaling")
		})
	}
}
