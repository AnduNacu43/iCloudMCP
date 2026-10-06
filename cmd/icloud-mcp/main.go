// Command icloud-mcp is a local MCP server that exposes the Mac's iCloud
// Calendar and Reminders through Apple's EventKit framework.
//
// Phase 0 scaffold: registers get_current_time and list_calendars only, to
// prove that cgo, EventKit and the permission prompt work on this Mac.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/BRO3886/go-eventkit/calendar"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const version = "0.0.1-dev"

type currentTimeOut struct {
	Now      string `json:"now" jsonschema:"current time in RFC 3339 with the local UTC offset"`
	TimeZone string `json:"time_zone" jsonschema:"IANA time zone of this Mac"`
	Today    string `json:"today" jsonschema:"today's date as YYYY-MM-DD"`
	Weekday  string `json:"weekday" jsonschema:"today's day of the week"`
}

type calendarOut struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Type     string `json:"type"`
	Source   string `json:"source"`
	Color    string `json:"color"`
	ReadOnly bool   `json:"read_only"`
}

type listCalendarsOut struct {
	Calendars []calendarOut `json:"calendars"`
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	server := mcp.NewServer(&mcp.Implementation{Name: "icloud-mcp", Version: version}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_current_time",
		Description: "Returns the current date, time and time zone of this Mac. Call this before interpreting relative dates such as 'tomorrow' or 'next week'.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, currentTimeOut, error) {
		now := time.Now()
		return nil, currentTimeOut{
			Now:      now.Format(time.RFC3339),
			TimeZone: localZoneName(),
			Today:    now.Format(time.DateOnly),
			Weekday:  now.Weekday().String(),
		}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_calendars",
		Description: "Lists every calendar on this Mac (iCloud, local, subscribed and others) with its ID and whether it is read-only.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, listCalendarsOut, error) {
		client, err := calendar.New()
		if err != nil {
			return nil, listCalendarsOut{}, fmt.Errorf("calendar access failed: %w. Grant access under System Settings > Privacy & Security > Calendars for the app running this server", err)
		}
		cals, err := client.Calendars()
		if err != nil {
			return nil, listCalendarsOut{}, err
		}
		out := listCalendarsOut{Calendars: make([]calendarOut, 0, len(cals))}
		for _, c := range cals {
			out.Calendars = append(out.Calendars, calendarOut{
				ID: c.ID, Title: c.Title, Type: c.Type.String(), Source: c.Source, Color: c.Color, ReadOnly: c.ReadOnly,
			})
		}
		return nil, out, nil
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("starting icloud-mcp", "version", version, "transport", "stdio")
	err := server.Run(ctx, &mcp.StdioTransport{})
	switch {
	case err == nil || ctx.Err() != nil || isClientDisconnect(err):
		logger.Info("session ended")
	default:
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

// isClientDisconnect reports whether err means the client closed stdin, which
// is how a normal stdio session ends. The SDK formats the underlying io.EOF
// with %v, so errors.Is cannot see it and the message is checked instead.
func isClientDisconnect(err error) bool {
	return errors.Is(err, io.EOF) || strings.HasSuffix(err.Error(), ": "+io.EOF.Error())
}

// localZoneName returns the IANA name of the local time zone, falling back to
// the abbreviation when the name is not available.
func localZoneName() string {
	if name := time.Local.String(); name != "" && name != "Local" {
		return name
	}
	if tz := os.Getenv("TZ"); tz != "" {
		return tz
	}
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if _, zone, ok := strings.Cut(target, "zoneinfo/"); ok {
			return zone
		}
	}
	name, _ := time.Now().Zone()
	return name
}
