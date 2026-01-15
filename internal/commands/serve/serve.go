package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"gorm.io/gorm"

	"github.com/user/mattermost-tools/internal/config"
	"github.com/user/mattermost-tools/internal/dashboard"
	"github.com/user/mattermost-tools/internal/database"
	"github.com/user/mattermost-tools/internal/logger"
	"github.com/user/mattermost-tools/internal/mappings"
	"github.com/user/mattermost-tools/pkg/github"
	"github.com/user/mattermost-tools/pkg/mattermost"
	"github.com/user/mattermost-tools/pkg/release"
	"github.com/user/mattermost-tools/web"
)

var (
	configFile string
	port       int
	debug      bool
)

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start HTTP server for Mattermost slash commands",
		RunE:  runServe,
	}

	cmd.Flags().StringVarP(&configFile, "config", "c", "config.yaml", "Path to config file")
	cmd.Flags().IntVarP(&port, "port", "p", 8080, "Port to listen on")
	cmd.Flags().BoolVar(&debug, "debug", false, "Enable debug logging (full request/response)")

	return cmd
}

type SlashCommandRequest struct {
	Token       string `json:"token"`
	TeamID      string `json:"team_id"`
	ChannelID   string `json:"channel_id"`
	UserID      string `json:"user_id"`
	UserName    string `json:"user_name"`
	Command     string `json:"command"`
	Text        string `json:"text"`
	ResponseURL string `json:"response_url"`
}

type SlashCommandResponse struct {
	ResponseType string `json:"response_type,omitempty"`
	Text         string `json:"text"`
}

var prURLRegex = regexp.MustCompile(`github\.com/([^/]+)/([^/]+)/pull/(\d+)`)

func parsePRURL(url string) (owner, repo, number string, ok bool) {
	matches := prURLRegex.FindStringSubmatch(url)
	if len(matches) != 4 {
		return "", "", "", false
	}
	return matches[1], matches[2], matches[3], true
}

