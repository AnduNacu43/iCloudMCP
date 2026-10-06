// Package store defines the calendar and reminder storage interfaces used by
// the MCP tools, together with the domain types they exchange.
//
// The tools depend only on these interfaces. The eventkit sub-package
// implements them over Apple's EventKit, and the fake sub-package implements
// them in memory for tests.
package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Sentinel errors. Implementations wrap them with a human-readable message so
// callers can both test with errors.Is and show the message to the user.
var (
	ErrNotFound     = errors.New("not found")
	ErrAccessDenied = errors.New("access denied")
	ErrReadOnly     = errors.New("read-only")
	ErrInvalid      = errors.New("invalid input")
)

// Calendar is an event calendar such as an iCloud, local or subscribed one.
type Calendar struct {
	ID       string
	Title    string
	Type     string // local, caldav, exchange, subscription, birthday
	Source   string // account name, for example "iCloud"
	Color    string // hex, for example "#1BADF8"
	ReadOnly bool
}

// Event is a calendar event, or one occurrence of a recurring event.
type Event struct {
	ID                 string
	Title              string
	Start              time.Time
	End                time.Time
	AllDay             bool
	Calendar           string
	CalendarID         string
	Location           string
	Notes              string
	URL                string
	Recurring          bool
	AlertMinutesBefore []int
	Status             string // none, confirmed, tentative, canceled
}

// Span selects which occurrences of a recurring event a change applies to.
type Span int

const (
	SpanThis   Span = iota // only this occurrence
	SpanFuture             // this and all future occurrences
)

// EventFilter selects events in a time range. Calendar is a calendar ID or
// title; empty means all calendars. Search matches title, location and notes.
type EventFilter struct {
	Start    time.Time
	End      time.Time
	Calendar string
	Search   string
}

// NewEvent describes an event to create. Calendar is an ID or title; empty
// means the system's default calendar for new events.
type NewEvent struct {
	Title              string
	Start              time.Time
	End                time.Time
	AllDay             bool
	Calendar           string
	Location           string
	Notes              string
	URL                string
	AlertMinutesBefore []int
}

// EventUpdate holds the fields to change on an event. Nil fields are left
// untouched.
type EventUpdate struct {
	Title              *string
	Start              *time.Time
	End                *time.Time
	AllDay             *bool
	Calendar           *string
	Location           *string
	Notes              *string
	URL                *string
	AlertMinutesBefore *[]int
}

// CalendarStore reads and writes calendar events.
type CalendarStore interface {
	ListCalendars(ctx context.Context) ([]Calendar, error)
	ListEvents(ctx context.Context, f EventFilter) ([]Event, error)
	GetEvent(ctx context.Context, id string) (Event, error)
	CreateEvent(ctx context.Context, in NewEvent) (Event, error)
	UpdateEvent(ctx context.Context, id string, u EventUpdate, span Span) (Event, error)
	DeleteEvent(ctx context.Context, id string, span Span) error
}

// Priority is a reminder priority.
type Priority string

const (
	PriorityNone   Priority = "none"
	PriorityLow    Priority = "low"
	PriorityMedium Priority = "medium"
	PriorityHigh   Priority = "high"
)

// ParsePriority converts a priority name to a Priority. The empty string
// means none.
func ParsePriority(s string) (Priority, error) {
	switch p := Priority(strings.ToLower(strings.TrimSpace(s))); p {
	case "":
		return PriorityNone, nil
	case PriorityNone, PriorityLow, PriorityMedium, PriorityHigh:
		return p, nil
	default:
		return "", fmt.Errorf("%w: priority must be none, low, medium or high, got %q", ErrInvalid, s)
	}
}

// ReminderList is a list of reminders, such as "Groceries".
type ReminderList struct {
	ID       string
	Title    string
	Color    string
	Source   string
	Count    int
	ReadOnly bool
}

// Reminder is a single reminder.
type Reminder struct {
	ID             string
	Title          string
	Notes          string
	List           string
	ListID         string
	Due            *time.Time
	Completed      bool
	CompletionDate *time.Time
	Priority       Priority
	Flagged        bool
	URL            string
	Recurring      bool
}

// ReminderFilter selects reminders. List is a list ID or title; empty means
// all lists. A nil Completed returns both completed and open reminders.
type ReminderFilter struct {
	List      string
	Completed *bool
	DueBefore *time.Time
	DueAfter  *time.Time
	Search    string
}

// NewReminder describes a reminder to create. List is an ID or title; empty
// means the system's default reminders list.
type NewReminder struct {
	Title    string
	List     string
	Notes    string
	Due      *time.Time
	Priority Priority
	Flagged  bool
	URL      string
}

// ReminderUpdate holds the fields to change on a reminder. Nil fields are
// left untouched. ClearDue removes the due date and wins over Due.
type ReminderUpdate struct {
	Title    *string
	Notes    *string
	List     *string
	Due      *time.Time
	ClearDue bool
	Priority *Priority
	Flagged  *bool
	URL      *string
}

// ReminderStore reads and writes reminders.
type ReminderStore interface {
	ListReminderLists(ctx context.Context) ([]ReminderList, error)
	ListReminders(ctx context.Context, f ReminderFilter) ([]Reminder, error)
	GetReminder(ctx context.Context, id string) (Reminder, error)
	CreateReminder(ctx context.Context, in NewReminder) (Reminder, error)
	UpdateReminder(ctx context.Context, id string, u ReminderUpdate) (Reminder, error)
	SetCompleted(ctx context.Context, id string, completed bool) (Reminder, error)
	DeleteReminder(ctx context.Context, id string) error
}

// ResolveCalendar finds the calendar named by ref, which is an exact ID or a
// case-insensitive title. A title shared by several calendars is ambiguous
// and must be given as an ID.
func ResolveCalendar(cals []Calendar, ref string) (Calendar, error) {
	return resolve(cals, ref, "calendar",
		func(c Calendar) string { return c.ID },
		func(c Calendar) string { return c.Title })
}

// ResolveList finds the reminder list named by ref, which is an exact ID or a
// case-insensitive title. A title shared by several lists is ambiguous and
// must be given as an ID.
func ResolveList(lists []ReminderList, ref string) (ReminderList, error) {
	return resolve(lists, ref, "reminder list",
		func(l ReminderList) string { return l.ID },
		func(l ReminderList) string { return l.Title })
}

func resolve[T any](items []T, ref, kind string, id, title func(T) string) (T, error) {
	var zero T
	ref = strings.TrimSpace(ref)
	for _, it := range items {
		if id(it) == ref {
			return it, nil
		}
	}
	var matches []T
	for _, it := range items {
		if strings.EqualFold(title(it), ref) {
			matches = append(matches, it)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		names := make([]string, 0, len(items))
		for _, it := range items {
			names = append(names, title(it))
		}
		return zero, fmt.Errorf("%w: no %s matches %q (available: %s)", ErrNotFound, kind, ref, strings.Join(names, ", "))
	default:
		ids := make([]string, 0, len(matches))
		for _, m := range matches {
			ids = append(ids, id(m))
		}
		return zero, fmt.Errorf("%w: %d %ss are named %q; pass one of these IDs instead: %s", ErrInvalid, len(matches), kind, ref, strings.Join(ids, ", "))
	}
}

// TitleIsUnique reports whether no other item shares the title of the item
// with the given ID. EventKit writes address calendars and lists by title,
// so a duplicated title cannot be targeted reliably.
func TitleIsUnique[T any](items []T, target T, title func(T) string) bool {
	n := 0
	for _, it := range items {
		if strings.EqualFold(title(it), title(target)) {
			n++
		}
	}
	return n <= 1
}
