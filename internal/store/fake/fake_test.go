package fake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AnduNacu43/iCloudMCP/internal/store"
)

var ctx = context.Background()

func at(hour int) time.Time { return time.Date(2026, 10, 7, hour, 0, 0, 0, time.UTC) }

func newCalendars() *CalendarStore {
	return NewCalendarStore(
		store.Calendar{ID: "cal-home", Title: "Home"},
		store.Calendar{ID: "cal-work", Title: "Work"},
		store.Calendar{ID: "cal-hol", Title: "Holidays", ReadOnly: true},
	)
}

func TestListEventsFiltersByOverlapCalendarAndSearch(t *testing.T) {
	s := newCalendars()
	s.AddEvent(store.Event{Title: "Late", Start: at(15), End: at(16), CalendarID: "cal-work"})
	s.AddEvent(store.Event{Title: "Standup", Start: at(9), End: at(10), CalendarID: "cal-work", Notes: "daily sync"})
	s.AddEvent(store.Event{Title: "Gym", Start: at(9), End: at(11), CalendarID: "cal-home"})
	s.AddEvent(store.Event{Title: "Tomorrow", Start: at(33), End: at(34), CalendarID: "cal-home"})

	all, _ := s.ListEvents(ctx, store.EventFilter{Start: at(10), End: at(16)})
	if got := titles(all); got != "Gym,Late" {
		t.Errorf("overlap filter gave %s, want Gym,Late (Standup ends exactly at the range start)", got)
	}
	work, _ := s.ListEvents(ctx, store.EventFilter{Start: at(0), End: at(24), Calendar: "work"})
	if got := titles(work); got != "Standup,Late" {
		t.Errorf("calendar filter gave %s, want Standup,Late sorted by start", got)
	}
	found, _ := s.ListEvents(ctx, store.EventFilter{Start: at(0), End: at(24), Search: "SYNC"})
	if got := titles(found); got != "Standup" {
		t.Errorf("search gave %s, want Standup", got)
	}
	if _, err := s.ListEvents(ctx, store.EventFilter{Start: at(0), End: at(24), Calendar: "Gym"}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown calendar err = %v, want ErrNotFound", err)
	}
}

func TestCreateEventDefaultsAndReadOnly(t *testing.T) {
	s := newCalendars()
	e, err := s.CreateEvent(ctx, store.NewEvent{Title: "Dentist", Start: at(9), End: at(10)})
	if err != nil || e.CalendarID != "cal-home" || e.Calendar != "Home" {
		t.Fatalf("default calendar: got %+v, %v", e, err)
	}
	if _, err := s.CreateEvent(ctx, store.NewEvent{Title: "x", Start: at(9), End: at(10), Calendar: "Holidays"}); !errors.Is(err, store.ErrReadOnly) {
		t.Errorf("read-only create err = %v, want ErrReadOnly", err)
	}
	if _, err := s.CreateEvent(ctx, store.NewEvent{Title: "x", Start: at(10), End: at(9)}); !errors.Is(err, store.ErrInvalid) {
		t.Errorf("end before start err = %v, want ErrInvalid", err)
	}
}

func TestUpdateAndDeleteEvent(t *testing.T) {
	s := newCalendars()
	e := s.AddEvent(store.Event{Title: "Lunch", Start: at(12), End: at(13), CalendarID: "cal-home"})

	title, cal, alerts := "Team lunch", "Work", []int{10}
	got, err := s.UpdateEvent(ctx, e.ID, store.EventUpdate{Title: &title, Calendar: &cal, AlertMinutesBefore: &alerts}, store.SpanThis)
	if err != nil || got.Title != title || got.CalendarID != "cal-work" || got.AlertMinutesBefore[0] != 10 || !got.Start.Equal(at(12)) {
		t.Fatalf("update gave %+v, %v", got, err)
	}
	if err := s.DeleteEvent(ctx, e.ID, store.SpanThis); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetEvent(ctx, e.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("get after delete err = %v, want ErrNotFound", err)
	}
}

func TestRecurringEventsNeedFutureSpan(t *testing.T) {
	s := newCalendars()
	e := s.AddEvent(store.Event{Title: "Standup", Start: at(9), End: at(10), CalendarID: "cal-work", Recurring: true})
	title := "Daily standup"
	if _, err := s.UpdateEvent(ctx, e.ID, store.EventUpdate{Title: &title}, store.SpanThis); !errors.Is(err, store.ErrInvalid) {
		t.Errorf("span this on a series err = %v, want ErrInvalid", err)
	}
	if _, err := s.UpdateEvent(ctx, e.ID, store.EventUpdate{Title: &title}, store.SpanFuture); err != nil || s.LastSpan != store.SpanFuture {
		t.Errorf("span future: err = %v, LastSpan = %v", err, s.LastSpan)
	}
}

