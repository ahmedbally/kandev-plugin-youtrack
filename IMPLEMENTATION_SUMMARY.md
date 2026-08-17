# YouTrack Plugin — Implementation Summary

## Plugin: `kandev-plugin-youtrack` v0.1.0

### Architecture
- **External plugin** (not built-in) using `pkg/pluginsdk`
- Per-workspace config via `Host.SetState` + `Host.SetSecret` (encrypted vault)
- Background poller goroutine for issue watchers
- No database access — all state through Host API

### Backend (Go)
| File | Purpose |
|---|---|
| `cmd/kandev-plugin-youtrack/main.go` | Entry point: `StartPoller` + `pluginsdk.Serve` |
| `internal/plugin/plugin.go` | Plugin struct, `loadConfig` (reads Host state + secrets per workspace) |
| `internal/plugin/actions.go` | 13 workspace/task-scoped actions: connection.check/save/delete, projects.list, issues.list/create_task/link, issue.change_state, watches.list/create/update/delete/trigger, context.options |
| `internal/plugin/agent_tool.go` | `search_issues` MCP tool for kanban-task agents |
| `internal/plugin/webhook.go` | Inbound YouTrack webhook → task creation |
| `internal/plugin/watcher.go` | Watch model, background poller (30s tick), inflight cap, dedup via seen-issues map, prompt placeholders |
| `internal/youtrack/client.go` | YouTrack REST client: enriched Issue model (assignee avatar, state, priority, tags, project, timestamps), search with `$top`/`$skip` pagination, bare-array + object response handling, relative avatar URL → absolute fix |

### Frontend (`ui/bundle.js`)
- **Dashboard** (`/youtrack`): Jira-identical list toolbar (search + views popover + save-view popover + sort + refresh), filter bar (Project/State/Assignee popover pills with checkboxes), issue rows (key + summary + state badge + assignee avatar + updated + tags + Start task dropdown with presets), cursor pagination
- **Integration settings**: Connection card (enable toggle in section header `action` slot, health banner, base URL + project + token + query fields, Test connection + Remove configuration), Watchers section (SettingsSection + SettingsCard + table + full dialog), Task presets section (icon select + label + hint + edit prompt + trash remove)
- **Registrations**: nav item (integrations section), route, task action (link), keybinding, `registerIntegrationSettings` with `action: YouTrackEnableToggle`
- **Boot-sync**: `initialize()` reads all workspaces' enabled state from `host.storage` and pushes to registry via `host.setIntegrationEnabled` — sidebar badges correct on reload without visiting the page

### Manifest
- `api_version: 1`, `version: "0.9.0"`
- Capabilities: `api_read: [tasks, workflows, workspaces, repositories, agent_profiles]`, `api_write: [tasks]`, `state`, `secrets`, `user_state`
- 13 actions, 1 webhook, 1 agent tool, UI bundle + keybinding

---

## Kandev Host Contributions

### 1. Plugin UI exports (`host.ui`)
Added to `PLUGIN_UI` in `host-api.ts` + `PluginUIShape` in SDK:
- `IntegrationAuthStatusBanner` — native auth health banner
- `IntegrationEnabledControl` — enable/disable switch with save-coordinator drafting
- `SettingsSection` / `SettingsCard` — native settings wrappers with dirty tracking
- `WorkspaceScopedSection` — per-workspace section resolution

### 2. `host.useSettingsSaveContributor`
- Exposed the existing settings-save coordinator hook to plugins
- Wired through `buildHostApi` + generation-fenced in `host-runtime-resources.ts`
- Plugin integration settings route is already inside `SettingsSaveProvider` (mounted at `settings-layout-client.tsx:124`)
- Plugins get the native floating Save changes/Reset bar for free

### 3. `host.setIntegrationEnabled(workspaceId, enabled)`
- New Host API method for per-workspace integration enabled state
- `pluginRegistry.setIntegrationEnabled(pluginId, wsId, enabled)` — no-op guard when unchanged (prevents flicker)
- `pluginRegistry.isIntegrationEnabled(pluginId, wsId)` — read by sidebar badge
- Reactive through existing `usePluginRegistry()` / `useSyncExternalStore` subscription
- Cleanup in `unregisterPlugin`

### 4. `registerIntegrationSettings({ ..., action })`
- Optional `action?: Component` rendered in the host's `SettingsSection` header action slot
- `plugin-integration-settings-route.tsx` passes it to `<SettingsSection action={...}>`
- Mirrors Jira's `<SettingsSection action={<JiraEnabledControl />}>` pattern

