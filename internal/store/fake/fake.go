// Package fake provides in-memory implementations of the store interfaces
// for tests. They follow the same rules as the EventKit adapter: calendars
// and lists are resolved by ID or title, read-only containers reject writes,
// and a single occurrence of a repeating event cannot be changed.
package fake

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/AnduNacu43/iCloudMCP/internal/store"
)

// CalendarStore is an in-memory store.CalendarStore.
type CalendarStore struct {
	mu        sync.Mutex
	calendars []store.Calendar
	events    []store.Event
	nextID    int

	// Err, when set, is returned by every method. Use it to simulate a
	// denied permission.
	Err error
	// LastSpan records the span passed to the latest update or delete.
	LastSpan store.Span
}

var _ store.CalendarStore = (*CalendarStore)(nil)

// NewCalendarStore returns a store holding the given calendars. The first
// writable calendar is the default for new events.
func NewCalendarStore(calendars ...store.Calendar) *CalendarStore {
	return &CalendarStore{calendars: slices.Clone(calendars)}
}

// AddEvent seeds an event, filling in its ID and calendar title, and returns
// the stored copy.
func (s *CalendarStore) AddEvent(e store.Event) store.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.ID == "" {
		e.ID = s.newID("evt")
	}
	for _, c := range s.calendars {
		if c.ID == e.CalendarID {
			e.Calendar = c.Title
		}
	}
	s.events = append(s.events, cloneEvent(e))
	return cloneEvent(e)
}

func (s *CalendarStore) newID(prefix string) string {
	s.nextID++
	return fmt.Sprintf("%s-%d", prefix, s.nextID)
}

func (s *CalendarStore) ListCalendars(ctx context.Context) ([]store.Calendar, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return nil, s.Err
	}
	return slices.Clone(s.calendars), nil
}

func (s *CalendarStore) ListEvents(ctx context.Context, f store.EventFilter) ([]store.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return nil, s.Err
	}
	calID := ""
	if f.Calendar != "" {
		c, err := store.ResolveCalendar(s.calendars, f.Calendar)
		if err != nil {
			return nil, err
		}
		calID = c.ID
	}
	out := []store.Event{}
	for _, e := range s.events {
		if !e.Start.Before(f.End) || !e.End.After(f.Start) {
			continue
		}
		if calID != "" && e.CalendarID != calID {
			continue
		}
		if f.Search != "" && !containsFold(f.Search, e.Title, e.Location, e.Notes) {
			continue
		}
		out = append(out, cloneEvent(e))
	}
	slices.SortStableFunc(out, func(a, b store.Event) int { return a.Start.Compare(b.Start) })
	return out, nil
}

func (s *CalendarStore) GetEvent(ctx context.Context, id string) (store.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return store.Event{}, s.Err
	}
	i, err := s.find(id)
	if err != nil {
		return store.Event{}, err
	}
	return cloneEvent(s.events[i]), nil
}

func (s *CalendarStore) CreateEvent(ctx context.Context, in store.NewEvent) (store.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return store.Event{}, s.Err
	}
	cal, err := s.writableCalendar(in.Calendar)
	if err != nil {
		return store.Event{}, err
	}
	if in.End.Before(in.Start) {
		return store.Event{}, fmt.Errorf("%w: end is before start", store.ErrInvalid)
	}
	alerts := []int{}
	if in.AlertMinutesBefore != nil {
		alerts = slices.Clone(in.AlertMinutesBefore)
	}
	e := store.Event{
		ID:                 s.newID("evt"),
		Title:              in.Title,
		Start:              in.Start,
		End:                in.End,
		AllDay:             in.AllDay,
		Calendar:           cal.Title,
		CalendarID:         cal.ID,
		Location:           in.Location,
		Notes:              in.Notes,
		URL:                in.URL,
		AlertMinutesBefore: alerts,
		Status:             "none",
	}
	s.events = append(s.events, e)
	return cloneEvent(e), nil
}

func (s *CalendarStore) UpdateEvent(ctx context.Context, id string, u store.EventUpdate, span store.Span) (store.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return store.Event{}, s.Err
	}
	s.LastSpan = span
	i, err := s.checkWritable(id, span)
	if err != nil {
		return store.Event{}, err
	}
	e := s.events[i]
	if u.Calendar != nil {
		cal, err := s.writableCalendar(*u.Calendar)
		if err != nil {
			return store.Event{}, err
		}
		e.Calendar, e.CalendarID = cal.Title, cal.ID
	}
	setIf(&e.Title, u.Title)
	setIf(&e.Start, u.Start)
	setIf(&e.End, u.End)
	setIf(&e.AllDay, u.AllDay)
	setIf(&e.Location, u.Location)
	setIf(&e.Notes, u.Notes)
	setIf(&e.URL, u.URL)
	if u.AlertMinutesBefore != nil {
		e.AlertMinutesBefore = slices.Clone(*u.AlertMinutesBefore)
	}
	if e.End.Before(e.Start) {
		return store.Event{}, fmt.Errorf("%w: end is before start", store.ErrInvalid)
	}
	s.events[i] = e
	return cloneEvent(e), nil
}

