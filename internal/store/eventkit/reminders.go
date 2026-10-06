package eventkit

import (
	"context"
	"fmt"
	"time"

	"github.com/BRO3886/go-eventkit/reminders"

	"github.com/AnduNacu43/iCloudMCP/internal/store"
)

// ReminderStore implements store.ReminderStore over EventKit.
type ReminderStore struct {
	lazy *lazyClient[reminders.Client]
}

var _ store.ReminderStore = (*ReminderStore)(nil)

// NewReminderStore returns a store that requests Reminders access on first
// use. A zero timeout means DefaultAccessTimeout.
func NewReminderStore(accessTimeout time.Duration) *ReminderStore {
	return &ReminderStore{lazy: newLazyClient("Reminders", accessTimeout, reminders.New)}
}

func (s *ReminderStore) ListReminderLists(ctx context.Context) ([]store.ReminderList, error) {
	c, err := s.lazy.get(ctx)
	if err != nil {
		return nil, err
	}
	return s.lists(c)
}

func (s *ReminderStore) lists(c *reminders.Client) ([]store.ReminderList, error) {
	lists, err := c.Lists()
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]store.ReminderList, 0, len(lists))
	for _, l := range lists {
		out = append(out, toList(l))
	}
	return out, nil
}

func (s *ReminderStore) ListReminders(ctx context.Context, f store.ReminderFilter) ([]store.Reminder, error) {
	c, err := s.lazy.get(ctx)
	if err != nil {
		return nil, err
	}
	var opts []reminders.ListOption
	var listID string
	if f.List != "" {
		lists, err := s.lists(c)
		if err != nil {
			return nil, err
		}
		l, err := store.ResolveList(lists, f.List)
		if err != nil {
			return nil, err
		}
		// go-eventkit filters lists by title only; the ID check below drops
		// reminders from other lists with the same title.
		opts = append(opts, reminders.WithList(l.Title))
		listID = l.ID
	}
	if f.Completed != nil {
		opts = append(opts, reminders.WithCompleted(*f.Completed))
	}
	if f.DueBefore != nil {
		opts = append(opts, reminders.WithDueBefore(*f.DueBefore))
	}
	if f.DueAfter != nil {
		opts = append(opts, reminders.WithDueAfter(*f.DueAfter))
	}
	if f.Search != "" {
		opts = append(opts, reminders.WithSearch(f.Search))
	}
	items, err := c.Reminders(opts...)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]store.Reminder, 0, len(items))
	for _, r := range items {
		if listID != "" && r.ListID != listID {
			continue
		}
		out = append(out, toReminder(r))
	}
	return out, nil
}

func (s *ReminderStore) GetReminder(ctx context.Context, id string) (store.Reminder, error) {
	c, err := s.lazy.get(ctx)
	if err != nil {
		return store.Reminder{}, err
	}
	r, err := c.Reminder(id)
	if err != nil {
		return store.Reminder{}, mapError(err)
	}
	return toReminder(*r), nil
}

func (s *ReminderStore) CreateReminder(ctx context.Context, in store.NewReminder) (store.Reminder, error) {
	c, err := s.lazy.get(ctx)
	if err != nil {
		return store.Reminder{}, err
	}
	input := reminders.CreateReminderInput{
		Title:    in.Title,
		Notes:    in.Notes,
		DueDate:  in.Due,
		Priority: fromPriority(in.Priority),
		URL:      in.URL,
		Flagged:  in.Flagged,
	}
	if in.List != "" {
		title, err := s.writableListTitle(c, in.List)
		if err != nil {
			return store.Reminder{}, err
		}
		input.ListName = title
	}
	r, err := c.CreateReminder(input)
	if err != nil {
		return store.Reminder{}, mapError(err)
	}
	return toReminder(*r), nil
}

func (s *ReminderStore) UpdateReminder(ctx context.Context, id string, u store.ReminderUpdate) (store.Reminder, error) {
	c, err := s.lazy.get(ctx)
	if err != nil {
		return store.Reminder{}, err
	}
	if err := s.checkWritableReminder(c, id); err != nil {
		return store.Reminder{}, err
	}
	input := reminders.UpdateReminderInput{
		Title:        u.Title,
		Notes:        u.Notes,
		DueDate:      u.Due,
		ClearDueDate: u.ClearDue,
		Flagged:      u.Flagged,
		URL:          u.URL,
	}
	if u.ClearDue {
		input.DueDate = nil
	}
	if u.Priority != nil {
		p := fromPriority(*u.Priority)
		input.Priority = &p
	}
	if u.List != nil {
		title, err := s.writableListTitle(c, *u.List)
		if err != nil {
			return store.Reminder{}, err
		}
		input.ListName = &title
	}
	r, err := c.UpdateReminder(id, input)
	if err != nil {
		return store.Reminder{}, mapError(err)
	}
	return toReminder(*r), nil
}