func runServe(cmd *cobra.Command, args []string) error {
	log := logger.Get()
	logger.SetDebug(debug)

	cfg, err := config.Load(configFile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("loading config: %w", err)
	}
	if cfg == nil {
		cfg = &config.Config{}
	}

	ghToken := os.Getenv("GITHUB_TOKEN")
	if ghToken == "" {
		ghToken = cfg.GitHubToken
	}
	if ghToken == "" {
		return fmt.Errorf("GITHUB_TOKEN environment variable is required")
	}

	var db *gorm.DB
	if cfg.Serve.Dashboard.Enabled {
		sqlitePath := cfg.Serve.Dashboard.SQLitePath
		if sqlitePath == "" {
			sqlitePath = "./releases.db"
		}
		var err error
		db, err = database.NewSQLiteDB(sqlitePath)
		if err != nil {
			return fmt.Errorf("initializing database: %w", err)
		}
		log.Info().Str("path", sqlitePath).Msg("Dashboard database initialized")
	}

	listenPort := port
	if !cmd.Flags().Changed("port") && cfg.Serve.Port > 0 {
		listenPort = cfg.Serve.Port
	}

	org := cfg.Org
	if org == "" {
		return fmt.Errorf("org is required in config")
	}

	ignoredRepos := make(map[string]struct{})
	for _, r := range cfg.IgnoreRepos {
		ignoredRepos[r] = struct{}{}
	}

	ghClient := github.NewClient(ghToken)

	var mmBot *mattermost.Bot
	if cfg.Serve.MattermostURL != "" && cfg.Serve.MattermostToken != "" {
		mmBot = mattermost.NewBot(cfg.Serve.MattermostURL, cfg.Serve.MattermostToken)
	}

	var dashboardServer *dashboard.Server
	if cfg.Serve.Dashboard.Enabled && db != nil {
		sessionSecret := []byte(cfg.Serve.MattermostToken)
		if len(sessionSecret) < 32 {
			sessionSecret = append(sessionSecret, make([]byte, 32-len(sessionSecret))...)
		}

		dashboardServer, err = dashboard.NewServer(context.Background(), dashboard.ServerConfig{
			DB: db,
			AuthConfig: dashboard.AuthConfig{
				Issuer:       cfg.Serve.Dashboard.Keycloak.Issuer,
				ClientID:     cfg.Serve.Dashboard.Keycloak.ClientID,
				ClientSecret: cfg.Serve.Dashboard.Keycloak.ClientSecret,
				RedirectURL:  cfg.Serve.Dashboard.Keycloak.RedirectURL,
			},
			SessionSecret: sessionSecret,
			GitHubClient:  ghClient,
			Org:           org,
			IgnoredRepos:  ignoredRepos,
			MattermostBot: mmBot,
			BaseURL:       cfg.Serve.Dashboard.BaseURL,
		})
		if err != nil {
			return fmt.Errorf("initializing dashboard server: %w", err)
		}
		log.Info().Str("url", cfg.Serve.Dashboard.BaseURL).Msg("Dashboard enabled")
	}

	var wsClient *mattermost.WebSocketClient
	var playbooksClient *mattermost.PlaybooksClient
	var releaseManager *release.Manager
	if cfg.Serve.MattermostURL != "" && cfg.Serve.MattermostToken != "" {
		playbooksClient = mattermost.NewPlaybooksClient(cfg.Serve.MattermostURL, cfg.Serve.MattermostToken)
		releaseManager = release.NewManager(ghClient, mmBot, playbooksClient, org, ignoredRepos)
		wsClient = mattermost.NewWebSocketClient(cfg.Serve.MattermostURL, cfg.Serve.MattermostToken)
		if debug {
			wsClient.SetDebugLog(func(format string, args ...interface{}) {
				debugLog("[WS] "+format, args...)
			})
		}
	}

	var ciTracker *dashboard.CITracker
	if dashboardServer != nil && mmBot != nil {
		baseURL := cfg.Serve.Dashboard.BaseURL
		dashboardServer.Service().SetFullApprovalCallback(func(rel *database.Release) {
			message := fmt.Sprintf("✅ **Release Ready to Deploy**\n`%s` → `%s`\nApproved by: Dev (%s), QA (%s)\n[View Details](%s/releases/%s)",
				rel.SourceBranch, rel.DestBranch,
				rel.DevApprovedBy, rel.QAApprovedBy,
				baseURL, rel.ID)
			mmBot.PostMessage(context.Background(), rel.ChannelID, message)
		})
	}

	if dashboardServer != nil && ghClient != nil {
		ciTracker = dashboard.NewCITracker(dashboardServer.Service(), ghClient, org, 30*time.Second)
		dashboardServer.SetCITracker(ciTracker)
		ciTracker.Start()
		log.Info().Msg("CI tracker started")
	}

	var argocdTracker *dashboard.ArgoCDTracker
	if dashboardServer != nil && len(cfg.Serve.Dashboard.ArgoCD.Environments) > 0 {
		argocdTracker = dashboard.NewArgoCDTracker(dashboardServer.Service(), &cfg.Serve.Dashboard.ArgoCD)
		dashboardServer.SetArgoCDTracker(argocdTracker)
		argocdTracker.Start()
		log.Info().Msg("ArgoCD tracker started")
	}

	allowedTokens := make(map[string]struct{})
	for _, t := range cfg.Serve.AllowedTokens {
		allowedTokens[t] = struct{}{}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/summarize-pr", withDebug("summarize-pr", withTokenAuth(allowedTokens, handleSummarizePR(ghClient))))
	mux.HandleFunc("/reviews", withDebug("reviews", withTokenAuth(allowedTokens, handleReviews(ghClient, org, ignoredRepos))))
	mux.HandleFunc("/changes", withDebug("changes", withTokenAuth(allowedTokens, handleChanges(ghClient, org, ignoredRepos, mmBot, releaseManager))))
	mux.HandleFunc("/bot-mention", withDebug("bot-mention", withTokenAuth(allowedTokens, handleBotMention(ghClient, org, ignoredRepos, mmBot, releaseManager))))
	mux.HandleFunc("/health", handleHealth)

	if dashboardServer != nil {
		mux.Handle("/api/", dashboardServer.Handler())
		mux.Handle("/auth/", dashboardServer.Handler())
		if err := dashboardServer.ServeStaticFiles(web.StaticFiles, "dist"); err != nil {
			return fmt.Errorf("serving static files: %w", err)
		}
		mux.Handle("/", dashboardServer.Handler())
	}

	server := &http.Server{
		Addr:         fmt.Sprintf("127.0.0.1:%d", listenPort),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 5 * time.Minute,
	}

	go func() {
		log.Info().
			Int("port", listenPort).
			Bool("debug", debug).
			Int("allowed_tokens", len(allowedTokens)).
			Bool("bot_configured", mmBot != nil).
			Msg("Starting server")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("Server error")
		}
	}()

	if wsClient != nil {
		go func() {
			for {
				ctx := context.Background()
				if err := wsClient.Connect(ctx); err != nil {
					log.Error().Err(err).Msg("WebSocket connection failed, retrying in 5 seconds")
					time.Sleep(5 * time.Second)
					wsClient = mattermost.NewWebSocketClient(cfg.Serve.MattermostURL, cfg.Serve.MattermostToken)
					if debug {
						wsClient.SetDebugLog(func(format string, args ...interface{}) {
							log.Debug().Msgf("[WS] "+format, args...)
						})
					}
					continue
				}
				log.Info().Msg("WebSocket connected - listening for bot mentions")

				wsClient.OnMessage(func(event *mattermost.WebSocketEvent) {
					if event.Event != "posted" {
						return
					}
					handleWebSocketMessage(wsClient, mmBot, ghClient, org, ignoredRepos, cfg.Serve.CommandPermissions, releaseManager, cfg.Serve.Release, dashboardServer, cfg.Serve.Dashboard.BaseURL, event)
				})

				if err := wsClient.Listen(ctx); err != nil {
					log.Error().Err(err).Msg("WebSocket error, reconnecting in 5 seconds")
					time.Sleep(5 * time.Second)
					wsClient = mattermost.NewWebSocketClient(cfg.Serve.MattermostURL, cfg.Serve.MattermostToken)
					if debug {
						wsClient.SetDebugLog(func(format string, args ...interface{}) {
							log.Debug().Msgf("[WS] "+format, args...)
						})
					}
					continue
				}
				break
			}
		}()
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("Shutting down server")
	if ciTracker != nil {
		ciTracker.Stop()
	}
	if argocdTracker != nil {
		argocdTracker.Stop()
	}
	if wsClient != nil {
		wsClient.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return server.Shutdown(ctx)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func handleWebSocketMessage(wsClient *mattermost.WebSocketClient, mmBot *mattermost.Bot, ghClient *github.Client, org string, ignoredRepos map[string]struct{}, permissions map[string][]string, releaseManager *release.Manager, releaseCfg config.ReleaseConfig, dashboardServer *dashboard.Server, dashboardBaseURL string, event *mattermost.WebSocketEvent) {
	post, err := wsClient.ParsePost(event)
	if err != nil {
		debugLog("[WS] Failed to parse post: %v", err)
		return
	}

	debugLog("[WS] Post from user %s (bot=%s): %q", post.UserID, wsClient.GetBotUserID(), post.Message)

	if post.UserID == wsClient.GetBotUserID() {
		debugLog("[WS] Ignoring own message")
		return
	}

	mentioned := wsClient.IsMentioned(post.Message)
	debugLog("[WS] IsMentioned=%v (looking for @%s)", mentioned, wsClient.GetBotUsername())

	if !mentioned {
		return
	}

	debugLog("[WS] Bot mentioned in channel %s by %s: %s", post.ChannelID, post.Username, post.Message)

	message := post.Message
	message = removeMention(message, wsClient.GetBotUsername())
	message = strings.TrimSpace(message)

	parts := strings.Fields(message)
	threadID := post.ThreadID()
	ctx := context.Background()

	if len(parts) == 0 {
		mmBot.PostMessageInThread(ctx, post.ChannelID, threadID, fmt.Sprintf("%s\n_Requested by @%s_", botHelpText, post.Username))
		return
	}

	command := strings.ToLower(parts[0])
	args := parts[1:]

	debugLog("[WS] Command: %q, Args: %v, ThreadID: %s", command, args, threadID)

	if !hasPermission(permissions, command, post.Username) {
		debugLog("[WS] Permission denied for user %s on command %s", post.Username, command)
		mmBot.PostMessageInThread(ctx, post.ChannelID, threadID, fmt.Sprintf("⛔ You don't have permission to use the `%s` command.\n\n_Requested by @%s_", command, post.Username))
		return
	}

	switch command {
	case "help", "-h", "--help", "h":
		mmBot.PostMessageInThread(ctx, post.ChannelID, threadID, fmt.Sprintf("%s\n_Requested by @%s_", botHelpText, post.Username))

	case "do-not-touch", "dnt":
		catURL := fmt.Sprintf("https://cataas.com/cat?t=%d", time.Now().UnixNano())
		mmBot.PostMessageInThread(ctx, post.ChannelID, threadID, fmt.Sprintf("🚨 **INCIDENT REPORTED**\n\n@%s touched the bot. This incident has been logged and will be reported to the appropriate authorities.\n\n![angry cat](%s)\n\n_Requested by @%s_", post.Username, catURL, post.Username))

	case "reviews":
		handleReviewsWS(ctx, mmBot, ghClient, org, ignoredRepos, post.ChannelID, threadID, post.Username, post.Username)

	case "summarize-pr", "summarize", "summary":
		if len(args) == 0 {
			mmBot.PostMessageInThread(ctx, post.ChannelID, threadID, fmt.Sprintf("Usage: `@pusheen summarize-pr <github-pr-url>`\n\n_Requested by @%s_", post.Username))
			return
		}
		handleSummarizePRWS(ctx, mmBot, ghClient, post.ChannelID, threadID, post.Username, args[0])

	case "changes":
		if len(args) != 2 {
			mmBot.PostMessageInThread(ctx, post.ChannelID, threadID, fmt.Sprintf("Usage: `@pusheen changes <source-branch> <dest-branch>`\nExample: `@pusheen changes uat master`\n\n_Requested by @%s_", post.Username))
			return
		}
		mmBot.PostMessageInThread(ctx, post.ChannelID, threadID, fmt.Sprintf("⏳ Analyzing changes from `%s` to `%s`... Results will be posted shortly.\n\n_Requested by @%s_", args[0], args[1], post.Username))
		go processChangesAsync(ghClient, org, ignoredRepos, mmBot, post.ChannelID, threadID, post.Username, args[0], args[1], releaseManager)

	case "release-prs", "releases", "pending-releases":
		if len(args) != 2 {
			mmBot.PostMessageInThread(ctx, post.ChannelID, threadID, fmt.Sprintf("Usage: `@pusheen release-prs <source-branch> <dest-branch>`\nExample: `@pusheen release-prs uat master`\n\n_Requested by @%s_", post.Username))
			return
		}
		mmBot.PostMessageInThread(ctx, post.ChannelID, threadID, fmt.Sprintf("⏳ Checking release PRs from `%s` to `%s`...\n\n_Requested by @%s_", args[0], args[1], post.Username))
		go processReleasePRsAsync(ghClient, org, ignoredRepos, mmBot, post.ChannelID, threadID, post.Username, args[0], args[1])

	case "create-release", "new-release":
		if len(args) != 2 {
			mmBot.PostMessageInThread(ctx, post.ChannelID, threadID, fmt.Sprintf("Usage: `@pusheen create-release <source-branch> <dest-branch>`\nExample: `@pusheen create-release uat master`\n\n_Requested by @%s_", post.Username))
			return
		}
		if dashboardServer == nil {
			mmBot.PostMessageInThread(ctx, post.ChannelID, threadID, fmt.Sprintf("Dashboard not configured.\n\n_Requested by @%s_", post.Username))
			return
		}
		mmBot.PostMessageInThread(ctx, post.ChannelID, threadID, fmt.Sprintf("Creating release from `%s` to `%s`...\n\n_Requested by @%s_", args[0], args[1], post.Username))
		go processCreateReleaseAsync(dashboardServer.Service(), ghClient, org, ignoredRepos, mmBot, dashboardBaseURL, post.ChannelID, threadID, post.Username, args[0], args[1])

	case "refresh":
		if releaseManager == nil {
			mmBot.PostMessageInThread(ctx, post.ChannelID, threadID, fmt.Sprintf("Release management not configured.\n\n_Requested by @%s_", post.Username))
			return
		}
		rel := releaseManager.GetReleaseByChannel(post.ChannelID)
		if rel == nil {
			mmBot.PostMessageInThread(ctx, post.ChannelID, threadID, fmt.Sprintf("No active release in this channel.\n\n_Requested by @%s_", post.Username))
			return
		}
		mmBot.PostMessageInThread(ctx, post.ChannelID, threadID, fmt.Sprintf("Refreshing release status...\n\n_Requested by @%s_", post.Username))
		go processRefreshReleaseAsync(releaseManager, mmBot, post.ChannelID, threadID, post.Username)

	default:
		mmBot.PostMessageInThread(ctx, post.ChannelID, threadID, fmt.Sprintf("Unknown command: `%s`\n\n%s\n_Requested by @%s_", command, botHelpText, post.Username))
	}
}

func handleReviewsWS(ctx context.Context, mmBot *mattermost.Bot, ghClient *github.Client, org string, ignoredRepos map[string]struct{}, channelID, threadID, mmUsername, requestedBy string) {
	ghUsername, ok := mappings.GitHubFromMattermost(mmUsername)
	if !ok {
		mmBot.PostMessageInThread(ctx, channelID, threadID, fmt.Sprintf("Your Mattermost username (%s) is not mapped to a GitHub account.\n\n_Requested by @%s_", mmUsername, requestedBy))
		return
	}

	repos, err := ghClient.ListRepositories(ctx, org)
	if err != nil {
		mmBot.PostMessageInThread(ctx, channelID, threadID, fmt.Sprintf("Failed to fetch repositories: %v\n\n_Requested by @%s_", err, requestedBy))
		return
	}

	var myPRs []reviewPR
	now := time.Now()

	for _, repo := range repos {
		if repo.Archived {
			continue
		}
		if _, ignored := ignoredRepos[repo.Name]; ignored {
			continue
		}

		prs, err := ghClient.ListPullRequests(ctx, org, repo.Name)
		if err != nil {
			continue
		}

		for _, pr := range prs {
			if pr.Draft {
				continue
			}

			for _, reviewer := range pr.RequestedReviewers {
				if reviewer.Login == ghUsername {
					myPRs = append(myPRs, reviewPR{
						Repo:      repo,
						PR:        pr,
						Staleness: now.Sub(pr.UpdatedAt),
					})
					break
				}
			}
		}
	}

	if len(myPRs) == 0 {
		mmBot.PostMessageInThread(ctx, channelID, threadID, fmt.Sprintf("🎉 No PRs waiting for your review!\n\n_Requested by @%s_", requestedBy))
		return
	}

	mmBot.PostMessageInThread(ctx, channelID, threadID, fmt.Sprintf("%s\n_Requested by @%s_", formatReviewsList(myPRs), requestedBy))
}

func handleSummarizePRWS(ctx context.Context, mmBot *mattermost.Bot, ghClient *github.Client, channelID, threadID, requestedBy, prURL string) {
	owner, repo, number, ok := parsePRURL(prURL)
	if !ok {
		mmBot.PostMessageInThread(ctx, channelID, threadID, fmt.Sprintf("Invalid PR URL. Expected format: https://github.com/owner/repo/pull/123\n\n_Requested by @%s_", requestedBy))
		return
	}

	comments, err := ghClient.GetPRComments(ctx, owner, repo, number)
	if err != nil {
		mmBot.PostMessageInThread(ctx, channelID, threadID, fmt.Sprintf("Failed to fetch PR comments: %v\n\n_Requested by @%s_", err, requestedBy))
		return
	}

	var latestSummary *github.IssueComment
	for i := len(comments) - 1; i >= 0; i-- {
		if comments[i].User.Login == "gemini-code-assist[bot]" {
			latestSummary = &comments[i]
			break
		}
	}

	if latestSummary == nil {
		mmBot.PostMessageInThread(ctx, channelID, threadID, fmt.Sprintf("No gemini-code-assist summary found for this PR.\n\n_Requested by @%s_", requestedBy))
		return
	}

	mmBot.PostMessageInThread(ctx, channelID, threadID, fmt.Sprintf("**PR Summary** ([%s/%s#%s](https://github.com/%s/%s/pull/%s))\n\n%s\n\n_Requested by @%s_", owner, repo, number, owner, repo, number, latestSummary.Body, requestedBy))
}

func withTokenAuth(allowedTokens map[string]struct{}, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(allowedTokens) == 0 {
			next(w, r)
			return
		}

		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		token := r.FormValue("token")
		if _, ok := allowedTokens[token]; !ok {
			debugLog("[AUTH] Unauthorized - token not in allowed list: %q", token)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

type debugResponseWriter struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (d *debugResponseWriter) WriteHeader(status int) {
	d.status = status
	d.ResponseWriter.WriteHeader(status)
}

func (d *debugResponseWriter) Write(b []byte) (int, error) {
	d.body.Write(b)
	return d.ResponseWriter.Write(b)
}

func withDebug(name string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !debug {
			next(w, r)
			return
		}

		debugLog("[%s] === REQUEST ===", name)
		debugLog("[%s] Method: %s", name, r.Method)
		debugLog("[%s] URL: %s", name, r.URL.String())
		debugLog("[%s] Headers:", name)
		for k, v := range r.Header {
			debugLog("[%s]   %s: %v", name, k, v)
		}

		if err := r.ParseForm(); err == nil {
			debugLog("[%s] Form values:", name)
			for k, v := range r.Form {
				if k == "token" {
					debugLog("[%s]   %s: [REDACTED] (len=%d)", name, k, len(v[0]))
				} else {
					debugLog("[%s]   %s: %v", name, k, v)
				}
			}
		}

		dw := &debugResponseWriter{ResponseWriter: w, status: 200}
		next(dw, r)

		debugLog("[%s] === RESPONSE ===", name)
		debugLog("[%s] Status: %d", name, dw.status)
		debugLog("[%s] Body: %s", name, dw.body.String())
		debugLog("[%s] ================", name)
	}
}

func debugLog(format string, args ...interface{}) {
	if debug {
		fmt.Fprintf(os.Stderr, "[DEBUG] "+format+"\n", args...)
	}
}

func removeMention(message, username string) string {
	lowerMsg := strings.ToLower(message)
	lowerMention := "@" + strings.ToLower(username)
	idx := strings.Index(lowerMsg, lowerMention)
	if idx == -1 {
		return message
	}
	return message[:idx] + message[idx+len(lowerMention):]
}

func hasPermission(permissions map[string][]string, command, username string) bool {
	if permissions == nil {
		return true
	}

	allowedUsers, exists := permissions[command]
	if !exists || len(allowedUsers) == 0 {
		return true
	}

	for _, u := range allowedUsers {
		if strings.EqualFold(u, username) {
			return true
		}
	}
	return false
}

func handleSummarizePR(ghClient *github.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := r.ParseForm(); err != nil {
			respondError(w, "Failed to parse request")
			return
		}

		text := r.FormValue("text")
		if text == "" {
			respondError(w, "Usage: /summarize-pr <github-pr-url>")
			return
		}

		owner, repo, number, ok := parsePRURL(text)
		if !ok {
			respondError(w, "Invalid PR URL. Expected format: https://github.com/owner/repo/pull/123")
			return
		}

		ctx := r.Context()
		comments, err := ghClient.GetPRComments(ctx, owner, repo, number)
		if err != nil {
			respondError(w, fmt.Sprintf("Failed to fetch PR comments: %v", err))
			return
		}

		var latestSummary *github.IssueComment
		for i := len(comments) - 1; i >= 0; i-- {
			if comments[i].User.Login == "gemini-code-assist[bot]" {
				latestSummary = &comments[i]
				break
			}
		}

		if latestSummary == nil {
			respondJSON(w, SlashCommandResponse{
				ResponseType: "ephemeral",
				Text:         "No gemini-code-assist summary found for this PR.",
			})
			return
		}

		respondJSON(w, SlashCommandResponse{
			ResponseType: "in_channel",
			Text:         fmt.Sprintf("**PR Summary** ([%s/%s#%s](https://github.com/%s/%s/pull/%s))\n\n%s", owner, repo, number, owner, repo, number, latestSummary.Body),
		})
	}
}

func handleReviews(ghClient *github.Client, org string, ignoredRepos map[string]struct{}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := r.ParseForm(); err != nil {
			respondError(w, "Failed to parse request")
			return
		}

		mmUsername := r.FormValue("user_name")
		ghUsername, ok := mappings.GitHubFromMattermost(mmUsername)
		if !ok {
			respondError(w, fmt.Sprintf("Your Mattermost username (%s) is not mapped to a GitHub account.", mmUsername))
			return
		}

		ctx := r.Context()

		repos, err := ghClient.ListRepositories(ctx, org)
		if err != nil {
			respondError(w, fmt.Sprintf("Failed to fetch repositories: %v", err))
			return
		}

		var myPRs []reviewPR
		now := time.Now()

		for _, repo := range repos {
			if repo.Archived {
				continue
			}
			if _, ignored := ignoredRepos[repo.Name]; ignored {
				continue
			}

			prs, err := ghClient.ListPullRequests(ctx, org, repo.Name)
			if err != nil {
				continue
			}

			for _, pr := range prs {
				if pr.Draft {
					continue
				}

				for _, reviewer := range pr.RequestedReviewers {
					if reviewer.Login == ghUsername {
						myPRs = append(myPRs, reviewPR{
							Repo:      repo,
							PR:        pr,
							Staleness: now.Sub(pr.UpdatedAt),
						})
						break
					}
				}
			}
		}

		if len(myPRs) == 0 {
			respondJSON(w, SlashCommandResponse{
				ResponseType: "ephemeral",
				Text:         "🎉 No PRs waiting for your review!",
			})
			return
		}

		respondJSON(w, SlashCommandResponse{
			ResponseType: "ephemeral",
			Text:         formatReviewsList(myPRs),
		})
	}
}

type reviewPR struct {
	Repo      github.Repository
	PR        github.PullRequest
	Staleness time.Duration
}

func formatReviewsList(prs []reviewPR) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("### 📋 PRs waiting for your review (%d)\n\n", len(prs)))

	for _, rp := range prs {
		emoji := stalenessEmoji(rp.Staleness)
		stale := formatDuration(rp.Staleness)
		sb.WriteString(fmt.Sprintf("%s [%s#%d](%s) %s\n", emoji, rp.Repo.Name, rp.PR.Number, rp.PR.HTMLURL, rp.PR.Title))
		sb.WriteString(fmt.Sprintf("   _by %s · %s stale_\n\n", rp.PR.User.Login, stale))
	}

	return sb.String()
}

