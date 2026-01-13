package dashboard

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"gorm.io/gorm"

	"github.com/user/mattermost-tools/pkg/github"
	"github.com/user/mattermost-tools/pkg/mattermost"
)

type Server struct {
	db       *gorm.DB
	service  *Service
	auth     *Auth
	handlers *Handlers
	mux      *http.ServeMux
}

type ServerConfig struct {
	DB            *gorm.DB
	AuthConfig    AuthConfig
	SessionSecret []byte
	GitHubClient  *github.Client
	Org           string
	IgnoredRepos  map[string]struct{}
	MattermostBot *mattermost.Bot
	BaseURL       string
}

func NewServer(ctx context.Context, cfg ServerConfig) (*Server, error) {
	service := NewService(cfg.DB)

	var auth *Auth
	var err error
	if cfg.AuthConfig.Issuer != "" {
		auth, err = NewAuth(ctx, cfg.AuthConfig, cfg.SessionSecret)
		if err != nil {
			return nil, err
		}
	}

	handlers := NewHandlers(service, auth, cfg.GitHubClient, cfg.Org, cfg.IgnoredRepos, cfg.MattermostBot, cfg.BaseURL)

	s := &Server{
		db:       cfg.DB,
		service:  service,
		auth:     auth,
		handlers: handlers,
		mux:      http.NewServeMux(),
	}

	s.setupRoutes()
	return s, nil
}

func (s *Server) setupRoutes() {
	if s.auth != nil {
		s.mux.HandleFunc("/auth/login", s.auth.HandleLogin)
		s.mux.HandleFunc("/auth/callback", s.auth.HandleCallback)
		s.mux.HandleFunc("/auth/logout", s.auth.HandleLogout)
		s.mux.HandleFunc("/api/me", s.auth.HandleMe)
	}

	s.mux.HandleFunc("/api/releases", s.handleReleases)
	s.mux.HandleFunc("/api/releases/", s.handleRelease)
	s.mux.HandleFunc("/api/users/me/github", s.handleMyGitHub)
	s.mux.HandleFunc("/api/users/me/profile", s.handleMyProfile)
}

func (s *Server) handleMyGitHub(w http.ResponseWriter, r *http.Request) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.handlers.GetMyGitHub(w, r)
		case http.MethodPut:
			s.handlers.SetMyGitHub(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}

	if s.auth != nil {
		s.auth.RequireAuth(handler)(w, r)
	} else {
		handler(w, r)
	}
}

func (s *Server) handleMyProfile(w http.ResponseWriter, r *http.Request) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.handlers.GetMyProfile(w, r)
		case http.MethodPut:
			s.handlers.UpdateMyProfile(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}

	if s.auth != nil {
		s.auth.RequireAuth(handler)(w, r)
	} else {
		handler(w, r)
	}
}

func (s *Server) handleReleases(w http.ResponseWriter, r *http.Request) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.handlers.ListReleases(w, r)
		case http.MethodPost:
			s.handlers.CreateRelease(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}

	if s.auth != nil {
		s.auth.RequireAuth(handler)(w, r)
	} else {
		handler(w, r)
	}
}

func (s *Server) handleRelease(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/releases/")
	parts := strings.Split(path, "/")

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if len(parts) > 1 && parts[1] == "history" {
				s.handlers.GetHistory(w, r)
			} else {
				s.handlers.GetRelease(w, r)
			}
		case http.MethodPatch:
			if len(parts) > 1 && parts[1] == "repos" {
				s.handlers.UpdateRepo(w, r)
			} else {
				s.handlers.UpdateRelease(w, r)
			}
		case http.MethodPost:
			if len(parts) > 1 && parts[1] == "approve" {
				s.handlers.ApproveRelease(w, r)
			} else if len(parts) > 1 && parts[1] == "refresh" {
				s.handlers.RefreshRelease(w, r)
			} else if len(parts) > 1 && parts[1] == "decline" {
				s.handlers.DeclineRelease(w, r)
			} else if len(parts) > 1 && parts[1] == "poke" {
				s.handlers.PokeParticipants(w, r)
			} else if len(parts) > 3 && parts[1] == "repos" && parts[3] == "confirm" {
				s.handlers.ConfirmRepo(w, r)
			}
		case http.MethodDelete:
			if len(parts) > 1 && parts[1] == "approve" {
				s.handlers.RevokeApproval(w, r)
			} else if len(parts) > 3 && parts[1] == "repos" && parts[3] == "confirm" {
				s.handlers.UnconfirmRepo(w, r)
			}
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}

	if s.auth != nil {
		s.auth.RequireAuth(handler)(w, r)
	} else {
		handler(w, r)
	}
}

func (s *Server) Service() *Service {
	return s.service
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) ServeStaticFiles(staticFS embed.FS, subdir string) error {
	subFS, err := fs.Sub(staticFS, subdir)
	if err != nil {
		return err
	}

	fileServer := http.FileServer(http.FS(subFS))
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/") {
			return
		}
		if r.URL.Path != "/" && !strings.Contains(r.URL.Path, ".") {
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})

	return nil
}
