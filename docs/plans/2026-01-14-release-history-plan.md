# Release History Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Track and display all changes to releases in a comprehensive audit log.

**Architecture:** New ReleaseHistory table stores events, existing handlers call RecordHistory service method, GET endpoint returns history, Vue component displays timeline.

**Tech Stack:** Go 1.21+, GORM, SQLite, Vue 3, TypeScript, Tailwind CSS

---

### Task 1: Add ReleaseHistory Model

**Files:**
- Modify: `internal/database/models.go`

**Step 1: Add the ReleaseHistory struct**

Add after the `User` struct:

```go
type ReleaseHistory struct {
	ID        uint   `gorm:"primaryKey;autoIncrement"`
	ReleaseID string `gorm:"not null;index"`
	Action    string `gorm:"not null"`
	Actor     string
	Details   string
	CreatedAt int64  `gorm:"not null;index"`
}
```

**Step 2: Verify it compiles**

Run: `go build ./...`
Expected: Success, no errors

**Step 3: Commit**

```bash
git add internal/database/models.go
git commit -m "feat: add ReleaseHistory model for audit logging"
```

---

### Task 2: Add Database Migration

**Files:**
- Modify: `internal/database/database.go`

**Step 1: Add ReleaseHistory to AutoMigrate**

Find the `AutoMigrate` call and add `&ReleaseHistory{}` to the list of models.

**Step 2: Verify it compiles**

Run: `go build ./...`
Expected: Success

**Step 3: Commit**

```bash
git add internal/database/database.go
git commit -m "feat: add ReleaseHistory to database migration"
```

---

### Task 3: Add RecordHistory Service Method

**Files:**
- Modify: `internal/dashboard/service.go`

**Step 1: Add RecordHistory method**

Add to the Service struct methods:

```go
func (s *Service) RecordHistory(ctx context.Context, releaseID, action, actor string, details map[string]any) error {
	var detailsJSON string
	if details != nil {
		data, err := json.Marshal(details)
		if err != nil {
			return fmt.Errorf("marshaling history details: %w", err)
		}
		detailsJSON = string(data)
	}

	history := database.ReleaseHistory{
		ReleaseID: releaseID,
		Action:    action,
		Actor:     actor,
		Details:   detailsJSON,
		CreatedAt: time.Now().Unix(),
	}

	if err := s.db.WithContext(ctx).Create(&history).Error; err != nil {
		return fmt.Errorf("creating history entry: %w", err)
	}

	return nil
}
```

**Step 2: Add required imports**

Ensure `encoding/json` and `time` are imported.

**Step 3: Verify it compiles**

Run: `go build ./...`
Expected: Success

**Step 4: Commit**

```bash
git add internal/dashboard/service.go
git commit -m "feat: add RecordHistory service method"
```

---

### Task 4: Add GetHistory Service Method

**Files:**
- Modify: `internal/dashboard/service.go`

**Step 1: Add GetHistory method**

```go
func (s *Service) GetHistory(ctx context.Context, releaseID string) ([]database.ReleaseHistory, error) {
	var history []database.ReleaseHistory
	err := s.db.WithContext(ctx).
		Where("release_id = ?", releaseID).
		Order("created_at DESC").
		Find(&history).Error
	if err != nil {
		return nil, fmt.Errorf("fetching history: %w", err)
	}
	return history, nil
}
```

**Step 2: Verify it compiles**

Run: `go build ./...`
Expected: Success

**Step 3: Commit**

```bash
git add internal/dashboard/service.go
git commit -m "feat: add GetHistory service method"
```

---

### Task 5: Add GetHistory Handler

**Files:**
- Modify: `internal/dashboard/handlers.go`

**Step 1: Add GetHistory handler**

```go
func (h *Handlers) GetHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/releases/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	releaseID := parts[0]

	history, err := h.service.GetHistory(r.Context(), releaseID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}
```

**Step 2: Verify it compiles**

