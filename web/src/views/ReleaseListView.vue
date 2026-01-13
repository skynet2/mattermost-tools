<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { releaseApi } from '@/api/client'
import type { Release } from '@/api/types'

const router = useRouter()
const releases = ref<Release[]>([])
const loading = ref(true)
const statusFilter = ref('')

const showCreateModal = ref(false)
const creating = ref(false)
const sourceBranch = ref('')
const destBranch = ref('')

async function loadReleases() {
  loading.value = true
  try {
    releases.value = await releaseApi.list(statusFilter.value || undefined)
  } finally {
    loading.value = false
  }
}

async function createRelease() {
  if (!sourceBranch.value || !destBranch.value) return
  creating.value = true
  try {
    const release = await releaseApi.create(sourceBranch.value, destBranch.value)
    showCreateModal.value = false
    sourceBranch.value = ''
    destBranch.value = ''
    router.push(`/releases/${release.ID}`)
  } catch (error: any) {
    alert(error.response?.data || 'Failed to create release')
  } finally {
    creating.value = false
  }
}

function cancelCreateModal() {
  showCreateModal.value = false
  sourceBranch.value = ''
  destBranch.value = ''
}

function formatDate(timestamp: number) {
  return new Date(timestamp * 1000).toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit'
  })
}

function statusBadge(status: string) {
  switch (status) {
    case 'approved':
      return { class: 'bg-green-100 text-green-800', text: 'Approved' }
    case 'deployed':
      return { class: 'bg-blue-100 text-blue-800', text: 'Deployed' }
    case 'declined':
      return { class: 'bg-red-100 text-red-800', text: 'Declined' }
    default:
      return { class: 'bg-yellow-100 text-yellow-800', text: 'Pending' }
  }
}

onMounted(loadReleases)
</script>