func stalenessEmoji(d time.Duration) string {
	days := int(d.Hours() / 24)
	switch {
	case days < 1:
		return "🟢"
	case days < 3:
		return "🟡"
	case days < 7:
		return "🟠"
	default:
		return "🔴"
	}
}

func formatDuration(d time.Duration) string {
	days := int(d.Hours() / 24)
	if days >= 60 {
		return fmt.Sprintf("%d months", days/30)
	}
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}

const botHelpText = `**Available Commands:**

• **changes <source> <dest>** - Compare branches and summarize undeployed changes
  Example: ` + "`@pusheen changes uat master`" + `

• **release-prs <source> <dest>** - Check for release PRs between branches
  Example: ` + "`@pusheen release-prs uat master`" + `

• **create-release <source> <dest>** - Create a release playbook run
  Example: ` + "`@pusheen create-release uat master`" + `

• **refresh** - Refresh release status (in release channel)

• **reviews** - Show PRs waiting for your review
  Example: ` + "`@pusheen reviews`" + `

• **summarize-pr <url>** - Get AI summary from a GitHub PR
  Example: ` + "`@pusheen summarize-pr https://github.com/org/repo/pull/123`" + `

• **do-not-touch** - Do NOT use this command

• **help** - Show this help message
`

func handleBotMention(ghClient *github.Client, org string, ignoredRepos map[string]struct{}, mmBot *mattermost.Bot, releaseManager *release.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := r.ParseForm(); err != nil {
			debugLog("[bot-mention] Failed to parse form: %v", err)
			respondError(w, "Failed to parse request")
			return
		}

		text := strings.TrimSpace(r.FormValue("text"))
		triggerWord := r.FormValue("trigger_word")

		debugLog("[bot-mention] Raw text: %q", text)
		debugLog("[bot-mention] Trigger word: %q", triggerWord)

		text = strings.TrimPrefix(text, triggerWord)
		text = strings.TrimSpace(text)

		debugLog("[bot-mention] After removing trigger: %q", text)

		parts := strings.Fields(text)
		debugLog("[bot-mention] Parsed parts: %v (count=%d)", parts, len(parts))

		if len(parts) == 0 {
			debugLog("[bot-mention] No command found, showing help")
			respondJSON(w, SlashCommandResponse{Text: botHelpText})
			return
		}

		command := strings.ToLower(parts[0])
		args := parts[1:]

		debugLog("[bot-mention] Command: %q, Args: %v", command, args)

		switch command {
		case "help", "-h", "--help", "h":
			respondJSON(w, SlashCommandResponse{Text: botHelpText})

		case "reviews":
			handleReviewsFromMention(w, r, ghClient, org, ignoredRepos)

		case "summarize-pr", "summarize", "summary":
			if len(args) == 0 {
				respondError(w, "Usage: `@pusheen summarize-pr <github-pr-url>`")
				return
			}
			handleSummarizePRFromMention(w, r, ghClient, args[0])

		case "changes":
			if len(args) != 2 {
				respondError(w, "Usage: `@pusheen changes <source-branch> <dest-branch>`\nExample: `@pusheen changes uat master`")
				return
			}
			handleChangesFromMention(w, r, ghClient, org, ignoredRepos, mmBot, releaseManager, args[0], args[1])

		default:
			respondJSON(w, SlashCommandResponse{
				Text: fmt.Sprintf("Unknown command: `%s`\n\n%s", command, botHelpText),
			})
		}
	}
}

