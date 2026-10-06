package eventkit

import (
	"context"
	"fmt"
	"time"

	"github.com/BRO3886/go-eventkit/calendar"

	"github.com/AnduNacu43/iCloudMCP/internal/store"
)

// CalendarStore implements store.CalendarStore over EventKit.
type CalendarStore struct {
	lazy *lazyClient[calendar.Client]
}

var _ store.CalendarStore = (*CalendarStore)(nil)

// NewCalendarStore returns a store that requests Calendars access on first
// use. A zero timeout means DefaultAccessTimeout.
func NewCalendarStore(accessTimeout time.Duration) *CalendarStore {
	return &CalendarStore{lazy: newLazyClient("Calendars", accessTimeout, calendar.New)}
}

func (s *CalendarStore) ListCalendars(ctx context.Context) ([]store.Calendar, error) {
	c, err := s.lazy.get(ctx)
	if err != nil {
		return nil, err
	}
	return s.calendars(c)
}

func (s *CalendarStore) calendars(c *calendar.Client) ([]store.Calendar, error) {
	cals, err := c.Calendars()
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]store.Calendar, 0, len(cals))
	for _, cal := range cals {
		out = append(out, toCalendar(cal))
	}
	return out, nil
}

func (s *CalendarStore) ListEvents(ctx context.Context, f store.EventFilter) ([]store.Event, error) {
	c, err := s.lazy.get(ctx)
	if err != nil {
		return nil, err
	}
	var opts []calendar.ListOption
	if f.Calendar != "" {
		cals, err := s.calendars(c)
		if err != nil {
			return nil, err
		}
		cal, err := store.ResolveCalendar(cals, f.Calendar)
		if err != nil {
			return nil, err
		}
		opts = append(opts, calendar.WithCalendarID(cal.ID))
	}
	if f.Search != "" {
		opts = append(opts, calendar.WithSearch(f.Search))
	}
	events, err := c.Events(f.Start, f.End, opts...)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]store.Event, 0, len(events))
	for _, e := range events {
		out = append(out, toEvent(e))
	}
	return out, nil
}

func (s *CalendarStore) GetEvent(ctx context.Context, id string) (store.Event, error) {
	c, err := s.lazy.get(ctx)
	if err != nil {
		return store.Event{}, err
	}
	e, err := c.Event(id)
	if err != nil {
		return store.Event{}, mapError(err)
	}
	return toEvent(*e), nil
}

func (s *CalendarStore) CreateEvent(ctx context.Context, in store.NewEvent) (store.Event, error) {
	c, err := s.lazy.get(ctx)
	if err != nil {
		return store.Event{}, err
	}
	input := calendar.CreateEventInput{
		Title:     in.Title,
		StartDate: in.Start,
		EndDate:   in.End,
		AllDay:    in.AllDay,
		Location:  in.Location,
		Notes:     in.Notes,
		URL:       in.URL,
	}
	if in.Calendar != "" {
		title, err := s.writableCalendarTitle(c, in.Calendar)
		if err != nil {
			return store.Event{}, err
		}
		input.Calendar = title
	}
	if in.AlertMinutesBefore != nil {
		// Explicit alerts replace the calendar's default alerts.
		input.Alerts = toAlerts(in.AlertMinutesBefore)
		input.SuppressDefaultAlarms = true
	}
	e, err := c.CreateEvent(input)
	if err != nil {
		return store.Event{}, mapError(err)
	}
	return toEvent(*e), nil
}

func (s *CalendarStore) UpdateEvent(ctx context.Context, id string, u store.EventUpdate, span store.Span) (store.Event, error) {
	c, err := s.lazy.get(ctx)
	if err != nil {
		return store.Event{}, err
	}
	if err := s.checkWritableEvent(c, id, span); err != nil {
		return store.Event{}, err
	}
	input := calendar.UpdateEventInput{
		Title:     u.Title,
		StartDate: u.Start,
		EndDate:   u.End,
		AllDay:    u.AllDay,
		Location:  u.Location,
		Notes:     u.Notes,
		URL:       u.URL,
	}
	if u.Calendar != nil {
		title, err := s.writableCalendarTitle(c, *u.Calendar)
		if err != nil {
			return store.Event{}, err
		}
		input.Calendar = &title
	}
	if u.AlertMinutesBefore != nil {
		alerts := toAlerts(*u.AlertMinutesBefore)
		input.Alerts = &alerts
	}
	e, err := c.UpdateEvent(id, input, toSpan(span))
	if err != nil {
		return store.Event{}, mapError(err)
	}
	return toEvent(*e), nil
}

