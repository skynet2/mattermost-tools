package dashboard

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gorilla/sessions"
	"golang.org/x/oauth2"
)

type AuthConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

type Auth struct {
	provider     *oidc.Provider
	oauth2Config *oauth2.Config
	verifier     *oidc.IDTokenVerifier
	store        *sessions.CookieStore
}

type UserInfo struct {
	ID       string `json:"sub"`
	Username string `json:"preferred_username"`
	Email    string `json:"email"`
	Name     string `json:"name"`
}

func NewAuth(ctx context.Context, cfg AuthConfig, sessionSecret []byte) (*Auth, error) {
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("creating oidc provider: %w", err)
	}

	oauth2Config := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})

	return &Auth{
		provider:     provider,
		oauth2Config: oauth2Config,
		verifier:     verifier,
		store:        sessions.NewCookieStore(sessionSecret),
	}, nil
}

func (a *Auth) HandleLogin(w http.ResponseWriter, r *http.Request) {
	state := generateState()
	session, _ := a.store.Get(r, "auth-session")
	session.Values["state"] = state
	session.Save(r, w)

	http.Redirect(w, r, a.oauth2Config.AuthCodeURL(state), http.StatusFound)
}

func (a *Auth) HandleCallback(w http.ResponseWriter, r *http.Request) {
	session, _ := a.store.Get(r, "auth-session")
	savedState, ok := session.Values["state"].(string)
	if !ok || savedState != r.URL.Query().Get("state") {
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	token, err := a.oauth2Config.Exchange(r.Context(), code)
	if err != nil {
		http.Error(w, "Failed to exchange token", http.StatusInternalServerError)
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "No id_token in response", http.StatusInternalServerError)
		return
	}

	idToken, err := a.verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		http.Error(w, "Failed to verify token", http.StatusInternalServerError)
		return
	}

	var claims UserInfo
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "Failed to parse claims", http.StatusInternalServerError)
		return
	}

	session.Values["user"] = claims
	session.Values["state"] = ""
	session.Save(r, w)

	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *Auth) HandleLogout(w http.ResponseWriter, r *http.Request) {
	session, _ := a.store.Get(r, "auth-session")
	session.Values["user"] = nil
	session.Options.MaxAge = -1
	session.Save(r, w)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *Auth) HandleMe(w http.ResponseWriter, r *http.Request) {
	session, _ := a.store.Get(r, "auth-session")
	user, ok := session.Values["user"].(UserInfo)
	if !ok {
		http.Error(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func (a *Auth) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := a.store.Get(r, "auth-session")
		if _, ok := session.Values["user"].(UserInfo); !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (a *Auth) GetUser(r *http.Request) *UserInfo {
	session, _ := a.store.Get(r, "auth-session")
	user, ok := session.Values["user"].(UserInfo)
	if !ok {
		return nil
	}
	return &user
}

func generateState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

func init() {
	gob.Register(UserInfo{})
}