Run: `go build ./...`
Expected: Success

**Step 3: Commit**

```bash
git add internal/dashboard/handlers.go
git commit -m "feat: add GetHistory handler"
```

---

### Task 6: Add History Route

**Files:**
- Modify: `internal/dashboard/server.go`

**Step 1: Add history route to handleRelease**

In the `handleRelease` method, add a case for the history endpoint. Find the switch statement and add:

```go
} else if len(parts) > 1 && parts[1] == "history" {
	s.handlers.GetHistory(w, r)
```

Add this before the default case in the POST method handling, or as a GET-only check.

**Step 2: Verify it compiles**

Run: `go build ./...`
Expected: Success

**Step 3: Commit**

```bash
git add internal/dashboard/server.go
git commit -m "feat: add history route"
```

---

### Task 7: Record History in Existing Handlers - Approvals

**Files:**
- Modify: `internal/dashboard/handlers.go`

**Step 1: Update ApproveRelease handler**

After the approval is saved successfully, add:

```go
h.service.RecordHistory(r.Context(), releaseID, "approval_added", userEmail, map[string]any{
	"type": approvalType,
})
```

Where `userEmail` comes from the auth context and `approvalType` is "dev" or "qa".

**Step 2: Update RevokeApproval handler**

After the revocation is saved, add:

```go
h.service.RecordHistory(r.Context(), releaseID, "approval_revoked", userEmail, map[string]any{
	"type": approvalType,
})
```

**Step 3: Verify it compiles**

Run: `go build ./...`
Expected: Success

**Step 4: Commit**

```bash
git add internal/dashboard/handlers.go
git commit -m "feat: record history for approvals"
```

---

### Task 8: Record History in Existing Handlers - Release Actions

**Files:**
- Modify: `internal/dashboard/handlers.go`

**Step 1: Update DeclineRelease handler**

After decline is saved:

```go
h.service.RecordHistory(r.Context(), releaseID, "release_declined", userEmail, nil)
```

**Step 2: Update RefreshRelease handler**

After refresh completes:

```go
h.service.RecordHistory(r.Context(), releaseID, "release_refreshed", "system", nil)
```

**Step 3: Update UpdateRelease handler**

After notes update:

```go
if updates.Notes != nil {
	h.service.RecordHistory(r.Context(), releaseID, "notes_updated", userEmail, nil)
}
if updates.BreakingChanges != nil {
	h.service.RecordHistory(r.Context(), releaseID, "breaking_changes_updated", userEmail, nil)
}
```

**Step 4: Verify it compiles**

Run: `go build ./...`
Expected: Success

**Step 5: Commit**

```bash
git add internal/dashboard/handlers.go
git commit -m "feat: record history for release actions"
```

---

### Task 9: Record History in Existing Handlers - Repo Actions

**Files:**
- Modify: `internal/dashboard/handlers.go`

**Step 1: Update UpdateRepo handler for exclusions**

When excluded changes:

```go
if updates.Excluded != nil {
	action := "repo_excluded"
	if !*updates.Excluded {
		action = "repo_included"
	}
	h.service.RecordHistory(r.Context(), releaseID, action, userEmail, map[string]any{
		"repo": repo.RepoName,
	})
}
```

**Step 2: Update UpdateRepo handler for dependencies**

When depends_on changes:

```go
if updates.DependsOn != nil {
	h.service.RecordHistory(r.Context(), releaseID, "repo_dependencies_updated", userEmail, map[string]any{
		"repo": repo.RepoName,
		"deps": updates.DependsOn,
	})
}
```

**Step 3: Update ConfirmRepo handler**

After confirmation:

```go
h.service.RecordHistory(r.Context(), releaseID, "repo_confirmed", userEmail, map[string]any{
	"repo":    repo.RepoName,
	"github":  githubUser,
})
```

**Step 4: Verify it compiles**

Run: `go build ./...`
Expected: Success