func TestReadOnlyEventsCannotChange(t *testing.T) {
	s := newCalendars()
	e := s.AddEvent(store.Event{Title: "New Year", Start: at(0), End: at(24), CalendarID: "cal-hol"})
	if err := s.DeleteEvent(ctx, e.ID, store.SpanThis); !errors.Is(err, store.ErrReadOnly) {
		t.Errorf("delete in read-only calendar err = %v, want ErrReadOnly", err)
	}
}

func TestErrOverridesEverything(t *testing.T) {
	s := newCalendars()
	s.Err = store.ErrAccessDenied
	if _, err := s.ListCalendars(ctx); !errors.Is(err, store.ErrAccessDenied) {
		t.Errorf("err = %v, want ErrAccessDenied", err)
	}
}

func newReminders() *ReminderStore {
	s := NewReminderStore(
		store.ReminderList{ID: "l-inbox", Title: "Inbox"},
		store.ReminderList{ID: "l-shop", Title: "Shopping"},
		store.ReminderList{ID: "l-shared", Title: "Family", ReadOnly: true},
	)
	s.Now = func() time.Time { return at(18) }
	return s
}

func TestListRemindersFilters(t *testing.T) {
	s := newReminders()
	d9, d20 := at(9), at(20)
	s.AddReminder(store.Reminder{Title: "Milk", ListID: "l-shop", Due: &d9})
	s.AddReminder(store.Reminder{Title: "Eggs", ListID: "l-shop", Completed: true})
	s.AddReminder(store.Reminder{Title: "Call mum", ListID: "l-inbox", Due: &d20, Notes: "about the weekend"})

	open := false
	r, _ := s.ListReminders(ctx, store.ReminderFilter{Completed: &open})
	if got := remTitles(r); got != "Milk,Call mum" {
		t.Errorf("open reminders = %s", got)
	}
	r, _ = s.ListReminders(ctx, store.ReminderFilter{List: "shopping"})
	if got := remTitles(r); got != "Milk,Eggs" {
		t.Errorf("Shopping list = %s", got)
	}
	noon := at(12)
	r, _ = s.ListReminders(ctx, store.ReminderFilter{DueBefore: &noon})
	if got := remTitles(r); got != "Milk" {
		t.Errorf("due before noon = %s (undated reminders must be excluded)", got)
	}
	r, _ = s.ListReminders(ctx, store.ReminderFilter{DueAfter: &noon, Search: "WEEKEND"})
	if got := remTitles(r); got != "Call mum" {
		t.Errorf("due after noon with search = %s", got)
	}

	lists, _ := s.ListReminderLists(ctx)
	if lists[1].Count != 1 {
		t.Errorf("Shopping open count = %d, want 1", lists[1].Count)
	}
}

func TestReminderLifecycle(t *testing.T) {
	s := newReminders()
	due := at(9)
	r, err := s.CreateReminder(ctx, store.NewReminder{Title: "Pay rent", Due: &due, Priority: store.PriorityHigh})
	if err != nil || r.ListID != "l-inbox" || r.Priority != store.PriorityHigh {
		t.Fatalf("create gave %+v, %v", r, err)
	}
	due = at(23)
	if got, _ := s.GetReminder(ctx, r.ID); !got.Due.Equal(at(9)) {
		t.Error("changing the caller's time after create must not change the stored reminder")
	}

	list, low := "Shopping", store.PriorityLow
	r, err = s.UpdateReminder(ctx, r.ID, store.ReminderUpdate{List: &list, Priority: &low, ClearDue: true})
	if err != nil || r.ListID != "l-shop" || r.Priority != store.PriorityLow || r.Due != nil {
		t.Fatalf("update gave %+v, %v", r, err)
	}

	r, _ = s.SetCompleted(ctx, r.ID, true)
	if !r.Completed || r.CompletionDate == nil || !r.CompletionDate.Equal(at(18)) {
		t.Errorf("complete gave %+v", r)
	}
	r, _ = s.SetCompleted(ctx, r.ID, false)
	if r.Completed || r.CompletionDate != nil {
		t.Errorf("uncomplete gave %+v", r)
	}

	if err := s.DeleteReminder(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetReminder(ctx, r.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("get after delete err = %v", err)
	}
}

func TestReadOnlyReminderList(t *testing.T) {
	s := newReminders()
	if _, err := s.CreateReminder(ctx, store.NewReminder{Title: "x", List: "Family"}); !errors.Is(err, store.ErrReadOnly) {
		t.Errorf("create in read-only list err = %v", err)
	}
	r := s.AddReminder(store.Reminder{Title: "Shared", ListID: "l-shared"})
	if _, err := s.SetCompleted(ctx, r.ID, true); !errors.Is(err, store.ErrReadOnly) {
		t.Errorf("complete in read-only list err = %v", err)
	}
}

func titles(events []store.Event) string {
	s := ""
	for i, e := range events {
		if i > 0 {
			s += ","
		}
		s += e.Title
	}
	return s
}

func remTitles(rs []store.Reminder) string {
	s := ""
	for i, r := range rs {
		if i > 0 {
			s += ","
		}
		s += r.Title
	}
	return s
}
