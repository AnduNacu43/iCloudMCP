# iCloud MCP server in Go — Calendar + Reminders, read/write

## Context

Goal: a local MCP server, written in Go, that lets Claude (Claude Desktop and Claude Code on this Mac) read and write the user's iCloud Calendar events and Reminders.

Research finding that shaped the design: Apple moved Reminders off CalDAV in iOS 13 / macOS Catalina (2019). `caldav.icloud.com` still serves calendars, but modern Reminders lists are only reachable through Apple's **EventKit** framework on a Mac. Pure CalDAV therefore cannot satisfy the Reminders requirement. (Sources: [BusyMac FAQ](https://www.busymac.com/docs/faqs/112990-reminders-in-ios-13-and-macos-catalina-drops-support-for-caldav/), [Apple dev forums](https://developer.apple.com/forums/thread/129740), [thomascrouzet/icloud-mcp](https://github.com/thomascrouzet/icloud-mcp) which excludes Reminders for this reason.)

Decisions confirmed with the user:
- **Backend: EventKit for both** calendars and reminders, via cgo. One permission model, no app-specific password, macOS syncs to iCloud. Trade-off accepted: macOS-only binary that must run in a logged-in session.
- **Transport: stdio**, launched by Claude Desktop / Claude Code on this Mac.
- **v1 scope: core CRUD.** Recurrence editing, list/calendar management, invitations, HTTP transport and TCC hardening are later phases.

Environment verified: macOS 15.7.7 (arm64), Go 1.27.1, Xcode CLT + Apple clang 17, `CGO_ENABLED=1`, Claude Code 2.1.291, Claude Desktop installed with an existing `claude_desktop_config.json`. Project dir `/Users/andunacu/Documents/CODE/iCloudMCP` is empty and not yet a git repo.

## Libraries

| Need | Choice | Notes |
|---|---|---|
| MCP protocol | `github.com/modelcontextprotocol/go-sdk/mcp` v1.7.0 (official) | `mcp.NewServer`, generic `mcp.AddTool[In,Out]` (auto input/output schema + structured content), `mcp.StdioTransport`, `mcp.ToolAnnotations`, in-memory transports for tests. |
| EventKit bridge | `github.com/BRO3886/go-eventkit` v0.15.0, packages `calendar` and `reminders` | Pure-Go API over cgo/Obj-C. Pre-1.0 (API may shift) — wrap it behind our own interface so it is swappable. Non-darwin builds compile to stubs returning `ErrUnsupported`. |

Assumption: module path `github.com/andunacu/icloud-mcp` (rename if you publish elsewhere).

## Architecture

```
Claude Desktop / Claude Code
        │ stdio (JSON-RPC)
        ▼
cmd/icloud-mcp            flags, slog→stderr, server.Run(StdioTransport)
        │
internal/tools            13 MCP tools: typed In/Out structs, validation, error→IsError
        │
internal/store            interfaces CalendarStore, ReminderStore + domain types
   ├── eventkit/          adapter over go-eventkit (darwin build tag); lazy init + timeout
   └── fake/              in-memory implementation for tests
```

Why the `store` interface layer: lets every tool be unit-tested in-process with the fake backend (no TCC prompt, runs anywhere), and isolates the pre-1.0 go-eventkit API.

### Project layout

```
go.mod, go.sum, .gitignore, README.md
cmd/icloud-mcp/main.go
internal/store/store.go              interfaces + types (Calendar, Event, ReminderList, Reminder, inputs)
internal/store/eventkit/calendar.go  //go:build darwin
internal/store/eventkit/reminders.go //go:build darwin
internal/store/eventkit/init.go      lazy, once-guarded client creation with timeout; maps ErrAccessDenied to an actionable message
internal/store/fake/fake.go          in-memory stores
internal/tools/server.go             NewServer(stores) → *mcp.Server with all tools registered
internal/tools/time.go               parse RFC 3339 / YYYY-MM-DD / relative defaults; format output
internal/tools/calendar.go           calendar tool handlers
internal/tools/reminders.go          reminder tool handlers
internal/tools/*_test.go             tests over mcp in-memory transport + fake stores
```

## Tool surface (v1)

Annotations: `ReadOnlyHint` on list/get tools, `DestructiveHint` on deletes, `IdempotentHint` on delete/complete. All tools return structured output (typed `Out`) plus a short text summary. Deletes require an exact ID; no bulk or search-based deletion in v1.