func handleReviewsFromMention(w http.ResponseWriter, r *http.Request, ghClient *github.Client, org string, ignoredRepos map[string]struct{}) {
	mmUsername := r.FormValue("user_name")
	ghUsername, ok := mappings.GitHubFromMattermost(mmUsername)
	if !ok {
		respondError(w, fmt.Sprintf("Your Mattermost username (%s) is not mapped to a GitHub account.", mmUsername))
		return
	}

	ctx := r.Context()

	repos, err := ghClient.ListRepositories(ctx, org)
	if err != nil {
		respondError(w, fmt.Sprintf("Failed to fetch repositories: %v", err))
		return
	}

	var myPRs []reviewPR
	now := time.Now()

	for _, repo := range repos {
		if repo.Archived {
			continue
		}
		if _, ignored := ignoredRepos[repo.Name]; ignored {
			continue
		}

		prs, err := ghClient.ListPullRequests(ctx, org, repo.Name)
		if err != nil {
			continue
		}

		for _, pr := range prs {
			if pr.Draft {
				continue
			}

			for _, reviewer := range pr.RequestedReviewers {
				if reviewer.Login == ghUsername {
					myPRs = append(myPRs, reviewPR{
						Repo:      repo,
						PR:        pr,
						Staleness: now.Sub(pr.UpdatedAt),
					})
					break
				}
			}
		}
	}

	if len(myPRs) == 0 {
		respondJSON(w, SlashCommandResponse{
			Text: "🎉 No PRs waiting for your review!",
		})
		return
	}

	respondJSON(w, SlashCommandResponse{
		Text: formatReviewsList(myPRs),
	})
}

