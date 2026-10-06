package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/AnduNacu43/iCloudMCP/internal/store"
)

// ReminderListOut describes one reminder list.
type ReminderListOut struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Color    string `json:"color"`
	Source   string `json:"source" jsonschema:"account the list belongs to"`
	Count    int    `json:"count" jsonschema:"number of reminders in the list"`
	ReadOnly bool   `json:"read_only" jsonschema:"true when reminders cannot be added or changed"`
}

// ListReminderListsOut is the output of list_reminder_lists.
type ListReminderListsOut struct {
	Lists []ReminderListOut `json:"lists"`
}

// ReminderOut describes one reminder.
type ReminderOut struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Notes          string `json:"notes,omitempty"`
	List           string `json:"list"`
	ListID         string `json:"list_id"`
	Due            string `json:"due,omitempty" jsonschema:"RFC 3339; absent when there is no due date"`
	Completed      bool   `json:"completed"`
	CompletionDate string `json:"completion_date,omitempty" jsonschema:"RFC 3339"`
	Priority       string `json:"priority" jsonschema:"none, low, medium or high"`
	Flagged        bool   `json:"flagged"`
	URL            string `json:"url,omitempty"`
	Recurring      bool   `json:"recurring"`
}

// ListRemindersIn is the input of list_reminders.
type ListRemindersIn struct {
	List      string `json:"list,omitempty" jsonschema:"list ID or title; omit for all lists"`
	Completed bool   `json:"completed,omitempty" jsonschema:"true lists completed reminders instead of open ones; defaults to false"`
	DueBefore string `json:"due_before,omitempty" jsonschema:"only reminders due before this time; a bare date includes that whole day"`
	DueAfter  string `json:"due_after,omitempty" jsonschema:"only reminders due after this time; a bare date includes that whole day"`
	Search    string `json:"search,omitempty" jsonschema:"case-insensitive text matched against title and notes"`
}

// ListRemindersOut is the output of list_reminders.
type ListRemindersOut struct {
	Reminders []ReminderOut `json:"reminders"`
}

// CreateReminderIn is the input of create_reminder.
type CreateReminderIn struct {
	Title    string `json:"title"`
	List     string `json:"list,omitempty" jsonschema:"list ID or title; defaults to the default reminders list"`
	Notes    string `json:"notes,omitempty"`
	Due      string `json:"due,omitempty" jsonschema:"due time; a bare date is due at 09:00 local time that day"`
	Priority string `json:"priority,omitempty" jsonschema:"none (default), low, medium or high"`
	Flagged  bool   `json:"flagged,omitempty"`
	URL      string `json:"url,omitempty"`
}

// UpdateReminderIn is the input of update_reminder.
type UpdateReminderIn struct {
	ID           string  `json:"id"`
	Title        *string `json:"title,omitempty"`
	List         *string `json:"list,omitempty" jsonschema:"move the reminder to this list ID or title"`
	Notes        *string `json:"notes,omitempty" jsonschema:"empty string clears it"`
	Due          *string `json:"due,omitempty" jsonschema:"new due time; a bare date is due at 09:00 local time that day"`
	ClearDueDate bool    `json:"clear_due_date,omitempty" jsonschema:"remove the due date"`
	Priority     *string `json:"priority,omitempty" jsonschema:"none, low, medium or high"`
	Flagged      *bool   `json:"flagged,omitempty"`
	URL          *string `json:"url,omitempty" jsonschema:"empty string clears it"`
}

// CompleteReminderIn is the input of complete_reminder.
type CompleteReminderIn struct {
	ID        string `json:"id"`
	Completed *bool  `json:"completed,omitempty" jsonschema:"false marks the reminder as not done; defaults to true"`
}

var priorityValues = []string{"none", "low", "medium", "high"}

func (h *handlers) registerReminderTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_reminder_lists",
		Description: "Lists every reminder list on this Mac (iCloud and other accounts) with its ID, reminder count and whether it is read-only.",
		Annotations: readOnly("List reminder lists"),
	}, h.listReminderLists)

	mcp.AddTool(s, &mcp.Tool{
		Name: "list_reminders",
		Description: "Lists reminders, open ones by default. Filter by list, due date range or text. " +
			"Use it to find a reminder's ID before updating, completing or deleting it.",
		Annotations: readOnly("List reminders"),
	}, h.listReminders)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_reminder",
		Description: "Returns one reminder by ID.",
		Annotations: readOnly("Get reminder"),
	}, h.getReminder)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_reminder",
		Description: "Creates a reminder. Returns the created reminder with its ID.",
		InputSchema: schemaFor[CreateReminderIn](map[string][]string{"priority": priorityValues}),
		Annotations: additive("Create reminder"),
	}, h.createReminder)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_reminder",
		Description: "Changes fields of an existing reminder; omitted fields stay as they are. Use complete_reminder to mark it done.",
		InputSchema: schemaFor[UpdateReminderIn](map[string][]string{"priority": priorityValues}),
		Annotations: modifying("Update reminder", true),
	}, h.updateReminder)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "complete_reminder",
		Description: "Marks a reminder as done, or as not done with completed=false.",
		Annotations: modifying("Complete reminder", true),
	}, h.completeReminder)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_reminder",
		Description: "Permanently deletes a reminder by ID. Confirm with the user before deleting; to tick a reminder off, use complete_reminder instead.",
		Annotations: modifying("Delete reminder", true),
	}, h.deleteReminder)
}

