# iCloudMCP

A local MCP server, written in Go, that lets Claude read and write your iCloud
Calendar events and Reminders on a Mac through Apple's EventKit framework.

Status: early scaffold. The design and the staged build plan are in
[i-want-to-build-purrfect-pond.md](i-want-to-build-purrfect-pond.md).

## Build

Requires macOS, Go 1.24+ and the Xcode Command Line Tools (cgo is used for
EventKit).

```sh
go build -o icloud-mcp ./cmd/icloud-mcp
```

## Try it with Claude Code

```sh
claude mcp add --transport stdio icloud -- "$PWD/icloud-mcp"
```

The first calendar call triggers a macOS permission prompt for the app that
launched the server (your terminal or Claude Desktop). Approve it under
System Settings > Privacy & Security > Calendars.