func handleSummarizePRFromMention(w http.ResponseWriter, r *http.Request, ghClient *github.Client, prURL string) {
	owner, repo, number, ok := parsePRURL(prURL)
	if !ok {
		respondError(w, "Invalid PR URL. Expected format: https://github.com/owner/repo/pull/123")
		return
	}

	ctx := r.Context()
	comments, err := ghClient.GetPRComments(ctx, owner, repo, number)
	if err != nil {
		respondError(w, fmt.Sprintf("Failed to fetch PR comments: %v", err))
		return
	}

	var latestSummary *github.IssueComment
	for i := len(comments) - 1; i >= 0; i-- {
		if comments[i].User.Login == "gemini-code-assist[bot]" {
			latestSummary = &comments[i]
			break
		}
	}

	if latestSummary == nil {
		respondJSON(w, SlashCommandResponse{
			Text: "No gemini-code-assist summary found for this PR.",
		})
		return
	}

	respondJSON(w, SlashCommandResponse{
		ResponseType: "in_channel",
		Text:         fmt.Sprintf("**PR Summary** ([%s/%s#%s](https://github.com/%s/%s/pull/%s))\n\n%s", owner, repo, number, owner, repo, number, latestSummary.Body),
	})
}

func handleChangesFromMention(w http.ResponseWriter, r *http.Request, ghClient *github.Client, org string, ignoredRepos map[string]struct{}, mmBot *mattermost.Bot, releaseManager *release.Manager, sourceBranch, destBranch string) {
	if mmBot == nil {
		respondError(w, "Bot not configured. Set mattermost_url and mattermost_token in config.")
		return
	}

	channelID := r.FormValue("channel_id")
	userName := r.FormValue("user_name")

	respondJSON(w, SlashCommandResponse{
		Text: fmt.Sprintf("⏳ Analyzing changes from `%s` to `%s`... Results will be posted shortly.", sourceBranch, destBranch),
	})

	go processChangesAsync(ghClient, org, ignoredRepos, mmBot, channelID, "", userName, sourceBranch, destBranch, releaseManager)
}

