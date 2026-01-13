package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	GitHubToken string      `yaml:"github_token"`
	Org         string      `yaml:"org"`
	IgnoreRepos []string    `yaml:"ignore_repos"`
	PRs         PRsConfig   `yaml:"prs"`
	Serve       ServeConfig `yaml:"serve"`
}

type PRsConfig struct {
	WebhookURL string `yaml:"webhook_url"`
}

type ServeConfig struct {
	Port               int                 `yaml:"port"`
	MattermostURL      string              `yaml:"mattermost_url"`
	MattermostToken    string              `yaml:"mattermost_token"`
	AllowedTokens      []string            `yaml:"allowed_tokens"`
	CommandPermissions map[string][]string `yaml:"command_permissions"`
	Release            ReleaseConfig       `yaml:"release"`
	Dashboard          DashboardConfig     `yaml:"dashboard"`
}

type ReleaseConfig struct {
	TeamID           string   `yaml:"team_id"`
	PlaybookID       string   `yaml:"playbook_id"`
	DefaultReviewers []string `yaml:"default_reviewers"`
	DefaultQA        []string `yaml:"default_qa"`
}

type DashboardConfig struct {
	Enabled    bool           `yaml:"enabled"`
	BaseURL    string         `yaml:"base_url"`
	SQLitePath string         `yaml:"sqlite_path"`
	Keycloak   KeycloakConfig `yaml:"keycloak"`
}

type KeycloakConfig struct {
	Issuer       string `yaml:"issuer"`
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	RedirectURL  string `yaml:"redirect_url"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