<template>
  <div>
    <div class="sm:flex sm:items-center sm:justify-between mb-8">
      <div>
        <h1 class="text-2xl font-bold text-gray-900">Shipyard</h1>
        <p class="mt-1 text-sm text-gray-500">Prepare and ship releases across repositories</p>
      </div>
      <div class="mt-4 sm:mt-0 flex items-center gap-3">
        <select
          v-model="statusFilter"
          @change="loadReleases"
          class="block rounded-lg border-gray-300 bg-white px-4 py-2 pr-8 text-sm font-medium text-gray-700 shadow-sm ring-1 ring-gray-300 hover:bg-gray-50 focus:outline-none focus:ring-2 focus:ring-indigo-500"
        >
          <option value="">All Releases</option>
          <option value="pending">Pending</option>
          <option value="approved">Approved</option>
          <option value="declined">Declined</option>
          <option value="deployed">Deployed</option>
        </select>
        <button
          @click="showCreateModal = true"
          class="inline-flex items-center px-4 py-2 border border-transparent text-sm font-medium rounded-lg shadow-sm text-white bg-indigo-600 hover:bg-indigo-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-indigo-500"
        >
          <svg class="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
          </svg>
          New Release
        </button>
      </div>
    </div>

    <div v-if="loading" class="flex items-center justify-center py-12">
      <div class="animate-spin rounded-full h-8 w-8 border-b-2 border-indigo-600"></div>
    </div>

    <div v-else-if="releases.length === 0" class="text-center py-12">
      <svg class="mx-auto h-12 w-12 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M20 13V6a2 2 0 00-2-2H6a2 2 0 00-2 2v7m16 0v5a2 2 0 01-2 2H6a2 2 0 01-2-2v-5m16 0h-2.586a1 1 0 00-.707.293l-2.414 2.414a1 1 0 01-.707.293h-3.172a1 1 0 01-.707-.293l-2.414-2.414A1 1 0 006.586 13H4" />
      </svg>
      <h3 class="mt-2 text-sm font-medium text-gray-900">No releases in the shipyard</h3>
      <p class="mt-1 text-sm text-gray-500">Click "New Release" to start preparing a release.</p>
    </div>

    <div v-else class="bg-white shadow-sm rounded-lg ring-1 ring-gray-200 overflow-hidden">
      <table class="min-w-full divide-y divide-gray-200">
        <thead class="bg-gray-50">
          <tr>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Release</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Status</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Created</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Approvals</th>
          </tr>
        </thead>
        <tbody class="bg-white divide-y divide-gray-200">
          <tr
            v-for="release in releases"
            :key="release.ID"
            class="hover:bg-gray-50 cursor-pointer transition-colors"
            @click="$router.push(`/releases/${release.ID}`)"
          >
            <td class="px-6 py-4 whitespace-nowrap">
              <div class="flex items-center">
                <div class="flex-shrink-0 h-10 w-10 bg-indigo-100 rounded-lg flex items-center justify-center">
                  <svg class="h-5 w-5 text-indigo-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 7h12m0 0l-4-4m4 4l-4 4m0 6H4m0 0l4 4m-4-4l4-4" />
                  </svg>
                </div>
                <div class="ml-4">
                  <div class="text-sm font-medium text-gray-900">
                    {{ release.SourceBranch }} → {{ release.DestBranch }}
                  </div>
                  <div class="text-sm text-gray-500">
                    ID: {{ release.ID.slice(0, 8) }}...
                  </div>
                </div>
              </div>
            </td>
            <td class="px-6 py-4 whitespace-nowrap">
              <span
                :class="statusBadge(release.Status).class"
                class="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium"
              >
                {{ statusBadge(release.Status).text }}
              </span>
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
              {{ formatDate(release.CreatedAt) }}
            </td>
            <td class="px-6 py-4 whitespace-nowrap">
              <div class="flex space-x-2">
                <span
                  :class="release.DevApprovedBy ? 'bg-green-100 text-green-800' : 'bg-gray-100 text-gray-600'"
                  class="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium"
                >
                  Dev {{ release.DevApprovedBy ? '✓' : '' }}
                </span>
                <span
                  :class="release.QAApprovedBy ? 'bg-green-100 text-green-800' : 'bg-gray-100 text-gray-600'"
                  class="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium"
                >
                  QA {{ release.QAApprovedBy ? '✓' : '' }}
                </span>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Create Release Modal -->
    <div v-if="showCreateModal" class="fixed inset-0 z-50 overflow-y-auto">
      <div class="flex min-h-full items-end justify-center p-4 text-center sm:items-center sm:p-0">
        <div class="fixed inset-0 bg-gray-500 bg-opacity-75 transition-opacity" @click="cancelCreateModal"></div>
        <div class="relative transform overflow-hidden rounded-lg bg-white px-4 pb-4 pt-5 text-left shadow-xl transition-all sm:my-8 sm:w-full sm:max-w-md sm:p-6">
          <div>
            <div class="mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-indigo-100">
              <svg class="h-6 w-6 text-indigo-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
              </svg>
            </div>
            <div class="mt-3 text-center sm:mt-5">
              <h3 class="text-base font-semibold leading-6 text-gray-900">Create New Release</h3>
              <p class="mt-2 text-sm text-gray-500">
                Specify the source and destination branches for the release.
              </p>
            </div>
            <div class="mt-5 space-y-4">
              <div>
                <label for="sourceBranch" class="block text-sm font-medium text-gray-700">Source Branch</label>
                <input
                  id="sourceBranch"
                  v-model="sourceBranch"
                  type="text"
                  placeholder="e.g., develop, uat"
                  class="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm"
                  @keyup.enter="createRelease"
                />
              </div>
              <div>
                <label for="destBranch" class="block text-sm font-medium text-gray-700">Destination Branch</label>
                <input
                  id="destBranch"
                  v-model="destBranch"
                  type="text"
                  placeholder="e.g., master, staging"
                  class="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm"
                  @keyup.enter="createRelease"
                />
              </div>
            </div>
          </div>
          <div class="mt-5 sm:mt-6 sm:grid sm:grid-flow-row-dense sm:grid-cols-2 sm:gap-3">
            <button
              type="button"
              :disabled="creating || !sourceBranch || !destBranch"
              @click="createRelease"
              class="inline-flex w-full justify-center rounded-md bg-indigo-600 px-3 py-2 text-sm font-semibold text-white shadow-sm hover:bg-indigo-500 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-600 sm:col-start-2 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {{ creating ? 'Creating...' : 'Create' }}
            </button>
            <button
              type="button"
              @click="cancelCreateModal"
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
