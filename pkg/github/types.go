package github

import "time"

type Repository struct {
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Archived bool   `json:"archived"`
	HTMLURL  string `json:"html_url"`
}

type User struct {
	Login string `json:"login"`
}

type Team struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type PullRequest struct {
	Number             int       `json:"number"`
	Title              string    `json:"title"`
	HTMLURL            string    `json:"html_url"`
	State              string    `json:"state"`
	Draft              bool      `json:"draft"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	User               User      `json:"user"`
	RequestedReviewers []User    `json:"requested_reviewers"`
	RequestedTeams     []Team    `json:"requested_teams"`
}

type IssueComment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	User      User      `json:"user"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CompareResult struct {
	Status       string       `json:"status"`
	AheadBy      int          `json:"ahead_by"`
	BehindBy     int          `json:"behind_by"`
	TotalCommits int          `json:"total_commits"`
	Commits      []Commit     `json:"commits"`
	Files        []FileChange `json:"files"`
}

type Commit struct {
	SHA    string     `json:"sha"`
	Commit CommitData `json:"commit"`
	Author User       `json:"author"`
}

type CommitData struct {
	Message string `json:"message"`
	Author  struct {
		Name  string    `json:"name"`
		Email string    `json:"email"`
		Date  time.Time `json:"date"`
	} `json:"author"`
}

type FileChange struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Changes   int    `json:"changes"`
}