func (s *CalendarStore) DeleteEvent(ctx context.Context, id string, span store.Span) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return s.Err
	}
	s.LastSpan = span
	i, err := s.checkWritable(id, span)
	if err != nil {
		return err
	}
	s.events = slices.Delete(s.events, i, i+1)
	return nil
}

func (s *CalendarStore) find(id string) (int, error) {
	i := slices.IndexFunc(s.events, func(e store.Event) bool { return e.ID == id })
	if i < 0 {
		return 0, fmt.Errorf("%w: event %q", store.ErrNotFound, id)
	}
	return i, nil
}

func (s *CalendarStore) checkWritable(id string, span store.Span) (int, error) {
	i, err := s.find(id)
	if err != nil {
		return 0, err
	}
	e := s.events[i]
	if e.Recurring && span == store.SpanThis {
		return 0, fmt.Errorf("%w: %q repeats, and changing a single occurrence is not supported yet", store.ErrInvalid, e.Title)
	}
	for _, c := range s.calendars {
		if c.ID == e.CalendarID && c.ReadOnly {
			return 0, fmt.Errorf("%w: calendar %q cannot be modified", store.ErrReadOnly, c.Title)
		}
	}
	return i, nil
}

func (s *CalendarStore) writableCalendar(ref string) (store.Calendar, error) {
	if ref == "" {
		for _, c := range s.calendars {
			if !c.ReadOnly {
				return c, nil
			}
		}
		return store.Calendar{}, fmt.Errorf("%w: no writable calendar", store.ErrNotFound)
	}
	c, err := store.ResolveCalendar(s.calendars, ref)
	if err != nil {
		return store.Calendar{}, err
	}
	if c.ReadOnly {
		return store.Calendar{}, fmt.Errorf("%w: calendar %q cannot be modified", store.ErrReadOnly, c.Title)
	}
	return c, nil
}

func cloneEvent(e store.Event) store.Event {
	e.AlertMinutesBefore = slices.Clone(e.AlertMinutesBefore)
	if e.AlertMinutesBefore == nil {
		e.AlertMinutesBefore = []int{}
	}
	return e
}

// ReminderStore is an in-memory store.ReminderStore.
type ReminderStore struct {
	mu        sync.Mutex
	lists     []store.ReminderList
	reminders []store.Reminder
	nextID    int

	// Err, when set, is returned by every method.
	Err error
	// Now returns the completion time; it defaults to time.Now.
	Now func() time.Time
}

var _ store.ReminderStore = (*ReminderStore)(nil)

// NewReminderStore returns a store holding the given lists. The first
// writable list is the default for new reminders.
func NewReminderStore(lists ...store.ReminderList) *ReminderStore {
	return &ReminderStore{lists: slices.Clone(lists), Now: time.Now}
}

// AddReminder seeds a reminder, filling in its ID and list title, and
// returns the stored copy.
func (s *ReminderStore) AddReminder(r store.Reminder) store.Reminder {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.ID == "" {
		s.nextID++
		r.ID = fmt.Sprintf("rem-%d", s.nextID)
	}
	if r.Priority == "" {
		r.Priority = store.PriorityNone
	}
	for _, l := range s.lists {
		if l.ID == r.ListID {
			r.List = l.Title
		}
	}
	s.reminders = append(s.reminders, cloneReminder(r))
	return cloneReminder(r)
}

func (s *ReminderStore) ListReminderLists(ctx context.Context) ([]store.ReminderList, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return nil, s.Err
	}
	out := slices.Clone(s.lists)
	for i := range out {
		out[i].Count = 0
		for _, r := range s.reminders {
			if r.ListID == out[i].ID && !r.Completed {
				out[i].Count++
			}
		}
	}
	return out, nil
}

func (s *ReminderStore) ListReminders(ctx context.Context, f store.ReminderFilter) ([]store.Reminder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return nil, s.Err
	}
	listID := ""
	if f.List != "" {
		l, err := store.ResolveList(s.lists, f.List)
		if err != nil {
			return nil, err
		}
		listID = l.ID
	}
	out := []store.Reminder{}
	for _, r := range s.reminders {
		switch {
		case listID != "" && r.ListID != listID,
			f.Completed != nil && r.Completed != *f.Completed,
			f.DueBefore != nil && (r.Due == nil || !r.Due.Before(*f.DueBefore)),
			f.DueAfter != nil && (r.Due == nil || !r.Due.After(*f.DueAfter)),
			f.Search != "" && !containsFold(f.Search, r.Title, r.Notes):
			continue
		}
		out = append(out, cloneReminder(r))
	}
	return out, nil
}