### 5. Sidebar enabled badge for plugin integrations
- `integration-enabled.tsx`: `IntegrationEnabledBadgeFor` now handles plugin slugs
- Resolves slug → pluginId via `getIntegrationSetting(slug)?.pluginId`
- Reads `registry.isIntegrationEnabled(pluginId, workspaceId)` — per-workspace, no cross-workspace bleed
- `BadgeWorkspaceContext` added to `IntegrationsEnabledProvider` so badge rows know their workspace
- No localStorage, no events, no listener hacks

### 6. `settings-menu-branches.ts`
- Plugin integration contributions now set `integrationSlug: id` — so the sidebar badge system probes them

### 7. SDK types (`packages/plugin-sdk/src/index.ts`)
- `PluginHostApi`: `useSettingsSaveContributor`, `setIntegrationEnabled`
- `PluginUIShape`: `IntegrationAuthStatusBanner`, `IntegrationEnabledControl`, `SettingsSection`, `SettingsCard`, `WorkspaceScopedSection`
- `registerIntegrationSettings`: `action?: Component`
- `SettingsSaveContributor`, `SettingsSaveRevision` types exported
- `utils.integrationStatusRefreshMs` constant

### 8. Test mocks updated
- `host.test.ts`, `host-initialize-timeout.test.ts`, `host-lifecycle.test.ts`, `host.repository-providers.test.ts`
- Added `useSettingsSaveContributor: () => {}` and `setIntegrationEnabled: () => {}`

---

## Files Changed

### Kandev host (`kandev-cloned/apps/`)
| File | Change |
|---|---|
| `packages/plugin-sdk/src/index.ts` | PluginUIShape + PluginHostApi additions + SettingsSaveContributor types |
| `web/lib/plugins/host-api.ts` | PLUGIN_UI exports + buildHostApi wiring + pluginRegistry import |
| `web/lib/plugins/host-runtime-resources.ts` | Generation fence for useSettingsSaveContributor + setIntegrationEnabled |
| `web/lib/plugins/registry.ts` | integrationEnabled map + set/is + unregisterPlugin cleanup |
| `web/lib/plugins/types.ts` | IntegrationSettingsRegistration.action |
| `web/lib/plugins/host*.test.ts` (4 files) | Mock updates |
| `web/components/app-sidebar/sections/settings/integration-enabled.tsx` | Registry-driven badge, WorkspaceIdContext, deleted localStorage hack |
| `web/components/app-sidebar/sections/settings/settings-menu-branches.ts` | integrationSlug on plugin contributions |
| `web/src/plugin-integration-settings-route.tsx` | Render registration.action in SettingsSection |

### Plugin (`kandev-plugin-youtrack/`)
| File | Change |
|---|---|
| `manifest.yaml` | v0.9.0, 13 actions, capabilities, agent tool |
| `ui/bundle.js` | Full rewrite: dashboard + settings + watchers + presets + boot-sync |
| `internal/plugin/plugin.go` | Per-workspace config via Host state + secrets |
| `internal/plugin/actions.go` | 13 action handlers |
| `internal/plugin/agent_tool.go` | search_issues MCP tool |
| `internal/plugin/webhook.go` | Inbound webhook handler |
| `internal/plugin/watcher.go` | Watch model + poller + inflight cap + dedup |
| `internal/youtrack/client.go` | Enriched REST client with avatar URL fix |
| `cmd/kandev-plugin-youtrack/main.go` | StartPoller + Serve |
| `Dockerfile` | Multi-stage: Vite frontend + Go backend (CGO+fts5) + plugin binaries |
| `go.mod` | Independent module, replace directive for local SDK |

---

## Deployment
- Custom Kandev binary built from source with host changes embedded (Vite frontend + Go CGO+fts5)
- Server: `192.168.1.200`, systemd service `kandev.service`
- Build: Docker multi-stage on server (node:22-alpine for Vite, golang:1.26.1 for Go)
- Plugin installed via `POST /api/plugins/install` with tarball

## Pending for upstream contribution
- PLUGIN-API.md / plugins-authoring.md docs update
- ADR for integration-settings plugin surface (extends ADR 2026-08-05)
- `sdk-contract.test.ts` verification
- Plugin repo publish to GitHub
- Marketplace `plugin-registry/plugins.yaml` entry