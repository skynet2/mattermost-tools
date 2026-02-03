package argocd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

//go:generate mockgen -destination=mocks/http_doer_mock.go -package=mocks github.com/user/mattermost-tools/pkg/argocd HTTPDoer

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type Client struct {
	httpClient     HTTPDoer
	baseURL        string
	cfClientID     string
	cfClientSecret string
}

func NewClient(baseURL, cfClientID, cfClientSecret string) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		baseURL:        baseURL,
		cfClientID:     cfClientID,
		cfClientSecret: cfClientSecret,
	}
}

func NewClientWithHTTP(baseURL, cfClientID, cfClientSecret string, httpClient HTTPDoer) *Client {
	return &Client{
		httpClient:     httpClient,
		baseURL:        baseURL,
		cfClientID:     cfClientID,
		cfClientSecret: cfClientSecret,
	}
}

func (c *Client) GetApplication(ctx context.Context, appName string) (*AppStatus, error) {
	path := fmt.Sprintf("/api/v1/applications/%s", appName)
	resp, err := c.doRequest(ctx, path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ArgoCD API error: %d", resp.StatusCode)
	}

	var appResp applicationResponse
	if err := json.NewDecoder(resp.Body).Decode(&appResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return c.parseAppStatus(&appResp), nil
}

func (c *Client) doRequest(ctx context.Context, path string) (*http.Response, error) {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("CF-Access-Client-Id", c.cfClientID)
	req.Header.Set("CF-Access-Client-Secret", c.cfClientSecret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	return resp, nil
}

func (c *Client) parseAppStatus(appResp *applicationResponse) *AppStatus {
	return &AppStatus{
		Name:           appResp.Metadata.Name,
		SyncStatus:     appResp.Status.Sync.Status,
		HealthStatus:   appResp.Status.Health.Status,
		CurrentVersion: appResp.Spec.Source.TargetRevision,
	}
}
