<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { releaseApi, authApi } from '@/api/client'
import type { Release, ReleaseRepo, HistoryEntry, CIStatus, DeploymentStatusResponse, EnvDeploymentStatus } from '@/api/types'

const route = useRoute()
const router = useRouter()
const release = ref<Release | null>(null)
const repos = ref<ReleaseRepo[]>([])
const org = ref('')
const loading = ref(true)
const refreshing = ref(false)
const poking = ref(false)
const editingNotes = ref(false)
const editingBreaking = ref(false)
const notesText = ref('')
const breakingText = ref('')
const expandedSummaries = ref<Set<number>>(new Set())

const showGitHubModal = ref(false)
const githubUsername = ref('')
const pendingConfirmRepoId = ref<number | null>(null)
const myGitHubUser = ref<string | null>(null)
const showConfirmationDetails = ref(false)

const history = ref<HistoryEntry[]>([])
const historyExpanded = ref(false)
const historyLoading = ref(false)
const expandedHistoryEntries = ref<Set<number>>(new Set())
const openDependencyDropdown = ref<number | null>(null)
const syncing = ref(false)
let syncInterval: ReturnType<typeof setInterval> | null = null

const ciStatuses = ref<Map<number, CIStatus>>(new Map())
const ciLoading = ref(false)
const anyInProgress = ref(false)
const refreshingChartVersion = ref<number | null>(null)
let ciRefreshTimeout: ReturnType<typeof setTimeout> | null = null

const deploymentStatuses = ref<Map<number, DeploymentStatusResponse>>(new Map())
const deploymentLoading = ref(false)
const anyPendingDeployment = ref(false)
let deploymentRefreshTimeout: ReturnType<typeof setTimeout> | null = null

const releaseId = computed(() => route.params.id as string)

const sortedRepos = computed(() => {
  return [...repos.value].sort((a, b) => a.DeployOrder - b.DeployOrder)
})

async function loadRelease() {
  loading.value = true
  try {
    const data = await releaseApi.get(releaseId.value)
    release.value = data.release
    repos.value = data.repos
    org.value = data.org
    notesText.value = data.release.Notes || ''
    breakingText.value = data.release.BreakingChanges || ''
  } finally {
    loading.value = false
  }
}

async function refresh() {
  refreshing.value = true
  try {
    await releaseApi.refresh(releaseId.value)
    await loadRelease()
  } finally {
    refreshing.value = false
  }
}

async function poke() {
  poking.value = true
  try {
    await releaseApi.poke(releaseId.value)
    alert('Poke sent to participants with pending actions!')
  } catch (error: any) {
    if (error.response?.status === 400) {
      alert(error.response?.data || 'No pending actions to poke about')
    } else if (error.response?.status === 503) {
      alert('Mattermost bot is not configured')
    } else {
      alert('Failed to send poke')
    }
  } finally {
    poking.value = false
  }
}

async function saveNotes() {
  await releaseApi.update(releaseId.value, { notes: notesText.value })
  if (release.value) release.value.Notes = notesText.value
  editingNotes.value = false
}

async function saveBreaking() {
  await releaseApi.update(releaseId.value, { breaking_changes: breakingText.value })
  if (release.value) release.value.BreakingChanges = breakingText.value
  editingBreaking.value = false
}

async function toggleExcluded(repo: ReleaseRepo) {
  await releaseApi.updateRepo(releaseId.value, repo.ID, { excluded: !repo.Excluded })
  repo.Excluded = !repo.Excluded
}

async function updateDependsOn(repo: ReleaseRepo, deps: string[]) {
  await releaseApi.updateRepo(releaseId.value, repo.ID, { depends_on: deps })
  repo.DependsOn = JSON.stringify(deps)
}

function getAvailableDependencies(repo: ReleaseRepo): string[] {
  const currentDeps = getDependsOn(repo)
  return getOtherRepoNames(repo).filter(name => !currentDeps.includes(name))
}

async function addDependency(repo: ReleaseRepo, depName: string) {
  const deps = [...getDependsOn(repo), depName]
  await updateDependsOn(repo, deps)
  openDependencyDropdown.value = null
}

async function removeDependency(repo: ReleaseRepo, depName: string) {
  if (!confirm(`Remove dependency on "${depName}"?`)) return
  const deps = getDependsOn(repo).filter(d => d !== depName)
  await updateDependsOn(repo, deps)
}

function toggleDependencyDropdown(repoId: number) {
  openDependencyDropdown.value = openDependencyDropdown.value === repoId ? null : repoId
}

async function approve(type: 'dev' | 'qa') {
  await releaseApi.approve(releaseId.value, type)
  await loadRelease()
}

async function revoke(type: 'dev' | 'qa') {
  await releaseApi.revokeApproval(releaseId.value, type)
  await loadRelease()
}

async function decline() {
  if (!confirm('Are you sure you want to decline this release? This will clear all approvals.')) {
    return
  }
  await releaseApi.decline(releaseId.value)
  await loadRelease()
}

function formatDate(timestamp: number) {
  if (!timestamp) return 'Not yet'
  return new Date(timestamp * 1000).toLocaleString()
}

