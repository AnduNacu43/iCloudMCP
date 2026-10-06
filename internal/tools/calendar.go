package tools

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/AnduNacu43/iCloudMCP/internal/store"
)

// CurrentTimeOut is the output of get_current_time.
type CurrentTimeOut struct {
	Now      string `json:"now" jsonschema:"current time, RFC 3339 with the local UTC offset"`
	TimeZone string `json:"time_zone" jsonschema:"IANA time zone of this Mac"`
	Today    string `json:"today" jsonschema:"today's date, YYYY-MM-DD"`
	Weekday  string `json:"weekday" jsonschema:"today's day of the week"`
}

// CalendarOut describes one calendar.
type CalendarOut struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Type     string `json:"type" jsonschema:"local, caldav (iCloud and other CalDAV accounts), exchange, subscription or birthday"`
	Source   string `json:"source" jsonschema:"account the calendar belongs to"`
	Color    string `json:"color"`
	ReadOnly bool   `json:"read_only" jsonschema:"true when events cannot be added or changed"`
}

// ListCalendarsOut is the output of list_calendars.
type ListCalendarsOut struct {
	Calendars []CalendarOut `json:"calendars"`
}

// EventOut describes one event.
type EventOut struct {
	ID                 string `json:"id"`
	Title              string `json:"title"`
	Start              string `json:"start" jsonschema:"RFC 3339"`
	End                string `json:"end" jsonschema:"RFC 3339"`
	AllDay             bool   `json:"all_day"`
	Calendar           string `json:"calendar"`
	CalendarID         string `json:"calendar_id"`
	Location           string `json:"location,omitempty"`
	Notes              string `json:"notes,omitempty"`
	URL                string `json:"url,omitempty"`
	Recurring          bool   `json:"recurring" jsonschema:"true when the event is part of a repeating series"`
	AlertMinutesBefore []int  `json:"alert_minutes_before" jsonschema:"alerts, in minutes before the start"`
	Status             string `json:"status"`
}

// ListEventsIn is the input of list_events.
type ListEventsIn struct {
	Start    string `json:"start,omitempty" jsonschema:"range start; defaults to the start of today"`
	End      string `json:"end,omitempty" jsonschema:"range end; a bare date includes that whole day; defaults to 7 days after start; at most 366 days after start"`
	Calendar string `json:"calendar,omitempty" jsonschema:"calendar ID or title; omit for all calendars"`
	Search   string `json:"search,omitempty" jsonschema:"case-insensitive text matched against title, location and notes"`
}

// ListEventsOut is the output of list_events.
type ListEventsOut struct {
	Start  string     `json:"start" jsonschema:"effective range start"`
	End    string     `json:"end" jsonschema:"effective range end"`
	Events []EventOut `json:"events"`
}

// IDIn is the input of tools that take only an ID.
type IDIn struct {
	ID string `json:"id" jsonschema:"ID from a list or create tool"`
}

// CreateEventIn is the input of create_event.
type CreateEventIn struct {
	Title              string `json:"title"`
	Start              string `json:"start" jsonschema:"start time; a bare date creates an all-day event"`
	End                string `json:"end,omitempty" jsonschema:"end time; defaults to 1 hour after start. For all-day events, the last day (inclusive); defaults to the start day"`
	AllDay             bool   `json:"all_day,omitempty" jsonschema:"make an all-day event; implied when start is a bare date"`
	Calendar           string `json:"calendar,omitempty" jsonschema:"calendar ID or title; defaults to the default calendar for new events"`
	Location           string `json:"location,omitempty"`
	Notes              string `json:"notes,omitempty"`
	URL                string `json:"url,omitempty"`
	AlertMinutesBefore []int  `json:"alert_minutes_before,omitempty" jsonschema:"alerts in minutes before the start, for example [15] or [0, 60]; when given, they replace the calendar's default alerts"`
}