func (s *ReminderStore) GetReminder(ctx context.Context, id string) (store.Reminder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return store.Reminder{}, s.Err
	}
	i, err := s.find(id)
	if err != nil {
		return store.Reminder{}, err
	}
	return cloneReminder(s.reminders[i]), nil
}

func (s *ReminderStore) CreateReminder(ctx context.Context, in store.NewReminder) (store.Reminder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return store.Reminder{}, s.Err
	}
	l, err := s.writableList(in.List)
	if err != nil {
		return store.Reminder{}, err
	}
	p := in.Priority
	if p == "" {
		p = store.PriorityNone
	}
	s.nextID++
	r := store.Reminder{
		ID:       fmt.Sprintf("rem-%d", s.nextID),
		Title:    in.Title,
		Notes:    in.Notes,
		List:     l.Title,
		ListID:   l.ID,
		Due:      clonePtr(in.Due),
		Priority: p,
		Flagged:  in.Flagged,
		URL:      in.URL,
	}
	s.reminders = append(s.reminders, r)
	return cloneReminder(r), nil
}

func (s *ReminderStore) UpdateReminder(ctx context.Context, id string, u store.ReminderUpdate) (store.Reminder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return store.Reminder{}, s.Err
	}
	i, err := s.checkWritable(id)
	if err != nil {
		return store.Reminder{}, err
	}
	r := s.reminders[i]
	if u.List != nil {
		l, err := s.writableList(*u.List)
		if err != nil {
			return store.Reminder{}, err
		}
		r.List, r.ListID = l.Title, l.ID
	}
	setIf(&r.Title, u.Title)
	setIf(&r.Notes, u.Notes)
	setIf(&r.Priority, u.Priority)
	setIf(&r.Flagged, u.Flagged)
	setIf(&r.URL, u.URL)
	if u.Due != nil {
		d := *u.Due
		r.Due = &d
	}
	if u.ClearDue {
		r.Due = nil
	}
	s.reminders[i] = r
	return cloneReminder(r), nil
}

func (s *ReminderStore) SetCompleted(ctx context.Context, id string, completed bool) (store.Reminder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return store.Reminder{}, s.Err
	}
	i, err := s.checkWritable(id)
	if err != nil {
		return store.Reminder{}, err
	}
	r := &s.reminders[i]
	r.Completed = completed
	r.CompletionDate = nil
	if completed {
		now := s.Now()
		r.CompletionDate = &now
	}
	return cloneReminder(*r), nil
}

func (s *ReminderStore) DeleteReminder(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return s.Err
	}
	i, err := s.checkWritable(id)
	if err != nil {
		return err
	}
	s.reminders = slices.Delete(s.reminders, i, i+1)
	return nil
}

func (s *ReminderStore) find(id string) (int, error) {
	i := slices.IndexFunc(s.reminders, func(r store.Reminder) bool { return r.ID == id })
	if i < 0 {
		return 0, fmt.Errorf("%w: reminder %q", store.ErrNotFound, id)
	}
	return i, nil
}

func (s *ReminderStore) checkWritable(id string) (int, error) {
	i, err := s.find(id)
	if err != nil {
		return 0, err
	}
	for _, l := range s.lists {
		if l.ID == s.reminders[i].ListID && l.ReadOnly {
			return 0, fmt.Errorf("%w: reminder list %q cannot be modified", store.ErrReadOnly, l.Title)
		}
	}
	return i, nil
}

func (s *ReminderStore) writableList(ref string) (store.ReminderList, error) {
	if ref == "" {
		for _, l := range s.lists {
			if !l.ReadOnly {
				return l, nil
			}
		}
		return store.ReminderList{}, fmt.Errorf("%w: no writable reminder list", store.ErrNotFound)
	}
	l, err := store.ResolveList(s.lists, ref)
	if err != nil {
		return store.ReminderList{}, err
	}
	if l.ReadOnly {
		return store.ReminderList{}, fmt.Errorf("%w: reminder list %q cannot be modified", store.ErrReadOnly, l.Title)
	}
	return l, nil
}

func setIf[T any](dst *T, src *T) {
	if src != nil {
		*dst = *src
	}
}

func containsFold(query string, fields ...string) bool {
	q := strings.ToLower(query)
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), q) {
			return true
		}
	}
	return false
}

// cloneReminder copies the date pointers so callers cannot mutate stored
// reminders.
func cloneReminder(r store.Reminder) store.Reminder {
	r.Due = clonePtr(r.Due)
	r.CompletionDate = clonePtr(r.CompletionDate)
	return r
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