func handleChanges(ghClient *github.Client, org string, ignoredRepos map[string]struct{}, mmBot *mattermost.Bot, releaseManager *release.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := r.ParseForm(); err != nil {
			respondError(w, "Failed to parse request")
			return
		}

		if mmBot == nil {
			respondError(w, "Bot not configured. Set mattermost_url and mattermost_token in config.")
			return
		}

		text := strings.TrimSpace(r.FormValue("text"))
		parts := strings.Fields(text)
		if len(parts) != 2 {
			respondError(w, "Usage: /changes <source-branch> <dest-branch>\nExample: /changes uat master")
			return
		}

		sourceBranch := parts[0]
		destBranch := parts[1]
		channelID := r.FormValue("channel_id")
		userName := r.FormValue("user_name")

		respondJSON(w, SlashCommandResponse{
			ResponseType: "ephemeral",
			Text:         fmt.Sprintf("⏳ Analyzing changes from `%s` to `%s`... Results will be posted shortly.", sourceBranch, destBranch),
		})

		go processChangesAsync(ghClient, org, ignoredRepos, mmBot, channelID, "", userName, sourceBranch, destBranch, releaseManager)
	}
}

func processChangesAsync(ghClient *github.Client, org string, ignoredRepos map[string]struct{}, mmBot *mattermost.Bot, channelID, threadID, userName, sourceBranch, destBranch string, releaseManager *release.Manager) {
	ctx := context.Background()

	repos, err := ghClient.ListRepositories(ctx, org)
	if err != nil {
		mmBot.PostMessageInThread(ctx, channelID, threadID, fmt.Sprintf("@%s ❌ Failed to fetch repositories: %v\n\n_Requested by @%s_", userName, err, userName))
		return
	}

	var filteredRepos []github.Repository
	for _, repo := range repos {
		if repo.Archived {
			continue
		}
		if _, ignored := ignoredRepos[repo.Name]; ignored {
			continue
		}
		filteredRepos = append(filteredRepos, repo)
	}

	type repoChange struct {
		Repo       github.Repository
		Compare    *github.CompareResult
		Summary    string
		IsBreaking bool
	}

	var (
		results []repoChange
		mu      sync.Mutex
		wg      sync.WaitGroup
		sem     = make(chan struct{}, 4)
	)

	for _, repo := range filteredRepos {
		wg.Add(1)
		go func(repo github.Repository) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			compare, err := ghClient.CompareBranches(ctx, org, repo.Name, destBranch, sourceBranch)
			if err != nil || compare == nil || compare.TotalCommits == 0 || len(compare.Files) == 0 {
				return
			}

			summary, isBreaking := generateChangeSummary(repo.Name, compare)

			mu.Lock()
			results = append(results, repoChange{
				Repo:       repo,
				Compare:    compare,
				Summary:    summary,
				IsBreaking: isBreaking,
			})
			mu.Unlock()
		}(repo)
	}

	wg.Wait()

	if len(results) == 0 {
		mmBot.PostMessageInThread(ctx, channelID, threadID, fmt.Sprintf("@%s ✅ No changes found between `%s` and `%s`\n\n_Requested by @%s_", userName, sourceBranch, destBranch, userName))
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("### 📦 Undeployed Changes: `%s` → `%s`\n\n", sourceBranch, destBranch))
	sb.WriteString(fmt.Sprintf("Found changes in **%d** repositories:\n\n", len(results)))

	for _, rc := range results {
		var emoji string
		if rc.IsBreaking {
			emoji = "🚨"
		} else if rc.Compare.TotalCommits > 10 {
			emoji = "📚"
		} else if rc.Compare.TotalCommits > 5 {
			emoji = "📝"
		} else {
			emoji = "📄"
		}

		sb.WriteString(fmt.Sprintf("**%s [%s](%s)** (%d commits)\n",
			emoji, rc.Repo.Name, rc.Repo.HTMLURL, rc.Compare.TotalCommits))
		sb.WriteString(fmt.Sprintf("%s\n\n", rc.Summary))
	}

	sb.WriteString(fmt.Sprintf("_Requested by @%s_", userName))
	mmBot.PostMessageInThread(ctx, channelID, threadID, sb.String())

	if releaseManager != nil {
		// Best-effort refresh: if this channel has an active release, update it with current data.
		// Errors are intentionally ignored since the changes command already succeeded.
		_, _ = releaseManager.RefreshRelease(ctx, channelID)
	}
}

