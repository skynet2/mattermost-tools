package mattermost

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type WebSocketClient struct {
	baseURL     string
	token       string
	botUserID   string
	botUsername string
	conn        *websocket.Conn
	httpClient  HTTPDoer
	handlers    []MessageHandler
	mu          sync.Mutex
	writeMu     sync.Mutex
	done        chan struct{}
	debugLog    func(format string, args ...interface{})
	seq         int64
}

type MessageHandler func(event *WebSocketEvent)

type WebSocketEvent struct {
	Event     string                 `json:"event"`
	Data      map[string]interface{} `json:"data"`
	Broadcast struct {
		ChannelID string `json:"channel_id"`
		UserID    string `json:"user_id"`
	} `json:"broadcast"`
	Seq int `json:"seq"`
}

type PostedData struct {
	ChannelID   string `json:"channel_id"`
	UserID      string `json:"user_id"`
	Post        string `json:"post"`
	ChannelType string `json:"channel_type"`
}

type Post struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	RootID    string `json:"root_id"`
	UserID    string `json:"user_id"`
	Message   string `json:"message"`
	Username  string `json:"username,omitempty"`
}

func (p *Post) ThreadID() string {
	return p.RootID
}

func NewWebSocketClient(baseURL, token string) *WebSocketClient {
	return &WebSocketClient{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		token:      token,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		done:       make(chan struct{}),
		debugLog:   func(format string, args ...interface{}) {},
	}
}

func (c *WebSocketClient) SetDebugLog(fn func(format string, args ...interface{})) {
	c.debugLog = fn
}

func (c *WebSocketClient) OnMessage(handler MessageHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers = append(c.handlers, handler)
}

func (c *WebSocketClient) Connect(ctx context.Context) error {
	userID, username, err := c.getBotUserInfo(ctx)
	if err != nil {
		return fmt.Errorf("getting bot user info: %w", err)
	}
	c.botUserID = userID
	c.botUsername = username
	c.debugLog("Bot user ID: %s, username: %s", c.botUserID, c.botUsername)

	wsURL := strings.Replace(c.baseURL, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL = wsURL + "/api/v4/websocket"

	c.debugLog("Connecting to WebSocket: %s", wsURL)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.token)

	conn, resp, err := dialer.DialContext(ctx, wsURL, header)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("websocket dial failed with status %d: %w", resp.StatusCode, err)
		}
		return fmt.Errorf("websocket dial: %w", err)
	}
	c.conn = conn

	if err := c.authenticate(); err != nil {
		c.conn.Close()
		return fmt.Errorf("authentication: %w", err)
	}

	c.debugLog("WebSocket connected and authenticated")
	return nil
}

func (c *WebSocketClient) getBotUserInfo(ctx context.Context) (string, string, error) {
	url := c.baseURL + "/api/v4/users/me"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var user struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return "", "", err
	}

	c.debugLog("Bot username: %s, ID: %s", user.Username, user.ID)
	return user.ID, user.Username, nil
}

func (c *WebSocketClient) authenticate() error {
	seq := atomic.AddInt64(&c.seq, 1)
	authMsg := map[string]interface{}{
		"seq":    seq,
		"action": "authentication_challenge",
		"data": map[string]string{
			"token": c.token,
		},
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteJSON(authMsg)
}

func (c *WebSocketClient) Listen(ctx context.Context) error {
	go c.pingLoop(ctx)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.done:
			return nil
		default:
			c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			_, message, err := c.conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					return nil
				}
				return fmt.Errorf("read message: %w", err)
			}

			var event WebSocketEvent
			if err := json.Unmarshal(message, &event); err != nil {
				c.debugLog("Failed to unmarshal event: %v", err)
				continue
			}

			if event.Event == "" {
				continue
			}

			c.debugLog("Received event: %s", event.Event)

			c.mu.Lock()
			handlers := make([]MessageHandler, len(c.handlers))
			copy(handlers, c.handlers)
			c.mu.Unlock()

			for _, handler := range handlers {
				handler(&event)
			}
		}
	}
}

func (c *WebSocketClient) pingLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case <-ticker.C:
			seq := atomic.AddInt64(&c.seq, 1)
			pingMsg := map[string]interface{}{
				"seq":    seq,
				"action": "ping",
			}
			c.debugLog("Sending ping (seq=%d)", seq)
			c.writeMu.Lock()
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			err := c.conn.WriteJSON(pingMsg)
			c.writeMu.Unlock()
			if err != nil {
				c.debugLog("Ping failed: %v", err)
				return
			}
		}
	}
}

func (c *WebSocketClient) Close() error {
	close(c.done)
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *WebSocketClient) GetBotUserID() string {
	return c.botUserID
}

func (c *WebSocketClient) GetBotUsername() string {
	return c.botUsername
}

func (c *WebSocketClient) ParsePost(event *WebSocketEvent) (*Post, error) {
	postStr, ok := event.Data["post"].(string)
	if !ok {
		return nil, fmt.Errorf("no post data")
	}

	var post Post
	if err := json.Unmarshal([]byte(postStr), &post); err != nil {
		return nil, err
	}

	if senderName, ok := event.Data["sender_name"].(string); ok {
		post.Username = strings.TrimPrefix(senderName, "@")
	}

	return &post, nil
}

func (c *WebSocketClient) IsMentioned(message string) bool {
	lowerMsg := strings.ToLower(message)
	lowerUsername := strings.ToLower(c.botUsername)
	return strings.Contains(lowerMsg, "@"+lowerUsername)
}