// UpdateEventIn is the input of update_event.
type UpdateEventIn struct {
	ID                 string  `json:"id"`
	Title              *string `json:"title,omitempty"`
	Start              *string `json:"start,omitempty" jsonschema:"new start; when end is omitted the event keeps its length"`
	End                *string `json:"end,omitempty" jsonschema:"new end; for all-day events, the last day (inclusive)"`
	AllDay             *bool   `json:"all_day,omitempty"`
	Calendar           *string `json:"calendar,omitempty" jsonschema:"move the event to this calendar ID or title"`
	Location           *string `json:"location,omitempty" jsonschema:"empty string clears it"`
	Notes              *string `json:"notes,omitempty" jsonschema:"empty string clears it"`
	URL                *string `json:"url,omitempty" jsonschema:"empty string clears it"`
	AlertMinutesBefore *[]int  `json:"alert_minutes_before,omitempty" jsonschema:"replaces all alerts; [] removes them"`
	Span               string  `json:"span,omitempty" jsonschema:"for repeating events: 'future' changes this and later occurrences. 'this' (the default) changes a single occurrence, which is only supported for events that do not repeat"`
}

// DeleteEventIn is the input of delete_event.
type DeleteEventIn struct {
	ID   string `json:"id"`
	Span string `json:"span,omitempty" jsonschema:"for repeating events: 'future' deletes this and later occurrences. 'this' (the default) is only supported for events that do not repeat"`
}

var spanEnum = map[string][]string{"span": {"this", "future"}}

func (h *handlers) registerCalendarTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_current_time",
		Description: "Returns the current date, time, weekday and time zone of this Mac. Call it before turning relative dates such as 'tomorrow' or 'next Friday' into times.",
		Annotations: readOnly("Get current time"),
	}, h.getCurrentTime)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_calendars",
		Description: "Lists every calendar on this Mac (iCloud, local, subscribed and other accounts) with its ID and whether it is read-only.",
		Annotations: readOnly("List calendars"),
	}, h.listCalendars)

	mcp.AddTool(s, &mcp.Tool{
		Name: "list_events",
		Description: "Lists calendar events that overlap a time range, sorted by start. Defaults to the next 7 days starting today. " +
			"Use it to find an event's ID before updating or deleting it. Each occurrence of a repeating event is listed separately.",
		Annotations: readOnly("List events"),
	}, h.listEvents)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_event",
		Description: "Returns one calendar event by ID.",
		Annotations: readOnly("Get event"),
	}, h.getEvent)

	mcp.AddTool(s, &mcp.Tool{
		Name: "create_event",
		Description: "Creates a calendar event. Pass start and end as RFC 3339 or as local time without an offset. " +
			"Pass a bare date (YYYY-MM-DD) as start for an all-day event. Returns the created event with its ID.",
		Annotations: additive("Create event"),
	}, h.createEvent)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_event",
		Description: "Changes fields of an existing event; omitted fields stay as they are. Moving the start without an end keeps the event's length.",
		InputSchema: schemaFor[UpdateEventIn](spanEnum),
		Annotations: modifying("Update event", true),
	}, h.updateEvent)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_event",
		Description: "Permanently deletes an event by ID. Confirm with the user before deleting.",
		InputSchema: schemaFor[DeleteEventIn](spanEnum),
		Annotations: modifying("Delete event", true),
	}, h.deleteEvent)
}

func (h *handlers) getCurrentTime(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, CurrentTimeOut, error) {
	now := h.now().In(h.loc)
	out := CurrentTimeOut{
		Now:      now.Format(time.RFC3339),
		TimeZone: zoneName(h.loc, now),
		Today:    now.Format(time.DateOnly),
		Weekday:  now.Weekday().String(),
	}
	return result(out, "It is %s, %s (%s).", out.Weekday, out.Now, out.TimeZone), out, nil
}

