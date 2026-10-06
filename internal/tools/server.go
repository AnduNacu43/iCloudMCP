// Package tools exposes the calendar and reminder stores as MCP tools.
package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/AnduNacu43/iCloudMCP/internal/store"
)

// Config holds the server's dependencies.
type Config struct {
	Calendars store.CalendarStore
	Reminders store.ReminderStore
	Version   string
	// Now returns the current time. It defaults to time.Now.
	Now func() time.Time
	// Location is the zone for interpreting and formatting times. It
	// defaults to time.Local.
	Location *time.Location
}

// handlers carries the configuration into the tool handlers.
type handlers struct {
	cal store.CalendarStore
	rem store.ReminderStore
	now func() time.Time
	loc *time.Location
}

// NewServer returns an MCP server with every calendar and reminder tool
// registered.
func NewServer(cfg Config) *mcp.Server {
	h := &handlers{cal: cfg.Calendars, rem: cfg.Reminders, now: cfg.Now, loc: cfg.Location}
	if h.now == nil {
		h.now = time.Now
	}
	if h.loc == nil {
		h.loc = time.Local
	}
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "icloud-mcp",
		Title:   "iCloud Calendar and Reminders",
		Version: cfg.Version,
	}, &mcp.ServerOptions{
		Instructions: "Reads and writes the Calendar events and Reminders on this Mac, including iCloud. " +
			"Call get_current_time before resolving relative dates such as 'tomorrow'. " +
			"Times are RFC 3339; a timestamp without an offset means the Mac's local time, and a bare YYYY-MM-DD date means a whole day. " +
			"Get IDs from the list tools before calling get, update, complete or delete tools.",
	})
	h.registerCalendarTools(s)
	h.registerReminderTools(s)
	return s
}

var (
	falseVal = false
	trueVal  = true
)

func readOnly(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, OpenWorldHint: &falseVal}
}

func additive(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, DestructiveHint: &falseVal, OpenWorldHint: &falseVal}
}

func modifying(title string, idempotent bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, DestructiveHint: &trueVal, IdempotentHint: idempotent, OpenWorldHint: &falseVal}
}

// schemaFor infers the input schema for T and restricts the named string
// properties to the given values.
func schemaFor[T any](enums map[string][]string) *jsonschema.Schema {
	s, err := jsonschema.For[T](nil)
	if err != nil {
		panic(fmt.Sprintf("schema for %T: %v", *new(T), err))
	}
	for prop, values := range enums {
		p, ok := s.Properties[prop]
		if !ok {
			panic(fmt.Sprintf("schema for %T has no property %q", *new(T), prop))
		}
		for _, v := range values {
			p.Enum = append(p.Enum, v)
		}
		if len(p.Types) > 0 { // nullable pointer field
			p.Enum = append(p.Enum, nil)
		}
	}
	return s
}

// result builds a tool result with a one-line summary for people followed
// by the structured output as JSON, for clients that only read text.
func result(out any, format string, args ...any) *mcp.CallToolResult {
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		data = []byte(fmt.Sprintf("%+v", out))
	}
	return &mcp.CallToolResult{Content: []mcp.Content{
		&mcp.TextContent{Text: fmt.Sprintf(format, args...)},
		&mcp.TextContent{Text: string(data)},
	}}
}

// requireText trims s and fails when it is empty.
func requireText(field, s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("%w: %s must not be empty", store.ErrInvalid, field)
	}
	return s, nil
}

// plural returns "1 event" or "3 events".
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// DeleteOut is the output of the delete tools.
type DeleteOut struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}
