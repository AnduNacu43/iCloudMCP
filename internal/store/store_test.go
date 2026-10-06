package store

import (
	"errors"
	"strings"
	"testing"
)

func TestResolveCalendar(t *testing.T) {
	cals := []Calendar{
		{ID: "A1", Title: "Home"},
		{ID: "B2", Title: "Work"},
		{ID: "C3", Title: "Shared"},
		{ID: "D4", Title: "shared"},
	}
	tests := []struct {
		name    string
		ref     string
		wantID  string
		wantErr error
	}{
		{"by id", "B2", "B2", nil},
		{"by title, any case", "  hOmE ", "A1", nil},
		{"id wins over title", "C3", "C3", nil},
		{"missing", "Gym", "", ErrNotFound},
		{"ambiguous title", "SHARED", "", ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveCalendar(cals, tt.ref)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got.ID != tt.wantID {
				t.Errorf("ID = %q, want %q", got.ID, tt.wantID)
			}
		})
	}
}

func TestResolveErrorMessagesHelpTheCaller(t *testing.T) {
	lists := []ReminderList{{ID: "1", Title: "Groceries"}, {ID: "2", Title: "Errands"}, {ID: "3", Title: "Errands"}}

	_, err := ResolveList(lists, "Chores")
	if !strings.Contains(err.Error(), "Groceries, Errands") {
		t.Errorf("not-found message should list available names, got %q", err)
	}
	_, err = ResolveList(lists, "errands")
	if !strings.Contains(err.Error(), "2, 3") {
		t.Errorf("ambiguous message should list candidate IDs, got %q", err)
	}
}

func TestParsePriority(t *testing.T) {
	for in, want := range map[string]Priority{"": PriorityNone, "none": PriorityNone, "Low": PriorityLow, " medium ": PriorityMedium, "HIGH": PriorityHigh} {
		got, err := ParsePriority(in)
		if err != nil || got != want {
			t.Errorf("ParsePriority(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParsePriority("urgent"); !errors.Is(err, ErrInvalid) {
		t.Errorf("ParsePriority(urgent) err = %v, want ErrInvalid", err)
	}
}

func TestTitleIsUnique(t *testing.T) {
	cals := []Calendar{{ID: "1", Title: "Work"}, {ID: "2", Title: "work"}, {ID: "3", Title: "Home"}}
	title := func(c Calendar) string { return c.Title }
	if TitleIsUnique(cals, cals[0], title) {
		t.Error("Work is shared with work and should not be unique")
	}
	if !TitleIsUnique(cals, cals[2], title) {
		t.Error("Home should be unique")
	}
}