func (s *CalendarStore) DeleteEvent(ctx context.Context, id string, span store.Span) error {
	c, err := s.lazy.get(ctx)
	if err != nil {
		return err
	}
	if err := s.checkWritableEvent(c, id, span); err != nil {
		return err
	}
	return mapError(c.DeleteEvent(id, toSpan(span)))
}

// checkWritableEvent fails early, with a clear message, for events that a
// write would mishandle: those in read-only calendars, and single
// occurrences of repeating events.
func (s *CalendarStore) checkWritableEvent(c *calendar.Client, id string, span store.Span) error {
	e, err := c.Event(id)
	if err != nil {
		return mapError(err)
	}
	// go-eventkit addresses an event by its eventIdentifier, which every
	// occurrence of a repeating event shares, and resolves it to the first
	// occurrence. A "this occurrence only" change would land on the wrong
	// date, so only whole-series changes are allowed.
	if e.Recurring && span == store.SpanThis {
		return fmt.Errorf("%w: %q repeats, and changing a single occurrence is not supported yet. Use span \"future\" to change the whole series, or edit that occurrence in Calendar.app", store.ErrInvalid, e.Title)
	}
	cals, err := s.calendars(c)
	if err != nil {
		return err
	}
	for _, cal := range cals {
		if cal.ID == e.CalendarID && cal.ReadOnly {
			return fmt.Errorf("%w: calendar %q cannot be modified", store.ErrReadOnly, cal.Title)
		}
	}
	return nil
}

// writableCalendarTitle resolves ref to a calendar that can receive events
// and returns its title, which is how go-eventkit addresses calendars on
// writes.
func (s *CalendarStore) writableCalendarTitle(c *calendar.Client, ref string) (string, error) {
	cals, err := s.calendars(c)
	if err != nil {
		return "", err
	}
	cal, err := store.ResolveCalendar(cals, ref)
	if err != nil {
		return "", err
	}
	if cal.ReadOnly {
		return "", fmt.Errorf("%w: calendar %q cannot be modified", store.ErrReadOnly, cal.Title)
	}
	if !store.TitleIsUnique(cals, cal, func(c store.Calendar) string { return c.Title }) {
		return "", fmt.Errorf("%w: calendar %q shares its name with another calendar, so it cannot be targeted reliably. Rename one of them in Calendar.app", store.ErrInvalid, cal.Title)
	}
	return cal.Title, nil
}

func toCalendar(c calendar.Calendar) store.Calendar {
	return store.Calendar{
		ID:       c.ID,
		Title:    c.Title,
		Type:     c.Type.String(),
		Source:   c.Source,
		Color:    c.Color,
		ReadOnly: c.ReadOnly,
	}
}

func toEvent(e calendar.Event) store.Event {
	alerts := make([]int, 0, len(e.Alerts))
	for _, a := range e.Alerts {
		alerts = append(alerts, int((-a.RelativeOffset).Minutes()))
	}
	return store.Event{
		ID:                 e.ID,
		Title:              e.Title,
		Start:              e.StartDate,
		End:                e.EndDate,
		AllDay:             e.AllDay,
		Calendar:           e.Calendar,
		CalendarID:         e.CalendarID,
		Location:           e.Location,
		Notes:              e.Notes,
		URL:                e.URL,
		Recurring:          e.Recurring,
		AlertMinutesBefore: alerts,
		Status:             e.Status.String(),
	}
}

// toAlerts converts minutes-before-start into EventKit relative offsets,
// which are negative for alerts before the event.
func toAlerts(minutes []int) []calendar.Alert {
	alerts := make([]calendar.Alert, 0, len(minutes))
	for _, m := range minutes {
		alerts = append(alerts, calendar.Alert{RelativeOffset: -time.Duration(m) * time.Minute})
	}
	return alerts
}

func toSpan(s store.Span) calendar.Span {
	if s == store.SpanFuture {
		return calendar.SpanFutureEvents
	}
	return calendar.SpanThisEvent
}