function getDependsOn(repo: ReleaseRepo): string[] {
  if (!repo.DependsOn) return []
  try {
    const parsed = JSON.parse(repo.DependsOn)
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

function getOtherRepoNames(currentRepo: ReleaseRepo): string[] {
  return repos.value
    .filter(r => r.ID !== currentRepo.ID && !r.Excluded)
    .map(r => r.RepoName)
}

function getContributors(repo: ReleaseRepo): string[] {
  if (!repo.Contributors) return []
  try {
    const parsed = JSON.parse(repo.Contributors)
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

function getConfirmedBy(repo: ReleaseRepo): string[] {
  if (!repo.ConfirmedBy) return []
  try {
    const parsed = JSON.parse(repo.ConfirmedBy)
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

function isRepoConfirmed(repo: ReleaseRepo): boolean {
  const contributors = getContributors(repo)
  const confirmed = getConfirmedBy(repo)
  return confirmed.length > contributors.length / 2
}

function canConfirm(repo: ReleaseRepo): boolean {
  // Don't show if repo is fully confirmed
  if (isRepoConfirmed(repo)) return false

  // If user has GitHub linked, check they're a contributor and haven't confirmed yet
  if (myGitHubUser.value) {
    const contributors = getContributors(repo)
    const confirmed = getConfirmedBy(repo)
    // Must be a contributor and not already confirmed
    return contributors.includes(myGitHubUser.value) && !confirmed.includes(myGitHubUser.value)
  }

  // No GitHub linked yet - show button so they can link and try
  return true
}

function hasConfirmed(repo: ReleaseRepo): boolean {
  if (!myGitHubUser.value) return false
  const confirmed = getConfirmedBy(repo)
  return confirmed.includes(myGitHubUser.value)
}

function getConfirmationDisplay(repo: ReleaseRepo): { progress: string; icons: string } {
  const contributors = getContributors(repo)
  const confirmed = getConfirmedBy(repo)
  const total = contributors.length
  const count = confirmed.length
  const icons = '✓'.repeat(count) + '○'.repeat(Math.max(0, total - count))
  return { progress: `${count}/${total}`, icons }
}

const allReposConfirmed = computed(() => {
  const nonExcluded = repos.value.filter(r => !r.Excluded)
  return nonExcluded.every(r => isRepoConfirmed(r))
})

const confirmedReposCount = computed(() => {
  return repos.value.filter(r => !r.Excluded && isRepoConfirmed(r)).length
})

const totalNonExcludedRepos = computed(() => {
  return repos.value.filter(r => !r.Excluded).length
})

function getInfraChanges(repo: ReleaseRepo): string[] {
  if (!repo.InfraChanges) return []
  try {
    const parsed = JSON.parse(repo.InfraChanges)
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

const reposWithInfraChanges = computed(() => {
  return repos.value.filter(r => !r.Excluded && getInfraChanges(r).length > 0)
})

const allInfraTypes = computed(() => {
  const types = new Set<string>()
  for (const repo of reposWithInfraChanges.value) {
    for (const t of getInfraChanges(repo)) {
      types.add(t)
    }
  }
  return Array.from(types)
})

function getInfraColor(type: string): string {
  const colors: Record<string, string> = {
    terraform: 'bg-purple-100 text-purple-800 border-purple-300',
    helm: 'bg-blue-100 text-blue-800 border-blue-300',
    docker: 'bg-cyan-100 text-cyan-800 border-cyan-300',
    'ci/cd': 'bg-orange-100 text-orange-800 border-orange-300',
    kubernetes: 'bg-indigo-100 text-indigo-800 border-indigo-300',
  }
  return colors[type] || 'bg-gray-100 text-gray-800 border-gray-300'
}

function formatContributors(repo: ReleaseRepo): string {
  const contributors = getContributors(repo)
  if (contributors.length === 0) return ''
  if (contributors.length <= 2) return contributors.join(', ')
  return `${contributors.slice(0, 2).join(', ')} +${contributors.length - 2}`
}

function getCompareUrl(repo: ReleaseRepo): string {
  if (!release.value || !org.value) return ''
  return `https://github.com/${org.value}/${repo.RepoName}/compare/${release.value.DestBranch}...${release.value.SourceBranch}`
}

function toggleSummary(repoId: number) {
  if (expandedSummaries.value.has(repoId)) {
    expandedSummaries.value.delete(repoId)
  } else {
    expandedSummaries.value.add(repoId)
  }
  expandedSummaries.value = new Set(expandedSummaries.value)
}

async function loadMyGitHub() {
  try {
    const result = await authApi.getMyGitHub()
    myGitHubUser.value = result.github_user
  } catch {
    myGitHubUser.value = null
  }
}

async function loadHistory() {
  historyLoading.value = true
  try {
    history.value = await releaseApi.getHistory(releaseId.value)
  } finally {
    historyLoading.value = false
  }
}

async function toggleHistory() {
  historyExpanded.value = !historyExpanded.value
  if (historyExpanded.value && history.value.length === 0) {
    await loadHistory()
  }
}

function formatHistoryAction(entry: HistoryEntry): string {
  const details = entry.Details ? JSON.parse(entry.Details) : {}
  const repoName = details.repo_name || details.repo || 'unknown'
  switch (entry.Action) {
    case 'notes_updated':
      return 'Updated release notes'
    case 'breaking_changes_updated':
      return 'Updated breaking changes'
    case 'repo_excluded':
      return `Excluded repository: ${repoName}`
    case 'repo_included':
      return `Included repository: ${repoName}`
    case 'repo_dependencies_updated':
      return `Updated dependencies for: ${repoName}`
    case 'approval_added':
      return `Added ${details.type || ''} approval`
    case 'approval_revoked':
      return `Revoked ${details.type || ''} approval`
    case 'release_refreshed':
      return 'Refreshed from GitHub'
    case 'release_declined':
      return 'Declined release'
    case 'repo_confirmed':
      return `Confirmed repository: ${repoName}`
    case 'repo_unconfirmed':
      return `Revoked confirmation for: ${repoName}`
    case 'participants_poked':
      return 'Sent reminder to participants'
    case 'release_created':
      return 'Created release'
    case 'repos_synced':
      return `Synced ${details.count || ''} repositories`
    default:
      return entry.Action.replace(/_/g, ' ')
  }
}

function getHistoryIcon(action: string): string {
  switch (action) {
    case 'notes_updated':
    case 'breaking_changes_updated':
      return '📝'
    case 'repo_excluded':
      return '➖'
    case 'repo_included':
      return '➕'
    case 'repo_dependencies_updated':
      return '🔗'
    case 'approval_added':
      return '✅'
    case 'approval_revoked':
      return '❌'
    case 'release_refreshed':
      return '🔄'
    case 'release_declined':
      return '🚫'
    case 'repo_confirmed':
      return '✓'
    case 'repo_unconfirmed':
      return '↩'
    case 'participants_poked':
      return '🔔'
    case 'release_created':
      return '🚀'
    case 'repos_synced':
      return '📦'
    default:
      return '•'
  }
}

function hasHistoryDetails(entry: HistoryEntry): boolean {
  if (!entry.Details) return false
  const details = JSON.parse(entry.Details)
  if (entry.Action === 'repo_dependencies_updated') {
    return (details.added && details.added.length > 0) || (details.removed && details.removed.length > 0)
  }
  return details.old !== undefined || details.new !== undefined
}

function getHistoryDetails(entry: HistoryEntry): { old: string; new: string } | null {
  if (!entry.Details) return null
  const details = JSON.parse(entry.Details)
  if (details.old === undefined && details.new === undefined) return null
  return { old: details.old || '', new: details.new || '' }
}

function getDependencyChanges(entry: HistoryEntry): { added: string[]; removed: string[] } | null {
  if (!entry.Details) return null
  const details = JSON.parse(entry.Details)
  if (!details.added && !details.removed) return null
  return { added: details.added || [], removed: details.removed || [] }
}

function toggleHistoryEntry(entryId: number) {
  if (expandedHistoryEntries.value.has(entryId)) {
    expandedHistoryEntries.value.delete(entryId)
  } else {
    expandedHistoryEntries.value.add(entryId)
  }
  expandedHistoryEntries.value = new Set(expandedHistoryEntries.value)
}

async function loadCIStatus() {
  ciLoading.value = true
  try {
    const response = await releaseApi.getCIStatus(releaseId.value)
    const statusMap = new Map<number, CIStatus>()
    for (const status of response.statuses) {
      statusMap.set(status.repo_id, status)
    }
    ciStatuses.value = statusMap
    anyInProgress.value = response.any_in_progress

    if (response.any_in_progress) {
      ciRefreshTimeout = setTimeout(loadCIStatus, 10000)
    }
  } catch {
    ciStatuses.value = new Map()
    anyInProgress.value = false
  } finally {
    ciLoading.value = false
  }
}

async function refreshChartVersion(repoId: number) {
  refreshingChartVersion.value = repoId
  try {
    const result = await releaseApi.refreshChartVersion(releaseId.value, repoId)
    const status = ciStatuses.value.get(repoId)
    if (status) {
      status.chart_name = result.chart_name
      status.chart_version = result.chart_version
      ciStatuses.value = new Map(ciStatuses.value)
    }
  } catch (error: any) {
    alert(error.response?.data || 'Failed to refresh chart info')
  } finally {
    refreshingChartVersion.value = null
  }
}

function getCIStatusIcon(status: string): string {
  switch (status) {
    case 'success':
      return '&#10003;'
    case 'failure':
      return '&#10007;'
    case 'in_progress':
      return '&#9203;'
    case 'pending':
      return '&#9711;'
    default:
      return '&#8226;'
  }
}

function getCIStatusClass(status: string): string {
  switch (status) {
    case 'success':
      return 'text-green-600 bg-green-100'
    case 'failure':
      return 'text-red-600 bg-red-100'
    case 'in_progress':
      return 'text-yellow-600 bg-yellow-100'
    case 'pending':
      return 'text-gray-600 bg-gray-100'
    default:
      return 'text-gray-600 bg-gray-100'
  }
}

function getCIStatusText(status: string): string {
  switch (status) {
    case 'success':
      return 'Success'
    case 'failure':
      return 'Failed'
    case 'in_progress':
      return 'In Progress'
    case 'pending':
      return 'Pending'
    default:
      return status
  }
}

function formatCITime(timestamp: number): string {
  if (!timestamp) return '-'
  const date = new Date(timestamp * 1000)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffMins = Math.floor(diffMs / 60000)
  const diffHours = Math.floor(diffMs / 3600000)

  if (diffMins < 1) return 'just now'
  if (diffMins < 60) return `${diffMins}m ago`
  if (diffHours < 24) return `${diffHours}h ago`
  return date.toLocaleDateString()
}

async function loadDeploymentStatus() {
  deploymentLoading.value = true
  try {
    const response = await releaseApi.getDeploymentStatus(releaseId.value)
    const statusMap = new Map<number, DeploymentStatusResponse>()
    for (const status of response.statuses) {
      statusMap.set(status.repo_id, status)
    }
    deploymentStatuses.value = statusMap
    anyPendingDeployment.value = response.any_pending

    if (response.any_pending) {
      deploymentRefreshTimeout = setTimeout(loadDeploymentStatus, 15000)
    }
  } catch {
    deploymentStatuses.value = new Map()
    anyPendingDeployment.value = false
  } finally {
    deploymentLoading.value = false
  }
}

function getDeploymentStatusIcon(env: EnvDeploymentStatus | undefined): string {
  if (!env) return '&#8226;'
  if (env.sync_status === 'Synced' && env.health_status === 'Healthy') {
    return '&#10003;'
  }
  if (env.health_status === 'Progressing') {
    return '&#9203;'
  }
  if (env.sync_status === 'OutOfSync') {
    return '&#9888;'
  }
  if (env.health_status === 'Degraded') {
    return '&#10007;'
  }
  if (env.health_status === 'Missing') {
    return '&#8987;'
  }
  return '&#8226;'
}

function getDeploymentStatusClass(env: EnvDeploymentStatus | undefined): string {
  if (!env) return 'text-gray-400 bg-gray-50'
  if (env.sync_status === 'Synced' && env.health_status === 'Healthy') {
    return 'text-green-600 bg-green-100'
  }
  if (env.health_status === 'Progressing') {
    return 'text-yellow-600 bg-yellow-100'
  }
  if (env.sync_status === 'OutOfSync') {
    return 'text-orange-600 bg-orange-100'
  }
  if (env.health_status === 'Degraded') {
    return 'text-red-600 bg-red-100'
  }
  if (env.health_status === 'Missing') {
    return 'text-gray-600 bg-gray-100'
  }
  return 'text-gray-400 bg-gray-50'
}

function getDeploymentStatusTooltip(env: EnvDeploymentStatus | undefined): string {
  if (!env) return 'Not configured'
  const parts = []
  parts.push(`Sync: ${env.sync_status}`)
  parts.push(`Health: ${env.health_status}`)
  if (env.current_version) {
    parts.push(`Current: ${env.current_version}`)
  }
  if (env.expected_version && env.current_version !== env.expected_version) {
    parts.push(`Expected: ${env.expected_version}`)
  }
  return parts.join(' | ')
}

function getDeploymentEnv(repoId: number, envName: string): EnvDeploymentStatus | undefined {
  const status = deploymentStatuses.value.get(repoId)
  return status?.environments?.[envName]
}

function parseRolloutStatus(env: EnvDeploymentStatus | undefined): { replicas: number; ready: number; updated: number } | null {
  if (!env?.rollout_status) return null
  try {
    return JSON.parse(env.rollout_status)
  } catch {
    return null
  }
}

async function handleConfirmClick(repo: ReleaseRepo) {
  if (!myGitHubUser.value) {
    pendingConfirmRepoId.value = repo.ID
    showGitHubModal.value = true
    return
  }
  await confirmRepo(repo.ID)
}

async function confirmRepo(repoId: number) {
  try {
    await releaseApi.confirmRepo(releaseId.value, repoId)
    const repo = repos.value.find(r => r.ID === repoId)
    if (repo && myGitHubUser.value) {
      const confirmed = getConfirmedBy(repo)
      if (!confirmed.includes(myGitHubUser.value)) {
        confirmed.push(myGitHubUser.value)
        repo.ConfirmedBy = JSON.stringify(confirmed)
      }
    }
  } catch (error: any) {
    if (error.response?.status === 403) {
      alert('You are not a contributor to this repository')
    } else if (error.response?.status === 400) {
      alert(error.response?.data || 'Cannot confirm this repository')
    } else {
      alert('Failed to confirm repository')
    }
  }
}

async function unconfirmRepo(repoId: number) {
  try {
    await releaseApi.unconfirmRepo(releaseId.value, repoId)
    const repo = repos.value.find(r => r.ID === repoId)
    if (repo && myGitHubUser.value) {
      const confirmed = getConfirmedBy(repo)
      const idx = confirmed.indexOf(myGitHubUser.value)
      if (idx !== -1) {
        confirmed.splice(idx, 1)
        repo.ConfirmedBy = JSON.stringify(confirmed)
      }
    }
  } catch (error: any) {
    if (error.response?.status === 400) {
      alert(error.response?.data || 'Cannot revoke confirmation')
    } else {
      alert('Failed to revoke confirmation')
    }
  }
}

async function saveGitHubUsername() {
  if (!githubUsername.value.trim()) return
  const username = githubUsername.value.trim().replace(/^@/, '')
  await authApi.setMyGitHub(username)
  myGitHubUser.value = username
  showGitHubModal.value = false

  if (pendingConfirmRepoId.value !== null) {
    await confirmRepo(pendingConfirmRepoId.value)
    pendingConfirmRepoId.value = null
  }
  githubUsername.value = ''
}

function cancelGitHubModal() {
  showGitHubModal.value = false
  pendingConfirmRepoId.value = null
  githubUsername.value = ''
}

async function approveWithWarning(type: 'dev' | 'qa') {
  if (type === 'qa' && !allReposConfirmed.value) {
    const unconfirmed = totalNonExcludedRepos.value - confirmedReposCount.value
    if (!confirm(`${unconfirmed} repo(s) not yet confirmed. Continue anyway?`)) {
      return
    }
  }

  if (type === 'dev' && !release.value?.QAApprovedBy) {
    if (!confirm('QA has not approved yet. Continue anyway?')) {
      return
    }
  }

  await approve(type)
}

function handleClickOutside(event: MouseEvent) {
  const target = event.target as HTMLElement
  if (!target.closest('.relative')) {
    openDependencyDropdown.value = null
  }
}

onMounted(async () => {
  document.addEventListener('click', handleClickOutside)
  await Promise.all([loadRelease(), loadMyGitHub(), loadCIStatus(), loadDeploymentStatus()])

  if (route.query.syncing === '1') {
    syncing.value = true
    router.replace({ query: {} })

    syncInterval = setInterval(async () => {
      await loadRelease()
      if (repos.value.length > 0) {
        syncing.value = false
        if (syncInterval) {
          clearInterval(syncInterval)
          syncInterval = null
        }
      }
    }, 3000)

    setTimeout(() => {
      if (syncInterval) {
        clearInterval(syncInterval)
        syncInterval = null
        syncing.value = false
      }
    }, 60000)
  }
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
  if (syncInterval) {
    clearInterval(syncInterval)
    syncInterval = null
  }
  if (ciRefreshTimeout) {
    clearTimeout(ciRefreshTimeout)
    ciRefreshTimeout = null
  }
  if (deploymentRefreshTimeout) {
    clearTimeout(deploymentRefreshTimeout)
    deploymentRefreshTimeout = null
  }
})
</script>

<template>
  <div v-if="loading" class="flex items-center justify-center py-12">
    <div class="animate-spin rounded-full h-8 w-8 border-b-2 border-indigo-600"></div>
  </div>

  <div v-else-if="release" class="space-y-6">
    <!-- Syncing Banner -->
    <div v-if="syncing" class="bg-blue-50 border border-blue-200 rounded-lg p-4 flex items-center gap-3">
      <div class="animate-spin rounded-full h-5 w-5 border-2 border-blue-600 border-t-transparent"></div>
      <div>
        <p class="text-sm font-medium text-blue-800">Fetching repositories from GitHub...</p>
        <p class="text-xs text-blue-600">This may take a few moments. The page will update automatically.</p>
      </div>
    </div>

    <!-- Header -->
    <div class="flex items-start justify-between">
      <div>
        <button
          @click="router.push('/releases')"
          class="text-sm text-gray-500 hover:text-gray-700 flex items-center mb-2"
        >
          <svg class="w-4 h-4 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
          </svg>
          Back to releases
        </button>
        <h1 class="text-2xl font-bold text-gray-900">
          {{ release.SourceBranch }} → {{ release.DestBranch }}
        </h1>
        <p class="mt-1 text-sm text-gray-500">
          Created {{ formatDate(release.CreatedAt) }}
          <span v-if="release.LastRefreshedAt"> · Updated {{ formatDate(release.LastRefreshedAt) }}</span>
        </p>
      </div>
      <div class="flex items-center gap-3">
        <button
          v-if="release.Status !== 'declined'"
          @click="poke"
          :disabled="poking"
          class="inline-flex items-center px-4 py-2 border border-amber-300 shadow-sm text-sm font-medium rounded-lg text-amber-700 bg-white hover:bg-amber-50 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-amber-500 disabled:opacity-50"
        >
          <svg class="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9" />
          </svg>
          {{ poking ? 'Sending...' : 'Poke Participants' }}
        </button>
        <button
          v-if="release.Status !== 'declined'"
          @click="decline"
          class="inline-flex items-center px-4 py-2 border border-red-300 shadow-sm text-sm font-medium rounded-lg text-red-700 bg-white hover:bg-red-50 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-red-500"
        >
          <svg class="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
          </svg>
          Decline Release
        </button>
        <button
          @click="refresh"
          :disabled="refreshing"
          class="inline-flex items-center px-4 py-2 border border-gray-300 shadow-sm text-sm font-medium rounded-lg text-gray-700 bg-white hover:bg-gray-50 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-indigo-500 disabled:opacity-50"
        >
          <svg class="w-4 h-4 mr-2" :class="{ 'animate-spin': refreshing }" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          {{ refreshing ? 'Refreshing...' : 'Refresh from GitHub' }}
        </button>
      </div>
    </div>

    <!-- Approval Banner -->
    <div v-if="release.Status === 'approved'" class="rounded-lg bg-green-50 p-4 border border-green-200">
      <div class="flex">
        <div class="flex-shrink-0">
          <svg class="h-5 w-5 text-green-400" viewBox="0 0 20 20" fill="currentColor">
            <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z" clip-rule="evenodd" />
          </svg>
        </div>
        <div class="ml-3">
          <h3 class="text-sm font-medium text-green-800">Release Approved</h3>
          <p class="mt-1 text-sm text-green-700">This release is fully approved and ready to deploy!</p>
        </div>
      </div>
    </div>

    <!-- Declined Banner -->
    <div v-if="release.Status === 'declined'" class="rounded-lg bg-red-50 p-4 border border-red-200">
      <div class="flex">
        <div class="flex-shrink-0">
          <svg class="h-5 w-5 text-red-400" viewBox="0 0 20 20" fill="currentColor">
            <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z" clip-rule="evenodd" />
          </svg>
        </div>
        <div class="ml-3">
          <h3 class="text-sm font-medium text-red-800">Release Declined</h3>
          <p class="mt-1 text-sm text-red-700">
            Declined by {{ release.DeclinedBy }} on {{ formatDate(release.DeclinedAt) }}
          </p>
        </div>
      </div>
    </div>

    <!-- Infrastructure Changes Warning -->
    <div v-if="reposWithInfraChanges.length > 0" class="rounded-lg bg-amber-50 p-4 border-2 border-amber-400 shadow-md">
      <div class="flex">
        <div class="flex-shrink-0">
          <svg class="h-6 w-6 text-amber-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
          </svg>
        </div>
        <div class="ml-3 flex-1">
          <h3 class="text-base font-bold text-amber-800">⚠️ Infrastructure Changes Detected</h3>
          <p class="mt-1 text-sm text-amber-700">
            This release contains changes to infrastructure configurations. Please review carefully before deploying.
          </p>
          <div class="mt-3 flex flex-wrap gap-2">
            <span
              v-for="type in allInfraTypes"
              :key="type"
              :class="getInfraColor(type)"
              class="inline-flex items-center px-3 py-1 rounded-full text-sm font-semibold border uppercase"
            >
              {{ type }}
            </span>
          </div>
          <div class="mt-3 text-sm text-amber-700">
            <strong>Affected repositories:</strong>
            <ul class="mt-1 list-disc list-inside">
              <li v-for="repo in reposWithInfraChanges" :key="repo.ID" class="ml-2">
                {{ repo.RepoName }}
                <span class="text-xs">
                  ({{ getInfraChanges(repo).join(', ') }})
                </span>
              </li>
            </ul>
          </div>
        </div>
      </div>
    </div>

    <!-- Pipeline Progress Bar -->
    <div class="bg-white shadow-sm rounded-lg ring-1 ring-gray-200 p-4">
      <div class="flex items-center justify-center space-x-4">
        <div class="relative">
          <button
            @click="showConfirmationDetails = !showConfirmationDetails"
            class="flex items-center px-3 py-1.5 rounded-full text-sm font-medium cursor-pointer hover:ring-2 hover:ring-offset-1 hover:ring-indigo-300 transition-all"
            :class="allReposConfirmed ? 'bg-green-100 text-green-800' : 'bg-yellow-100 text-yellow-800'"
          >
            <span v-if="allReposConfirmed" class="mr-1.5">✓</span>
            <span v-else class="mr-1.5">○</span>
            Dev Confirmations ({{ confirmedReposCount }}/{{ totalNonExcludedRepos }})
            <svg class="w-4 h-4 ml-1.5 transition-transform" :class="{ 'rotate-180': showConfirmationDetails }" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
            </svg>
          </button>
        </div>
        <svg class="w-5 h-5 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
        </svg>
        <div class="flex items-center">
          <div
            class="flex items-center px-3 py-1.5 rounded-full text-sm font-medium"
            :class="release.QAApprovedBy ? 'bg-green-100 text-green-800' : 'bg-gray-100 text-gray-600'"
          >
            <span v-if="release.QAApprovedBy" class="mr-1.5">✓</span>
            <span v-else class="mr-1.5">○</span>
            QA Approval
          </div>
        </div>
        <svg class="w-5 h-5 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
        </svg>
        <div class="flex items-center">
          <div
            class="flex items-center px-3 py-1.5 rounded-full text-sm font-medium"
            :class="release.DevApprovedBy ? 'bg-green-100 text-green-800' : 'bg-gray-100 text-gray-600'"
          >
            <span v-if="release.DevApprovedBy" class="mr-1.5">✓</span>
            <span v-else class="mr-1.5">○</span>
            Dev Lead
          </div>
        </div>
      </div>

      <!-- Confirmation Details Dropdown -->
      <div v-if="showConfirmationDetails" class="mt-4 border-t border-gray-200 pt-4">
        <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
          <div
            v-for="repo in sortedRepos.filter(r => !r.Excluded)"
            :key="repo.ID"
            class="border rounded-lg p-3"
            :class="isRepoConfirmed(repo) ? 'border-green-200 bg-green-50' : 'border-gray-200 bg-gray-50'"
          >
            <div class="flex items-center justify-between mb-2">
              <span class="font-medium text-sm text-gray-900 truncate" :title="repo.RepoName">{{ repo.RepoName }}</span>
              <span
                class="text-xs px-2 py-0.5 rounded-full"
                :class="isRepoConfirmed(repo) ? 'bg-green-200 text-green-800' : 'bg-yellow-200 text-yellow-800'"
              >
                {{ getConfirmationDisplay(repo).progress }}
              </span>
            </div>
            <div class="space-y-1">
              <div
                v-for="contributor in getContributors(repo)"
                :key="contributor"
                class="flex items-center text-xs"
              >
                <span
                  class="w-4 h-4 mr-2 flex items-center justify-center rounded-full text-white text-xs"
                  :class="getConfirmedBy(repo).includes(contributor) ? 'bg-green-500' : 'bg-gray-300'"
                >
                  {{ getConfirmedBy(repo).includes(contributor) ? '✓' : '' }}
                </span>
                <span :class="getConfirmedBy(repo).includes(contributor) ? 'text-green-700' : 'text-gray-500'">
                  {{ contributor }}
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Approvals -->
    <div class="bg-white shadow-sm rounded-lg ring-1 ring-gray-200 p-6">
      <h2 class="text-lg font-medium text-gray-900 mb-4">Approvals</h2>
      <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
        <!-- Dev Approval -->
        <div class="border rounded-lg p-4" :class="release.DevApprovedBy ? 'border-green-200 bg-green-50' : 'border-gray-200'">
          <div class="flex items-center justify-between">
            <div>
              <h3 class="font-medium text-gray-900">Dev Lead</h3>
              <p v-if="release.DevApprovedBy" class="text-sm text-green-700">
                ✓ Approved by {{ release.DevApprovedBy }}
                <br><span class="text-xs text-green-600">{{ formatDate(release.DevApprovedAt) }}</span>
              </p>
              <p v-else class="text-sm text-gray-500">Not yet approved</p>
            </div>
            <button
              v-if="release.DevApprovedBy"
              @click="revoke('dev')"
              class="text-sm text-red-600 hover:text-red-800"
            >
              Revoke
            </button>
            <button
              v-else
              @click="approveWithWarning('dev')"
              class="inline-flex items-center px-3 py-1.5 border border-transparent text-sm font-medium rounded-md text-white bg-indigo-600 hover:bg-indigo-700"
            >
              Approve
            </button>
          </div>
        </div>

        <!-- QA Approval -->
        <div class="border rounded-lg p-4" :class="release.QAApprovedBy ? 'border-green-200 bg-green-50' : 'border-gray-200'">
          <div class="flex items-center justify-between">
            <div>
              <h3 class="font-medium text-gray-900">QA</h3>
              <p v-if="release.QAApprovedBy" class="text-sm text-green-700">
                ✓ Approved by {{ release.QAApprovedBy }}
                <br><span class="text-xs text-green-600">{{ formatDate(release.QAApprovedAt) }}</span>
              </p>
              <p v-else class="text-sm text-gray-500">Not yet approved</p>
            </div>
            <button
              v-if="release.QAApprovedBy"
              @click="revoke('qa')"
              class="text-sm text-red-600 hover:text-red-800"
            >
              Revoke
            </button>
            <button
              v-else
              @click="approveWithWarning('qa')"
              class="inline-flex items-center px-3 py-1.5 border border-transparent text-sm font-medium rounded-md text-white bg-indigo-600 hover:bg-indigo-700"
            >
              Approve
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- Notes & Breaking Changes -->
    <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
      <!-- Notes -->
      <div class="bg-white shadow-sm rounded-lg ring-1 ring-gray-200 p-6">
        <div class="flex items-center justify-between mb-4">
          <h2 class="text-lg font-medium text-gray-900">Notes</h2>
          <button
            @click="editingNotes = !editingNotes"
            class="text-sm text-indigo-600 hover:text-indigo-800"
          >
            {{ editingNotes ? 'Cancel' : 'Edit' }}
          </button>
        </div>
        <div v-if="editingNotes">
          <textarea
            v-model="notesText"
            rows="4"
            class="block w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm"
            placeholder="Add release notes..."
          ></textarea>
          <button
            @click="saveNotes"
            class="mt-3 inline-flex items-center px-3 py-2 border border-transparent text-sm font-medium rounded-md text-white bg-indigo-600 hover:bg-indigo-700"
          >
            Save
          </button>
        </div>
        <p v-else class="text-sm text-gray-600 whitespace-pre-wrap">{{ release.Notes || 'No notes added' }}</p>
      </div>

      <!-- Breaking Changes -->
      <div class="bg-white shadow-sm rounded-lg ring-1 ring-gray-200 p-6" :class="release.BreakingChanges ? 'ring-red-200' : ''">
        <div class="flex items-center justify-between mb-4">
          <h2 class="text-lg font-medium text-gray-900 flex items-center">
            <svg v-if="release.BreakingChanges" class="w-5 h-5 text-red-500 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
            </svg>
            Breaking Changes
          </h2>
          <button
            @click="editingBreaking = !editingBreaking"
            class="text-sm text-indigo-600 hover:text-indigo-800"
          >
            {{ editingBreaking ? 'Cancel' : 'Edit' }}
          </button>
        </div>
        <div v-if="editingBreaking">
          <textarea
            v-model="breakingText"
            rows="4"
            class="block w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm"
            placeholder="Document any breaking changes..."
          ></textarea>
          <button
            @click="saveBreaking"
            class="mt-3 inline-flex items-center px-3 py-2 border border-transparent text-sm font-medium rounded-md text-white bg-indigo-600 hover:bg-indigo-700"
          >
            Save
          </button>
        </div>
        <p v-else class="text-sm whitespace-pre-wrap" :class="release.BreakingChanges ? 'text-red-700' : 'text-gray-600'">
          {{ release.BreakingChanges || 'None documented' }}
        </p>
      </div>
    </div>

    <!-- Repositories -->
    <div class="bg-white shadow-sm rounded-lg ring-1 ring-gray-200 overflow-hidden">
      <div class="px-6 py-4 border-b border-gray-200">
        <h2 class="text-lg font-medium text-gray-900">Repositories ({{ repos.length }})</h2>
      </div>
      <table class="min-w-full divide-y divide-gray-200">
        <thead class="bg-gray-50">
          <tr>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Order</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Repository</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Summary</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">PR</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Confirmed</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Depends On</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Action</th>
          </tr>
        </thead>
        <tbody class="bg-white divide-y divide-gray-200">
          <tr v-for="repo in sortedRepos" :key="repo.ID" :class="{ 'opacity-50 bg-gray-50': repo.Excluded }">
            <td class="px-6 py-4 whitespace-nowrap">
              <span class="inline-flex items-center justify-center w-6 h-6 rounded-full bg-indigo-100 text-indigo-800 text-sm font-medium">
                {{ repo.DeployOrder }}
              </span>
            </td>
            <td class="px-6 py-4">
              <div class="flex items-center">
                <span v-if="repo.IsBreaking" class="mr-2 text-red-500" title="Breaking changes">🚨</span>
                <span class="text-sm font-medium text-gray-900" :class="{ 'line-through': repo.Excluded }">
                  {{ repo.RepoName }}
                </span>
              </div>
              <div class="mt-1 flex items-center gap-3 text-xs text-gray-500">
                <span title="Commits">{{ repo.CommitCount }} commits</span>
                <span class="text-green-600" title="Lines added">+{{ repo.Additions }}</span>
                <span class="text-red-600" title="Lines deleted">-{{ repo.Deletions }}</span>
              </div>
              <div v-if="formatContributors(repo)" class="mt-1 text-xs text-gray-400">
                {{ formatContributors(repo) }}
              </div>
              <div v-if="getInfraChanges(repo).length > 0" class="mt-2 flex flex-wrap gap-1">
                <span
                  v-for="type in getInfraChanges(repo)"
                  :key="type"
                  :class="getInfraColor(type)"
                  class="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium border"
                >
                  {{ type }}
                </span>
              </div>
            </td>
            <td class="px-6 py-4 text-sm text-gray-600 max-w-md">
              <div
                v-if="repo.Summary"
                @click="toggleSummary(repo.ID)"
                class="cursor-pointer hover:bg-gray-50 rounded p-1 -m-1"
              >
                <p :class="{ 'line-clamp-2': !expandedSummaries.has(repo.ID) }">{{ repo.Summary }}</p>
                <span class="text-xs text-indigo-500 mt-1 inline-block">
                  {{ expandedSummaries.has(repo.ID) ? '▲ Show less' : '▼ Show more' }}
                </span>
              </div>
              <p v-else class="text-gray-400">No summary available</p>
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-sm">
              <div v-if="repo.PRURL" class="flex items-center gap-2">
                <a
                  :href="repo.PRURL"
                  target="_blank"
                  class="text-indigo-600 hover:text-indigo-800"
                >
                  #{{ repo.PRNumber }}
                </a>
                <span
                  v-if="repo.PRMerged"
                  class="inline-flex items-center px-1.5 py-0.5 rounded text-xs font-medium bg-purple-100 text-purple-800"
                  title="PR is merged"
                >
                  Merged
                </span>
                <span
                  v-else
                  class="inline-flex items-center px-1.5 py-0.5 rounded text-xs font-medium bg-yellow-100 text-yellow-800"
                  title="PR is open"
                >
                  Open
                </span>
              </div>
              <a
                v-else
                :href="getCompareUrl(repo)"
                target="_blank"
                class="inline-flex items-center px-2 py-1 text-xs font-medium text-green-700 bg-green-100 rounded hover:bg-green-200"
              >
                + Create PR
              </a>
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-sm">
              <div v-if="repo.Excluded" class="text-gray-400">—</div>
              <div v-else-if="isRepoConfirmed(repo)" class="flex items-center gap-2">
                <span class="text-green-600 font-medium">✓ Confirmed</span>
                <button
                  v-if="hasConfirmed(repo)"
                  @click="unconfirmRepo(repo.ID)"
                  class="text-xs text-red-600 hover:text-red-800"
                >
                  Revoke
                </button>
              </div>
              <div v-else class="flex items-center gap-2">
                <span class="text-gray-500" :title="getConfirmationDisplay(repo).icons">
                  {{ getConfirmationDisplay(repo).progress }}
                </span>
                <span class="text-xs text-gray-400">{{ getConfirmationDisplay(repo).icons }}</span>
                <button
                  v-if="canConfirm(repo)"
                  @click="handleConfirmClick(repo)"
                  class="inline-flex items-center px-2 py-1 border border-transparent text-xs font-medium rounded text-indigo-700 bg-indigo-100 hover:bg-indigo-200"
                >
                  Confirm
                </button>
                <button
                  v-if="hasConfirmed(repo)"
                  @click="unconfirmRepo(repo.ID)"
                  class="text-xs text-red-600 hover:text-red-800"
                >
                  Revoke
                </button>
              </div>
            </td>
            <td class="px-6 py-4">
              <div class="space-y-2">
                <div v-if="getDependsOn(repo).length > 0" class="flex flex-wrap gap-1">
                  <span
                    v-for="dep in getDependsOn(repo)"
                    :key="dep"
                    class="inline-flex items-center gap-1 px-2 py-1 bg-indigo-100 text-indigo-800 rounded text-xs"
                  >
                    {{ dep }}
                    <button
                      @click="removeDependency(repo, dep)"
                      class="text-indigo-600 hover:text-indigo-900 font-bold"
                      title="Remove dependency"
                    >
                      ×
                    </button>
                  </span>
                </div>
                <div v-else class="text-xs text-gray-400">None</div>
                <div class="relative">
                  <button
                    v-if="getAvailableDependencies(repo).length > 0"
                    @click="toggleDependencyDropdown(repo.ID)"
                    class="inline-flex items-center px-2 py-1 text-xs text-gray-600 hover:text-gray-900 hover:bg-gray-100 rounded"
                  >
                    <svg class="w-3 h-3 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
                    </svg>
                    Add
                  </button>
                  <div
                    v-if="openDependencyDropdown === repo.ID"
                    class="absolute z-10 mt-1 w-48 bg-white rounded-md shadow-lg border border-gray-200 py-1 max-h-40 overflow-y-auto"
                  >
                    <button
                      v-for="dep in getAvailableDependencies(repo)"
                      :key="dep"
                      @click="addDependency(repo, dep)"
                      class="block w-full text-left px-3 py-1.5 text-sm text-gray-700 hover:bg-indigo-50 hover:text-indigo-900"
                    >
                      {{ dep }}
                    </button>
                  </div>
                </div>
              </div>
            </td>
            <td class="px-6 py-4 whitespace-nowrap">
              <button
                @click="toggleExcluded(repo)"
                :class="repo.Excluded ? 'bg-green-100 text-green-800 hover:bg-green-200' : 'bg-red-100 text-red-800 hover:bg-red-200'"
                class="inline-flex items-center px-2.5 py-1 rounded-md text-xs font-medium"
              >
                {{ repo.Excluded ? 'Include' : 'Exclude' }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- CI/CD Status Section -->
    <div class="bg-white shadow-sm rounded-lg ring-1 ring-gray-200 overflow-hidden">
      <div class="px-6 py-4 border-b border-gray-200 flex items-center justify-between">
        <div class="flex items-center">
          <svg class="w-5 h-5 text-gray-400 mr-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z" />
          </svg>
          <h2 class="text-lg font-medium text-gray-900">CI/CD Status</h2>
          <span v-if="anyInProgress" class="ml-3 inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-yellow-100 text-yellow-800">
            <span class="animate-pulse mr-1">&#9679;</span> Auto-refreshing
          </span>
        </div>
        <button
          @click="loadCIStatus"
          :disabled="ciLoading"
          class="inline-flex items-center px-3 py-1.5 text-sm text-gray-600 hover:text-gray-900 hover:bg-gray-100 rounded disabled:opacity-50"
        >
          <svg class="w-4 h-4 mr-1" :class="{ 'animate-spin': ciLoading }" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          Refresh
        </button>
      </div>

      <div v-if="ciLoading && ciStatuses.size === 0" class="flex items-center justify-center py-8">
        <div class="animate-spin rounded-full h-6 w-6 border-b-2 border-indigo-600"></div>
      </div>

      <div v-else-if="ciStatuses.size === 0" class="px-6 py-8 text-center text-gray-500">
        No CI status available
      </div>

      <table v-else class="min-w-full divide-y divide-gray-200">
        <thead class="bg-gray-50">
          <tr>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Repository</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Status</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Run #</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Chart</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Completed</th>
          </tr>
        </thead>
        <tbody class="bg-white divide-y divide-gray-200">
          <tr v-for="repo in sortedRepos.filter(r => !r.Excluded)" :key="'ci-' + repo.ID">
            <td class="px-6 py-4 whitespace-nowrap">
              <span class="text-sm font-medium text-gray-900">{{ repo.RepoName }}</span>
            </td>
            <td class="px-6 py-4 whitespace-nowrap">
              <span
                v-if="ciStatuses.get(repo.ID)"
                class="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium"
                :class="getCIStatusClass(ciStatuses.get(repo.ID)!.status)"
              >
                <span v-html="getCIStatusIcon(ciStatuses.get(repo.ID)!.status)" class="mr-1"></span>
                {{ getCIStatusText(ciStatuses.get(repo.ID)!.status) }}
              </span>
              <span v-else class="text-sm text-gray-400">-</span>
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-sm">
              <a
                v-if="ciStatuses.get(repo.ID)?.run_url"
                :href="ciStatuses.get(repo.ID)!.run_url"
                target="_blank"
                class="text-indigo-600 hover:text-indigo-800"
              >
                #{{ ciStatuses.get(repo.ID)!.run_number }}
              </a>
              <span v-else class="text-gray-400">-</span>
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-sm">
              <div class="flex items-center gap-2">
                <div v-if="ciStatuses.get(repo.ID)?.chart_version">
                  <div v-if="ciStatuses.get(repo.ID)?.chart_name" class="text-gray-600 text-xs">
                    {{ ciStatuses.get(repo.ID)!.chart_name }}
                  </div>
                  <div class="text-gray-900 font-mono">
                    v{{ ciStatuses.get(repo.ID)!.chart_version }}
                  </div>
                </div>
                <span v-else class="text-gray-400">-</span>
                <button
                  v-if="ciStatuses.get(repo.ID)?.status === 'success' && !ciStatuses.get(repo.ID)?.chart_version"
                  @click="refreshChartVersion(repo.ID)"
                  :disabled="refreshingChartVersion === repo.ID"
                  class="inline-flex items-center p-1 text-gray-400 hover:text-indigo-600 disabled:opacity-50"
                  title="Parse chart info from job logs"
                >
                  <svg class="w-4 h-4" :class="{ 'animate-spin': refreshingChartVersion === repo.ID }" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                  </svg>
                </button>
              </div>
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
              {{ ciStatuses.get(repo.ID) ? formatCITime(ciStatuses.get(repo.ID)!.completed_at) : '-' }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Deployment Status Section -->
    <div class="bg-white shadow-sm rounded-lg ring-1 ring-gray-200 overflow-hidden">
      <div class="px-6 py-4 border-b border-gray-200 flex items-center justify-between">
        <div class="flex items-center">
          <svg class="w-5 h-5 text-gray-400 mr-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
          </svg>
          <h2 class="text-lg font-medium text-gray-900">Deployment Status</h2>
          <span v-if="anyPendingDeployment" class="ml-3 inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-yellow-100 text-yellow-800">
            <span class="animate-pulse mr-1">&#9679;</span> Auto-refreshing
          </span>
        </div>
        <button
          @click="loadDeploymentStatus"
          :disabled="deploymentLoading"
          class="inline-flex items-center px-3 py-1.5 text-sm text-gray-600 hover:text-gray-900 hover:bg-gray-100 rounded disabled:opacity-50"
        >
          <svg class="w-4 h-4 mr-1" :class="{ 'animate-spin': deploymentLoading }" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          Refresh
        </button>
      </div>

      <div v-if="deploymentLoading && deploymentStatuses.size === 0" class="flex items-center justify-center py-8">
        <div class="animate-spin rounded-full h-6 w-6 border-b-2 border-indigo-600"></div>
      </div>

      <div v-else-if="deploymentStatuses.size === 0" class="px-6 py-8 text-center text-gray-500">
        No deployment status available
      </div>

      <table v-else class="min-w-full divide-y divide-gray-200">
        <thead class="bg-gray-50">
          <tr>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Repository</th>
            <th class="px-6 py-3 text-center text-xs font-medium text-gray-500 uppercase tracking-wider">QA</th>
            <th class="px-6 py-3 text-center text-xs font-medium text-gray-500 uppercase tracking-wider">UAT</th>
            <th class="px-6 py-3 text-center text-xs font-medium text-gray-500 uppercase tracking-wider">Prod</th>
          </tr>
        </thead>
        <tbody class="bg-white divide-y divide-gray-200">
          <tr v-for="repo in sortedRepos.filter(r => !r.Excluded)" :key="'deploy-' + repo.ID">
            <td class="px-6 py-4 whitespace-nowrap">
              <span class="text-sm font-medium text-gray-900">{{ repo.RepoName }}</span>
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-center">
              <span
                class="inline-flex items-center px-2.5 py-1 rounded-full text-xs font-medium"
                :class="getDeploymentStatusClass(getDeploymentEnv(repo.ID, 'qa'))"
                :title="getDeploymentStatusTooltip(getDeploymentEnv(repo.ID, 'qa'))"
              >
                <span v-html="getDeploymentStatusIcon(getDeploymentEnv(repo.ID, 'qa'))" class="mr-1"></span>
                <span v-if="getDeploymentEnv(repo.ID, 'qa')?.current_version" class="font-mono">
                  {{ getDeploymentEnv(repo.ID, 'qa')?.current_version }}
                </span>
                <span v-else>-</span>
              </span>
              <div v-if="parseRolloutStatus(getDeploymentEnv(repo.ID, 'qa'))" class="text-xs text-gray-500 mt-1">
                {{ parseRolloutStatus(getDeploymentEnv(repo.ID, 'qa'))?.ready }}/{{ parseRolloutStatus(getDeploymentEnv(repo.ID, 'qa'))?.replicas }} ready
              </div>
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-center">
              <span
                class="inline-flex items-center px-2.5 py-1 rounded-full text-xs font-medium"
                :class="getDeploymentStatusClass(getDeploymentEnv(repo.ID, 'uat'))"
                :title="getDeploymentStatusTooltip(getDeploymentEnv(repo.ID, 'uat'))"
              >
                <span v-html="getDeploymentStatusIcon(getDeploymentEnv(repo.ID, 'uat'))" class="mr-1"></span>
                <span v-if="getDeploymentEnv(repo.ID, 'uat')?.current_version" class="font-mono">
                  {{ getDeploymentEnv(repo.ID, 'uat')?.current_version }}
                </span>
                <span v-else>-</span>
              </span>
              <div v-if="parseRolloutStatus(getDeploymentEnv(repo.ID, 'uat'))" class="text-xs text-gray-500 mt-1">
                {{ parseRolloutStatus(getDeploymentEnv(repo.ID, 'uat'))?.ready }}/{{ parseRolloutStatus(getDeploymentEnv(repo.ID, 'uat'))?.replicas }} ready
              </div>
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-center">
              <span
                class="inline-flex items-center px-2.5 py-1 rounded-full text-xs font-medium"
                :class="getDeploymentStatusClass(getDeploymentEnv(repo.ID, 'prod'))"
                :title="getDeploymentStatusTooltip(getDeploymentEnv(repo.ID, 'prod'))"
              >
                <span v-html="getDeploymentStatusIcon(getDeploymentEnv(repo.ID, 'prod'))" class="mr-1"></span>
                <span v-if="getDeploymentEnv(repo.ID, 'prod')?.current_version" class="font-mono">
                  {{ getDeploymentEnv(repo.ID, 'prod')?.current_version }}
                </span>
                <span v-else>-</span>
              </span>
              <div v-if="parseRolloutStatus(getDeploymentEnv(repo.ID, 'prod'))" class="text-xs text-gray-500 mt-1">
                {{ parseRolloutStatus(getDeploymentEnv(repo.ID, 'prod'))?.ready }}/{{ parseRolloutStatus(getDeploymentEnv(repo.ID, 'prod'))?.replicas }} ready
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- History Section -->
    <div class="bg-white shadow-sm rounded-lg ring-1 ring-gray-200 overflow-hidden">
      <button
        @click="toggleHistory"
        class="w-full px-6 py-4 flex items-center justify-between hover:bg-gray-50 transition-colors"
      >
        <div class="flex items-center">
          <svg class="w-5 h-5 text-gray-400 mr-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <h2 class="text-lg font-medium text-gray-900">History</h2>
        </div>
        <svg
          class="w-5 h-5 text-gray-400 transition-transform"
          :class="{ 'rotate-180': historyExpanded }"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
        </svg>
      </button>

      <div v-if="historyExpanded" class="border-t border-gray-200">
        <div v-if="historyLoading" class="flex items-center justify-center py-8">
          <div class="animate-spin rounded-full h-6 w-6 border-b-2 border-indigo-600"></div>
        </div>

        <div v-else-if="history.length === 0" class="px-6 py-8 text-center text-gray-500">
          No history recorded yet
        </div>

        <div v-else class="divide-y divide-gray-100">
          <div
            v-for="entry in history"
            :key="entry.ID"
            class="px-6 py-3"
          >
            <div
              class="flex items-start gap-3"
              :class="{ 'cursor-pointer hover:bg-gray-50 -mx-6 px-6 -my-3 py-3': hasHistoryDetails(entry) }"
              @click="hasHistoryDetails(entry) && toggleHistoryEntry(entry.ID)"
            >
              <span class="text-lg flex-shrink-0 w-6 text-center">{{ getHistoryIcon(entry.Action) }}</span>
              <div class="flex-1 min-w-0">
                <p class="text-sm text-gray-900">
                  {{ formatHistoryAction(entry) }}
                  <span v-if="hasHistoryDetails(entry)" class="text-xs text-indigo-500 ml-2">
                    {{ expandedHistoryEntries.has(entry.ID) ? '▲ hide' : '▼ show changes' }}
                  </span>
                </p>
                <p class="text-xs text-gray-500 mt-0.5">
                  <span v-if="entry.Actor">{{ entry.Actor }} · </span>
                  {{ formatDate(entry.CreatedAt) }}
                </p>
              </div>
            </div>
            <div v-if="hasHistoryDetails(entry) && expandedHistoryEntries.has(entry.ID)" class="mt-3 ml-9 space-y-2">
              <template v-if="entry.Action === 'repo_dependencies_updated'">
                <div v-if="getDependencyChanges(entry)?.added.length" class="flex flex-wrap gap-1 items-center">
                  <span class="text-xs font-medium text-green-700">Added:</span>
                  <span
                    v-for="dep in getDependencyChanges(entry)?.added"
                    :key="dep"
                    class="inline-flex items-center px-2 py-0.5 bg-green-100 text-green-800 rounded text-xs"
                  >
                    + {{ dep }}
                  </span>
                </div>
                <div v-if="getDependencyChanges(entry)?.removed.length" class="flex flex-wrap gap-1 items-center">
                  <span class="text-xs font-medium text-red-700">Removed:</span>
                  <span
                    v-for="dep in getDependencyChanges(entry)?.removed"
                    :key="dep"
                    class="inline-flex items-center px-2 py-0.5 bg-red-100 text-red-800 rounded text-xs"
                  >
                    − {{ dep }}
                  </span>
                </div>
              </template>
              <template v-else>
                <div v-if="getHistoryDetails(entry)?.old" class="text-sm">
                  <span class="text-xs font-medium text-gray-500 uppercase">Before:</span>
                  <pre class="mt-1 p-2 bg-red-50 border border-red-100 rounded text-xs text-red-800 whitespace-pre-wrap overflow-x-auto">{{ getHistoryDetails(entry)?.old }}</pre>
                </div>
                <div v-if="getHistoryDetails(entry)?.new" class="text-sm">
                  <span class="text-xs font-medium text-gray-500 uppercase">After:</span>
                  <pre class="mt-1 p-2 bg-green-50 border border-green-100 rounded text-xs text-green-800 whitespace-pre-wrap overflow-x-auto">{{ getHistoryDetails(entry)?.new }}</pre>
                </div>
              </template>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- GitHub Username Modal -->
    <div v-if="showGitHubModal" class="fixed inset-0 z-50 overflow-y-auto">
      <div class="flex min-h-full items-end justify-center p-4 text-center sm:items-center sm:p-0">
        <div class="fixed inset-0 bg-gray-500 bg-opacity-75 transition-opacity" @click="cancelGitHubModal"></div>
        <div class="relative transform overflow-hidden rounded-lg bg-white px-4 pb-4 pt-5 text-left shadow-xl transition-all sm:my-8 sm:w-full sm:max-w-sm sm:p-6">
          <div>
            <div class="mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-indigo-100">
              <svg class="h-6 w-6 text-indigo-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
              </svg>
            </div>
            <div class="mt-3 text-center sm:mt-5">
              <h3 class="text-base font-semibold leading-6 text-gray-900">Link Your GitHub Account</h3>
              <div class="mt-2">
                <p class="text-sm text-gray-500">
                  Enter your GitHub username to confirm changes. This only needs to be done once.
                </p>
              </div>
              <div class="mt-4">
                <input
                  v-model="githubUsername"
                  type="text"
                  placeholder="@username"
                  class="block w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm"
                  @keyup.enter="saveGitHubUsername"
                />
              </div>
            </div>
          </div>
          <div class="mt-5 sm:mt-6 sm:grid sm:grid-flow-row-dense sm:grid-cols-2 sm:gap-3">
            <button
              type="button"
              @click="saveGitHubUsername"
              class="inline-flex w-full justify-center rounded-md bg-indigo-600 px-3 py-2 text-sm font-semibold text-white shadow-sm hover:bg-indigo-500 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-600 sm:col-start-2"
            >
              Save
            </button>
            <button
              type="button"
              @click="cancelGitHubModal"
              class="mt-3 inline-flex w-full justify-center rounded-md bg-white px-3 py-2 text-sm font-semibold text-gray-900 shadow-sm ring-1 ring-inset ring-gray-300 hover:bg-gray-50 sm:col-start-1 sm:mt-0"
            >
              Cancel
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