func (h *handlers) listReminderLists(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ListReminderListsOut, error) {
	lists, err := h.rem.ListReminderLists(ctx)
	if err != nil {
		return nil, ListReminderListsOut{}, err
	}
	out := ListReminderListsOut{Lists: make([]ReminderListOut, 0, len(lists))}
	for _, l := range lists {
		out.Lists = append(out.Lists, ReminderListOut{
			ID: l.ID, Title: l.Title, Color: l.Color, Source: l.Source, Count: l.Count, ReadOnly: l.ReadOnly,
		})
	}
	return result(out, "Found %s.", plural(len(lists), "reminder list")), out, nil
}

func (h *handlers) listReminders(ctx context.Context, _ *mcp.CallToolRequest, in ListRemindersIn) (*mcp.CallToolResult, ListRemindersOut, error) {
	completed := in.Completed
	f := store.ReminderFilter{
		List:      strings.TrimSpace(in.List),
		Completed: &completed,
		Search:    strings.TrimSpace(in.Search),
	}
	if m, err := parseOptionalMoment("due_before", in.DueBefore, h.loc); err != nil {
		return nil, ListRemindersOut{}, err
	} else if m != nil {
		t := m.Time
		if m.DateOnly {
			t = addDays(m.Time, 1, h.loc) // include the whole day
		}
		f.DueBefore = &t
	}
	if m, err := parseOptionalMoment("due_after", in.DueAfter, h.loc); err != nil {
		return nil, ListRemindersOut{}, err
	} else if m != nil {
		t := m.Time
		if m.DateOnly {
			t = t.Add(-1) // include reminders due at midnight that day
		}
		f.DueAfter = &t
	}
	if f.DueBefore != nil && f.DueAfter != nil && !f.DueBefore.After(*f.DueAfter) {
		return nil, ListRemindersOut{}, fmt.Errorf("%w: due_before must be later than due_after", store.ErrInvalid)
	}

	items, err := h.rem.ListReminders(ctx, f)
	if err != nil {
		return nil, ListRemindersOut{}, err
	}
	out := ListRemindersOut{Reminders: make([]ReminderOut, 0, len(items))}
	for _, r := range items {
		out.Reminders = append(out.Reminders, h.reminderOut(r))
	}
	state := "open"
	if completed {
		state = "completed"
	}
	return result(out, "Found %d %s %s.", len(items), state, pluralNoun(len(items), "reminder")), out, nil
}

func (h *handlers) getReminder(ctx context.Context, _ *mcp.CallToolRequest, in IDIn) (*mcp.CallToolResult, ReminderOut, error) {
	id, err := requireText("id", in.ID)
	if err != nil {
		return nil, ReminderOut{}, err
	}
	r, err := h.rem.GetReminder(ctx, id)
	if err != nil {
		return nil, ReminderOut{}, err
	}
	out := h.reminderOut(r)
	return result(out, "%s", h.describeReminder(r)), out, nil
}

func (h *handlers) createReminder(ctx context.Context, _ *mcp.CallToolRequest, in CreateReminderIn) (*mcp.CallToolResult, ReminderOut, error) {
	title, err := requireText("title", in.Title)
	if err != nil {
		return nil, ReminderOut{}, err
	}
	priority, err := store.ParsePriority(in.Priority)
	if err != nil {
		return nil, ReminderOut{}, err
	}
	if err := checkURL(in.URL); err != nil {
		return nil, ReminderOut{}, err
	}
	nr := store.NewReminder{
		Title:    title,
		List:     strings.TrimSpace(in.List),
		Notes:    in.Notes,
		Priority: priority,
		Flagged:  in.Flagged,
		URL:      strings.TrimSpace(in.URL),
	}
	if m, err := parseOptionalMoment("due", in.Due, h.loc); err != nil {
		return nil, ReminderOut{}, err
	} else if m != nil {
		due := dueTime(*m, h.loc)
		nr.Due = &due
	}

	r, err := h.rem.CreateReminder(ctx, nr)
	if err != nil {
		return nil, ReminderOut{}, err
	}
	out := h.reminderOut(r)
	return result(out, "Created %s", h.describeReminder(r)), out, nil
}