**Step 5: Commit**

```bash
git add internal/dashboard/handlers.go
git commit -m "feat: record history for repo actions"
```

---

### Task 10: Record History for Poke

**Files:**
- Modify: `internal/dashboard/handlers.go`

**Step 1: Update PokeParticipants handler**

After poke is sent:

```go
h.service.RecordHistory(r.Context(), releaseID, "participants_poked", userEmail, map[string]any{
	"count": len(pendingActions),
})
```

**Step 2: Verify it compiles**

Run: `go build ./...`
Expected: Success

**Step 3: Commit**

```bash
git add internal/dashboard/handlers.go
git commit -m "feat: record history for poke action"
```

---

### Task 11: Add Frontend Types

**Files:**
- Modify: `web/src/api/types.ts`

**Step 1: Add HistoryEntry interface**

```typescript
export interface HistoryEntry {
  ID: number
  ReleaseID: string
  Action: string
  Actor: string
  Details: string
  CreatedAt: number
}
```

**Step 2: Verify frontend builds**

Run: `cd web && npm run build`
Expected: Success

**Step 3: Commit**

```bash
git add web/src/api/types.ts
git commit -m "feat: add HistoryEntry type"
```

---

### Task 12: Add Frontend API Method

**Files:**
- Modify: `web/src/api/client.ts`

**Step 1: Add getHistory method to releaseApi**

```typescript
getHistory: async (id: string): Promise<HistoryEntry[]> => {
  const { data } = await api.get(`/releases/${id}/history`)
  return data
}
```

**Step 2: Add HistoryEntry to imports**

Update the import line to include `HistoryEntry`.

**Step 3: Verify frontend builds**

Run: `cd web && npm run build`
Expected: Success

**Step 4: Commit**

```bash
git add web/src/api/client.ts
git commit -m "feat: add getHistory API method"
```

---

### Task 13: Add History UI Section

**Files:**
- Modify: `web/src/views/ReleaseDetailView.vue`

**Step 1: Add state variables**

In the script setup section, add:

```typescript
const history = ref<HistoryEntry[]>([])
const historyExpanded = ref(false)
const historyLoading = ref(false)
```

**Step 2: Add HistoryEntry to imports**

Update the import from `@/api/types`.

**Step 3: Add loadHistory function**

```typescript
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
```

**Step 4: Add helper functions**

```typescript
function formatHistoryAction(entry: HistoryEntry): string {
  const details = entry.Details ? JSON.parse(entry.Details) : {}
  const actions: Record<string, string> = {
    'release_created': 'Created release',
    'release_refreshed': 'Refreshed from GitHub',
    'release_declined': 'Declined release',
    'approval_added': `Approved ${details.type?.toUpperCase() || ''}`,
    'approval_revoked': `Revoked ${details.type?.toUpperCase() || ''} approval`,
    'notes_updated': 'Updated notes',
    'breaking_changes_updated': 'Updated breaking changes',
    'repo_excluded': `Excluded ${details.repo || 'repository'}`,
    'repo_included': `Included ${details.repo || 'repository'}`,
    'repo_confirmed': `Confirmed ${details.repo || 'repository'}`,
    'repo_dependencies_updated': `Updated dependencies for ${details.repo || 'repository'}`,
    'participants_poked': `Poked ${details.count || ''} participants`,
  }
  return actions[entry.Action] || entry.Action
}

function getHistoryIcon(action: string): { icon: string; color: string } {
  const icons: Record<string, { icon: string; color: string }> = {
    'release_created': { icon: '🚀', color: 'text-blue-600' },
    'release_refreshed': { icon: '🔄', color: 'text-gray-500' },
    'release_declined': { icon: '❌', color: 'text-red-600' },
    'approval_added': { icon: '✅', color: 'text-green-600' },
    'approval_revoked': { icon: '↩️', color: 'text-orange-500' },
    'notes_updated': { icon: '📝', color: 'text-blue-500' },
    'breaking_changes_updated': { icon: '⚠️', color: 'text-amber-500' },
    'repo_excluded': { icon: '🚫', color: 'text-gray-500' },
    'repo_included': { icon: '➕', color: 'text-green-500' },
    'repo_confirmed': { icon: '✓', color: 'text-green-600' },
    'repo_dependencies_updated': { icon: '🔗', color: 'text-purple-500' },
    'participants_poked': { icon: '🔔', color: 'text-amber-500' },
  }
  return icons[action] || { icon: '•', color: 'text-gray-500' }
}
```

