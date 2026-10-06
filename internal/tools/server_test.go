package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // fixed zone data for the tests

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/AnduNacu43/iCloudMCP/internal/store"
	"github.com/AnduNacu43/iCloudMCP/internal/store/fake"
)

var bucharest = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Bucharest")
	if err != nil {
		panic(err)
	}
	return loc
}()

// testNow is Tuesday 6 October 2026, 18:00 in Bucharest (UTC+3).
var testNow = time.Date(2026, 10, 6, 18, 0, 0, 0, bucharest)

func local(month time.Month, day, hour, min int) time.Time {
	return time.Date(2026, month, day, hour, min, 0, 0, bucharest)
}

type env struct {
	t   *testing.T
	cs  *mcp.ClientSession
	cal *fake.CalendarStore
	rem *fake.ReminderStore
}

func newEnv(t *testing.T) *env {
	t.Helper()
	cal := fake.NewCalendarStore(
		store.Calendar{ID: "cal-home", Title: "Home", Type: "caldav", Source: "iCloud"},
		store.Calendar{ID: "cal-work", Title: "Work", Type: "caldav", Source: "iCloud"},
		store.Calendar{ID: "cal-hol", Title: "Holidays", Type: "subscription", ReadOnly: true},
	)
	rem := fake.NewReminderStore(
		store.ReminderList{ID: "l-inbox", Title: "Inbox", Source: "iCloud"},
		store.ReminderList{ID: "l-shop", Title: "Shopping", Source: "iCloud"},
		store.ReminderList{ID: "l-family", Title: "Family", ReadOnly: true},
	)
	rem.Now = func() time.Time { return testNow }

	server := NewServer(Config{Calendars: cal, Reminders: rem, Version: "test", Now: func() time.Time { return testNow }, Location: bucharest})
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
	t.Cleanup(func() { cs.Close(); ss.Wait() })
	return &env{t: t, cs: cs, cal: cal, rem: rem}
}

func (e *env) call(name string, args any) (*mcp.CallToolResult, error) {
	e.t.Helper()
	return e.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
}

// callOK calls a tool, fails the test on any error, and decodes the
// structured output into T.
func callOK[T any](e *env, name string, args any) T {
	e.t.Helper()
	res, err := e.call(name, args)
	if err != nil {
		e.t.Fatalf("%s: protocol error: %v", name, err)
	}
	if res.IsError {
		e.t.Fatalf("%s: tool error: %s", name, text(res))
	}
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		e.t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		e.t.Fatalf("%s: decoding %s: %v", name, data, err)
	}
	return out
}

// callErr calls a tool that must fail and returns the error text. Input
// rejected by the schema may surface as a protocol error or a tool error.
func callErr(e *env, name string, args any, wantSubstr string) {
	e.t.Helper()
	res, err := e.call(name, args)
	var msg string
	switch {
	case err != nil:
		msg = err.Error()
	case res.IsError:
		msg = text(res)
	default:
		e.t.Fatalf("%s(%v): want an error containing %q, got success: %s", name, args, wantSubstr, text(res))
	}
	if !strings.Contains(msg, wantSubstr) {
		e.t.Errorf("%s(%v): error %q does not contain %q", name, args, msg, wantSubstr)
	}
}

func text(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func rfc(t time.Time) string { return t.Format(time.RFC3339) }

func TestToolSurface(t *testing.T) {
	e := newEnv(t)
	res, err := e.cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		tools[tool.Name] = tool
	}
	want := map[string]string{
		"get_current_time": "read", "list_calendars": "read", "list_events": "read", "get_event": "read",
		"create_event": "add", "update_event": "change", "delete_event": "change",
		"list_reminder_lists": "read", "list_reminders": "read", "get_reminder": "read",
		"create_reminder": "add", "update_reminder": "change", "complete_reminder": "change", "delete_reminder": "change",
	}
	if len(tools) != len(want) {
		t.Errorf("got %d tools, want %d", len(tools), len(want))
	}
	for name, kind := range want {
		tool, ok := tools[name]
		if !ok {
			t.Errorf("missing tool %s", name)
			continue
		}
		a := tool.Annotations
		if a == nil || tool.Description == "" || tool.OutputSchema == nil {
			t.Errorf("%s: needs annotations, a description and an output schema", name)
			continue
		}
		switch kind {
		case "read":
			if !a.ReadOnlyHint {
				t.Errorf("%s should be read-only", name)
			}
		case "add":
			if a.ReadOnlyHint || a.DestructiveHint == nil || *a.DestructiveHint {
				t.Errorf("%s should be a non-destructive write", name)
			}
		case "change":
			if a.ReadOnlyHint || a.DestructiveHint == nil || !*a.DestructiveHint || !a.IdempotentHint {
				t.Errorf("%s should be an idempotent destructive write", name)
			}
		}
	}

	schema, _ := json.Marshal(tools["update_event"].InputSchema)
	if !strings.Contains(string(schema), `"enum":["this","future"]`) {
		t.Errorf("update_event span should be an enum, schema: %s", schema)
	}
}

