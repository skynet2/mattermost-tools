package mattermost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type Bot struct {
	baseURL    string
	token      string
	httpClient HTTPDoer
}

func NewBot(baseURL, token string) *Bot {
	return &Bot{
		baseURL:    baseURL,
		token:      token,
		httpClient: &http.Client{},
	}
}

type postPayload struct {
	ChannelID string `json:"channel_id"`
	RootID    string `json:"root_id,omitempty"`
	Message   string `json:"message"`
}

type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

func (b *Bot) PostMessage(ctx context.Context, channelID, message string) error {
	return b.PostMessageInThread(ctx, channelID, "", message)
}

func (b *Bot) PostMessageInThread(ctx context.Context, channelID, rootID, message string) error {
	payload := postPayload{
		ChannelID: channelID,
		RootID:    rootID,
		Message:   message,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling payload: %w", err)
	}

	url := fmt.Sprintf("%s/api/v4/posts", b.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.token)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("API error: %d", resp.StatusCode)
	}

	return nil
}

type postResponse struct {
	ID string `json:"id"`
}

func (b *Bot) PostMessageWithID(ctx context.Context, channelID, message string) (string, error) {
	payload := postPayload{
		ChannelID: channelID,
		Message:   message,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshaling payload: %w", err)
	}

	url := fmt.Sprintf("%s/api/v4/posts", b.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.token)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("API error: %d", resp.StatusCode)
	}

	var post postResponse
	if err := json.NewDecoder(resp.Body).Decode(&post); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}

	return post.ID, nil
}

type updatePostPayload struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

func (b *Bot) UpdatePost(ctx context.Context, postID, message string) error {
	payload := updatePostPayload{
		ID:      postID,
		Message: message,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling payload: %w", err)
	}

	url := fmt.Sprintf("%s/api/v4/posts/%s", b.baseURL, postID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.token)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("API error: %d", resp.StatusCode)
	}

	return nil
}

func (b *Bot) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	url := fmt.Sprintf("%s/api/v4/users/username/%s", b.baseURL, username)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+b.token)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error: %d", resp.StatusCode)
	}

	var user User
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &user, nil
}

func (b *Bot) GetMe(ctx context.Context) (*User, error) {
	url := fmt.Sprintf("%s/api/v4/users/me", b.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+b.token)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error: %d", resp.StatusCode)
	}

	var user User
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &user, nil
}
