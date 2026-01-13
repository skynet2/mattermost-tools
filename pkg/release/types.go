package release

type RepoStatus struct {
	Name         string
	Commits      int
	Contributors []string
	PRURL        string
	PRNumber     int
	HasPR        bool
	DevApproved  bool
	QAApproved   bool
}

type Release struct {
	ID            string
	ChannelID     string
	SourceBranch  string
	DestBranch    string
	Repos         []RepoStatus
	CreatedBy     string
	CreatedAt     int64
	SummaryPostID string
}