func TestGetCurrentTime(t *testing.T) {
	e := newEnv(t)
	out := callOK[CurrentTimeOut](e, "get_current_time", nil)
	want := CurrentTimeOut{Now: "2026-10-06T18:00:00+03:00", TimeZone: "Europe/Bucharest", Today: "2026-10-06", Weekday: "Tuesday"}
	if out != want {
		t.Errorf("got %+v, want %+v", out, want)
	}
}

func TestResultsCarrySummaryAndJSON(t *testing.T) {
	e := newEnv(t)
	res, err := e.call("list_calendars", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) != 2 {
		t.Fatalf("want a summary and a JSON block, got %d content items", len(res.Content))
	}
	if got := res.Content[0].(*mcp.TextContent).Text; got != "Found 3 calendars." {
		t.Errorf("summary = %q", got)
	}
	if !strings.Contains(res.Content[1].(*mcp.TextContent).Text, `"read_only": true`) {
		t.Error("second block should be the JSON output")
	}
}

func TestListCalendars(t *testing.T) {
	e := newEnv(t)
	out := callOK[ListCalendarsOut](e, "list_calendars", nil)
	if len(out.Calendars) != 3 || out.Calendars[2].Title != "Holidays" || !out.Calendars[2].ReadOnly || out.Calendars[0].Source != "iCloud" {
		t.Errorf("got %+v", out.Calendars)
	}
}

func TestListEvents(t *testing.T) {
	e := newEnv(t)
	e.cal.AddEvent(store.Event{Title: "Yesterday", Start: local(10, 5, 9, 0), End: local(10, 5, 10, 0), CalendarID: "cal-home"})
	e.cal.AddEvent(store.Event{Title: "This morning", Start: local(10, 6, 9, 0), End: local(10, 6, 10, 0), CalendarID: "cal-work"})
	e.cal.AddEvent(store.Event{Title: "Saturday", Start: local(10, 10, 12, 0), End: local(10, 10, 13, 0), CalendarID: "cal-home", Notes: "bring cake"})
	e.cal.AddEvent(store.Event{Title: "In two weeks", Start: local(10, 20, 9, 0), End: local(10, 20, 10, 0), CalendarID: "cal-home"})

	out := callOK[ListEventsOut](e, "list_events", nil)
	if out.Start != "2026-10-06T00:00:00+03:00" || out.End != "2026-10-13T00:00:00+03:00" {
		t.Errorf("default range = %s to %s, want today to a week later", out.Start, out.End)
	}
	if got := eventTitles(out.Events); got != "This morning,Saturday" {
		t.Errorf("default range events = %s", got)
	}

	out = callOK[ListEventsOut](e, "list_events", map[string]any{"start": "2026-10-10", "end": "2026-10-10"})
	if got := eventTitles(out.Events); got != "Saturday" {
		t.Errorf("a bare end date should include that day, got %s", got)
	}
	if out.Events[0].Start != "2026-10-10T12:00:00+03:00" || out.Events[0].Calendar != "Home" {
		t.Errorf("event output = %+v", out.Events[0])
	}

	out = callOK[ListEventsOut](e, "list_events", map[string]any{"start": "2026-10-01", "end": "2026-10-31", "calendar": "work"})
	if got := eventTitles(out.Events); got != "This morning" {
		t.Errorf("calendar filter = %s", got)
	}
	out = callOK[ListEventsOut](e, "list_events", map[string]any{"start": "2026-10-01", "end": "2026-10-31", "search": "CAKE"})
	if got := eventTitles(out.Events); got != "Saturday" {
		t.Errorf("search = %s", got)
	}
	// A naive time is local time, and RFC 3339 keeps its offset.
	out = callOK[ListEventsOut](e, "list_events", map[string]any{"start": "2026-10-06T05:30:00Z", "end": "2026-10-06T09:30"})
	if got := eventTitles(out.Events); got != "This morning" {
		t.Errorf("mixed formats = %s", got)
	}

	callErr(e, "list_events", map[string]any{"start": "2026-10-10", "end": "2026-10-09"}, "end must be after start")
	callErr(e, "list_events", map[string]any{"start": "2026-01-01", "end": "2027-06-01"}, "at most 366 days")
	callErr(e, "list_events", map[string]any{"start": "next tuesday"}, "is not a valid time")
	callErr(e, "list_events", map[string]any{"calendar": "Gym"}, "available: Home, Work, Holidays")
}

