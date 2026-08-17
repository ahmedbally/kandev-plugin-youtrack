(function () {
  function safe(fn) {
    try { fn(); } catch (e) { console.warn("[kandev-plugin-youtrack]", e); }
  }

  function normalizeIssueId(reference) {
    if (typeof reference !== "string") return "";
    var trimmed = reference.trim();
    var idx = trimmed.indexOf("/issue/");
    if (idx >= 0) return trimmed.slice(idx + "/issue/".length).split(/[?#]/)[0];
    return trimmed;
  }

  function getActiveWorkspaceId(host) {
    try { return host.store.getState().workspaces.activeId || ""; } catch (e) { return ""; }
  }

  function relTime(host, iso) {
    if (!iso) return "";
    try { return host.utils.formatRelativeTime(iso); } catch (e) { return ""; }
  }

  var SORTS = [
    { key: "updated", label: "Recently updated" },
    { key: "created", label: "Newest" },
    { key: "priority", label: "Priority" },
  ];

  var PRIORITY_RANK = { critical: 0, showstopper: 0, high: 1, major: 1, medium: 2, normal: 2, minor: 3, low: 4 };

  function sortIssues(issues, sortKey) {
    var list = issues.slice();
    list.sort(function (a, b) {
      if (sortKey === "updated") return (b.updated || "").localeCompare(a.updated || "");
      if (sortKey === "created") return (b.created || "").localeCompare(a.created || "");
      if (sortKey === "priority") return rank(a) - rank(b);
      return 0;
    });
    return list;
    function rank(x) { return PRIORITY_RANK[(x.priority || "").toLowerCase()] ?? 9; }
  }

  function collectUnique(issues, field) {
    var seen = {};
    issues.forEach(function (it) { if (it[field]) seen[it[field]] = true; });
    return Object.keys(seen).sort();
  }

  window.registerKandevPlugin("kandev-plugin-youtrack", {
    initialize: function (registry, host) {
      var React = host.React;
      var jsx = host.jsx;
      var ui = host.ui;
      var useSave = typeof host.useSettingsSaveContributor === "function" ? host.useSettingsSaveContributor : null;
      var Banner = ui.IntegrationAuthStatusBanner || null;
      var Card = ui.SettingsCard || ui.Card;

      function YouTrackIcon(props) {
        var className = props && props.className ? props.className : "";
        return jsx("svg", { className: className, viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", "aria-hidden": true, "data-testid": "youtrack-icon" },
          jsx("path", { d: "M3 4h18l-7 8 7 8H3V4z" }),
          jsx("path", { d: "M8 8h6M8 12h4" }));
      }

      function Avatar(props) {
        var url = props.url, name = props.name || "?", size = props.size || 20;
        var initials = name.split(/\s+/).map(function (p) { return p.charAt(0); }).slice(0, 2).join("").toUpperCase();
        if (url) return jsx("img", { src: url, alt: name, title: name, width: size, height: size, style: { borderRadius: "9999px", flexShrink: 0 } });
        return jsx("span", { title: name, style: { width: size, height: size, borderRadius: "9999px", background: "var(--muted)", color: "var(--muted-foreground)", display: "inline-flex", alignItems: "center", justifyContent: "center", fontSize: Math.floor(size / 2.4), fontWeight: 600, flexShrink: 0 } }, initials);
      }

      function useYouTrackEnabled(wsId) {
        var state = React.useState(false);
        var enabled = state[0]; var setEnabled = state[1];
        React.useEffect(function () {
          if (!wsId) return;
          host.storage.get("workspace", wsId, "youtrack_enabled").then(function (entry) {
            var v = entry ? entry.value === true || entry.value === "true" : false;
            setEnabled(v);
            if (v && typeof host.setIntegrationEnabled === "function") host.setIntegrationEnabled(wsId, true);
          }, function () { setEnabled(false); });
        }, [wsId]);
        var persist = function (next) {
          setEnabled(next);
          if (wsId) host.storage.set("workspace", wsId, "youtrack_enabled", next);
          if (typeof host.setIntegrationEnabled === "function" && wsId) host.setIntegrationEnabled(wsId, next);
        };
        return { enabled: enabled, setEnabled: persist };
      }

      // Header toggle rendered in SettingsSection action slot via the host's
      // registerIntegrationSettings action prop. Falls back to the in-card
      // toggle in YouTrackIntegrationSettings when the host doesn't support
      // setIntegrationEnabled (stock 0.88.0).
      function YouTrackEnableToggle(props) {
        var wsId = (props && props.workspaceId) || getActiveWorkspaceId(host);
        var ctl = useYouTrackEnabled(wsId);
        return jsx(ui.Switch, {
          checked: ctl.enabled,
          onCheckedChange: ctl.setEnabled,
          "aria-label": "Enable YouTrack",
          "data-testid": "youtrack-enabled-toggle",
        });
      }

      // ── StartTaskMenu ─────────────────────────────────────────────────────
      function StartTaskMenu(props) {
        var openState = React.useState(false);
        var open = openState[0]; var setOpen = openState[1];
        var issue = props.issue, presets = props.presets || [], onLaunch = props.onLaunch;
        return jsx("div", { className: "relative" },
          jsx(ui.Button, { size: "sm", variant: "outline", className: "cursor-pointer h-7 px-2 gap-1 text-xs", "data-testid": "youtrack-create-task", onClick: function () { setOpen(function (p) { return !p; }); } },
            "+ Start task"),
          open ? jsx("div", { className: "absolute right-0 top-full mt-1 z-20 min-w-[200px] rounded-md border bg-popover shadow-md", "data-testid": "youtrack-preset-menu" },
            jsx("div", { className: "px-3 py-1.5 text-sm cursor-pointer hover:bg-muted/50", onClick: function () { setOpen(false); onLaunch(issue); } }, "Default"),
            presets.map(function (p) {
              return jsx("div", { key: p.id, title: p.hint || "", className: "px-3 py-1.5 text-sm cursor-pointer hover:bg-muted/50", onClick: function () { setOpen(false); onLaunch(issue, p); } },
                jsx("div", { className: "font-medium" }, p.label),
                p.hint ? jsx("div", { className: "text-[11px] text-muted-foreground" }, p.hint) : null);
            }),
          ) : null);
      }

      // ── FilterPill (Jira filter-pills.tsx) ────────────────────────────────
      function FilterPill(props) {
        var openState = React.useState(false);
        var open = openState[0]; var setOpen = openState[1];
        return jsx(ui.Popover, { open: open, onOpenChange: setOpen },
          jsx("div", { className: "inline-flex items-stretch rounded-md border text-xs overflow-hidden " + (props.active ? "border-primary/40 bg-primary/5" : "bg-background"), "data-testid": "youtrack-filter-pill-" + props.id },
            jsx(ui.PopoverTrigger, { asChild: true },
              jsx("button", { type: "button", className: "cursor-pointer px-2.5 py-1.5 flex items-center gap-1.5 hover:bg-muted/50 transition-colors" },
                jsx("span", { className: "text-muted-foreground" }, props.label),
                props.summary ? jsx("span", { className: "font-medium" }, props.summary) : null,
                jsx("span", { className: "text-muted-foreground", style: { fontSize: "0.75rem" } }, "\u25BE"))),
            props.active ? jsx("button", { type: "button", onClick: function () { props.onClear && props.onClear(); }, className: "cursor-pointer px-1.5 border-l hover:bg-muted flex items-center", title: "Clear" }, "\u00D7") : null),
          jsx(ui.PopoverContent, { align: "start", className: "w-64 p-0" }, props.children));
      }

      // ── IssueRow (Jira ticket-row.tsx) ────────────────────────────────────
      function IssueRow(props) {
        var issue = props.issue;
        return jsx("div", { className: "flex items-start gap-3 py-3 border-b last:border-b-0", "data-testid": "youtrack-issue-row" },
          jsx("div", { className: "flex-1 min-w-0 space-y-1" },
            jsx("div", { className: "flex items-center gap-2 text-xs text-muted-foreground" },
              jsx("span", { className: "font-mono" }, issue.idReadable),
              issue.project_short ? jsx("span", null, "\u00B7 ", issue.project_short) : null,
              issue.priority ? jsx("span", null, "\u00B7 ", issue.priority) : null),
            jsx("div", { className: "text-sm font-medium truncate", title: issue.summary }, issue.summary),
            jsx("div", { className: "flex items-center gap-2 flex-wrap" },
              issue.state ? jsx(ui.Badge, { variant: "outline", className: issue.state_resolved ? "border-green-500/30 bg-green-500/10 text-green-600" : "" }, issue.state) : null,
              issue.assignee_name
                ? jsx("div", { className: "flex items-center gap-1.5 min-w-0" },
                    jsx(Avatar, { url: issue.assignee_avatar, name: issue.assignee_name, size: 20 }),
                    jsx("span", { className: "text-xs text-muted-foreground truncate" }, issue.assignee_name))
                : jsx("span", { className: "text-xs text-muted-foreground" }, "Unassigned"),
              issue.updated ? jsx("span", { className: "text-xs text-muted-foreground" }, "Updated " + relTime(host, issue.updated)) : null,
              (issue.tags && issue.tags.length) ? issue.tags.slice(0, 3).map(function (t) { return jsx(ui.Badge, { key: t, variant: "outline", className: "text-[10px]" }, t); }) : null)),
          jsx("div", { className: "flex items-center gap-1 shrink-0" },
            jsx(ui.Button, { asChild: true, variant: "ghost", size: "sm", className: "cursor-pointer" },
              jsx("a", { href: issue.url, target: "_blank", rel: "noopener noreferrer", title: "Open in YouTrack" }, "\u2197")),
            jsx(StartTaskMenu, { issue: issue, presets: props.presets, onLaunch: props.onStartTask }),
          ));
      }

      // ── Dashboard (Jira list-toolbar + filter-bar + ticket-row) ──────────
      function YouTrackDashboard() {
        var wsId = getActiveWorkspaceId(host);
        var enabledCtl = useYouTrackEnabled(wsId);
        var connState = React.useState(null); var connection = connState[0]; var setConnection = connState[1];
        var issuesState = React.useState([]); var issues = issuesState[0]; var setIssues = issuesState[1];
        var loadingState = React.useState(false); var loading = loadingState[0]; var setLoading = loadingState[1];
        var queryState = React.useState("assignee: me"); var query = queryState[0]; var setQuery = queryState[1];
        var sortState = React.useState("updated"); var sortKey = sortState[0]; var setSort = sortState[1];
        var viewsState = React.useState([]); var views = viewsState[0]; var setViews = viewsState[1];
        var activeViewIdState = React.useState(null); var activeViewId = activeViewIdState[0]; var setActiveViewId = activeViewIdState[1];
        var presetsState = React.useState([]); var presets = presetsState[0]; var setPresets = presetsState[1];
        var projectsState = React.useState([]); var projects = projectsState[0]; var setProjects = projectsState[1];
        var filterState = React.useState({ projects: [], states: [], assignee: "anyone" }); var filters = filterState[0]; var setFilters = filterState[1];
        var cursorState = React.useState(""); var cursor = cursorState[0]; var setCursor = cursorState[1];
        var hasMoreState = React.useState(false); var hasMore = hasMoreState[0]; var setHasMore = hasMoreState[1];

        React.useEffect(function () {
          host.api.invokeAction("connection.check", { workspaceId: wsId })
            .then(function (result) { setConnection(result); if (result.default_query) setQuery(result.default_query); })
            .catch(function (err) { setConnection({ connected: false, error: err instanceof Error ? err.message : "Connection check failed" }); });
          host.storage.get("workspace", wsId, "youtrack_views").then(function (e) { if (e && Array.isArray(e.value)) setViews(e.value); }, function () {});
          host.storage.get("workspace", wsId, "youtrack_presets").then(function (e) { if (e && Array.isArray(e.value)) setPresets(e.value); }, function () {});
          host.api.invokeAction("projects.list", { workspaceId: wsId }).then(function (r) { setProjects(r.projects || []); }).catch(function () {});
        }, [wsId]);

        function load() {
          setLoading(true);
          host.api.invokeAction("issues.list", { workspaceId: wsId, body: { query: query, top: 100, cursor: cursor } })
            .then(function (result) {
              if (cursor) { setIssues(function (p) { return p.concat(result.issues || []); }); }
              else { setIssues(result.issues || []); }
              setHasMore(Boolean(result.has_more));
              setCursor(result.next_cursor || "");
            })
            .catch(function () {})
            .finally(function () { setLoading(false); });
        }

        React.useEffect(function () {
          if (connection && connection.connected && enabledCtl.enabled) { setCursor(""); setHasMore(false); load(); }
        }, [connection && connection.connected, enabledCtl.enabled]);

        function search() { setCursor(""); setHasMore(false); load(); }

        function createTask(issue, preset) {
          var prompt = "Work on YouTrack issue " + (issue.idReadable || issue.id) + ": " + issue.summary + "\n\n" + (issue.description || "");
          if (preset && preset.prompt_template) {
            prompt = preset.prompt_template
              .split("{key}").join(issue.idReadable || "")
              .split("{url}").join(issue.url || "")
              .split("{title}").join(issue.summary || "")
              .split("{description}").join(issue.description || "");
          }
          host.api.invokeAction("issues.create_task", { workspaceId: wsId, body: { issue_id: issue.idReadable || issue.id, start_agent: true, agent_prompt: prompt } })
            .then(function (r) { if (r && r.task_id) host.navigate("/t/" + r.task_id); })
            .catch(function () {});
        }

        function saveView(name) {
          var next = views.filter(function (v) { return v.id !== name; }).concat([{ id: name, name: name, query: query, sort: sortKey, filters: filters, builtin: false }]);
          setViews(next); setActiveViewId(name);
          host.storage.set("workspace", wsId, "youtrack_views", next);
        }
        function applyView(v) { setQuery(v.query); setSort(v.sort || "updated"); setFilters(Object.assign({ projects: [], states: [], assignee: "anyone" }, v.filters)); setActiveViewId(v.id); search(); }
        function deleteView(id) { var next = views.filter(function (v) { return v.id !== id; }); setViews(next); if (activeViewId === id) setActiveViewId(null); host.storage.set("workspace", wsId, "youtrack_views", next); }

        var stateOptions = collectUnique(issues, "state");
        var filtered = issues.filter(function (it) {
          if (filters.projects.length && filters.projects.indexOf(it.project_short) < 0) return false;
          if (filters.states.length && filters.states.indexOf(it.state) < 0) return false;
          if (filters.assignee === "me" && it.assignee_name !== (connection && connection.login)) return false;
          if (filters.assignee === "unassigned" && it.assignee_name) return false;
          return true;
        });
        var sorted = sortIssues(filtered, sortKey);
        var activeView = views.find(function (v) { return v.id === activeViewId; });
        var hasActiveFilters = filters.projects.length > 0 || filters.states.length > 0 || filters.assignee !== "anyone";

        var checking = !connection;
        var notConfigured = connection && !connection.connected && connection.error && connection.error.indexOf("not configured") >= 0;

        if (checking) return jsx("div", { className: "flex items-center justify-center py-20" }, jsx(ui.Spinner, null));
        if (notConfigured) return jsx("div", { className: "flex flex-col items-center justify-center py-20 text-center" },
          jsx(YouTrackIcon, { className: "h-10 w-10 text-muted-foreground" }),
          jsx("h2", { className: "text-lg font-semibold mt-4" }, "YouTrack is not configured"),
          jsx("p", { className: "text-sm text-muted-foreground mt-1" }, "Configure YouTrack in Settings > Integrations to start using it."),
          jsx(ui.Button, { className: "mt-4 cursor-pointer", onClick: function () { host.navigate(wsId ? "/settings/workspaces/" + wsId + "/integrations/youtrack" : "/settings/integrations/youtrack"); } }, "Configure YouTrack"));
        if (!enabledCtl.enabled) return jsx("div", { className: "flex flex-col items-center justify-center py-20 text-center" },
          jsx(YouTrackIcon, { className: "h-10 w-10 text-muted-foreground" }),
          jsx("h2", { className: "text-lg font-semibold mt-4" }, "YouTrack is disabled"),
          jsx("p", { className: "text-sm text-muted-foreground mt-1" }, "Enable YouTrack in Settings > Integrations for this workspace."),
          jsx("div", { className: "flex gap-2 mt-4" },
            jsx(ui.Button, { className: "cursor-pointer", onClick: function () { host.navigate(wsId ? "/settings/workspaces/" + wsId + "/integrations/youtrack" : "/settings/integrations/youtrack"); } }, "Open settings"),
            jsx(ui.Button, { variant: "outline", className: "cursor-pointer", onClick: function () { enabledCtl.setEnabled(true); } }, "Enable now")));

        return jsx("div", { className: "flex flex-col h-full", "data-testid": "youtrack-dashboard" },
          jsx("div", { className: "flex items-center gap-2 px-6 py-2.5 border-b shrink-0 flex-wrap" },
            jsx("div", { className: "relative flex-1 max-w-md min-w-[200px]" },
              jsx("span", { className: "absolute left-2.5 top-1/2 -translate-y-1/2 text-muted-foreground pointer-events-none text-sm" }, "\u2315"),
              jsx(ui.Input, { "data-testid": "youtrack-query-input", value: query, onChange: function (e) { setQuery(e.target.value); }, onKeyDown: function (e) { if (e.key === "Enter") search(); }, placeholder: "Search YouTrack issues", className: "h-8 text-xs pl-8" })),
            jsx(ui.Popover, null,
              jsx(ui.PopoverTrigger, { asChild: true },
                jsx(ui.Button, { variant: "outline", size: "sm", className: "cursor-pointer h-8 text-xs gap-1.5" }, "\u2691 ", activeView ? activeView.name : "No view")),
              jsx(ui.PopoverContent, { align: "start", className: "w-60 p-0" },
                views.length === 0 ? jsx("div", { className: "px-3 py-2 text-xs text-muted-foreground" }, "No saved views") :
                  views.map(function (v) {
                    return jsx("div", { key: v.id, className: "group flex items-center px-2" },
                      jsx("button", { type: "button", onClick: function () { applyView(v); }, className: "flex-1 flex items-center gap-2 px-2 py-1.5 text-sm cursor-pointer rounded hover:bg-muted/50" },
                        activeViewId === v.id ? jsx("span", { className: "text-xs" }, "\u2713") : jsx("span", { className: "text-xs opacity-0" }, "\u2713"),
                        jsx("span", { className: "truncate" }, v.name)),
                      !v.builtin ? jsx("button", { type: "button", onClick: function (e) { e.stopPropagation(); deleteView(v.id); }, className: "cursor-pointer opacity-0 group-hover:opacity-100 p-1 rounded hover:bg-muted" }, "\u00D7") : null);
                  }))),
            jsx(ui.Popover, null,
              jsx(ui.PopoverTrigger, { asChild: true },
                jsx(ui.Button, { variant: "ghost", size: "sm", className: "cursor-pointer h-8 text-xs gap-1.5", title: "Save current filters as a view" }, "+ Save view")),
              jsx(ui.PopoverContent, { align: "start", className: "w-64 p-3 space-y-2" },
                jsx("div", { className: "text-xs font-semibold" }, "Save current filters as"),
                jsx(ui.Input, { autoFocus: true, placeholder: "My open bugs", className: "h-8 text-xs", onKeyDown: function (e) { if (e.key === "Enter") { var n = e.target.value.trim(); if (n) { saveView(n); e.target.value = ""; } } } }),
                jsx("div", { className: "flex justify-end gap-1" },
                  jsx(ui.Button, { size: "sm", variant: "ghost", className: "cursor-pointer h-7 text-xs" }, "Cancel"),
                  jsx(ui.Button, { size: "sm", className: "cursor-pointer h-7 text-xs", onClick: function (e) { var inp = e.currentTarget.parentElement.parentElement.querySelector("input"); var n = inp.value.trim(); if (n) { saveView(n); inp.value = ""; } } }, "Save")))),
            jsx("div", { className: "ml-auto flex items-center gap-1" },
              jsx("span", { className: "text-xs text-muted-foreground tabular-nums mr-2" }, loading ? "Loading..." : sorted.length + " issues"),
              jsx(ui.Select, { value: sortKey, onValueChange: setSort },
                jsx(ui.SelectTrigger, { className: "h-7 text-xs gap-1.5 cursor-pointer", "data-testid": "youtrack-sort" }, jsx(ui.SelectValue, null)),
                jsx(ui.SelectContent, null, SORTS.map(function (s) { return jsx(ui.SelectItem, { key: s.key, value: s.key, className: "cursor-pointer" }, s.label); }))),
              jsx(ui.Button, { variant: "ghost", size: "sm", onClick: search, disabled: loading, className: "cursor-pointer h-7 w-7", title: "Refresh" }, loading ? "\u21BB" : "\u21BA"))),

          jsx("div", { className: "flex items-center gap-2 flex-wrap px-6 py-2.5 border-b shrink-0 bg-muted/20", "data-testid": "youtrack-filters" },
            jsx(FilterPill, { id: "project", label: "Project", summary: filters.projects.length > 0 ? filters.projects.join(", ") : null, active: filters.projects.length > 0, onClear: function () { setFilters(function (f) { return Object.assign({}, f, { projects: [] }); }); } },
              projects.map(function (p) {
                var checked = filters.projects.indexOf(p.shortName) >= 0;
                return jsx("label", { key: p.id, className: "flex items-center gap-2 px-3 py-1.5 cursor-pointer hover:bg-muted/50" },
                  jsx(ui.Checkbox, { checked: checked, onCheckedChange: function () { setFilters(function (f) { var arr = checked ? f.projects.filter(function (x) { return x !== p.shortName; }) : f.projects.concat([p.shortName]); return Object.assign({}, f, { projects: arr }); }); } }),
                  jsx("span", { className: "font-mono text-xs" }, p.shortName),
                  jsx("span", { className: "text-xs text-muted-foreground truncate" }, p.name));
              })),
            jsx(FilterPill, { id: "state", label: "State", summary: filters.states.length > 0 ? filters.states.join(", ") : null, active: filters.states.length > 0, onClear: function () { setFilters(function (f) { return Object.assign({}, f, { states: [] }); }); } },
              stateOptions.map(function (s) {
                var checked = filters.states.indexOf(s) >= 0;
                return jsx("label", { key: s, className: "flex items-center gap-2 px-3 py-1.5 cursor-pointer hover:bg-muted/50" },
                  jsx(ui.Checkbox, { checked: checked, onCheckedChange: function () { setFilters(function (f) { var arr = checked ? f.states.filter(function (x) { return x !== s; }) : f.states.concat([s]); return Object.assign({}, f, { states: arr }); }); } }),
                  jsx("span", { className: "text-sm" }, s));
              })),
            jsx(FilterPill, { id: "assignee", label: "Assignee", summary: filters.assignee !== "anyone" ? (filters.assignee === "me" ? "Me" : "Unassigned") : null, active: filters.assignee !== "anyone", onClear: function () { setFilters(function (f) { return Object.assign({}, f, { assignee: "anyone" }); }); } },
              [["anyone", "Anyone"], ["me", "Me"], ["unassigned", "Unassigned"]].map(function (o) {
                return jsx("button", { key: o[0], type: "button", onClick: function () { setFilters(function (f) { return Object.assign({}, f, { assignee: o[0] }); }); }, className: "w-full text-left px-3 py-1.5 text-sm cursor-pointer hover:bg-muted/50" + (filters.assignee === o[0] ? " font-medium" : "") }, o[1]);
              })),
            hasActiveFilters ? jsx(ui.Button, { variant: "ghost", size: "sm", onClick: function () { setFilters({ projects: [], states: [], assignee: "anyone" }); }, className: "cursor-pointer h-7 text-xs ml-1" }, "Clear filters") : null),

          jsx("div", { className: "flex-1 overflow-y-auto px-6", "data-testid": "youtrack-issue-list" },
            sorted.length === 0 && !loading ? jsx("div", { className: "py-20 text-center text-sm text-muted-foreground" }, "No issues match the current query and filters.") : null,
            sorted.map(function (issue) { return jsx(IssueRow, { key: issue.idReadable || issue.id, issue: issue, presets: presets, onStartTask: createTask }); }),
            hasMore ? jsx("div", { className: "py-4 text-center" },
              jsx(ui.Button, { variant: "outline", size: "sm", "data-testid": "youtrack-load-more", disabled: loading, onClick: function () { load(); }, className: "cursor-pointer" }, loading ? "Loading..." : "Load more")) : null));
      }

      // ── Watchers (Jira-identical SettingsSection + SettingsCard) ─────────
      function YouTrackWatchersSection(props) {
        var wsId = props && props.workspaceId ? props.workspaceId : "";
        var watchesState = React.useState([]); var watches = watchesState[0]; var setWatches = watchesState[1];
        var loadingState = React.useState(true); var loading = loadingState[0]; var setLoading = loadingState[1];
        var dialogState = React.useState(null); var dialog = dialogState[0]; var setDialog = dialogState[1];
        var busyState = React.useState(false); var busy = busyState[0]; var setBusy = busyState[1];

        function load() { setLoading(true); host.api.invokeAction("watches.list", { workspaceId: wsId }).then(function (r) { setWatches(r.watches || []); }).catch(function () {}).finally(function () { setLoading(false); }); }
        React.useEffect(function () { load(); }, [wsId]);

        function remove(w) { if (!window.confirm("Delete this watcher?")) return; host.api.invokeAction("watches.delete", { workspaceId: wsId, body: { id: w.id } }).then(load).catch(function (e) { host.toast && host.toast.error(String(e)); }); }
        function trigger(w) { setBusy(true); host.api.invokeAction("watches.trigger", { workspaceId: wsId, body: { id: w.id } }).then(function (r) { host.toast && host.toast.success(r.new_tasks > 0 ? "Created " + r.new_tasks + " new task(s)" : "No new matching issues"); }).catch(function (e) { host.toast && host.toast.error(String(e)); }).finally(function () { setBusy(false); }); }
        function toggle(w, enabled) { host.api.invokeAction("watches.update", { workspaceId: wsId, body: { id: w.id, enabled: enabled } }).then(load).catch(function (e) { host.toast && host.toast.error(String(e)); }); }

        return jsx("section", { className: "space-y-4", "data-testid": "youtrack-watchers-section" },
          // Header — Jira: flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between
          jsx("div", { className: "flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between" },
            jsx("div", { className: "min-w-0" },
              jsx("div", { className: "flex items-center gap-2" },
                jsx("h3", { className: "text-lg font-semibold flex items-center gap-2" },
                  jsx(YouTrackIcon, { className: "h-5 w-5" }),
                  "YouTrack watchers")),
              jsx("p", { className: "text-sm text-muted-foreground mt-1" }, "Poll a YouTrack query and auto-create a Kandev task for each newly-matching issue.")),
            jsx("div", { className: "w-full shrink-0 sm:w-auto" },
              jsx(ui.Button, { size: "sm", className: "cursor-pointer", onClick: function () { setDialog({ mode: "create" }); }, "data-testid": "youtrack-new-watcher" }, "+ New watcher"))),
          // Card — Jira: SettingsCard with CardContent pt-6
          jsx(Card, { "data-testid": "youtrack-watchers-card" },
            jsx(ui.CardContent, { className: "pt-6" },
              loading ? jsx("div", { className: "py-4 text-center" }, jsx(ui.Spinner, null)) :
              watches.length === 0 ? jsx("p", { className: "text-sm text-muted-foreground py-4 text-center" }, "No YouTrack watchers configured. Create one to auto-create tasks from queries.") :
              jsx(ui.Table, null,
                jsx(ui.TableHeader, null, jsx(ui.TableRow, null,
                  jsx(ui.TableHead, null, "Enabled"),
                  jsx(ui.TableHead, null, "Query"),
                  jsx(ui.TableHead, null, "Every"),
                  jsx(ui.TableHead, null, "Cap"),
                  jsx(ui.TableHead, null, "Created"),
                  jsx(ui.TableHead, { className: "text-right" }, "Actions"))),
                jsx(ui.TableBody, null, watches.map(function (w) {
                  return jsx(ui.TableRow, { key: w.id, "data-testid": "youtrack-watch-row" },
                    jsx(ui.TableCell, null, jsx(ui.Switch, { checked: w.enabled, onCheckedChange: function (v) { toggle(w, v); }, "aria-label": "Pause or resume polling" })),
                    jsx(ui.TableCell, { className: "font-medium" }, w.query),
                    jsx(ui.TableCell, null, Math.round(w.interval_seconds / 60) + " min"),
                    jsx(ui.TableCell, null, w.max_inflight > 0 ? String(w.max_inflight) : "\u2014"),
                    jsx(ui.TableCell, { className: "text-muted-foreground text-xs" }, relTime(host, w.created_at)),
                    jsx(ui.TableCell, { className: "text-right" },
                      jsx("div", { className: "inline-flex gap-1.5" },
                        jsx(ui.Button, { size: "sm", variant: "outline", disabled: busy, onClick: function () { trigger(w); }, className: "cursor-pointer" }, "Check now"),
                        jsx(ui.Button, { size: "sm", variant: "outline", onClick: function () { setDialog({ mode: "edit", watch: w }); }, className: "cursor-pointer" }, "Edit"),
                        jsx(ui.Button, { size: "sm", variant: "destructive", onClick: function () { remove(w); }, className: "cursor-pointer" }, "Delete"))));
                }))))),
          dialog ? jsx(WatchDialog, { wsId: wsId, mode: dialog.mode, watch: dialog.watch, onClose: function () { setDialog(null); }, onSaved: function () { setDialog(null); load(); } }) : null);
      }

      function WatchDialog(props) {
        var isEdit = props.mode === "edit"; var w = props.watch || {};
        var qS = React.useState(w.query || ""); var query = qS[0]; var setQuery = qS[1];
        var wfS = React.useState(w.workflow_id || ""); var workflowId = wfS[0]; var setWorkflow = wfS[1];
        var stS = React.useState(w.workflow_step_id || ""); var stepId = stS[0]; var setStep = stS[1];
        var rS = React.useState(w.repository_id || ""); var repoId = rS[0]; var setRepo = rS[1];
        var bS = React.useState(w.base_branch || ""); var branch = bS[0]; var setBranch = bS[1];
        var aS = React.useState(w.agent_profile_id || ""); var agentId = aS[0]; var setAgent = aS[1];
        var eS = React.useState(w.executor_profile_id || ""); var execId = eS[0]; var setExec = eS[1];
        var pS = React.useState(w.prompt || ""); var prompt = pS[0]; var setPrompt = pS[1];
        var iS = React.useState(String(w.interval_seconds || 300)); var interval = iS[0]; var setInterval_ = iS[1];
        var cS = React.useState(w.max_inflight > 0 ? String(w.max_inflight) : ""); var cap = cS[0]; var setCap = cS[1];
        var enS = React.useState(w.enabled !== false); var enabled = enS[0]; var setEnabled = enS[1];
        var svS = React.useState(false); var saving = svS[0]; var setSaving = svS[1];
        var oS = React.useState({ workflows: [], repositories: [], agent_profiles: [], executor_profiles: [] }); var opts = oS[0]; var setOpts = oS[1];
        React.useEffect(function () { host.api.invokeAction("context.options", { workspaceId: props.wsId }).then(setOpts).catch(function () {}); }, [props.wsId]);
        var steps = (opts.workflows.find(function (wf) { return wf.id === workflowId; }) || {}).steps || [];
        var repo = opts.repositories.find(function (r) { return r.id === repoId; });
        function submit() {
          setSaving(true);
          var body = { query: query, interval_seconds: parseInt(interval, 10) || 300, prompt: prompt, workflow_id: workflowId, workflow_step_id: stepId, repository_id: repoId, base_branch: branch, agent_profile_id: agentId, executor_profile_id: execId, enabled: enabled };
          var capNum = parseInt(cap, 10); body.max_inflight = (!isNaN(capNum) && capNum > 0) ? capNum : 0;
          var call = isEdit ? host.api.invokeAction("watches.update", { workspaceId: props.wsId, body: Object.assign({ id: props.watch.id }, body) }) : host.api.invokeAction("watches.create", { workspaceId: props.wsId, body: body });
          call.then(function () { setSaving(false); props.onSaved(); }).catch(function (e) { setSaving(false); host.toast && host.toast.error(String(e)); });
        }
        var ls = "text-sm font-semibold"; var hs = "text-xs text-muted-foreground mt-1"; var fg = "mb-4";
        return jsx("div", { className: "fixed inset-0 bg-black/50 flex items-center justify-center z-50", onClick: function (e) { if (e.target === e.currentTarget) props.onClose(); } },
          jsx("div", { className: "bg-card border rounded-xl p-5 w-[min(560px,94vw)] max-h-[90vh] overflow-y-auto", "data-testid": "youtrack-watch-dialog" },
            jsx("h3", { className: "font-semibold mb-1" }, isEdit ? "Edit watcher" : "Create YouTrack watcher"),
            jsx("p", { className: "text-sm text-muted-foreground mb-4" }, "Poll a YouTrack query and auto-create a Kandev task for each newly-matching issue. Optionally bind a repository so each task runs against that codebase, or leave it unset to run with no repository."),
            jsx("div", { className: fg }, jsx("div", { className: ls }, "Query"), jsx(ui.Input, { "data-testid": "youtrack-watch-query", value: query, placeholder: "for: me state: Open", onChange: function (e) { setQuery(e.target.value); }, className: "mt-1" }), jsx("p", { className: hs }, "YouTrack search query. The watcher polls this query and creates one task per newly-matching issue.")),
            jsx("div", { className: fg }, jsx("div", { className: ls }, "Workflow"), jsx(ui.Select, { value: workflowId, onValueChange: function (v) { setWorkflow(v); setStep(""); } }, jsx(ui.SelectTrigger, { className: "w-full cursor-pointer mt-1" }, jsx(ui.SelectValue, { placeholder: "Select workflow" })), jsx(ui.SelectContent, null, opts.workflows.map(function (wf) { return jsx(ui.SelectItem, { key: wf.id, value: wf.id, className: "cursor-pointer" }, wf.name); }))), jsx("p", { className: hs }, "Tasks are created in this workflow.")),
            jsx("div", { className: fg }, jsx("div", { className: ls }, "Initial step"), jsx(ui.Select, { value: stepId, onValueChange: setStep, disabled: !workflowId }, jsx(ui.SelectTrigger, { className: "w-full cursor-pointer mt-1" }, jsx(ui.SelectValue, { placeholder: workflowId ? "Select a step" : "Select a workflow first" })), jsx(ui.SelectContent, null, steps.map(function (s) { return jsx(ui.SelectItem, { key: s.id, value: s.id, className: "cursor-pointer" }, s.name); }))), jsx("p", { className: hs }, "Optional; falls back to step default.")),
            jsx("div", { className: fg }, jsx("div", { className: ls }, "Repository"), jsx(ui.Select, { value: repoId, onValueChange: function (v) { setRepo(v); if (!branch) { var r = opts.repositories.find(function (x) { return x.id === v; }); if (r && r.default_branch) setBranch(r.default_branch); } } }, jsx(ui.SelectTrigger, { className: "w-full cursor-pointer mt-1" }, jsx(ui.SelectValue, { placeholder: "(no repository)" })), jsx(ui.SelectContent, null, opts.repositories.map(function (r) { return jsx(ui.SelectItem, { key: r.id, value: r.id, className: "cursor-pointer" }, r.name); }))), jsx("p", { className: hs }, "Optional; the repository the agent works in.")),
            jsx("div", { className: fg }, jsx("div", { className: ls }, "Base branch"), jsx(ui.Input, { value: branch, placeholder: repoId ? (repo && repo.default_branch) || "main" : "Pick a repository first", disabled: !repoId, onChange: function (e) { setBranch(e.target.value); }, className: "mt-1" }), jsx("p", { className: hs }, "The base branch the agent starts from.")),
            jsx("div", { className: "grid grid-cols-2 gap-3 mb-4" },
              jsx("div", null, jsx("div", { className: ls }, "Agent profile"), jsx(ui.Select, { value: agentId, onValueChange: setAgent }, jsx(ui.SelectTrigger, { className: "w-full cursor-pointer mt-1" }, jsx(ui.SelectValue, { placeholder: "(step default)" })), jsx(ui.SelectContent, null, opts.agent_profiles.map(function (a) { return jsx(ui.SelectItem, { key: a.id, value: a.id, className: "cursor-pointer" }, a.name); }))), jsx("p", { className: hs }, "Optional; falls back to step default.")),
              jsx("div", null, jsx("div", { className: ls }, "Executor profile"), jsx(ui.Select, { value: execId, onValueChange: setExec }, jsx(ui.SelectTrigger, { className: "w-full cursor-pointer mt-1" }, jsx(ui.SelectValue, { placeholder: "(step default)" })), jsx(ui.SelectContent, null, opts.executor_profiles.map(function (x) { return jsx(ui.SelectItem, { key: x.id, value: x.id, className: "cursor-pointer" }, x.name); }))), jsx("p", { className: hs }, "Optional; falls back to step default."))),
            jsx("div", { className: fg }, jsx("div", { className: ls }, "Prompt"), jsx(ui.Textarea, { value: prompt, placeholder: "Use {key}, {url}, {title}, {description} as placeholders.", onChange: function (e) { setPrompt(e.target.value); }, className: "mt-1 min-h-[80px]" }), jsx("p", { className: hs }, "The prompt sent to the agent for each new issue.")),
            jsx("div", { className: "grid grid-cols-2 gap-3 mb-4" },
              jsx("div", null, jsx("div", { className: ls }, "Poll interval (seconds)"), jsx(ui.Input, { type: "number", min: "60", max: "3600", value: interval, onChange: function (e) { setInterval_(e.target.value); }, className: "mt-1" }), jsx("p", { className: hs }, "How often to re-run the query. Minimum 60s, maximum 3600s.")),
              jsx("div", null, jsx("div", { className: ls }, "Max open tasks"), jsx(ui.Input, { type: "number", min: "0", placeholder: "no cap", value: cap, onChange: function (e) { setCap(e.target.value); }, className: "mt-1" }), jsx("p", { className: hs }, "Cap on open tasks created by this watcher. Leave blank for no cap."))),
            jsx("div", { className: "flex items-center gap-2 mb-4" }, jsx(ui.Switch, { checked: enabled, onCheckedChange: setEnabled, "aria-label": "Pause or resume polling" }), jsx("span", { className: "text-sm" }, enabled ? "Polling enabled" : "Paused")),
            jsx("div", { className: "flex gap-2 justify-end" },
              jsx(ui.Button, { variant: "outline", onClick: props.onClose, className: "cursor-pointer" }, "Cancel"),
              jsx(ui.Button, { "data-testid": "youtrack-watch-submit", disabled: !query.trim() || saving, onClick: submit, className: "cursor-pointer" }, saving ? "Saving..." : isEdit ? "Save watcher" : "Create watcher"))));
      }

      // ── Task presets (Jira-identical: icon+label+hint rows in SettingsCard) ─
      function defaultPresets() {
        return [
          { id: "p_fix", icon: "code", label: "Implement", hint: "Build the change, open a PR", prompt_template: "Fix YouTrack issue {key}: {title}\n\n{description}\n\nImplement, test, and verify the fix." },
          { id: "p_plan", icon: "search", label: "Investigate", hint: "Find the root cause", prompt_template: "Investigate {key}: {title}\n\n{description}\n\nFind the root cause and propose a fix plan." },
        ];
      }

      // Small inline-SVG icon set standing in for tabler icons (plugins cannot
      // import @tabler/icons-react; these mirror the shapes Jira's picker uses).
      var PRESET_ICON_PATHS = {
        code: jsx("path", { d: "M7 8l-4 4l4 4M17 8l4 4l-4 4M14 4l-4 16" }),
        search: [jsx("path", { key: "c", d: "M3 10a7 7 0 1 0 14 0a7 7 0 1 0 -14 0" }), jsx("path", { key: "l", d: "M21 21l-6 -6" })],
        bug: jsx("path", { d: "M9 19c-4.3 1.4 -4.3 -2.5 -6 -3m12 5v-3.5c0 -1 .1 -1.4 -.5 -2c2.8 -.3 5.5 -1.4 5.5 -6a4.6 4.6 0 0 0 -1.3 -3.2a4.2 4.2 0 0 0 -.1 -3.2s-1.1 -.3 -3.5 1.3a12.3 12.3 0 0 0 -6.2 0c-2.4 -1.6 -3.5 -1.3 -3.5 -1.3a4.2 4.2 0 0 0 -.1 3.2a4.6 4.6 0 0 0 -1.3 3.2c0 4.6 2.7 5.7 5.5 6c-.6 .6 -.6 1.2 -.5 2v3.5" }),
        rocket: [jsx("path", { key: "r1", d: "M4 13a8 8 0 0 1 7 7a6 6 0 0 0 3 -5a9 9 0 0 0 6 -9a3 3 0 0 0 -3 -3a9 9 0 0 0 -9 6a6 6 0 0 0 -5 3" }), jsx("path", { key: "r2", d: "M7 9a2 2 0 1 0 4 0a2 2 0 0 0 -4 0" })],
        sparkle: [jsx("path", { key: "s1", d: "M12 3l1.9 5.8a2 2 0 0 0 1.3 1.3l5.8 1.9l-5.8 1.9a2 2 0 0 0 -1.3 1.3l-1.9 5.8l-1.9 -5.8a2 2 0 0 0 -1.3 -1.3l-5.8 -1.9l5.8 -1.9a2 2 0 0 0 1.3 -1.3z" })],
        wand: [jsx("path", { key: "w1", d: "M6 18l12 -12" }), jsx("path", { key: "w2", d: "M14 4l0 0M20 10l0 0M4 8l0 0M8 4l0 0" })],
      };
      var PRESET_ICON_KEYS = Object.keys(PRESET_ICON_PATHS);

      function PresetIconGlyph(props) {
        var icon = PRESET_ICON_PATHS[props.icon] || PRESET_ICON_PATHS.code;
        return jsx("svg", { xmlns: "http://www.w3.org/2000/svg", width: "24", height: "24", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", "stroke-width": "2", "stroke-linecap": "round", "stroke-linejoin": "round", className: props.className || "h-4 w-4" }, icon);
      }

      function TrashIcon() {
        return jsx("svg", { xmlns: "http://www.w3.org/2000/svg", width: "24", height: "24", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", "stroke-width": "2", "stroke-linecap": "round", "stroke-linejoin": "round", className: "h-3.5 w-3.5" },
          jsx("path", { d: "M4 7l16 0" }),
          jsx("path", { d: "M10 11l0 6" }),
          jsx("path", { d: "M14 11l0 6" }),
          jsx("path", { d: "M5 7l1 12a2 2 0 0 0 2 2h8a2 2 0 0 0 2 -2l1 -12" }),
          jsx("path", { d: "M9 7v-3a1 1 0 0 1 1 -1h4a1 1 0 0 1 1 1v3" }));
      }

      function YouTrackPresetsSection(props) {
        var wsId = props && props.workspaceId ? props.workspaceId : "";
        var savedState = React.useState(defaultPresets()); var saved = savedState[0]; var setSaved = savedState[1];
        var draftState = React.useState(null); var draft = draftState[0]; var setDraft = draftState[1];
        var loadedState = React.useState(false); var loaded = loadedState[0]; var setLoaded = loadedState[1];
        var expandedState = React.useState(""); var expanded = expandedState[0]; var setExpanded = expandedState[1];

        React.useEffect(function () {
          host.storage.get("workspace", wsId, "youtrack_presets").then(function (entry) {
            if (entry && Array.isArray(entry.value)) { setSaved(entry.value); setDraft(entry.value.slice()); }
            else { setDraft(defaultPresets().slice()); }
            setLoaded(true);
          }, function () { setDraft(defaultPresets().slice()); setLoaded(true); });
        }, [wsId]);

        var dirty = loaded && JSON.stringify(draft || []) !== JSON.stringify(saved);

        function patch(id, field, value) { setDraft(function (prev) { return (prev || []).map(function (p) { if (p.id !== id) return p; var n = Object.assign({}, p); n[field] = value; return n; }); }); }
        function addPreset() { setDraft(function (prev) { return (prev || []).concat([{ id: "p_" + Date.now().toString(36), icon: "code", label: "", hint: "", prompt_template: "" }]); }); }
        function removePreset(id) { setDraft(function (prev) { return (prev || []).filter(function (p) { return p.id !== id; }); }); }
        function persist() { host.storage.set("workspace", wsId, "youtrack_presets", draft || []); setSaved((draft || []).slice()); }
        function discard() { setDraft(saved.slice()); }

        if (useSave) { useSave({ id: "youtrack-presets-" + wsId, revision: JSON.stringify(draft || []), isDirty: dirty, save: persist, discard: discard }); }
        if (!loaded) return jsx("div", { className: "py-4 text-center" }, jsx(ui.Spinner, { size: "sm" }));

        return jsx("section", { className: "space-y-4", "data-testid": "youtrack-presets-section" },
          // Header — Jira: flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between + Reset action
          jsx("div", { className: "flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between" },
            jsx("div", { className: "min-w-0" },
              jsx("div", { className: "flex items-center gap-2" },
                jsx("h3", { className: "text-lg font-semibold flex items-center gap-2" }, "Task presets")),
              jsx("p", { className: "text-sm text-muted-foreground mt-1" }, "Prompts shown on the YouTrack dashboard when starting a task from an issue.")),
            jsx("div", { className: "w-full shrink-0 sm:w-auto" },
              jsx("div", { className: "flex gap-2" },
                dirty ? jsx(ui.Button, { size: "sm", variant: "outline", onClick: discard, className: "cursor-pointer" }, "\u21BB Reset") : null))),
          // Card — Jira: space-y-2 p-4 with rounded-md border rows
          jsx(Card, { "data-testid": "youtrack-task-presets-card", "data-settings-dirty": dirty },
            jsx("div", { className: "space-y-2 p-4" },
              (draft || []).map(function (p) {
                var sp = saved.find(function (s) { return s.id === p.id; });
                var isExp = expanded === p.id;
                return jsx("div", { key: p.id, "data-testid": "youtrack-preset-row", className: "rounded-md border", "data-settings-dirty": JSON.stringify(p) !== JSON.stringify(sp) },
                  jsx("div", { className: "flex items-end gap-2 p-2" },
                    // Icon — Jira: small Select with icon preview
                    jsx("div", { className: "flex flex-col gap-0.5" },
                      jsx("span", { className: "text-[10px] text-muted-foreground" }, "Icon"),
                      jsx("div", { "data-settings-dirty": p.icon !== (sp && sp.icon), className: "rounded-md border border-transparent" },
                        jsx(ui.Select, { value: p.icon || "code", onValueChange: function (v) { patch(p.id, "icon", v); } },
                          jsx(ui.SelectTrigger, { className: "w-fit cursor-pointer !h-8 py-0.5 text-sm", "aria-label": "Icon" },
                            jsx(ui.SelectValue, null)),
                          jsx(ui.SelectContent, null, PRESET_ICON_KEYS.map(function (k) {
                            return jsx(ui.SelectItem, { key: k, value: k, className: "cursor-pointer" }, jsx(PresetIconGlyph, { icon: k, className: "h-4 w-4" }));
                          }))))),
                    // Label — Jira: h-8 w-40
                    jsx("div", { className: "flex flex-col gap-0.5" },
                      jsx("span", { className: "text-[10px] text-muted-foreground" }, "Label"),
                      jsx(ui.Input, { value: p.label, placeholder: "Label", "data-settings-dirty": p.label !== (sp && sp.label), onChange: function (e) { patch(p.id, "label", e.target.value); }, className: "h-8 w-40" })),
                    // Hint — Jira: flex-1
                    jsx("div", { className: "flex flex-col gap-0.5 flex-1" },
                      jsx("span", { className: "text-[10px] text-muted-foreground" }, "Hint"),
                      jsx(ui.Input, { value: p.hint, placeholder: "Hint (optional)", "data-settings-dirty": p.hint !== (sp && sp.hint), onChange: function (e) { patch(p.id, "hint", e.target.value); }, className: "h-8 w-full" })),
                    // Edit prompt — Jira: outline h-8 text-xs
                    jsx(ui.Button, { size: "sm", variant: "outline", onClick: function () { setExpanded(isExp ? "" : p.id); }, className: "cursor-pointer h-8 text-xs" }, "Edit prompt"),
                    // Remove — Jira: ghost icon h-8 w-8 text-destructive with trash SVG
                    jsx(ui.Button, { size: "sm", variant: "ghost", onClick: function () { removePreset(p.id); }, className: "cursor-pointer h-8 w-8 text-destructive", "aria-label": "Remove" }, jsx(TrashIcon, null))),
                  isExp ? jsx(ui.Textarea, { value: p.prompt_template, "data-settings-dirty": p.prompt_template !== (sp && sp.prompt_template), placeholder: "Use {key}, {url}, {title}, {description} as placeholders.", onChange: function (e) { patch(p.id, "prompt_template", e.target.value); }, className: "m-2 min-h-[90px] font-mono text-xs" }) : null);
              }),
              // Add preset — Jira: outline sm with + icon at bottom
              jsx(ui.Button, { size: "sm", variant: "outline", onClick: addPreset, "data-testid": "youtrack-preset-add", className: "cursor-pointer" }, "+ Add preset"))));
      }

      // ── Integration settings page (connection card, Jira-style) ───────────
      function configToForm(cfg) {
        if (!cfg) return { base_url: "", permanent_token: "", default_project: "", default_query: "" };
        return { base_url: cfg.base_url || "", permanent_token: cfg.has_token ? "********" : "", default_project: cfg.default_project || "", default_query: cfg.default_query || "" };
      }

      function YouTrackIntegrationSettings(props) {
        var wsId = props && props.workspaceId ? props.workspaceId : "";
        var enabledCtl = useYouTrackEnabled(wsId);
        var configState = React.useState(null); var config = configState[0]; var setConfig = configState[1];
        var formState = React.useState(configToForm(null)); var form = formState[0]; var setForm = formState[1];
        var loadingState = React.useState(true); var loading = loadingState[0]; var setLoading = loadingState[1];
        var savingState = React.useState(false); var saving = savingState[0]; var setSaving = savingState[1];
        var testingState = React.useState(false); var testing = testingState[0]; var setTesting = testingState[1];
        var testResultState = React.useState(null); var testResult = testResultState[0]; var setTestResult = testResultState[1];

        function loadConfig() { setLoading(true); host.api.invokeAction("connection.check", { workspaceId: wsId }).then(function (r) { setConfig(r); setForm(configToForm(r)); }).catch(function () {}).finally(function () { setLoading(false); }); }
        React.useEffect(function () { loadConfig(); }, [wsId]);

        var baseline = configToForm(config);
        var revision = JSON.stringify(form);
        var dirty = !loading && revision !== JSON.stringify(baseline);
        var hasToken = config && config.has_token;
        var hasConfig = !!(config && config.base_url);

        function update(field, value) { setForm(function (p) { var n = Object.assign({}, p); n[field] = value; return n; }); }

        function handleSave() {
          setSaving(true); var submitted = JSON.stringify(form);
          host.api.invokeAction("connection.save", { workspaceId: wsId, body: form })
            .then(function () { return host.api.invokeAction("connection.check", { workspaceId: wsId }); })
            .then(function (r) {
              setConfig(r);
              setForm(function (c) { return JSON.stringify(c) === submitted ? configToForm(r) : c; });
              setTestResult(null);
              host.toast && host.toast.success("YouTrack configuration saved");
              if (r.connected && !enabledCtl.enabled) { enabledCtl.setEnabled(true); }
            })
            .catch(function (e) { host.toast && host.toast.error("Save failed: " + String(e)); throw e; })
            .finally(function () { setSaving(false); });
        }
        function handleReset() { setForm(baseline); setTestResult(null); }
        function handleTest() { setTesting(true); setTestResult(null); host.api.invokeAction("connection.check", { workspaceId: wsId }).then(function (r) { setTestResult(r); }).catch(function (e) { setTestResult({ connected: false, error: String(e) }); }).finally(function () { setTesting(false); }); }
        function handleDelete() {
          if (!window.confirm("Remove the YouTrack configuration for this workspace? Watches and presets are removed too.")) return;
          host.api.invokeAction("connection.delete", { workspaceId: wsId })
            .then(function () {
              setConfig({ connected: false, has_token: false, error: "youtrack: not configured" });
              setForm(configToForm(null));
              setTestResult(null);
              host.toast && host.toast.success("YouTrack configuration removed");
            })
            .catch(function (e) { host.toast && host.toast.error(String(e)); });
        }

        var canSave = form.base_url !== "" && (form.permanent_token !== "" || hasToken);
        var invalidReason = ""; if (!form.base_url) invalidReason = "Base URL is required"; else if (!form.permanent_token && !hasToken) invalidReason = "Permanent token is required";
        if (useSave) { useSave({ id: "youtrack-config-" + wsId, revision: revision, isDirty: dirty, canSave: canSave, invalidReason: invalidReason || undefined, save: handleSave, discard: handleReset }); }

        var health = config && config.has_token ? { ok: config.connected, error: config.error || "", checkedAt: config.connected || config.error ? new Date() : null } : null;
        if (loading) return jsx("div", { className: "py-20 text-center" }, jsx(ui.Spinner, null));

        return jsx(React.Fragment, null,
          jsx(Card, { "data-settings-dirty": dirty },
            jsx(ui.CardContent, { className: "pt-6 space-y-4" },
              // In-card toggle: only on old hosts that don't support the action
              // slot + setIntegrationEnabled (stock 0.88.0). New hosts render
              // the toggle in the section header via the action prop.
              typeof host.setIntegrationEnabled !== "function" ? jsx("div", { className: "flex items-center justify-between" },
                jsx("span", { className: "text-sm font-medium" }, "Enable YouTrack for this workspace"),
                jsx(ui.Switch, { checked: enabledCtl.enabled, onCheckedChange: enabledCtl.setEnabled, "aria-label": "Enable YouTrack", "data-testid": "youtrack-enabled-toggle" })) : null,
              Banner && health ? jsx(Banner, { health: health }) : null,
              // Fields — Jira: grid gap-4 sm:grid-cols-2 with space-y-1.5 + Label
              jsx("div", { className: "grid gap-4 sm:grid-cols-2" },
                jsx("div", { className: "space-y-1.5" },
                  jsx(ui.Label, null, "YouTrack Base URL"),
                  jsx(ui.Input, { "data-testid": "youtrack-config-base-url", "data-settings-dirty": form.base_url !== baseline.base_url, value: form.base_url, placeholder: "https://youtrack.example.com", onChange: function (e) { update("base_url", e.target.value); }, disabled: loading })),
                jsx("div", { className: "space-y-1.5" },
                  jsx(ui.Label, null, "Default Project (optional)"),
                  jsx(ui.Input, { "data-testid": "youtrack-config-project", "data-settings-dirty": form.default_project !== baseline.default_project, value: form.default_project, placeholder: "FPU", onChange: function (e) { update("default_project", e.target.value); }, disabled: loading }))),
              jsx("div", { className: "space-y-1.5" },
                jsx(ui.Label, null, "Permanent Token",
                  hasToken ? jsx("span", { className: "text-xs text-muted-foreground ml-2" }, "Saved. Leave blank to keep.") : null),
                jsx(ui.Input, { "data-testid": "youtrack-config-token", "data-settings-dirty": form.permanent_token !== baseline.permanent_token, type: "password", value: form.permanent_token, placeholder: "perm-xxxxx", onChange: function (e) { update("permanent_token", e.target.value); }, disabled: loading }),
                jsx("p", { className: "text-xs text-muted-foreground" }, hasToken ? null :
                  jsx("span", null, "Hub permanent token. Create one at ",
                    jsx("a", { href: (form.base_url || "https://youtrack.example.com") + "/users/me?tab=Accounts", target: "_blank", rel: "noopener noreferrer", className: "text-primary underline" }, "Profile > Account Security > New token"), "."))),
              jsx("div", { className: "space-y-1.5" },
                jsx(ui.Label, null, "Default Query (optional)"),
                jsx(ui.Input, { "data-testid": "youtrack-config-query", "data-settings-dirty": form.default_query !== baseline.default_query, value: form.default_query, placeholder: "assignee: me", onChange: function (e) { update("default_query", e.target.value); }, disabled: loading }),
                jsx("p", { className: "text-xs text-muted-foreground" }, "YouTrack query used when the dashboard loads with no explicit query.")),
              testResult ? jsx(ui.Alert, { variant: testResult.connected ? "default" : "destructive" },
                jsx(ui.AlertDescription, { className: "text-sm" }, testResult.connected ? "Connected as " + (testResult.login || "unknown") : (testResult.error || "Connection failed"))) : null,
              jsx(ui.Separator, null),
              // Actions — Jira: flex flex-wrap items-center gap-2 with Test left, Remove right
              jsx("div", { className: "flex flex-wrap items-center gap-2" },
                jsx(ui.Button, { type: "button", variant: "outline", "data-testid": "youtrack-test", onClick: handleTest, disabled: testing || loading || !hasToken, className: "cursor-pointer" }, testing ? "Testing..." : "Test connection"),
                jsx("span", { className: "flex-1" }),
                hasConfig ? jsx(ui.Button, { type: "button", variant: "destructive", "data-testid": "youtrack-delete", onClick: handleDelete, className: "cursor-pointer" }, "Remove configuration") : null),
              !useSave && dirty ? jsx("div", { className: "flex gap-2" },
                jsx(ui.Button, { onClick: handleSave, disabled: saving || !canSave, className: "cursor-pointer" }, saving ? "Saving..." : "Save changes"),
                jsx(ui.Button, { variant: "outline", onClick: handleReset, className: "cursor-pointer" }, "Reset")) : null)),
          jsx(YouTrackWatchersSection, { workspaceId: wsId }),
          jsx(YouTrackPresetsSection, { workspaceId: wsId }),
          jsx("div", null, jsx(ui.Button, { "data-testid": "youtrack-open-dashboard", onClick: function () { host.navigate("/youtrack"); }, className: "cursor-pointer" }, "Open YouTrack dashboard")));
      }

      // ── Task link ─────────────────────────────────────────────────────────
      function linkYouTrackIssue(context) {
        return new Promise(function (resolve) {
          host.openTaskLinkDialog({
            title: "Link YouTrack issue",
            description: "Enter a YouTrack issue id (e.g. FPU-123) or URL.",
            inputLabel: "Issue",
            placeholder: "FPU-123",
            emptyError: "Enter a YouTrack issue id or URL.",
            failureMessage: "Failed to link YouTrack issue.",
            successMessage: "YouTrack issue linked",
            inputTestId: "youtrack-link-issue-input",
            errorTestId: "youtrack-link-issue-error",
            submitTestId: "youtrack-link-issue-submit",
            onSubmit: function (reference) {
              return host.api.invokeAction("issues.link", { workspaceId: context.workspaceId, taskId: context.taskId, body: { issue_id: normalizeIssueId(reference) } })
                .then(function (result) { if (!result.linked) throw new Error(result.error || "Failed to link issue"); });
            },
          });
          resolve();
        });
      }

      // ── Boot-sync: push per-workspace enabled state to the registry so the
      // sidebar badge is correct immediately after app reload, without the
      // user visiting the YouTrack integration page first. ─────────────────
      safe(function () {
        try {
          var items = (host.store.getState().workspaces && host.store.getState().workspaces.items) || [];
          items.forEach(function (ws) {
            host.storage.get("workspace", ws.id, "youtrack_enabled").then(function (entry) {
              if (entry && (entry.value === true || entry.value === "true")) {
                if (typeof host.setIntegrationEnabled === "function") host.setIntegrationEnabled(ws.id, true);
              }
            }, function () {});
          });
        } catch (e) {}
      });

      // ── Registrations ─────────────────────────────────────────────────────
      safe(function () { registry.registerTranslations({ en: { youtrack: "YouTrack" } }); });
      safe(function () { registry.registerNavItem({ id: "youtrack-integrations", get label() { return host.i18n.t("youtrack"); }, path: "/youtrack", icon: YouTrackIcon, section: "integrations" }); });
      safe(function () { registry.registerRoute("/youtrack", YouTrackDashboard, { topbar: { title: function () { return host.i18n.t("youtrack"); } } }); });
      safe(function () { registry.registerTaskAction({ id: "link-youtrack-issue", get label() { return host.i18n.t("youtrack"); }, icon: YouTrackIcon, placement: "link", run: linkYouTrackIssue }); });
      safe(function () { registry.registerKeybinding("open-youtrack", function () { host.navigate("/youtrack"); }); });
      if (typeof registry.registerIntegrationSettings === "function") {
        safe(function () { registry.registerIntegrationSettings({
          id: "youtrack", label: "YouTrack",
          description: "Connect YouTrack to list issues, watch queries, and start Kandev tasks from them.",
          icon: YouTrackIcon,
          Component: YouTrackIntegrationSettings,
          action: YouTrackEnableToggle,
        }); });
      } else { console.warn("[kandev-plugin-youtrack] registerIntegrationSettings not available."); }
    },
    destroy: function () {},
  });
})();