func processReleasePRsAsync(ghClient *github.Client, org string, ignoredRepos map[string]struct{}, mmBot *mattermost.Bot, channelID, threadID, userName, sourceBranch, destBranch string) {
	ctx := context.Background()

	repos, err := ghClient.ListRepositories(ctx, org)
	if err != nil {
		mmBot.PostMessageInThread(ctx, channelID, threadID, fmt.Sprintf("@%s ❌ Failed to fetch repositories: %v\n\n_Requested by @%s_", userName, err, userName))
		return
	}

	var filteredRepos []github.Repository
	for _, repo := range repos {
		if repo.Archived {
			continue
		}
		if _, ignored := ignoredRepos[repo.Name]; ignored {
			continue
		}
		filteredRepos = append(filteredRepos, repo)
	}

	type repoStatus struct {
		Repo     github.Repository
		Commits  int
		PR       *github.PullRequest
		HasError bool
	}

	var (
		results []repoStatus
		mu      sync.Mutex
		wg      sync.WaitGroup
		sem     = make(chan struct{}, 4)
	)

	for _, repo := range filteredRepos {
		wg.Add(1)
		go func(repo github.Repository) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			compare, err := ghClient.CompareBranches(ctx, org, repo.Name, destBranch, sourceBranch)
			if err != nil || compare == nil || compare.TotalCommits == 0 || len(compare.Files) == 0 {
				return
			}

			pr, _ := ghClient.FindPullRequest(ctx, org, repo.Name, sourceBranch, destBranch)

			mu.Lock()
			results = append(results, repoStatus{
				Repo:    repo,
				Commits: compare.TotalCommits,
				PR:      pr,
			})
			mu.Unlock()
		}(repo)
	}

	wg.Wait()

	if len(results) == 0 {
		mmBot.PostMessageInThread(ctx, channelID, threadID, fmt.Sprintf("@%s ✅ No pending changes between `%s` and `%s`\n\n_Requested by @%s_", userName, sourceBranch, destBranch, userName))
		return
	}

	var withPR, withoutPR []repoStatus
	for _, r := range results {
		if r.PR != nil {
			withPR = append(withPR, r)
		} else {
			withoutPR = append(withoutPR, r)
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("### 🚀 Release PRs: `%s` → `%s`\n\n", sourceBranch, destBranch))

	if len(withoutPR) > 0 {
		sb.WriteString(fmt.Sprintf("**⚠️ Missing Release PRs (%d)**\n", len(withoutPR)))
		for _, r := range withoutPR {
			sb.WriteString(fmt.Sprintf("- [%s](%s) - %d commits, no PR\n", r.Repo.Name, r.Repo.HTMLURL, r.Commits))
		}
		sb.WriteString("\n")
	}

	if len(withPR) > 0 {
		sb.WriteString(fmt.Sprintf("**✅ Open Release PRs (%d)**\n", len(withPR)))
		for _, r := range withPR {
			sb.WriteString(fmt.Sprintf("- [%s#%d](%s) - %d commits\n", r.Repo.Name, r.PR.Number, r.PR.HTMLURL, r.Commits))
		}
	}

	sb.WriteString(fmt.Sprintf("\n_Requested by @%s_", userName))
	mmBot.PostMessageInThread(ctx, channelID, threadID, sb.String())
}

