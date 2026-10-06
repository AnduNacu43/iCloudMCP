package eventkit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BRO3886/go-eventkit/calendar"
	"github.com/BRO3886/go-eventkit/reminders"

	"github.com/AnduNacu43/iCloudMCP/internal/store"
)

type dummy struct{ n int }

func TestLazyClientCachesSuccess(t *testing.T) {
	var calls atomic.Int32
	l := newLazyClient("Calendars", time.Second, func() (*dummy, error) {
		calls.Add(1)
		return &dummy{n: 7}, nil
	})
	for range 3 {
		c, err := l.get(context.Background())
		if err != nil || c.n != 7 {
			t.Fatalf("get = %v, %v", c, err)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("client created %d times, want 1", calls.Load())
	}
}

func TestLazyClientTimesOutThenPicksUpLateGrant(t *testing.T) {
	release := make(chan struct{})
	l := newLazyClient("Calendars", 20*time.Millisecond, func() (*dummy, error) {
		<-release // the user has not answered the prompt yet
		return &dummy{n: 1}, nil
	})

	_, err := l.get(context.Background())
	if !errors.Is(err, store.ErrAccessDenied) || !strings.Contains(err.Error(), "did not answer") {
		t.Fatalf("first get err = %v, want a timeout wrapped in ErrAccessDenied", err)
	}

	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		c, err := l.get(context.Background())
		if err == nil && c.n == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("late grant never picked up, last err = %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestLazyClientRetriesAfterDenial(t *testing.T) {
	var calls atomic.Int32
	l := newLazyClient("Reminders", time.Second, func() (*dummy, error) {
		if calls.Add(1) == 1 {
			return nil, reminders.ErrAccessDenied
		}
		return &dummy{n: 2}, nil
	})
	_, err := l.get(context.Background())
	if !errors.Is(err, store.ErrAccessDenied) || !strings.Contains(err.Error(), "Privacy & Security > Reminders") {
		t.Fatalf("first get err = %v, want an actionable access-denied error", err)
	}
	c, err := l.get(context.Background())
	if err != nil || c.n != 2 {
		t.Fatalf("second get = %v, %v; want a retried success", c, err)
	}
}

func TestLazyClientUnsupportedPlatform(t *testing.T) {
	l := newLazyClient("Calendars", time.Second, func() (*dummy, error) { return nil, calendar.ErrUnsupported })
	_, err := l.get(context.Background())
	if errors.Is(err, store.ErrAccessDenied) || !strings.Contains(err.Error(), "only available on macOS") {
		t.Errorf("err = %v, want a macOS-only message that is not access denied", err)
	}
}

func TestLazyClientHonoursContext(t *testing.T) {
	l := newLazyClient("Calendars", time.Minute, func() (*dummy, error) { select {} })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := l.get(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestMapError(t *testing.T) {
	tests := []struct {
		in   error
		want error
	}{
		{calendar.ErrNotFound, store.ErrNotFound},
		{fmt.Errorf("reminders: %w: gone", reminders.ErrNotFound), store.ErrNotFound},
		{errors.New("reminders: reminder not found: abc"), store.ErrNotFound},
		{errors.New("list not found: Chores (available: A, B)"), store.ErrNotFound},
		{calendar.ErrImmutable, store.ErrReadOnly},
		{reminders.ErrAccessDenied, store.ErrAccessDenied},
	}
	for _, tt := range tests {
		if got := mapError(tt.in); !errors.Is(got, tt.want) {
			t.Errorf("mapError(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
	if mapError(nil) != nil {
		t.Error("mapError(nil) should be nil")
	}
	other := errors.New("disk full")
	if got := mapError(other); got != other {
		t.Errorf("unknown errors should pass through, got %v", got)
	}
}

func TestPriorityMapping(t *testing.T) {
	for p := reminders.Priority(0); p <= 9; p++ {
		want := store.Priority(p.String())
		if got := toPriority(p); got != want {
			t.Errorf("toPriority(%d) = %q, want %q", p, got, want)
		}
	}
	for _, p := range []store.Priority{store.PriorityNone, store.PriorityLow, store.PriorityMedium, store.PriorityHigh} {
		if got := toPriority(fromPriority(p)); got != p {
			t.Errorf("round trip of %q gave %q", p, got)
		}
	}
}

func TestAlertConversion(t *testing.T) {
	alerts := toAlerts([]int{0, 15, 60})
	if alerts[1].RelativeOffset != -15*time.Minute {
		t.Errorf("15 minutes before should be offset -15m, got %v", alerts[1].RelativeOffset)
	}
	e := toEvent(calendar.Event{Alerts: alerts, Status: calendar.StatusConfirmed})
	if fmt.Sprint(e.AlertMinutesBefore) != "[0 15 60]" {
		t.Errorf("AlertMinutesBefore = %v, want [0 15 60]", e.AlertMinutesBefore)
	}
	if e.Status != "confirmed" {
		t.Errorf("Status = %q, want confirmed", e.Status)
	}
	if got := toEvent(calendar.Event{}).AlertMinutesBefore; got == nil {
		t.Error("an event without alerts should have an empty, non-nil alert list")
	}
}

func TestSpanMapping(t *testing.T) {
	if toSpan(store.SpanThis) != calendar.SpanThisEvent || toSpan(store.SpanFuture) != calendar.SpanFutureEvents {
		t.Error("span mapping is wrong")
	}
}
