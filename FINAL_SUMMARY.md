# YouTrack Plugin + Kandev Contribution — Final Summary

## Overview

Built a complete YouTrack integration as an external Kandev plugin, contributed the host-side APIs needed to make it indistinguishable from built-in integrations (Jira/Linear), and got both the contribution PR and plugin published.

---

## 1. Kandev Contribution (PR #2736 — MERGED)

https://github.com/kdlbs/kandev/pull/2736

### What was contributed

Exposed the native settings UI surface to external plugins so their integration settings pages are visually identical to built-in integrations (Jira, Linear, GitHub, etc.):

| API | Purpose |
|---|---|
| `host.useSettingsSaveContributor(contributor)` | Native floating Save changes / Reset bar with dirty tracking + navigation guard |
| `host.setIntegrationEnabled(integrationId, workspaceId, enabled)` | Per-workspace sidebar "Enabled" badge — reactive via `usePluginRegistry()` |
| `registerIntegrationSettings({ action })` | Optional header action component (e.g. enable toggle) rendered in `SettingsSection` action slot |
| `host.ui.IntegrationAuthStatusBanner` | Native green/red auth health banner |
| `host.ui.IntegrationEnabledControl` | Drafted enable/disable switch wired to save coordinator |
| `host.ui.SettingsSection` / `SettingsCard` | Native settings wrappers with dirty border tracking |
| `host.ui.WorkspaceScopedSection` | Per-workspace section resolution |
| `host.context.getWorkspaceIds()` | Reactive workspace list for boot-sync (added by maintainer) |
| `host.utils.integrationStatusRefreshMs` | 90s health refresh constant |

### Files changed (24 files, 839 insertions)

| File | Change |
|---|---|
| `packages/plugin-sdk/src/index.ts` | PluginUIShape additions, `setIntegrationEnabled`, `useSettingsSaveContributor`, `action` on `registerIntegrationSettings`, `SettingsSaveContributor` type, `getWorkspaceIds`/`subscribeWorkspaces` on `PluginContextApi` |
| `web/lib/plugins/host-api.ts` | `PLUGIN_UI` exports, `buildHostApi` wiring, `createPluginSettingsSaveContributorHook` (namespaced IDs), `createPluginUIApi` (per-plugin `IntegrationEnabledControl` wrapper) |
| `web/lib/plugins/host-runtime-resources.ts` | Generation-fenced pass-through for both new APIs |
| `web/lib/plugins/registry.ts` | `integrationEnabled: Map<integrationId, Map<workspaceId, boolean>>`, `set/is/getIntegrationEnabled`, ownership validation, `unregisterPlugin` cleanup |
| `web/lib/plugins/types.ts` | `IntegrationSettingsRegistration.action?: ComponentType<{ workspaceId?: string }>` |
| `web/lib/plugins/plugin-context-api.ts` | `getWorkspaceIds()` + `subscribeWorkspaces()` |
| `web/components/app-sidebar/sections/settings/integration-enabled.tsx` | Registry-driven plugin badge via `usePluginRegistry`, `BadgeWorkspaceContext`, deleted localStorage hack |
| `web/components/app-sidebar/sections/settings/settings-menu-branches.ts` | `integrationSlug: id` on plugin contributions, widened to `IntegrationSlug \| string` |
| `web/components/app-sidebar/sections/settings/use-settings-menu-branches.ts` | Honor hide-disabled navigation for plugin integrations |
| `web/src/plugin-integration-settings-route.tsx` | Render `registration.action` with `workspaceId` prop in `SettingsSection` |
| 4 test mock files | `useSettingsSaveContributor` + `setIntegrationEnabled` no-op additions |
| 8 new test files | Behavioral tests for registry, badge, route, runtime fencing, save coordinator, context API |
| `docs/plans/plugins/PLUGIN-API.md` | 65 lines documenting new APIs |
| `docs/public/plugins-authoring.md` | 74 lines documenting the new contract |

### Maintainer refinements (@carlosflorencio)

- Scoped enabled state by **integration ID** (not pluginId) — fixes multi-registration concern
- **Namespaced** save-contributor IDs (`plugin:<pluginId>:<contributorId>`) to prevent cross-plugin collisions
- Wrapped `IntegrationEnabledControl` per-plugin with namespaced contributor IDs
- Added `host.context.getWorkspaceIds()` + `subscribeWorkspaces()` — proper reactive workspace list
- Added behavioral tests + PLUGIN-API.md + plugins-authoring.md docs
- Merged with main to resolve file conflicts

### Review status
- Greptile: **pass** (all P1 findings resolved)
- CodeRabbit: **pass** (all findings resolved)
- CI: **pass** (all checks green after Prettier fix)
- Merged: **2026-08-17T21:46:56Z** at commit `79e232f1c`

---

## 2. Plugin Registry (PR #2768 — OPEN)

https://github.com/kdlbs/kandev/pull/2768

Adds `kandev-plugin-youtrack` to the official `plugin-registry/plugins.yaml`:

```yaml
  - id: kandev-plugin-youtrack
    repo: ahmedbally/kandev-plugin-youtrack
    categories: [integrations]
```

---

## 3. Plugin: kandev-plugin-youtrack v0.1.0

