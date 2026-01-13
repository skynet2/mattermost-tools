package mattermost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type PlaybooksClient struct {
	baseURL    string
	token      string
	httpClient HTTPDoer
}

func NewPlaybooksClient(baseURL, token string) *PlaybooksClient {
	return &PlaybooksClient{
		baseURL:    baseURL,
		token:      token,
		httpClient: &http.Client{},
	}
}

func NewPlaybooksClientWithHTTP(baseURL, token string, httpClient HTTPDoer) *PlaybooksClient {
	return &PlaybooksClient{
		baseURL:    baseURL,
		token:      token,
		httpClient: httpClient,
	}
}

func (c *PlaybooksClient) CreatePlaybook(ctx context.Context, req CreatePlaybookRequest) (*Playbook, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling playbook request: %w", err)
	}

	url := fmt.Sprintf("%s/plugins/playbooks/api/v0/playbooks", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	httpReq.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("playbooks API error: %d - %s", resp.StatusCode, string(body))
	}

	var playbook Playbook
	if err := json.NewDecoder(resp.Body).Decode(&playbook); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &playbook, nil
}

func (c *PlaybooksClient) CreateRun(ctx context.Context, req CreatePlaybookRunRequest) (*PlaybookRunResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling run request: %w", err)
	}

	url := fmt.Sprintf("%s/plugins/playbooks/api/v0/runs", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("playbooks API error: %d - %s", resp.StatusCode, string(body))
	}

	var runResp PlaybookRunResponse
	if err := json.NewDecoder(resp.Body).Decode(&runResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &runResp, nil
}

type playbookRunsResponse struct {
	Items []PlaybookRun `json:"items"`
}

func (c *PlaybooksClient) GetRunByChannelID(ctx context.Context, channelID string) (*PlaybookRun, error) {
	url := fmt.Sprintf("%s/plugins/playbooks/api/v0/runs?channel_id=%s", c.baseURL, channelID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("playbooks API error: %d", resp.StatusCode)
	}

	var runsResp playbookRunsResponse
	if err := json.NewDecoder(resp.Body).Decode(&runsResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if len(runsResp.Items) == 0 {
		return nil, nil
	}

	return &runsResp.Items[0], nil
}

type addParticipantRequest struct {
	UserID string `json:"user_id"`
}

func (c *PlaybooksClient) AddParticipants(ctx context.Context, runID string, userIDs []string) error {
	for _, userID := range userIDs {
		if err := c.addParticipant(ctx, runID, userID); err != nil {
			return fmt.Errorf("adding participant %s: %w", userID, err)
		}
	}
	return nil
}

func (c *PlaybooksClient) addParticipant(ctx context.Context, runID, userID string) error {
	payload := addParticipantRequest{UserID: userID}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling participant request: %w", err)
	}

	url := fmt.Sprintf("%s/plugins/playbooks/api/v0/runs/%s/participants", c.baseURL, runID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("playbooks API error: %d", resp.StatusCode)
	}

	return nil
}
