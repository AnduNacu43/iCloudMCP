package tools

import (
	"strings"
	"testing"

	"github.com/AnduNacu43/iCloudMCP/internal/store"
)

func seedReminders(e *env) (milk, eggs, call store.Reminder) {
	d9, d20 := local(10, 7, 9, 0), local(10, 9, 20, 0)
	milk = e.rem.AddReminder(store.Reminder{Title: "Milk", ListID: "l-shop", Due: &d9, Priority: store.PriorityHigh})
	eggs = e.rem.AddReminder(store.Reminder{Title: "Eggs", ListID: "l-shop", Completed: true})
	call = e.rem.AddReminder(store.Reminder{Title: "Call mum", ListID: "l-inbox", Due: &d20, Notes: "about the weekend"})
	return
}

func TestListReminderLists(t *testing.T) {
	e := newEnv(t)
	seedReminders(e)
	out := callOK[ListReminderListsOut](e, "list_reminder_lists", nil)
	if len(out.Lists) != 3 || out.Lists[1].Title != "Shopping" || out.Lists[1].Count != 1 || !out.Lists[2].ReadOnly {
		t.Errorf("got %+v", out.Lists)
	}
}

func TestListReminders(t *testing.T) {
	e := newEnv(t)
	seedReminders(e)

	out := callOK[ListRemindersOut](e, "list_reminders", nil)
	if got := reminderTitles(out.Reminders); got != "Milk,Call mum" {
		t.Errorf("open reminders by default, got %s", got)
	}
	if r := out.Reminders[0]; r.Due != "2026-10-07T09:00:00+03:00" || r.Priority != "high" || r.List != "Shopping" || r.ListID != "l-shop" {
		t.Errorf("reminder output = %+v", r)
	}
	if out.Reminders[0].CompletionDate != "" {
		t.Error("open reminders have no completion date")
	}

	out = callOK[ListRemindersOut](e, "list_reminders", map[string]any{"completed": true})
	if got := reminderTitles(out.Reminders); got != "Eggs" {
		t.Errorf("completed reminders = %s", got)
	}
	out = callOK[ListRemindersOut](e, "list_reminders", map[string]any{"list": "shopping"})
	if got := reminderTitles(out.Reminders); got != "Milk" {
		t.Errorf("Shopping list = %s", got)
	}
	out = callOK[ListRemindersOut](e, "list_reminders", map[string]any{"due_before": "2026-10-07"})
	if got := reminderTitles(out.Reminders); got != "Milk" {
		t.Errorf("due_before a bare date should include that day, got %s", got)
	}
	out = callOK[ListRemindersOut](e, "list_reminders", map[string]any{"due_after": "2026-10-08", "search": "WEEKEND"})
	if got := reminderTitles(out.Reminders); got != "Call mum" {
		t.Errorf("due_after with search = %s", got)
	}

	res, _ := e.call("list_reminders", map[string]any{"list": "Inbox"})
	if got := text(res); !strings.HasPrefix(got, "Found 1 open reminder.") {
		t.Errorf("summary = %q", got)
	}

	callErr(e, "list_reminders", map[string]any{"due_before": "2026-10-07", "due_after": "2026-10-09"}, "due_before must be later")
	callErr(e, "list_reminders", map[string]any{"list": "Chores"}, "available: Inbox, Shopping, Family")
}

func TestCreateReminder(t *testing.T) {
	e := newEnv(t)

	r := callOK[ReminderOut](e, "create_reminder", map[string]any{"title": "Pay rent", "due": "2026-10-09", "priority": "high", "flagged": true})
	if r.List != "Inbox" || r.Due != "2026-10-09T09:00:00+03:00" || r.Priority != "high" || !r.Flagged || r.Completed {
		t.Errorf("got %+v", r)
	}
	r = callOK[ReminderOut](e, "create_reminder", map[string]any{"title": "Bread", "list": "Shopping", "due": "2026-10-07T17:45", "url": "https://shop.example"})
	if r.ListID != "l-shop" || r.Due != "2026-10-07T17:45:00+03:00" || r.Priority != "none" || r.URL != "https://shop.example" {
		t.Errorf("got %+v", r)
	}
	r = callOK[ReminderOut](e, "create_reminder", map[string]any{"title": "Someday"})
	if r.Due != "" {
		t.Errorf("no due date expected, got %q", r.Due)
	}

	callErr(e, "create_reminder", map[string]any{"title": "x", "priority": "urgent"}, "priority")
	callErr(e, "create_reminder", map[string]any{"title": "x", "list": "Family"}, "cannot be modified")
	callErr(e, "create_reminder", map[string]any{"title": "x", "due": "soon"}, "is not a valid time")
}

func TestUpdateReminder(t *testing.T) {
	e := newEnv(t)
	milk, _, _ := seedReminders(e)

	r := callOK[ReminderOut](e, "update_reminder", map[string]any{"id": milk.ID, "title": "Oat milk", "list": "Inbox", "priority": "low", "due": "2026-10-08"})
	if r.Title != "Oat milk" || r.ListID != "l-inbox" || r.Priority != "low" || r.Due != "2026-10-08T09:00:00+03:00" {
		t.Errorf("got %+v", r)
	}
	r = callOK[ReminderOut](e, "update_reminder", map[string]any{"id": milk.ID, "clear_due_date": true, "flagged": true})
	if r.Due != "" || !r.Flagged || r.Title != "Oat milk" {
		t.Errorf("clear due date: %+v", r)
	}

	callErr(e, "update_reminder", map[string]any{"id": milk.ID, "due": "2026-10-08", "clear_due_date": true}, "not both")
	callErr(e, "update_reminder", map[string]any{"id": milk.ID, "title": ""}, "title must not be empty")
	callErr(e, "update_reminder", map[string]any{"id": "missing", "title": "x"}, "not found")
}

func TestCompleteAndDeleteReminder(t *testing.T) {
	e := newEnv(t)
	milk, _, _ := seedReminders(e)

	r := callOK[ReminderOut](e, "complete_reminder", map[string]any{"id": milk.ID})
	if !r.Completed || r.CompletionDate != "2026-10-06T18:00:00+03:00" {
		t.Errorf("complete: %+v", r)
	}
	r = callOK[ReminderOut](e, "complete_reminder", map[string]any{"id": milk.ID, "completed": false})
	if r.Completed || r.CompletionDate != "" {
		t.Errorf("reopen: %+v", r)
	}

	got := callOK[ReminderOut](e, "get_reminder", map[string]any{"id": milk.ID})
	if got.Title != "Milk" || got.Completed {
		t.Errorf("get_reminder: %+v", got)
	}

	shared := e.rem.AddReminder(store.Reminder{Title: "Family dinner", ListID: "l-family"})
	callErr(e, "delete_reminder", map[string]any{"id": shared.ID}, "cannot be modified")
	callErr(e, "complete_reminder", map[string]any{"id": shared.ID}, "cannot be modified")

	out := callOK[DeleteOut](e, "delete_reminder", map[string]any{"id": milk.ID})
	if !out.Deleted {
		t.Errorf("got %+v", out)
	}
	callErr(e, "get_reminder", map[string]any{"id": milk.ID}, "not found")
}

func reminderTitles(rs []ReminderOut) string {
	var titles []string
	for _, r := range rs {
		titles = append(titles, r.Title)
	}
	return strings.Join(titles, ",")
}
