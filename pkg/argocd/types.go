package argocd

type AppStatus struct {
	Name           string
	SyncStatus     string
	HealthStatus   string
	CurrentVersion string
}

type applicationResponse struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Status struct {
		Sync struct {
			Status string `json:"status"`
		} `json:"sync"`
		Health struct {
			Status string `json:"status"`
		} `json:"health"`
		Summary struct {
			Images []string `json:"images"`
		} `json:"summary"`
		Resources []resourceStatus `json:"resources"`
	} `json:"status"`
	Spec struct {
		Source struct {
			Chart          string `json:"chart"`
			TargetRevision string `json:"targetRevision"`
		} `json:"source"`
	} `json:"spec"`
}

type resourceStatus struct {
	Kind            string `json:"kind"`
	Name            string `json:"name"`
	Health          *health `json:"health,omitempty"`
}

type health struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}