func (h *handlers) listCalendars(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ListCalendarsOut, error) {
	cals, err := h.cal.ListCalendars(ctx)
	if err != nil {
		return nil, ListCalendarsOut{}, err
	}
	out := ListCalendarsOut{Calendars: make([]CalendarOut, 0, len(cals))}
	for _, c := range cals {
		out.Calendars = append(out.Calendars, CalendarOut{
			ID: c.ID, Title: c.Title, Type: c.Type, Source: c.Source, Color: c.Color, ReadOnly: c.ReadOnly,
		})
	}
	return result(out, "Found %s.", plural(len(cals), "calendar")), out, nil
}

func (h *handlers) listEvents(ctx context.Context, _ *mcp.CallToolRequest, in ListEventsIn) (*mcp.CallToolResult, ListEventsOut, error) {
	start := startOfDay(h.now(), h.loc)
	if m, err := parseOptionalMoment("start", in.Start, h.loc); err != nil {
		return nil, ListEventsOut{}, err
	} else if m != nil {
		start = m.Time
	}
	end := start.Add(defaultEventRange)
	if m, err := parseOptionalMoment("end", in.End, h.loc); err != nil {
		return nil, ListEventsOut{}, err
	} else if m != nil {
		end = m.Time
		if m.DateOnly {
			end = addDays(m.Time, 1, h.loc) // include the whole day
		}
	}
	if !end.After(start) {
		return nil, ListEventsOut{}, fmt.Errorf("%w: end must be after start", store.ErrInvalid)
	}
	if end.Sub(start) > maxEventRange {
		return nil, ListEventsOut{}, fmt.Errorf("%w: the range may span at most 366 days; split the request", store.ErrInvalid)
	}

	events, err := h.cal.ListEvents(ctx, store.EventFilter{
		Start:    start,
		End:      end,
		Calendar: strings.TrimSpace(in.Calendar),
		Search:   strings.TrimSpace(in.Search),
	})
	if err != nil {
		return nil, ListEventsOut{}, err
	}
	out := ListEventsOut{
		Start:  formatTime(start, h.loc),
		End:    formatTime(end, h.loc),
		Events: make([]EventOut, 0, len(events)),
	}
	for _, e := range events {
		out.Events = append(out.Events, h.eventOut(e))
	}
	return result(out, "Found %s between %s and %s.", plural(len(events), "event"), out.Start, out.End), out, nil
}

func (h *handlers) getEvent(ctx context.Context, _ *mcp.CallToolRequest, in IDIn) (*mcp.CallToolResult, EventOut, error) {
	id, err := requireText("id", in.ID)
	if err != nil {
		return nil, EventOut{}, err
	}
	e, err := h.cal.GetEvent(ctx, id)
	if err != nil {
		return nil, EventOut{}, err
	}
	out := h.eventOut(e)
	return result(out, "%s", h.describeEvent(e)), out, nil
}

func (h *handlers) createEvent(ctx context.Context, _ *mcp.CallToolRequest, in CreateEventIn) (*mcp.CallToolResult, EventOut, error) {
	title, err := requireText("title", in.Title)
	if err != nil {
		return nil, EventOut{}, err
	}
	start, err := parseMoment("start", in.Start, h.loc)
	if err != nil {
		return nil, EventOut{}, err
	}
	end, err := parseOptionalMoment("end", in.End, h.loc)
	if err != nil {
		return nil, EventOut{}, err
	}
	if err := checkURL(in.URL); err != nil {
		return nil, EventOut{}, err
	}
	if err := checkAlerts(in.AlertMinutesBefore); err != nil {
		return nil, EventOut{}, err
	}

	ne := store.NewEvent{
		Title:              title,
		AllDay:             in.AllDay || start.DateOnly,
		Calendar:           strings.TrimSpace(in.Calendar),
		Location:           in.Location,
		Notes:              in.Notes,
		URL:                strings.TrimSpace(in.URL),
		AlertMinutesBefore: in.AlertMinutesBefore,
	}
	if ne.AllDay {
		ne.Start, ne.End, err = h.allDayRange(start.Time, end)
	} else {
		ne.Start, ne.End, err = timedRange(start.Time, end, defaultEventDuration)
	}
	if err != nil {
		return nil, EventOut{}, err
	}

	e, err := h.cal.CreateEvent(ctx, ne)
	if err != nil {
		return nil, EventOut{}, err
	}
	out := h.eventOut(e)
	return result(out, "Created %s", h.describeEvent(e)), out, nil
}