https://github.com/ahmedbally/kandev-plugin-youtrack
Release: https://github.com/ahmedbally/kandev-plugin-youtrack/releases/tag/v0.1.0

### Architecture
- **External plugin** using `pkg/pluginsdk` only (never imports `internal/`)
- Per-workspace config via `Host.SetState` + `Host.SetSecret` (encrypted vault)
- Background poller goroutine for issue watchers
- No direct database access

### Backend (Go)

| File | Purpose |
|---|---|
| `cmd/kandev-plugin-youtrack/main.go` | Entry point: `StartPoller` + `pluginsdk.Serve` |
| `internal/plugin/plugin.go` | Plugin struct, per-workspace config loading via Host state + secrets |
| `internal/plugin/actions.go` | 13 workspace/task-scoped actions: `connection.check/save/delete`, `projects.list`, `issues.list/create_task/link`, `issue.change_state`, `watches.list/create/update/delete/trigger`, `context.options` |
| `internal/plugin/agent_tool.go` | `search_issues` MCP tool for kanban-task agents |
| `internal/plugin/webhook.go` | Inbound YouTrack webhook handler → task creation |
| `internal/plugin/watcher.go` | Watch model, background poller (30s tick), inflight cap, dedup via seen-issues map, prompt placeholder substitution |
| `internal/youtrack/client.go` | YouTrack REST client: enriched Issue model (assignee avatar, state, priority, tags, project, timestamps), `$top`/`$skip` pagination, bare-array + object response handling, relative avatar URL → absolute fix |

### Frontend (`ui/bundle.js`)

| Section | Implementation |
|---|---|
| **Dashboard** (`/youtrack`) | Jira-identical list toolbar (search + views popover + save-view popover + sort + refresh), filter bar (Project/State/Assignee popover pills with checkboxes), issue rows (key + summary + state badge + assignee avatar + updated + tags + Start task dropdown with presets), cursor pagination |
| **Connection card** | Enable toggle in section header (via `action` slot), `IntegrationAuthStatusBanner`, base URL + project + token + query fields, Test connection + Remove configuration, native floating Save/Reset bar via `useSettingsSaveContributor` |
| **Watchers section** | `SettingsSection` + `SettingsCard` + table (Enabled switch, query, interval, cap, created, Check now/Edit/Delete) + full create/edit dialog (query, workflow, step, repo, branch, agent, executor, prompt, interval, cap, enabled) |
| **Task presets section** | `SettingsSection` + `SettingsCard` with icon select + label + hint + edit prompt + trash remove rows, Add preset button, native save bar |
| **Registrations** | Nav item (integrations section), route, task action (link), keybinding, `registerIntegrationSettings` with `action: YouTrackEnableToggle` |
| **Boot-sync** | `initialize()` reads all workspaces' enabled state via `host.context.getWorkspaceIds()` and pushes to registry via `host.setIntegrationEnabled("youtrack", wsId, true)` |

### Manifest

```yaml
id: kandev-plugin-youtrack
api_version: 1
version: "0.1.0"
capabilities:
  api_read: [tasks, workflows, workspaces, repositories, agent_profiles]
  api_write: [tasks]
  state: true
  secrets: true
  user_state: true
```

### Actions (13)

| Key | Scope | Purpose |
|---|---|---|
| `connection.check` | workspace | Ping YouTrack `/api/users/me`, return config + connection status |
| `connection.save` | workspace | Save base URL + token + defaults to Host state/secrets |
| `connection.delete` | workspace | Remove config + secret + watches + dedup state |
| `projects.list` | workspace | List YouTrack projects for filter pill |
| `issues.list` | workspace | Search issues with query + pagination |
| `issues.create_task` | workspace | Create Kandev task from YouTrack issue |
| `issues.link` | task | Link YouTrack issue to existing Kandev task |
| `issue.change_state` | task | Change YouTrack issue state custom field |
| `watches.list` | workspace | List saved watchers |
| `watches.create` | workspace | Create new watcher |
| `watches.update` | workspace | Update watcher fields |
| `watches.delete` | workspace | Delete watcher |
| `watches.trigger` | workspace | Run watcher poll immediately |
| `context.options` | workspace | Return workflows/steps/repositories/agent/executor profiles for dialog |

---

## 4. Server Deployment

| Item | Value |
|---|---|
| Server | `192.168.1.200` (Ubuntu, systemd `kandev.service`) |
| Kandev version | Official v0.89.0 from npm (includes merged contribution) |
| Plugin | kandev-plugin-youtrack v0.1.0, active |
| Workspace | Salla (`73a6e4f3-...`), configured + enabled |
| YouTrack instance | `https://sallaops.youtrack.cloud` |
| Build method | Docker multi-stage on server (node:22-alpine for Vite, golang:1.26.1 for Go with CGO+fts5) |

---

## 5. What's left

| Item | Status |
|---|---|
| PR #2736 (host API) | **MERGED** |
| PR #2768 (registry) | **OPEN** — awaiting maintainer review |
| Plugin v0.1.0 | **Published** — GitHub release with tarball |
| Server | **Running** official v0.89.0 + plugin v0.1.0 |
| Loop workspace | Not enabled (left off to verify per-workspace isolation) |