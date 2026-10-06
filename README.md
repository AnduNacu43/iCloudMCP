# iCloudMCP

A local [MCP](https://modelcontextprotocol.io) server, written in Go, that lets
Claude read and write the Calendar events and Reminders on your Mac, including
iCloud, Google and Exchange accounts that are set up in macOS.

It talks to Apple's EventKit framework rather than CalDAV. Apple moved
Reminders off CalDAV in 2019, so EventKit is the only way to reach modern
iCloud Reminders. macOS syncs every change to iCloud as usual.

The design and the staged build plan are in
[i-want-to-build-purrfect-pond.md](i-want-to-build-purrfect-pond.md).

## Requirements

- macOS, signed in to a desktop session. EventKit does not work over SSH or
  from a launchd daemon.
- Go 1.24 or newer.
- Xcode Command Line Tools (`xcode-select --install`), because EventKit is
  reached through cgo.

## Build

```sh
go build -o icloud-mcp ./cmd/icloud-mcp
```

Flags:

| Flag | Default | Meaning |
|---|---|---|
| `-log-level` | `info` | `debug`, `info`, `warn` or `error`. Logs go to stderr only. |
| `-access-timeout` | `60s` | How long a tool call waits for macOS to answer a permission request. |
| `-version` | | Print the version and exit. |

## Register with Claude

### Claude Code

```sh
claude mcp add --transport stdio icloud -- /absolute/path/to/icloud-mcp
claude mcp list
```

Add `--scope user` to make the tools available in every project, not just
the current directory.

### Claude Desktop

Add the server to
`~/Library/Application Support/Claude/claude_desktop_config.json`, then quit
and reopen Claude Desktop:

```json
{
  "mcpServers": {
    "icloud": {
      "command": "/absolute/path/to/icloud-mcp",
      "args": []
    }
  }
}
```

## Permissions

The first calendar tool call asks macOS for Calendars access, and the first
reminder tool call asks for Reminders access. macOS attributes the request to
the app that launched the server, so each host needs its own grant:

- Claude Desktop, when used from Claude Desktop.
- Your terminal app (Terminal, iTerm, cmux and so on), when used from Claude
  Code.

The server starts and lists its tools without asking for anything. If the
prompt is not answered within the access timeout, the tool returns an error
saying so, and the next call picks up the answer.

To grant or revoke access later, open System Settings > Privacy & Security >
Calendars or Reminders and toggle the host app. A grant made there is picked
up on the next tool call.

## Tools

| Tool | What it does |
|---|---|
| `get_current_time` | Current date, time, weekday and IANA time zone, so Claude can resolve "tomorrow". |
| `list_calendars` | All calendars with ID, account, colour and read-only flag. |
| `list_events` | Events overlapping a range. Defaults to the next 7 days; at most 366 days. Filters by calendar and text. |
| `get_event` | One event by ID. |
| `create_event` | Creates a timed or all-day event with optional calendar, location, notes, URL and alerts. |
| `update_event` | Changes any event fields. Moving the start keeps the event's length. |
| `delete_event` | Deletes an event by ID. |
| `list_reminder_lists` | All reminder lists with ID, account, count and read-only flag. |
| `list_reminders` | Open reminders by default, or completed ones. Filters by list, due range and text. |
| `get_reminder` | One reminder by ID. |
| `create_reminder` | Creates a reminder with optional list, notes, due date, priority, flag and URL. |
| `update_reminder` | Changes any reminder fields, or clears the due date. |
| `complete_reminder` | Marks a reminder as done, or as not done. |
| `delete_reminder` | Deletes a reminder by ID. |

### Dates and times

- Inputs accept RFC 3339 (`2026-10-07T14:30:00+03:00`), local time without an
  offset (`2026-10-07T14:30`), or a bare date (`2026-10-07`).
- Outputs are always RFC 3339 in the Mac's time zone.
- A bare date as an event start creates an all-day event. Its `end` is the
  last day, inclusive.
- A timed event without an `end` lasts one hour.
- A reminder due date without a time is due at 09:00 local time, because
  EventKit always stores a time with a due date.

### Calendars and lists

Tools that take a calendar or list accept either its ID or its title, ignoring
case. When two calendars or lists share a title, pass the ID instead.

## Known limitations

- **Repeating events.** A single occurrence of a repeating event cannot be
  changed or deleted on its own. go-eventkit gives every occurrence the same
  ID and resolves it to the first one. Use `span: "future"` to change the
  whole series, or edit that occurrence in Calendar.app.
- **Shared titles.** Writes into a calendar or list whose title is shared with
  another one are rejected, because go-eventkit addresses them by title on
  writes. Rename one of them to fix this.
- **Not in v1.** Recurrence rules, recurring reminders, managing calendars and
  lists, invitations, and the HTTP transport are not supported yet. See the
  later phases in the plan.
- **Read-only containers.** Subscribed, birthday and some shared calendars and
  lists are read-only, and writes to them fail with a clear error.

## Development

```sh
go vet ./...
go test -race ./...
GOOS=linux CGO_ENABLED=0 go vet ./...   # the non-EventKit code stays portable
```

The tests run every tool against in-memory stores, so they need no
permissions and run on any OS.

Layout:

| Path | Contents |
|---|---|
| `cmd/icloud-mcp` | Flags, logging and the stdio server. |
| `internal/tools` | The MCP tools, date handling and request logging. |
| `internal/store` | Store interfaces, domain types and errors. |
| `internal/store/eventkit` | The EventKit adapter over go-eventkit. |
| `internal/store/fake` | In-memory stores for tests. |