func (h *handlers) updateEvent(ctx context.Context, _ *mcp.CallToolRequest, in UpdateEventIn) (*mcp.CallToolResult, EventOut, error) {
	id, err := requireText("id", in.ID)
	if err != nil {
		return nil, EventOut{}, err
	}
	span, err := parseSpan(in.Span)
	if err != nil {
		return nil, EventOut{}, err
	}
	u := store.EventUpdate{Location: in.Location, Notes: in.Notes}
	if in.Title != nil {
		t, err := requireText("title", *in.Title)
		if err != nil {
			return nil, EventOut{}, err
		}
		u.Title = &t
	}
	if in.Calendar != nil {
		c, err := requireText("calendar", *in.Calendar)
		if err != nil {
			return nil, EventOut{}, err
		}
		u.Calendar = &c
	}
	if in.URL != nil {
		v := strings.TrimSpace(*in.URL)
		if err := checkURL(v); err != nil {
			return nil, EventOut{}, err
		}
		u.URL = &v
	}
	if in.AlertMinutesBefore != nil {
		if err := checkAlerts(*in.AlertMinutesBefore); err != nil {
			return nil, EventOut{}, err
		}
		u.AlertMinutesBefore = in.AlertMinutesBefore
	}
	if in.Start != nil || in.End != nil || in.AllDay != nil {
		current, err := h.cal.GetEvent(ctx, id)
		if err != nil {
			return nil, EventOut{}, err
		}
		if err := h.applyTimeChange(&u, current, in); err != nil {
			return nil, EventOut{}, err
		}
	}

	e, err := h.cal.UpdateEvent(ctx, id, u, span)
	if err != nil {
		return nil, EventOut{}, err
	}
	out := h.eventOut(e)
	return result(out, "Updated %s", h.describeEvent(e)), out, nil
}

// applyTimeChange works out the new start, end and all-day flag from the
// fields the caller passed and the event's current times.
func (h *handlers) applyTimeChange(u *store.EventUpdate, cur store.Event, in UpdateEventIn) error {
	var start, end *moment
	var err error
	if in.Start != nil {
		if start, err = parseOptionalMoment("start", *in.Start, h.loc); err != nil {
			return err
		}
	}
	if in.End != nil {
		if end, err = parseOptionalMoment("end", *in.End, h.loc); err != nil {
			return err
		}
	}

	allDay := cur.AllDay
	switch {
	case in.AllDay != nil:
		allDay = *in.AllDay
	case start != nil:
		allDay = start.DateOnly
	}

	var newStart, newEnd time.Time
	if allDay {
		s := cur.Start
		if start != nil {
			s = start.Time
		}
		if end == nil && (start != nil || !cur.AllDay) {
			// Keep the number of days when moving, or make a one-day event
			// when converting a timed event.
			days := 0
			if cur.AllDay {
				days = daysBetween(cur.Start, cur.End, h.loc)
			}
			last := moment{Time: addDays(s, days, h.loc), DateOnly: true}
			end = &last
		}
		if end == nil {
			end = &moment{Time: cur.End}
		}
		newStart, newEnd, err = h.allDayRange(s, end)
	} else {
		s := cur.Start
		if start != nil {
			s = start.Time
		}
		duration := cur.End.Sub(cur.Start)
		if cur.AllDay || duration <= 0 {
			duration = defaultEventDuration
		}
		switch {
		case end != nil:
		case start != nil || cur.AllDay:
			end = &moment{Time: s.Add(duration)}
		default:
			end = &moment{Time: cur.End}
		}
		newStart, newEnd, err = timedRange(s, end, duration)
	}
	if err != nil {
		return err
	}
	u.Start, u.End, u.AllDay = &newStart, &newEnd, &allDay
	return nil
}

