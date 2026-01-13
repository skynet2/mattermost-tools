package mattermost

type PlaybookRun struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	OwnerUserID    string   `json:"owner_user_id"`
	TeamID         string   `json:"team_id"`
	ChannelID      string   `json:"channel_id"`
	PlaybookID     string   `json:"playbook_id"`
	CurrentStatus  string   `json:"current_status"`
	CreateAt       int64    `json:"create_at"`
	ParticipantIDs []string `json:"participant_ids"`
}

type CreatePlaybookRunRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	OwnerUserID string `json:"owner_user_id"`
	TeamID      string `json:"team_id"`
	PlaybookID  string `json:"playbook_id"`
}

type PlaybookRunResponse struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
}

type Checklist struct {
	Title string          `json:"title"`
	Items []ChecklistItem `json:"items"`
}

type ChecklistItem struct {
	Title string `json:"title"`
}

type Playbook struct {
	ID                      string      `json:"id"`
	Title                   string      `json:"title"`
	Description             string      `json:"description"`
	TeamID                  string      `json:"team_id"`
	CreatePublicPlaybookRun bool        `json:"create_public_playbook_run"`
	Public                  bool        `json:"public"`
	Checklists              []Checklist `json:"checklists"`
	MemberIDs               []string    `json:"member_ids"`
	InvitedUserIDs          []string    `json:"invited_user_ids"`
	InviteUsersEnabled      bool        `json:"invite_users_enabled"`
}

type CreatePlaybookRequest struct {
	Title                         string      `json:"title"`
	Description                   string      `json:"description"`
	TeamID                        string      `json:"team_id"`
	CreatePublicPlaybookRun       bool        `json:"create_public_playbook_run"`
	Public                        bool        `json:"public"`
	Checklists                    []Checklist `json:"checklists"`
	MemberIDs                     []string    `json:"member_ids"`
	InvitedUserIDs                []string    `json:"invited_user_ids"`
	InviteUsersEnabled            bool        `json:"invite_users_enabled"`
	ReminderTimerDefaultSeconds   int64       `json:"reminder_timer_default_seconds"`
}