func TestCreateEvent(t *testing.T) {
	e := newEnv(t)

	ev := callOK[EventOut](e, "create_event", map[string]any{"title": " Dentist ", "start": "2026-10-07T09:30", "alert_minutes_before": []int{15, 60}})
	if ev.Title != "Dentist" || ev.Start != "2026-10-07T09:30:00+03:00" || ev.End != "2026-10-07T10:30:00+03:00" || ev.AllDay {
		t.Errorf("timed event with default length = %+v", ev)
	}
	if ev.Calendar != "Home" || fmt.Sprint(ev.AlertMinutesBefore) != "[15 60]" {
		t.Errorf("default calendar and alerts = %+v", ev)
	}

	ev = callOK[EventOut](e, "create_event", map[string]any{"title": "Trip", "start": "2026-10-24", "end": "2026-10-26", "calendar": "cal-work"})
	if !ev.AllDay || ev.Start != "2026-10-24T00:00:00+03:00" || ev.End != "2026-10-26T00:00:00+02:00" || ev.Calendar != "Work" {
		t.Errorf("multi-day all-day event across the DST change = %+v", ev)
	}

	ev = callOK[EventOut](e, "create_event", map[string]any{"title": "Holiday", "start": "2026-10-09T15:00:00+03:00", "all_day": true})
	if !ev.AllDay || ev.Start != ev.End || ev.Start != "2026-10-09T00:00:00+03:00" {
		t.Errorf("all_day with a timed start = %+v", ev)
	}

	res, _ := e.call("create_event", map[string]any{"title": "Call", "start": "2026-10-07T09:00", "end": "2026-10-07T09:20"})
	if got := text(res); !strings.HasPrefix(got, `Created "Call" on Wed 7 Oct 2026, 09:00-09:20 in Home (id `) {
		t.Errorf("summary = %q", got)
	}

	callErr(e, "create_event", map[string]any{"title": "x", "start": "2026-10-07T10:00", "end": "2026-10-07T10:00"}, "end must be after start")
	callErr(e, "create_event", map[string]any{"title": "x", "start": "2026-10-07", "end": "2026-10-06"}, "must not be before its first day")
	callErr(e, "create_event", map[string]any{"title": "x", "start": "2026-10-07", "calendar": "Holidays"}, "cannot be modified")
	callErr(e, "create_event", map[string]any{"title": "  ", "start": "2026-10-07"}, "title must not be empty")
	callErr(e, "create_event", map[string]any{"title": "x", "start": "2026-10-07", "alert_minutes_before": []int{-5}}, "0 or more")
	callErr(e, "create_event", map[string]any{"title": "x", "start": "2026-10-07", "url": "example.com"}, "must be absolute")
	callErr(e, "create_event", map[string]any{"start": "2026-10-07"}, "title")
}