func (h *handlers) deleteEvent(ctx context.Context, _ *mcp.CallToolRequest, in DeleteEventIn) (*mcp.CallToolResult, DeleteOut, error) {
	id, err := requireText("id", in.ID)
	if err != nil {
		return nil, DeleteOut{}, err
	}
	span, err := parseSpan(in.Span)
	if err != nil {
		return nil, DeleteOut{}, err
	}
	if err := h.cal.DeleteEvent(ctx, id, span); err != nil {
		return nil, DeleteOut{}, err
	}
	out := DeleteOut{ID: id, Deleted: true}
	return result(out, "Deleted event %s.", id), out, nil
}

// allDayRange returns local midnight on the first day and on the last day
// (inclusive), which is how EventKit takes all-day events. A nil last day
// means a one-day event.
func (h *handlers) allDayRange(first time.Time, last *moment) (time.Time, time.Time, error) {
	start := startOfDay(first, h.loc)
	end := start
	if last != nil {
		end = startOfDay(last.Time, h.loc)
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: the last day of an all-day event must not be before its first day", store.ErrInvalid)
	}
	return start, end, nil
}

// timedRange returns start and the given end, or start plus the fallback
// duration when end is nil. The end must be after the start.
func timedRange(start time.Time, end *moment, fallback time.Duration) (time.Time, time.Time, error) {
	e := start.Add(fallback)
	if end != nil {
		e = end.Time
	}
	if !e.After(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: end must be after start", store.ErrInvalid)
	}
	return start, e, nil
}

func parseSpan(s string) (store.Span, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "this":
		return store.SpanThis, nil
	case "future":
		return store.SpanFuture, nil
	default:
		return 0, fmt.Errorf("%w: span must be \"this\" or \"future\", got %q", store.ErrInvalid, s)
	}
}

func checkURL(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme == "" {
		return fmt.Errorf("%w: url %q must be absolute, like https://example.com", store.ErrInvalid, s)
	}
	return nil
}

func checkAlerts(minutes []int) error {
	for _, m := range minutes {
		if m < 0 {
			return fmt.Errorf("%w: alert_minutes_before values must be 0 or more, got %d", store.ErrInvalid, m)
		}
	}
	return nil
}

func (h *handlers) eventOut(e store.Event) EventOut {
	alerts := e.AlertMinutesBefore
	if alerts == nil {
		alerts = []int{}
	}
	return EventOut{
		ID:                 e.ID,
		Title:              e.Title,
		Start:              formatTime(e.Start, h.loc),
		End:                formatTime(e.End, h.loc),
		AllDay:             e.AllDay,
		Calendar:           e.Calendar,
		CalendarID:         e.CalendarID,
		Location:           e.Location,
		Notes:              e.Notes,
		URL:                e.URL,
		Recurring:          e.Recurring,
		AlertMinutesBefore: alerts,
		Status:             e.Status,
	}
}

// describeEvent renders a one-line summary such as
// "\"Dentist\" on Wed 7 Oct 2026, 09:00-10:00 in Home (id evt-1).".
func (h *handlers) describeEvent(e store.Event) string {
	start, end := e.Start.In(h.loc), e.End.In(h.loc)
	var when string
	switch {
	case e.AllDay && daysBetween(start, end, h.loc) > 0:
		when = fmt.Sprintf("all day from %s to %s", start.Format("Mon 2 Jan 2006"), end.Format("Mon 2 Jan 2006"))
	case e.AllDay:
		when = "all day on " + start.Format("Mon 2 Jan 2006")
	case daysBetween(start, end, h.loc) == 0:
		when = fmt.Sprintf("on %s, %s-%s", start.Format("Mon 2 Jan 2006"), start.Format("15:04"), end.Format("15:04"))
	default:
		when = fmt.Sprintf("from %s to %s", start.Format("Mon 2 Jan 2006 15:04"), end.Format("Mon 2 Jan 2006 15:04"))
	}
	return fmt.Sprintf("%q %s in %s (id %s).", e.Title, when, e.Calendar, e.ID)
}
