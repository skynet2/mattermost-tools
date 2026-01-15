import axios from 'axios'
import type { Release, ReleaseWithRepos, UserInfo, UserProfile, HistoryEntry, CreateReleaseResponse, CIStatusResponse } from './types'

const api = axios.create({
  baseURL: '/api',
  withCredentials: true
})

api.interceptors.response.use(
  response => response,
  error => {
    if (error.response?.status === 401) {
      window.location.href = '/auth/login'
    }
    return Promise.reject(error)
  }
)

export const releaseApi = {
  list: async (status?: string): Promise<Release[]> => {
    const params = status ? { status } : {}
    const { data } = await api.get('/releases', { params })
    return data
  },

  create: async (sourceBranch: string, destBranch: string): Promise<CreateReleaseResponse> => {
    const { data } = await api.post('/releases', { source_branch: sourceBranch, dest_branch: destBranch })
    return data
  },

  get: async (id: string): Promise<ReleaseWithRepos> => {
    const { data } = await api.get(`/releases/${id}`)
    return data
  },

  update: async (id: string, updates: { notes?: string; breaking_changes?: string }) => {
    await api.patch(`/releases/${id}`, updates)
  },

  updateRepo: async (releaseId: string, repoId: number, updates: { excluded?: boolean; depends_on?: string[] }) => {
    await api.patch(`/releases/${releaseId}/repos/${repoId}`, updates)
  },

  approve: async (id: string, type: 'dev' | 'qa') => {
    await api.post(`/releases/${id}/approve/${type}`)
  },

  revokeApproval: async (id: string, type: 'dev' | 'qa') => {
    await api.delete(`/releases/${id}/approve/${type}`)
  },

  refresh: async (id: string) => {
    await api.post(`/releases/${id}/refresh`)
  },

  decline: async (id: string) => {
    await api.post(`/releases/${id}/decline`)
  },

  confirmRepo: async (releaseId: string, repoId: number) => {
    await api.post(`/releases/${releaseId}/repos/${repoId}/confirm`)
  },

  unconfirmRepo: async (releaseId: string, repoId: number) => {
    await api.delete(`/releases/${releaseId}/repos/${repoId}/confirm`)
  },

  poke: async (id: string) => {
    await api.post(`/releases/${id}/poke`)
  },

  getHistory: async (id: string): Promise<HistoryEntry[]> => {
    const { data } = await api.get(`/releases/${id}/history`)
    return data
  },

  getCIStatus: async (id: string): Promise<CIStatusResponse> => {
    const { data } = await api.get(`/releases/${id}/ci-status`)
    return data
  }
}

export const authApi = {
  me: async (): Promise<UserInfo | null> => {
    try {
      const { data } = await api.get('/me')
      return data
    } catch {
      return null
    }
  },

  login: () => {
    window.location.href = '/auth/login'
  },

  logout: () => {
    window.location.href = '/auth/logout'
  },

  getMyGitHub: async (): Promise<{ github_user: string | null }> => {
    const { data } = await api.get('/users/me/github')
    return data
  },

  setMyGitHub: async (githubUser: string): Promise<{ github_user: string }> => {
    const { data } = await api.put('/users/me/github', { github_user: githubUser })
    return data
  },

  getMyProfile: async (): Promise<UserProfile> => {
    const { data } = await api.get('/users/me/profile')
    return data
  },

  updateMyProfile: async (profile: { github_user: string; mattermost_user: string }): Promise<UserProfile> => {
    const { data } = await api.put('/users/me/profile', profile)
    return data
  }
}
