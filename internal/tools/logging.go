package tools

import (
	"context"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// LoggingMiddleware logs each incoming request. Tool calls are logged at
// info level with the tool name, duration and outcome; other methods are
// logged at debug level. Tool arguments and results are not logged because
// they hold personal calendar data; error messages are, since they are what
// a user needs to diagnose a failure.
func LoggingMiddleware(logger *slog.Logger) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			start := time.Now()
			res, err := next(ctx, method, req)
			attrs := []any{"method", method, "duration", time.Since(start).Round(time.Millisecond)}

			if method != "tools/call" {
				if err != nil {
					logger.Warn("request failed", append(attrs, "error", err)...)
				} else {
					logger.Debug("request", attrs...)
				}
				return res, err
			}

			if p, ok := req.GetParams().(*mcp.CallToolParamsRaw); ok {
				attrs = append(attrs, "tool", p.Name)
			}
			switch r, _ := res.(*mcp.CallToolResult); {
			case err != nil:
				logger.Warn("tool call failed", append(attrs, "error", err)...)
			case r != nil && r.IsError:
				logger.Info("tool call returned an error", append(attrs, "error", errorText(r))...)
			default:
				logger.Info("tool call", attrs...)
			}
			return res, err
		}
	}
}

func errorText(r *mcp.CallToolResult) string {
	for _, c := range r.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}