**Step 5: Verify frontend builds**

Run: `cd web && npm run build`
Expected: Success

**Step 6: Commit**

```bash
git add web/src/views/ReleaseDetailView.vue
git commit -m "feat: add history state and helper functions"
```

---

### Task 14: Add History Template Section

**Files:**
- Modify: `web/src/views/ReleaseDetailView.vue`

**Step 1: Add history section template**

Add after the repositories table, before the GitHub modal:

```html
<!-- History -->
<div class="bg-white shadow-sm rounded-lg ring-1 ring-gray-200 overflow-hidden">
  <button
    @click="toggleHistory"
    class="w-full px-6 py-4 flex items-center justify-between hover:bg-gray-50"
  >
    <h2 class="text-lg font-medium text-gray-900 flex items-center">
      <svg class="w-5 h-5 mr-2 transition-transform" :class="{ 'rotate-180': historyExpanded }" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
      </svg>
      History
    </h2>
    <button
      v-if="historyExpanded"
      @click.stop="loadHistory"
      class="text-sm text-indigo-600 hover:text-indigo-800"
    >
      Refresh
    </button>
  </button>

  <div v-if="historyExpanded" class="border-t border-gray-200">
    <div v-if="historyLoading" class="px-6 py-8 text-center">
      <div class="animate-spin rounded-full h-6 w-6 border-b-2 border-indigo-600 mx-auto"></div>
    </div>
    <div v-else-if="history.length === 0" class="px-6 py-8 text-center text-gray-500">
      No history entries yet
    </div>
    <div v-else class="divide-y divide-gray-100">
      <div
        v-for="entry in history"
        :key="entry.ID"
        class="px-6 py-3 flex items-start gap-3"
      >
        <span :class="getHistoryIcon(entry.Action).color" class="text-lg mt-0.5">
          {{ getHistoryIcon(entry.Action).icon }}
        </span>
        <div class="flex-1 min-w-0">
          <p class="text-sm text-gray-900">{{ formatHistoryAction(entry) }}</p>
          <p class="text-xs text-gray-500 mt-0.5">
            {{ entry.Actor || 'System' }} · {{ formatDate(entry.CreatedAt) }}
          </p>
        </div>
      </div>
    </div>
  </div>
</div>
```

**Step 2: Verify frontend builds**

Run: `cd web && npm run build`
Expected: Success

**Step 3: Commit**

```bash
git add web/src/views/ReleaseDetailView.vue
git commit -m "feat: add history UI section"
```

---

### Task 15: Final Verification

**Step 1: Build entire project**

Run: `go build ./... && cd web && npm run build`
Expected: Both succeed

**Step 2: Test manually (if server is running)**

1. Start the server
2. Open a release detail page
3. Click "History" to expand
4. Perform actions (approve, edit notes, etc.)
5. Refresh history to see new entries

**Step 3: Final commit if any fixes needed**

```bash
git add -A
git commit -m "fix: address any issues from testing"
```

---

## Summary

15 tasks covering:
- Database model and migration (Tasks 1-2)
- Service methods (Tasks 3-4)
- Handler and route (Tasks 5-6)
- Recording history in all existing handlers (Tasks 7-10)
- Frontend types and API (Tasks 11-12)
- Frontend UI components (Tasks 13-14)
- Final verification (Task 15)
