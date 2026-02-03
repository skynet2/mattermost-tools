package github_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/user/mattermost-tools/pkg/github"
	"github.com/user/mattermost-tools/pkg/github/mocks"
)

func TestNewClient_Success(t *testing.T) {
	client := github.NewClient("test-token")

	require.NotNil(t, client)
}

func TestNewClient_EmptyToken(t *testing.T) {
	client := github.NewClient("")

	require.NotNil(t, client)
}

func TestClient_ListRepositories_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	responseBody := `[
		{"name": "repo1", "full_name": "org/repo1", "archived": false, "html_url": "https://github.com/org/repo1"},
		{"name": "repo2", "full_name": "org/repo2", "archived": true, "html_url": "https://github.com/org/repo2"}
	]`

	mockHTTP := mocks.NewMockHTTPDoer(ctrl)
	mockHTTP.EXPECT().
		Do(gomock.Any()).
		DoAndReturn(func(req *http.Request) (*http.Response, error) {
			require.Equal(t, "https://api.github.com/orgs/testorg/repos?per_page=100&page=1", req.URL.String())
			require.Equal(t, "Bearer test-token", req.Header.Get("Authorization"))
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(responseBody)),
			}, nil
		})

	client := github.NewClientWithHTTP("test-token", mockHTTP)
	repos, err := client.ListRepositories(context.Background(), "testorg")

	require.NoError(t, err)
	require.Len(t, repos, 2)
	require.Equal(t, "repo1", repos[0].Name)
	require.False(t, repos[0].Archived)
	require.Equal(t, "repo2", repos[1].Name)
	require.True(t, repos[1].Archived)
}

func TestClient_ListRepositories_APIError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHTTP := mocks.NewMockHTTPDoer(ctrl)
	mockHTTP.EXPECT().
		Do(gomock.Any()).
		Return(&http.Response{
			StatusCode: 404,
			Body:       io.NopCloser(strings.NewReader(`{"message": "Not Found"}`)),
		}, nil)

	client := github.NewClientWithHTTP("test-token", mockHTTP)
	repos, err := client.ListRepositories(context.Background(), "nonexistent")

	require.Error(t, err)
	require.Contains(t, err.Error(), "404")
	require.Nil(t, repos)
}

func TestClient_ListPullRequests_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	responseBody := `[
		{
			"number": 123,
			"title": "feat: add feature",
			"html_url": "https://github.com/org/repo/pull/123",
			"state": "open",
			"draft": false,
			"created_at": "2025-01-01T10:00:00Z",
			"updated_at": "2025-01-10T10:00:00Z",
			"user": {"login": "author1"},
			"requested_reviewers": [{"login": "reviewer1"}, {"login": "reviewer2"}]
		},
		{
			"number": 124,
			"title": "fix: bug fix",
			"html_url": "https://github.com/org/repo/pull/124",
			"state": "open",
			"draft": true,
			"created_at": "2025-01-05T10:00:00Z",
			"updated_at": "2025-01-08T10:00:00Z",
			"user": {"login": "author2"},
			"requested_reviewers": []
		}
	]`

	mockHTTP := mocks.NewMockHTTPDoer(ctrl)
	mockHTTP.EXPECT().
		Do(gomock.Any()).
		DoAndReturn(func(req *http.Request) (*http.Response, error) {
			require.Equal(t, "https://api.github.com/repos/org/repo/pulls?state=open&per_page=100&page=1", req.URL.String())
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(responseBody)),
			}, nil
		})

	client := github.NewClientWithHTTP("test-token", mockHTTP)
	prs, err := client.ListPullRequests(context.Background(), "org", "repo")

	require.NoError(t, err)
	require.Len(t, prs, 2)
	require.Equal(t, 123, prs[0].Number)
	require.Equal(t, "feat: add feature", prs[0].Title)
	require.False(t, prs[0].Draft)
	require.Len(t, prs[0].RequestedReviewers, 2)
	require.True(t, prs[1].Draft)
}

func TestClient_FindPullRequest_Success(t *testing.T) {
	type tc struct {
		name   string
		owner  string
		repo   string
		head   string
		base   string
		prNum  int
		prURL  string
	}

	cases := []tc{
		{
			name:  "finds open PR",
			owner: "org",
			repo:  "repo",
			head:  "develop",
			base:  "main",
			prNum: 42,
			prURL: "https://github.com/org/repo/pull/42",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			responseBody := `[{"number": 42, "html_url": "https://github.com/org/repo/pull/42", "state": "open"}]`

			mockHTTP := mocks.NewMockHTTPDoer(ctrl)
			mockHTTP.EXPECT().
				Do(gomock.Any()).
				DoAndReturn(func(req *http.Request) (*http.Response, error) {
					require.Equal(t, "https://api.github.com/repos/org/repo/pulls?state=open&head=org:develop&base=main", req.URL.String())
					require.Equal(t, "Bearer test-token", req.Header.Get("Authorization"))
					return &http.Response{
						StatusCode: 200,
						Body:       io.NopCloser(strings.NewReader(responseBody)),
					}, nil
				})

			client := github.NewClientWithHTTP("test-token", mockHTTP)
			pr, err := client.FindPullRequest(context.Background(), c.owner, c.repo, c.head, c.base)

			require.NoError(t, err)
			require.NotNil(t, pr)
			require.Equal(t, c.prNum, pr.Number)
			require.Equal(t, c.prURL, pr.HTMLURL)
		})
	}
}

func TestClient_FindPullRequest_NoPRFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHTTP := mocks.NewMockHTTPDoer(ctrl)
	mockHTTP.EXPECT().
		Do(gomock.Any()).
		DoAndReturn(func(req *http.Request) (*http.Response, error) {
			require.Contains(t, req.URL.String(), "state=open")
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(`[]`)),
			}, nil
		})

	client := github.NewClientWithHTTP("test-token", mockHTTP)
	pr, err := client.FindPullRequest(context.Background(), "org", "repo", "feature", "main")

	require.NoError(t, err)
	require.Nil(t, pr)
}

func TestClient_FindPullRequest_UsesStateOpen(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHTTP := mocks.NewMockHTTPDoer(ctrl)
	mockHTTP.EXPECT().
		Do(gomock.Any()).
		DoAndReturn(func(req *http.Request) (*http.Response, error) {
			require.Contains(t, req.URL.String(), "state=open", "must use state=open to avoid returning merged PRs")
			require.NotContains(t, req.URL.String(), "state=all", "must not use state=all which returns old merged PRs")
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(`[]`)),
			}, nil
		})

	client := github.NewClientWithHTTP("test-token", mockHTTP)
	_, _ = client.FindPullRequest(context.Background(), "org", "repo", "develop", "main")
}

func TestClient_FindPullRequest_Failure(t *testing.T) {
	type tc struct {
		name       string
		statusCode int
		errContain string
	}

	cases := []tc{
		{name: "API error 404", statusCode: 404, errContain: "404"},
		{name: "API error 500", statusCode: 500, errContain: "500"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockHTTP := mocks.NewMockHTTPDoer(ctrl)
			mockHTTP.EXPECT().
				Do(gomock.Any()).
				Return(&http.Response{
					StatusCode: c.statusCode,
					Body:       io.NopCloser(strings.NewReader(`{}`)),
				}, nil)

			client := github.NewClientWithHTTP("test-token", mockHTTP)
			pr, err := client.FindPullRequest(context.Background(), "org", "repo", "develop", "main")

			require.Error(t, err)
			require.Contains(t, err.Error(), c.errContain)
			require.Nil(t, pr)
		})
	}
}