| Tool | Input | Output |
|---|---|---|
| `get_current_time` | — | now (RFC 3339 with offset), IANA time zone, today's date. Claude needs this for "tomorrow"/"next week". |
| `list_calendars` | — | id, title, type (iCloud/local/…), source, color, read_only |
| `list_events` | `start`, `end` (RFC 3339 or date; default today → +7d, max range 1 year), optional `calendar` (name or id), `search` | events[] |
| `get_event` | `id` | event |
| `create_event` | `title`, `start`, `end` (or `all_day` + date), optional `calendar` (default: EventKit default calendar), `location`, `notes`, `url`, `alert_minutes_before[]` | created event |
| `update_event` | `id` + any optional fields; recurring events: `span` = `this` (default) or `future` | updated event |
| `delete_event` | `id`, optional `span` | ok |
| `list_reminder_lists` | — | id, title, color, source, count, read_only |
| `list_reminders` | optional `list`, `completed` (default false), `due_before`, `due_after`, `search` | reminders[] |
| `get_reminder` | `id` | reminder |
| `create_reminder` | `title`, optional `list` (default: default list), `notes`, `due` (RFC 3339 or date), `priority` (none/low/medium/high), `flagged`, `url` | created reminder |
| `update_reminder` | `id` + optional fields, `clear_due_date` | updated reminder |
| `complete_reminder` | `id`, `completed` (default true) | updated reminder |
| `delete_reminder` | `id` | ok |

Event output fields: id, title, start, end, all_day, calendar, calendar_id, location, notes, url, recurring, alerts, status. Reminder output: id, title, notes, list, list_id, due, completed, completion_date, priority, flagged, url, recurring.

## Key design details

- **Time handling** (`internal/tools/time.go`): accept RFC 3339 and `YYYY-MM-DD`; a bare date on `start`/`end` implies all-day. Naive timestamps (no offset) are interpreted in the Mac's local zone. Output always RFC 3339 with offset. Reject `end <= start`.
- **Permission (TCC) handling**: do **not** call `calendar.New()`/`reminders.New()` at startup. Initialize lazily on first tool call behind `sync.Once`, with a 60 s timeout wrapper; if access is denied or times out, the tool returns an `IsError` result whose text tells the user to grant access under *System Settings → Privacy & Security → Calendars / Reminders* for the host app (Claude Desktop, or the terminal running Claude Code). Server startup and `tools/list` therefore never block. Note for README: the first prompt is attributed to the launching app, not our binary (go-eventkit documents this; fixing it is a later phase).
- **Errors**: domain errors (not found, read-only calendar, access denied, validation) become `CallToolResult{IsError:true}` with a plain-English message so Claude can recover; only transport/encoding failures return Go errors.
- **Logging**: `log/slog` to **stderr only** (stdout is the protocol channel). `--log-level` flag, default `info`; `--version` flag.
- **Build**: `go build ./cmd/icloud-mcp` on darwin (cgo needed). Keep `GOOS=linux CGO_ENABLED=0 go vet ./...` working via build tags so the non-EventKit code stays portable.
- **Dependencies**: only the two modules above plus stdlib. No helper scripts or Makefile; commands documented in README.

## Implementation phases

### Phase 0 — scaffold & smoke test (prove cgo + EventKit on this Mac)
1. `git init`, `.gitignore` (binary, `.DS_Store`).
2. `go mod init github.com/andunacu/icloud-mcp`; `go get github.com/modelcontextprotocol/go-sdk@v1.7.0 github.com/BRO3886/go-eventkit@v0.15.0`.
3. Minimal `cmd/icloud-mcp/main.go` that registers only `get_current_time` and `list_calendars` (direct go-eventkit call) and runs on stdio.
4. Build, then exercise it with Claude Code: `claude mcp add --transport stdio icloud -- /Users/andunacu/Documents/CODE/iCloudMCP/icloud-mcp`, ask Claude to list calendars. This triggers and verifies the TCC prompt and confirms iCloud calendars appear. Stop here if EventKit/cgo misbehaves.

