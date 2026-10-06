// Command icloud-mcp is a local MCP server that lets Claude read and write the
// Calendar events and Reminders on this Mac, including iCloud, through
// Apple's EventKit framework. It speaks MCP over stdio.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/AnduNacu43/iCloudMCP/internal/store/eventkit"
	"github.com/AnduNacu43/iCloudMCP/internal/tools"
)

// version is set at build time with -ldflags "-X main.version=v1.2.3".
var version = ""

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("icloud-mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	logLevel := fs.String("log-level", "info", "log level: debug, info, warn or error (logs go to stderr)")
	accessTimeout := fs.Duration("access-timeout", eventkit.DefaultAccessTimeout, "how long a tool call waits for macOS to answer a Calendars or Reminders permission request")
	showVersion := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintln(os.Stdout, buildVersion())
		return 0
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(*logLevel)); err != nil {
		fmt.Fprintf(stderr, "invalid -log-level %q: use debug, info, warn or error\n", *logLevel)
		return 2
	}
	if *accessTimeout <= 0 {
		fmt.Fprintln(stderr, "-access-timeout must be positive")
		return 2
	}
	// stdout carries the MCP protocol, so logs must only go to stderr.
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))

	server := tools.NewServer(tools.Config{
		Calendars: eventkit.NewCalendarStore(*accessTimeout),
		Reminders: eventkit.NewReminderStore(*accessTimeout),
		Version:   buildVersion(),
		Now:       time.Now,
		Location:  time.Local,
	})
	server.AddReceivingMiddleware(tools.LoggingMiddleware(logger))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("starting icloud-mcp", "version", buildVersion(), "transport", "stdio")
	err := server.Run(ctx, &mcp.StdioTransport{})
	switch {
	case err == nil || ctx.Err() != nil || isClientDisconnect(err):
		logger.Info("session ended")
		return 0
	default:
		logger.Error("server stopped", "error", err)
		return 1
	}
}

// buildVersion returns the version set at link time, else the module
// version recorded by go install, else the VCS revision.
func buildVersion() string {
	if version != "" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 7 {
			return "dev-" + s.Value[:7]
		}
	}
	return "dev"
}

// isClientDisconnect reports whether err means the client closed stdin, which
// is how a normal stdio session ends. The SDK formats the underlying io.EOF
// with %v, so errors.Is cannot see it and the message is checked instead.
func isClientDisconnect(err error) bool {
	return errors.Is(err, io.EOF) || strings.HasSuffix(err.Error(), ": "+io.EOF.Error())
}
