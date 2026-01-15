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
  Excluded: boolean
  DependsOn: string
  DeployOrder: number
  Summary: string
  IsBreaking: boolean
  ConfirmedBy: string
  ConfirmedAt: number
  InfraChanges: string
}

export interface ReleaseWithRepos {
  release: Release
  repos: ReleaseRepo[]
  org: string
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