### Phase 1 — store layer
5. `internal/store/store.go`: domain types and `CalendarStore` / `ReminderStore` interfaces (methods mirror the tool table: ListCalendars, ListEvents, GetEvent, CreateEvent, UpdateEvent, DeleteEvent; ListReminderLists, ListReminders, GetReminder, CreateReminder, UpdateReminder, SetCompleted, DeleteReminder). Sentinel errors: `ErrNotFound`, `ErrAccessDenied`, `ErrReadOnly`, `ErrInvalid`.
6. `internal/store/eventkit/`: adapter mapping go-eventkit types ↔ domain types (priority enum, alerts as minutes-before, `Span`, filter options `WithCalendar`/`WithList`/`WithSearch`/`WithCompleted`/`WithDueBefore`/`WithDueAfter`), lazy init with timeout, error mapping from go-eventkit sentinels.
7. `internal/store/fake/`: in-memory implementation with deterministic IDs, honoring the same sentinel errors.

### Phase 2 — MCP tools
8. `internal/tools/server.go`: `NewServer(cal store.CalendarStore, rem store.ReminderStore, version string) *mcp.Server`; register all tools with descriptions written for the model (when to use, date formats, defaults) and annotations.
9. `internal/tools/time.go` + `calendar.go`: the 7 calendar/time tools.
10. `internal/tools/reminders.go`: the 7 reminder tools.
11. Tests: spin up server + `mcp.NewInMemoryTransports()` + an `mcp.Client`, call every tool against the fake store; cover date parsing, defaults, validation errors, not-found → `IsError`, `complete_reminder` toggling, recurring-event `span`.

### Phase 3 — wiring, docs, client registration
12. Final `main.go`: flags, slog to stderr, build eventkit stores, `server.Run(ctx, &mcp.StdioTransport{})`, graceful exit on SIGINT/SIGTERM.
13. README: prerequisites (Xcode CLT), build, permission prompt behaviour and how to re-grant, registration snippets for Claude Code (`claude mcp add …`) and Claude Desktop (`mcpServers.icloud.command` in `~/Library/Application Support/Claude/claude_desktop_config.json`), tool list, known limitations.
14. Register with both clients and run the end-to-end checks below.

### Later phases (not in v1)
- Recurrence rules on create/update (go-eventkit `eventkit.Daily/Weekly/Monthly/Yearly`), recurring reminders, create/rename/delete calendars and lists.
- TCC hardening so the permission is granted to *our* binary once for all clients: embed `Info.plist` (`-sectcreate __TEXT __info_plist`, needs `CGO_LDFLAGS_ALLOW` + external linkmode), ad-hoc codesign, and a self-re-exec that disclaims TCC responsibility (pattern used by [mcp-server-apple-events](https://github.com/FradSer/mcp-server-apple-events)).
- Streamable HTTP transport on localhost; invitations/RSVP; free-busy; change notifications via `WatchChanges` → MCP `resources/list_changed`.
- Release packaging (goreleaser/Homebrew tap).

## Verification

Automated:
- `go vet ./... && go test ./...` (fake store; no permissions needed).
- `GOOS=linux CGO_ENABLED=0 go vet ./...` to confirm the portable packages still compile.
- `go build ./cmd/icloud-mcp` on this Mac.

Manual end-to-end (uses the real iCloud data on this Mac; use a dedicated test calendar/list and clean up):
1. `claude mcp add --transport stdio icloud -- <abs path to binary>`; `claude mcp list` shows it connected.
2. In Claude Code: "list my calendars" → iCloud calendars appear (first run triggers the Calendars permission prompt; Reminders prompt appears on first reminder tool).
3. "Create a test event tomorrow 10–11 called MCP smoke test with a 15-minute alert" → appears in Calendar.app within seconds; then update its title and delete it via Claude; confirm each in Calendar.app.
4. "Add a reminder 'MCP smoke test' due Friday, high priority" → appears in Reminders.app; complete it, then delete it; confirm.
5. Add the same command to `claude_desktop_config.json`, restart Claude Desktop, repeat step 2 to confirm the Desktop permission grant.
6. Negative path: revoke Calendars access in System Settings, call `list_events`, confirm a readable error with fix instructions rather than a hang.

## Risks / notes
- go-eventkit is pre-1.0 with few users; the store interface confines the blast radius, and the adapter can be replaced with our own cgo bindings if needed.
- EventKit requires a logged-in GUI session; the server cannot run headless (launchd daemon, SSH). Documented, not solved, in v1.
- Permission prompt is attributed to the host app in v1 (Claude Desktop, or the terminal app for Claude Code), so each host needs its own grant until the hardening phase.
- Shared or subscribed calendars/lists may be read-only; the adapter surfaces `ErrReadOnly` so writes fail clearly.