func (h *handlers) updateReminder(ctx context.Context, _ *mcp.CallToolRequest, in UpdateReminderIn) (*mcp.CallToolResult, ReminderOut, error) {
	id, err := requireText("id", in.ID)
	if err != nil {
		return nil, ReminderOut{}, err
	}
	u := store.ReminderUpdate{Notes: in.Notes, Flagged: in.Flagged, ClearDue: in.ClearDueDate}
	if in.Title != nil {
		t, err := requireText("title", *in.Title)
		if err != nil {
			return nil, ReminderOut{}, err
		}
		u.Title = &t
	}
	if in.List != nil {
		l, err := requireText("list", *in.List)
		if err != nil {
			return nil, ReminderOut{}, err
		}
		u.List = &l
	}
	if in.Due != nil {
		if in.ClearDueDate {
			return nil, ReminderOut{}, fmt.Errorf("%w: pass either due or clear_due_date, not both", store.ErrInvalid)
		}
		m, err := parseMoment("due", *in.Due, h.loc)
		if err != nil {
			return nil, ReminderOut{}, err
		}
		due := dueTime(m, h.loc)
		u.Due = &due
	}
	if in.Priority != nil {
		p, err := store.ParsePriority(*in.Priority)
		if err != nil {
			return nil, ReminderOut{}, err
		}
		u.Priority = &p
	}
	if in.URL != nil {
		v := strings.TrimSpace(*in.URL)
		if err := checkURL(v); err != nil {
			return nil, ReminderOut{}, err
		}
		u.URL = &v
	}

	r, err := h.rem.UpdateReminder(ctx, id, u)
	if err != nil {
		return nil, ReminderOut{}, err
	}
	out := h.reminderOut(r)
	return result(out, "Updated %s", h.describeReminder(r)), out, nil
}

func (h *handlers) completeReminder(ctx context.Context, _ *mcp.CallToolRequest, in CompleteReminderIn) (*mcp.CallToolResult, ReminderOut, error) {
	id, err := requireText("id", in.ID)
	if err != nil {
		return nil, ReminderOut{}, err
	}
	completed := in.Completed == nil || *in.Completed
	r, err := h.rem.SetCompleted(ctx, id, completed)
	if err != nil {
		return nil, ReminderOut{}, err
	}
	out := h.reminderOut(r)
	verb := "Completed"
	if !completed {
		verb = "Reopened"
	}
	return result(out, "%s %s", verb, h.describeReminder(r)), out, nil
}

func (h *handlers) deleteReminder(ctx context.Context, _ *mcp.CallToolRequest, in IDIn) (*mcp.CallToolResult, DeleteOut, error) {
	id, err := requireText("id", in.ID)
	if err != nil {
		return nil, DeleteOut{}, err
	}
	if err := h.rem.DeleteReminder(ctx, id); err != nil {
		return nil, DeleteOut{}, err
	}
	out := DeleteOut{ID: id, Deleted: true}
	return result(out, "Deleted reminder %s.", id), out, nil
}

func (h *handlers) reminderOut(r store.Reminder) ReminderOut {
	p := r.Priority
	if p == "" {
		p = store.PriorityNone
	}
	return ReminderOut{
		ID:             r.ID,
		Title:          r.Title,
		Notes:          r.Notes,
		List:           r.List,
		ListID:         r.ListID,
		Due:            formatOptionalTime(r.Due, h.loc),
		Completed:      r.Completed,
		CompletionDate: formatOptionalTime(r.CompletionDate, h.loc),
		Priority:       string(p),
		Flagged:        r.Flagged,
		URL:            r.URL,
		Recurring:      r.Recurring,
	}
}

// describeReminder renders a one-line summary such as
// "\"Pay rent\" in Inbox, due Fri 9 Oct 2026 09:00 (id rem-1).".
func (h *handlers) describeReminder(r store.Reminder) string {
	s := fmt.Sprintf("%q in %s", r.Title, r.List)
	if r.Due != nil {
		s += ", due " + r.Due.In(h.loc).Format("Mon 2 Jan 2006 15:04")
	}
	if r.Completed {
		s += ", done"
	}
	return s + fmt.Sprintf(" (id %s).", r.ID)
}

func pluralNoun(n int, noun string) string {
	if n == 1 {
		return noun
	}
	return noun + "s"
}
