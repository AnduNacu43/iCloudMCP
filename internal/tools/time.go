package tools

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/AnduNacu43/iCloudMCP/internal/store"
)

// Accepted layouts for timestamps without an offset. They are interpreted
// in the Mac's local time zone.
var naiveLayouts = []string{
	"2006-01-02T15:04:05",
	"2006-01-02T15:04",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
}

const (
	// maxEventRange caps list_events so a single call cannot pull years of
	// events.
	maxEventRange = 366 * 24 * time.Hour
	// defaultEventRange is the list_events window when no end is given.
	defaultEventRange = 7 * 24 * time.Hour
	// defaultEventDuration applies when create_event gets no end.
	defaultEventDuration = time.Hour
	// dateOnlyDueHour is the time of day given to a reminder due date
	// passed without a time; EventKit stores due dates with a time.
	dateOnlyDueHour = 9
)

// moment is a parsed tool timestamp. DateOnly is true when the caller passed
// YYYY-MM-DD, in which case Time is midnight local time on that day.
type moment struct {
	Time     time.Time
	DateOnly bool
}

// parseMoment accepts RFC 3339, an ISO timestamp without offset (local
// time), or a bare YYYY-MM-DD date. field names the argument for errors.
func parseMoment(field, s string, loc *time.Location) (moment, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return moment{Time: t}, nil
	}
	if t, err := time.ParseInLocation(time.DateOnly, s, loc); err == nil {
		return moment{Time: t, DateOnly: true}, nil
	}
	for _, layout := range naiveLayouts {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return moment{Time: t}, nil
		}
	}
	return moment{}, fmt.Errorf("%w: %s %q is not a valid time. Use RFC 3339 like 2026-10-07T14:30:00+03:00, a local time like 2026-10-07T14:30, or a date like 2026-10-07", store.ErrInvalid, field, s)
}

// parseOptionalMoment parses s when it is non-empty.
func parseOptionalMoment(field, s string, loc *time.Location) (*moment, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	m, err := parseMoment(field, s, loc)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// startOfDay returns local midnight on t's calendar day in loc.
func startOfDay(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// addDays moves a local midnight by n calendar days, staying correct across
// daylight-saving changes.
func addDays(t time.Time, n int, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d+n, 0, 0, 0, 0, loc)
}

// daysBetween counts calendar days from a to b in loc.
func daysBetween(a, b time.Time, loc *time.Location) int {
	a, b = startOfDay(a, loc), startOfDay(b, loc)
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	// Compare as UTC dates to avoid DST-length days.
	return int(time.Date(by, bm, bd, 0, 0, 0, 0, time.UTC).Sub(time.Date(ay, am, ad, 0, 0, 0, 0, time.UTC)).Hours() / 24)
}

// formatTime renders t as RFC 3339 in loc.
func formatTime(t time.Time, loc *time.Location) string {
	return t.In(loc).Format(time.RFC3339)
}

// formatOptionalTime renders t when it is set.
func formatOptionalTime(t *time.Time, loc *time.Location) string {
	if t == nil {
		return ""
	}
	return formatTime(*t, loc)
}

// dueTime turns a parsed reminder due value into the stored time. A bare
// date becomes that day at dateOnlyDueHour local time.
func dueTime(m moment, loc *time.Location) time.Time {
	if m.DateOnly {
		y, mo, d := m.Time.In(loc).Date()
		return time.Date(y, mo, d, dateOnlyDueHour, 0, 0, 0, loc)
	}
	return m.Time
}

// zoneName returns the IANA name of loc. time.Local reports itself as
// "Local", so its name is read from TZ or the /etc/localtime symlink, with
// the zone abbreviation as a last resort.
func zoneName(loc *time.Location, now time.Time) string {
	if name := loc.String(); name != "" && name != "Local" {
		return name
	}
	if loc == time.Local {
		if tz := strings.TrimPrefix(os.Getenv("TZ"), ":"); tz != "" {
			return tz
		}
		if target, err := os.Readlink("/etc/localtime"); err == nil {
			if _, zone, ok := strings.Cut(target, "zoneinfo/"); ok {
				return zone
			}
		}
	}
	name, _ := now.In(loc).Zone()
	return name
}
