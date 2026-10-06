// Package eventkit implements the store interfaces over Apple's EventKit via
// github.com/BRO3886/go-eventkit.
//
// go-eventkit compiles to stubs that return ErrUnsupported on non-darwin
// platforms, so this package builds everywhere and fails at call time
// instead.
package eventkit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/BRO3886/go-eventkit/calendar"
	"github.com/BRO3886/go-eventkit/reminders"

	"github.com/AnduNacu43/iCloudMCP/internal/store"
)

// DefaultAccessTimeout bounds how long a tool call waits for macOS to answer
// an access request. The first request shows a permission prompt and blocks
// until the user responds.
const DefaultAccessTimeout = 60 * time.Second

// lazyClient creates an EventKit client on first use instead of at startup,
// so the server can start and list its tools without triggering or waiting on
// a permission prompt.
//
// A single access request runs at a time. Callers wait for it up to the
// timeout, and a request still pending after a timeout keeps running so a
// later call can pick up its result. A failed request is retried on the next
// call, which picks up access granted later in System Settings.
type lazyClient[T any] struct {
	newClient func() (*T, error)
	timeout   time.Duration
	app       string // "Calendars" or "Reminders", for messages

	mu      sync.Mutex
	client  *T
	pending *attempt[T]
}

type attempt[T any] struct {
	done   chan struct{}
	client *T
	err    error
}

func newLazyClient[T any](app string, timeout time.Duration, newClient func() (*T, error)) *lazyClient[T] {
	if timeout <= 0 {
		timeout = DefaultAccessTimeout
	}
	return &lazyClient[T]{newClient: newClient, timeout: timeout, app: app}
}

func (l *lazyClient[T]) get(ctx context.Context) (*T, error) {
	l.mu.Lock()
	if l.client != nil {
		c := l.client
		l.mu.Unlock()
		return c, nil
	}
	a := l.pending
	if a == nil {
		a = &attempt[T]{done: make(chan struct{})}
		l.pending = a
		go l.run(a)
	}
	l.mu.Unlock()

	timer := time.NewTimer(l.timeout)
	defer timer.Stop()
	select {
	case <-a.done:
		if a.err != nil {
			return nil, l.accessError(a.err)
		}
		return a.client, nil
	case <-timer.C:
		return nil, fmt.Errorf("%w: macOS did not answer the %s access request within %s. A permission prompt may be waiting on screen; approve it, then try again", store.ErrAccessDenied, l.app, l.timeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (l *lazyClient[T]) run(a *attempt[T]) {
	c, err := l.newClient()
	l.mu.Lock()
	a.client, a.err = c, err
	if err == nil {
		l.client = c
	}
	l.pending = nil
	l.mu.Unlock()
	close(a.done)
}

func (l *lazyClient[T]) accessError(err error) error {
	if errors.Is(err, calendar.ErrUnsupported) || errors.Is(err, reminders.ErrUnsupported) {
		return fmt.Errorf("%s access is only available on macOS: %v", l.app, err)
	}
	return fmt.Errorf("%w: %s access was not granted (%v). Open System Settings > Privacy & Security > %s and enable the app that launched this server (Claude Desktop, or the terminal running Claude Code), then try again", store.ErrAccessDenied, l.app, err, l.app)
}

// mapError converts go-eventkit errors to store sentinels, keeping the
// original message for the user.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, calendar.ErrNotFound), errors.Is(err, reminders.ErrNotFound):
		return fmt.Errorf("%w: %v", store.ErrNotFound, err)
	case errors.Is(err, calendar.ErrImmutable), errors.Is(err, reminders.ErrImmutable):
		return fmt.Errorf("%w: %v", store.ErrReadOnly, err)
	case errors.Is(err, calendar.ErrAccessDenied), errors.Is(err, reminders.ErrAccessDenied):
		return fmt.Errorf("%w: %v", store.ErrAccessDenied, err)
	}
	// Several bridge calls report errors only as text.
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "not found"):
		return fmt.Errorf("%w: %v", store.ErrNotFound, err)
	case strings.Contains(msg, "read-only"), strings.Contains(msg, "immutable"), strings.Contains(msg, "does not allow"):
		return fmt.Errorf("%w: %v", store.ErrReadOnly, err)
	}
	return err
}