func TestUpdateEvent(t *testing.T) {
	e := newEnv(t)
	ev := e.cal.AddEvent(store.Event{Title: "Lunch", Start: local(10, 7, 12, 0), End: local(10, 7, 13, 30), CalendarID: "cal-home", Notes: "old"})

	out := callOK[EventOut](e, "update_event", map[string]any{"id": ev.ID, "start": "2026-10-08T13:00"})
	if out.Start != "2026-10-08T13:00:00+03:00" || out.End != "2026-10-08T14:30:00+03:00" {
		t.Errorf("moving the start should keep the length: %+v", out)
	}
	out = callOK[EventOut](e, "update_event", map[string]any{"id": ev.ID, "end": "2026-10-08T15:00", "notes": "", "calendar": "Work"})
	if out.Start != "2026-10-08T13:00:00+03:00" || out.End != "2026-10-08T15:00:00+03:00" || out.Notes != "" || out.Calendar != "Work" {
		t.Errorf("end, notes and calendar change: %+v", out)
	}
	out = callOK[EventOut](e, "update_event", map[string]any{"id": ev.ID, "title": "Long lunch"})
	if out.Title != "Long lunch" || out.End != "2026-10-08T15:00:00+03:00" {
		t.Errorf("title-only change should keep times: %+v", out)
	}
	out = callOK[EventOut](e, "update_event", map[string]any{"id": ev.ID, "all_day": true})
	if !out.AllDay || out.Start != "2026-10-08T00:00:00+03:00" || out.End != out.Start {
		t.Errorf("timed to all-day: %+v", out)
	}
	out = callOK[EventOut](e, "update_event", map[string]any{"id": ev.ID, "start": "2026-10-23", "end": "2026-10-26"})
	if out.Start != "2026-10-23T00:00:00+03:00" || out.End != "2026-10-26T00:00:00+02:00" {
		t.Errorf("all-day range: %+v", out)
	}
	out = callOK[EventOut](e, "update_event", map[string]any{"id": ev.ID, "start": "2026-11-02"})
	if out.Start != "2026-11-02T00:00:00+02:00" || out.End != "2026-11-05T00:00:00+02:00" {
		t.Errorf("moving an all-day event should keep its days: %+v", out)
	}
	out = callOK[EventOut](e, "update_event", map[string]any{"id": ev.ID, "start": "2026-11-02T10:00"})
	if out.AllDay || out.End != "2026-11-02T11:00:00+02:00" {
		t.Errorf("all-day to timed should default to one hour: %+v", out)
	}

	out = callOK[EventOut](e, "update_event", map[string]any{"id": ev.ID, "url": "https://meet.example/abc", "alert_minutes_before": []int{}, "location": "Cafe"})
	if out.URL != "https://meet.example/abc" || len(out.AlertMinutesBefore) != 0 || out.Location != "Cafe" {
		t.Errorf("url, alerts and location change: %+v", out)
	}

	callErr(e, "update_event", map[string]any{"id": ev.ID, "url": "meet.example"}, "must be absolute")
	callErr(e, "update_event", map[string]any{"id": ev.ID, "alert_minutes_before": []int{-1}}, "0 or more")
	callErr(e, "update_event", map[string]any{"id": ev.ID, "calendar": " "}, "calendar must not be empty")
	callErr(e, "update_event", map[string]any{"id": ev.ID, "end": "2026-11-02T09:00"}, "end must be after start")
	callErr(e, "update_event", map[string]any{"id": "nope", "title": "x"}, "not found")
	callErr(e, "update_event", map[string]any{"id": ev.ID, "span": "all"}, "span")
}

func TestRecurringEventSpan(t *testing.T) {
	e := newEnv(t)
	ev := e.cal.AddEvent(store.Event{Title: "Standup", Start: local(10, 7, 9, 0), End: local(10, 7, 9, 15), CalendarID: "cal-work", Recurring: true})

	callErr(e, "update_event", map[string]any{"id": ev.ID, "title": "Daily"}, "changing a single occurrence is not supported")
	out := callOK[EventOut](e, "update_event", map[string]any{"id": ev.ID, "title": "Daily", "span": "future"})
	if out.Title != "Daily" || e.cal.LastSpan != store.SpanFuture {
		t.Errorf("span future: %+v, LastSpan %v", out, e.cal.LastSpan)
	}
	callErr(e, "delete_event", map[string]any{"id": ev.ID}, "changing a single occurrence is not supported")
	callOK[DeleteOut](e, "delete_event", map[string]any{"id": ev.ID, "span": "future"})
}

func TestDeleteEvent(t *testing.T) {
	e := newEnv(t)
	ev := e.cal.AddEvent(store.Event{Title: "Old", Start: local(10, 7, 9, 0), End: local(10, 7, 10, 0), CalendarID: "cal-home"})
	hol := e.cal.AddEvent(store.Event{Title: "National day", Start: local(12, 1, 0, 0), End: local(12, 1, 0, 0), AllDay: true, CalendarID: "cal-hol"})

	out := callOK[DeleteOut](e, "delete_event", map[string]any{"id": ev.ID})
	if !out.Deleted || out.ID != ev.ID {
		t.Errorf("got %+v", out)
	}
	callErr(e, "get_event", map[string]any{"id": ev.ID}, "not found")
	callErr(e, "delete_event", map[string]any{"id": hol.ID}, "cannot be modified")

	got := callOK[EventOut](e, "get_event", map[string]any{"id": hol.ID})
	if got.Title != "National day" || !got.AllDay {
		t.Errorf("get_event = %+v", got)
	}
}

func TestAccessDeniedIsAToolError(t *testing.T) {
	e := newEnv(t)
	e.cal.Err = fmt.Errorf("%w: grant access in System Settings", store.ErrAccessDenied)
	e.rem.Err = errors.New("reminders unavailable")
	callErr(e, "list_events", nil, "grant access in System Settings")
	callErr(e, "list_reminders", nil, "reminders unavailable")
	// get_current_time does not depend on any store.
	callOK[CurrentTimeOut](e, "get_current_time", nil)
}

func eventTitles(events []EventOut) string {
	var titles []string
	for _, e := range events {
		titles = append(titles, e.Title)
	}
	return strings.Join(titles, ",")
}
