package mattermost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

//go:generate mockgen -destination=mocks/http_doer_mock.go -package=mocks github.com/user/mattermost-tools/pkg/mattermost HTTPDoer

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type Webhook struct {
	url        string
	httpClient HTTPDoer
}

func NewWebhook(url string) *Webhook {
	return &Webhook{
		url:        url,
		httpClient: &http.Client{},
	}
}

func NewWebhookWithHTTP(url string, httpClient HTTPDoer) *Webhook {
	return &Webhook{
		url:        url,
		httpClient: httpClient,
	}
}

type webhookPayload struct {
	Text string `json:"text"`
}

func (w *Webhook) Post(ctx context.Context, message string) error {
	payload := webhookPayload{Text: message}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook error: %d", resp.StatusCode)
	}

	return nil
}
