export interface Release {
  ID: string
  SourceBranch: string
  DestBranch: string
  Status: string
  Notes: string
  BreakingChanges: string
  CreatedBy: string
  ChannelID: string
  DevApprovedBy: string
  DevApprovedAt: number
  QAApprovedBy: string
  QAApprovedAt: number
  DeclinedBy: string
  DeclinedAt: number
  LastRefreshedAt: number
  CreatedAt: number
}

export interface ReleaseRepo {
  ID: number
  ReleaseID: string
  RepoName: string
  CommitCount: number
  Additions: number
  Deletions: number
  Contributors: string
  PRNumber: number
  PRURL: string
  PRMerged: boolean
  Excluded: boolean
  DependsOn: string
  DeployOrder: number
  Summary: string
  IsBreaking: boolean
  ConfirmedBy: string
  ConfirmedAt: number
  InfraChanges: string
  MergeCommitSHA: string
}

export interface ReleaseWithRepos {
  release: Release
  repos: ReleaseRepo[]
  org: string
  ci_summary?: CISummary
}

export interface CreateReleaseResponse extends Release {
  syncing?: boolean
}

export interface UserInfo {
  sub: string
  preferred_username: string
  email: string
  name: string
}

export interface UserProfile {
  email: string
  github_user: string
  mattermost_user: string
}

export interface HistoryEntry {
  ID: number
  ReleaseID: string
  Action: string
  Actor: string
  Details: string
  CreatedAt: number
}

export interface CIStatus {
  repo_name: string
  repo_id: number
  status: string
  run_number: number
  run_url: string
  chart_name: string
  chart_version: string
  started_at: number
  completed_at: number
}

export interface CIStatusResponse {
  statuses: CIStatus[]
  any_in_progress: boolean
}

export interface CISummary {
  total: number
  success: number
  failed: number
  in_progress: number
  pending: number
}
