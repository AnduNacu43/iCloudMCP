package tools

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/AnduNacu43/iCloudMCP/internal/store/fake"
)

func TestLoggingMiddleware(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	server := NewServer(Config{Calendars: fake.NewCalendarStore(), Reminders: fake.NewReminderStore(), Now: func() time.Time { return testNow }, Location: bucharest})
	server.AddReceivingMiddleware(LoggingMiddleware(logger))
	ct, st := mcp.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cs.Close(); ss.Wait() }()

	if _, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_current_time"}); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_event", Arguments: map[string]any{"id": "secret-id"}}); err != nil {
		t.Fatal(err)
	}

	logs := buf.String()
	for _, want := range []string{
		`msg="tool call" method=tools/call`,
		"tool=get_current_time",
		`msg="tool call returned an error"`,
		"tool=get_event",
	} {
		if !strings.Contains(logs, want) {
			t.Errorf("logs missing %q:\n%s", want, logs)
		}
	}
	if strings.Contains(logs, "method=initialize") {
		t.Error("non-tool requests should only be logged at debug level")
	}
}
