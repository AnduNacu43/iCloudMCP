package tools

import (
	"errors"
	"testing"
	"time"

	"github.com/AnduNacu43/iCloudMCP/internal/store"
)

func TestParseMoment(t *testing.T) {
	tests := []struct {
		in       string
		want     time.Time
		dateOnly bool
	}{
		{"2026-10-07T14:30:00+03:00", local(10, 7, 14, 30), false},
		{"2026-10-07T11:30:00Z", local(10, 7, 14, 30), false},
		{"2026-10-07T14:30:00", local(10, 7, 14, 30), false},
		{"2026-10-07T14:30", local(10, 7, 14, 30), false},
		{"2026-10-07 14:30", local(10, 7, 14, 30), false},
		{" 2026-10-07 ", local(10, 7, 0, 0), true},
	}
	for _, tt := range tests {
		m, err := parseMoment("start", tt.in, bucharest)
		if err != nil {
			t.Errorf("parseMoment(%q): %v", tt.in, err)
			continue
		}
		if !m.Time.Equal(tt.want) || m.DateOnly != tt.dateOnly {
			t.Errorf("parseMoment(%q) = %v (date only %v), want %v (%v)", tt.in, m.Time, m.DateOnly, tt.want, tt.dateOnly)
		}
	}
	for _, bad := range []string{"", "tomorrow", "07/10/2026", "2026-13-01"} {
		if _, err := parseMoment("start", bad, bucharest); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("parseMoment(%q) err = %v, want ErrInvalid", bad, err)
		}
	}
}

func TestDayArithmeticAcrossDST(t *testing.T) {
	// Daylight saving time ends in Bucharest on Sunday 25 October 2026.
	sat := local(10, 24, 0, 0)
	if got := addDays(sat, 2, bucharest); got.Format(time.RFC3339) != "2026-10-26T00:00:00+02:00" {
		t.Errorf("addDays across DST = %s", got.Format(time.RFC3339))
	}
	if got := daysBetween(sat, local(10, 26, 23, 59), bucharest); got != 2 {
		t.Errorf("daysBetween across DST = %d, want 2", got)
	}
}

func TestDueTime(t *testing.T) {
	if got := dueTime(moment{Time: local(10, 25, 0, 0), DateOnly: true}, bucharest); got.Format(time.RFC3339) != "2026-10-25T09:00:00+02:00" {
		t.Errorf("date-only due on the DST change day = %s", got.Format(time.RFC3339))
	}
	exact := local(10, 25, 17, 0)
	if got := dueTime(moment{Time: exact}, bucharest); !got.Equal(exact) {
		t.Errorf("timed due changed to %s", got)
	}
}