func processCreateReleaseAsync(dashboardSvc *dashboard.Service, ghClient *github.Client, org string, ignoredRepos map[string]struct{}, mmBot *mattermost.Bot, baseURL, channelID, threadID, userName, sourceBranch, destBranch string) {
	ctx := context.Background()
	log := logger.Get()

	log.Info().
		Str("user", userName).
		Str("source", sourceBranch).
		Str("dest", destBranch).
		Str("channel", channelID).
		Msg("Creating release")

	ownerUser, err := mmBot.GetUserByUsername(ctx, userName)
	if err != nil || ownerUser == nil {
		log.Error().Err(err).Str("user", userName).Msg("Failed to find user")
		mmBot.PostMessageInThread(ctx, channelID, threadID, "Failed to find user @"+userName+"\n\n_Requested by @"+userName+"_")
		return
	}

	rel, err := dashboardSvc.CreateRelease(ctx, dashboard.CreateReleaseRequest{
		SourceBranch: sourceBranch,
		DestBranch:   destBranch,
		CreatedBy:    ownerUser.ID,
		ChannelID:    channelID,
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to create release")
		mmBot.PostMessageInThread(ctx, channelID, threadID, "Failed to create release: "+err.Error()+"\n\n_Requested by @"+userName+"_")
		return
	}

	log.Info().Str("release_id", rel.ID).Msg("Release created, gathering repo data")

	repos, err := gatherRepoData(ctx, ghClient, org, ignoredRepos, sourceBranch, destBranch)
	if err != nil {
		log.Error().Err(err).Str("release_id", rel.ID).Msg("Failed to gather repos")
		mmBot.PostMessageInThread(ctx, channelID, threadID, "Failed to gather repos: "+err.Error()+"\n\n_Requested by @"+userName+"_")
		return
	}

	log.Info().Str("release_id", rel.ID).Int("repo_count", len(repos)).Msg("Repos gathered, saving to database")

	if err := dashboardSvc.AddRepos(ctx, rel.ID, repos); err != nil {
		log.Error().Err(err).Str("release_id", rel.ID).Msg("Failed to add repos")
		mmBot.PostMessageInThread(ctx, channelID, threadID, "Failed to add repos: "+err.Error()+"\n\n_Requested by @"+userName+"_")
		return
	}

	releaseURL := baseURL + "/releases/" + rel.ID
	message := "## Release: `" + sourceBranch + "` → `" + destBranch + "`\n**Repositories:** " +
		strconv.Itoa(len(repos)) + "\n[View Dashboard](" + releaseURL + ")\n\n_Requested by @" + userName + "_"

	log.Info().Str("release_id", rel.ID).Str("url", releaseURL).Msg("Posting release message")

	if err := mmBot.PostMessageInThread(ctx, channelID, threadID, message); err != nil {
		log.Error().Err(err).Msg("Failed to post message")
	}

	dashboardSvc.SetMattermostPostID(ctx, rel.ID, threadID)
	log.Info().Str("release_id", rel.ID).Msg("Release creation complete")
}

func gatherRepoData(ctx context.Context, ghClient *github.Client, org string, ignoredRepos map[string]struct{}, sourceBranch, destBranch string) ([]dashboard.RepoData, error) {
	repos, err := ghClient.ListRepositories(ctx, org)
	if err != nil {
		return nil, err
	}

	var results []dashboard.RepoData
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)

	for _, repo := range repos {
		if repo.Archived {
			continue
		}
		if _, ignored := ignoredRepos[repo.Name]; ignored {
			continue
		}

		wg.Add(1)
		go func(repo github.Repository) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			compare, err := ghClient.CompareBranches(ctx, org, repo.Name, destBranch, sourceBranch)
			if err != nil || compare == nil || compare.TotalCommits == 0 || len(compare.Files) == 0 {
				return
			}

			var contributors []string
			seen := make(map[string]struct{})
			for _, c := range compare.Commits {
				if c.Author.Login != "" {
					if _, ok := seen[c.Author.Login]; !ok {
						seen[c.Author.Login] = struct{}{}
						contributors = append(contributors, c.Author.Login)
					}
				}
			}

			var additions, deletions int
			for _, f := range compare.Files {
				additions += f.Additions
				deletions += f.Deletions
			}

			pr, _ := ghClient.FindPullRequest(ctx, org, repo.Name, sourceBranch, destBranch)

			summary, isBreaking := generateChangeSummary(repo.Name, compare)

			var mergeCommitSHA string
			if pr != nil && pr.Merged && pr.MergeCommitSHA != "" {
				mergeCommitSHA = pr.MergeCommitSHA
			}

			var headSHA string
			if len(compare.Commits) > 0 {
				headSHA = compare.Commits[len(compare.Commits)-1].SHA
			}

			data := dashboard.RepoData{
				RepoName:       repo.Name,
				CommitCount:    compare.TotalCommits,
				Additions:      additions,
				Deletions:      deletions,
				Contributors:   contributors,
				Summary:        summary,
				IsBreaking:     isBreaking,
				MergeCommitSHA: mergeCommitSHA,
				HeadSHA:        headSHA,
			}
			if pr != nil {
				data.PRNumber = pr.Number
				data.PRURL = pr.HTMLURL
			}

			mu.Lock()
			results = append(results, data)
			mu.Unlock()
		}(repo)
	}

	wg.Wait()
	return results, nil
}

func processRefreshReleaseAsync(releaseManager *release.Manager, mmBot *mattermost.Bot, channelID, threadID, userName string) {
	ctx := context.Background()

	_, err := releaseManager.RefreshRelease(ctx, channelID)
	if err != nil {
		mmBot.PostMessageInThread(ctx, channelID, threadID, fmt.Sprintf("Failed to refresh: %v\n\n_Requested by @%s_", err, userName))
		return
	}

	mmBot.PostMessageInThread(ctx, channelID, threadID, fmt.Sprintf("✅ Release summary updated.\n\n_Requested by @%s_", userName))
}

func generateChangeSummary(repoName string, compare *github.CompareResult) (string, bool) {
	log := logger.Get()
	log.Debug().Str("repo", repoName).Int("commits", compare.TotalCommits).Msg("Generating AI summary")

	var commitInfo strings.Builder
	commitInfo.WriteString(fmt.Sprintf("Repository: %s\n", repoName))
	commitInfo.WriteString(fmt.Sprintf("Total commits: %d\n\n", compare.TotalCommits))

	commitInfo.WriteString("Commits:\n")
	for _, c := range compare.Commits {
		msg := strings.Split(c.Commit.Message, "\n")[0]
		commitInfo.WriteString(fmt.Sprintf("- %s: %s\n", c.SHA[:7], msg))
	}

	commitInfo.WriteString("\nFiles changed:\n")
	for _, f := range compare.Files {
		commitInfo.WriteString(fmt.Sprintf("- %s (%s, +%d/-%d)\n", f.Filename, f.Status, f.Additions, f.Deletions))
	}

	prompt := fmt.Sprintf(`Analyze these git changes and provide a brief summary (2-3 sentences max).
Focus on: what features/fixes are included, any breaking changes or important notes.
If there are breaking changes, database migrations, API changes, or security updates, start your response with "BREAKING:" followed by the summary.
Otherwise just provide the summary directly.

%s`, commitInfo.String())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude", "-p", prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.Warn().Str("repo", repoName).Msg("AI summary timed out")
		} else {
			log.Warn().Str("repo", repoName).Err(err).Msg("AI summary failed")
		}
		return fmt.Sprintf("%d commits (AI summary unavailable)", compare.TotalCommits), false
	}

	summary := strings.TrimSpace(stdout.String())
	isBreaking := strings.HasPrefix(strings.ToUpper(summary), "BREAKING:")

	if isBreaking {
		summary = strings.TrimPrefix(summary, "BREAKING:")
		summary = strings.TrimPrefix(summary, "breaking:")
		summary = strings.TrimSpace(summary)
	}

	log.Debug().Str("repo", repoName).Bool("breaking", isBreaking).Msg("AI summary complete")
	return summary, isBreaking
}

func respondError(w http.ResponseWriter, msg string) {
	respondJSON(w, SlashCommandResponse{
		ResponseType: "ephemeral",
		Text:         msg,
	})
}

func respondJSON(w http.ResponseWriter, resp SlashCommandResponse) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