func (s *ReminderStore) SetCompleted(ctx context.Context, id string, completed bool) (store.Reminder, error) {
	c, err := s.lazy.get(ctx)
	if err != nil {
		return store.Reminder{}, err
	}
	if err := s.checkWritableReminder(c, id); err != nil {
		return store.Reminder{}, err
	}
	var r *reminders.Reminder
	if completed {
		r, err = c.CompleteReminder(id)
	} else {
		r, err = c.UncompleteReminder(id)
	}
	if err != nil {
		return store.Reminder{}, mapError(err)
	}
	return toReminder(*r), nil
}

func (s *ReminderStore) DeleteReminder(ctx context.Context, id string) error {
	c, err := s.lazy.get(ctx)
	if err != nil {
		return err
	}
	if err := s.checkWritableReminder(c, id); err != nil {
		return err
	}
	return mapError(c.DeleteReminder(id))
}

// checkWritableReminder fails early, with a clear message, when the
// reminder does not exist or sits in a read-only list.
func (s *ReminderStore) checkWritableReminder(c *reminders.Client, id string) error {
	r, err := c.Reminder(id)
	if err != nil {
		return mapError(err)
	}
	lists, err := s.lists(c)
	if err != nil {
		return err
	}
	for _, l := range lists {
		if l.ID == r.ListID && l.ReadOnly {
			return fmt.Errorf("%w: reminder list %q cannot be modified", store.ErrReadOnly, l.Title)
		}
	}
	return nil
}

// writableListTitle resolves ref to a list that can receive reminders and
// returns its title, which is how go-eventkit addresses lists on writes.
func (s *ReminderStore) writableListTitle(c *reminders.Client, ref string) (string, error) {
	lists, err := s.lists(c)
	if err != nil {
		return "", err
	}
	l, err := store.ResolveList(lists, ref)
	if err != nil {
		return "", err
	}
	if l.ReadOnly {
		return "", fmt.Errorf("%w: reminder list %q cannot be modified", store.ErrReadOnly, l.Title)
	}
	if !store.TitleIsUnique(lists, l, func(l store.ReminderList) string { return l.Title }) {
		return "", fmt.Errorf("%w: reminder list %q shares its name with another list, so it cannot be targeted reliably. Rename one of them in Reminders.app", store.ErrInvalid, l.Title)
	}
	return l.Title, nil
}

func toList(l reminders.List) store.ReminderList {
	return store.ReminderList{
		ID:       l.ID,
		Title:    l.Title,
		Color:    l.Color,
		Source:   l.Source,
		Count:    l.Count,
		ReadOnly: l.ReadOnly,
	}
}

func toReminder(r reminders.Reminder) store.Reminder {
	return store.Reminder{
		ID:             r.ID,
		Title:          r.Title,
		Notes:          r.Notes,
		List:           r.List,
		ListID:         r.ListID,
		Due:            r.DueDate,
		Completed:      r.Completed,
		CompletionDate: r.CompletionDate,
		Priority:       toPriority(r.Priority),
		Flagged:        r.Flagged,
		URL:            r.URL,
		Recurring:      r.Recurring,
	}
}

// toPriority maps EventKit's 0-9 scale: 0 none, 1-4 high, 5 medium, 6-9 low.
func toPriority(p reminders.Priority) store.Priority {
	switch {
	case p >= 1 && p <= 4:
		return store.PriorityHigh
	case p == 5:
		return store.PriorityMedium
	case p >= 6 && p <= 9:
		return store.PriorityLow
	default:
		return store.PriorityNone
	}
}

func fromPriority(p store.Priority) reminders.Priority {
	switch p {
	case store.PriorityHigh:
		return reminders.PriorityHigh
	case store.PriorityMedium:
		return reminders.PriorityMedium
	case store.PriorityLow:
		return reminders.PriorityLow
	default:
		return reminders.PriorityNone
	}
}
