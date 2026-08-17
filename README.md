# kandev-plugin-youtrack

A Kandev external plugin that integrates YouTrack issue tracking with Kandev tasks.

## Features

- **List YouTrack issues** in a dashboard inside Kandev (Settings → Plugins → YouTrack).
- **Start a Kandev task from a YouTrack issue** with one click; the issue id, url,
  and summary are written to the task's metadata, and the issue description becomes
  the task's description.
- **Link an existing Kandev task to a YouTrack issue** from the task's Link menu.
- **Change YouTrack issue state** from a linked task.
- **Optional inbound webhook**: point a YouTrack webhook at
  `https://<kandev>/api/plugins/kandev-plugin-youtrack/webhooks/youtrack` to create
  Kandev tasks automatically when issues match a configured query.
- **Agent tool**: `kandev_youtrack_v0_1_0_search_issues` exposed to kanban-task agents
  so an agent working on a task can search YouTrack without leaving the session.

## Auth model

YouTrack Hub permanent tokens (Profile → Account Security → New token) sent as
`Authorization: Bearer <token>`. The token is stored as a `secret: true` config field
in Kandev's encrypted vault; the browser never sees it. Every plugin backend call
reads it via the Host `GetConfig` RPC, which returns cleartext values to the
subprocess only.

## Build

```bash
go build -o server/plugin-$(go env GOOS)-$(go env GOARCH) ./cmd/kandev-plugin-youtrack
```

## Package and install

Use `kandev plugin-pack .` (or the `plugin-pack` tool from the kandev backend) to
produce `kandev-plugin-youtrack-0.1.0.tar.gz`, then install it from
Settings → Plugins → Install.

## Repository layout

```
manifest.yaml                 # plugin manifest (id, capabilities, actions, webhooks, agent_tools, ui)
go.mod                        # independent module; imports pkg/pluginsdk via replace
cmd/kandev-plugin-youtrack/   # main() -> pluginsdk.Serve
internal/plugin/              # Plugin + ActionHandler + AgentToolPlugin impl
internal/youtrack/            # REST client for the YouTrack Hub REST API
ui/bundle.js                  # host.jsx dashboard + nav item + task Link action + keybinding
```

## YouTrack REST endpoints used

| Purpose              | Endpoint                                           |
| -------------------- | -------------------------------------------------- |
| Current user probe   | `GET /api/users/me?fields=login,name,email`        |
| Search issues        | `GET /api/issues?query=<q>&$top=<n>&$skip=<s>&fields=id,idReadable,summary,description,resolved` |
| Get single issue     | `GET /api/issues/{idOrReadableId}?fields=...`     |
| List projects        | `GET /api/admin/projects?fields=id,name,shortName`|
| Change issue state   | `POST /api/issues/{id}?fields=...` (state custom field) |

The `fields` query parameter controls the returned projection, per YouTrack's REST
convention.