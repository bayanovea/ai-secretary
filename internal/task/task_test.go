package task

import (
	"errors"
	"testing"
	"time"
)

func TestStatusString(t *testing.T) {
	cases := []struct {
		in   Status
		want string
	}{
		{in: StatusNoDue, want: "no deadline"},
		{in: StatusCalm, want: "on track"},
		{in: StatusSoon, want: "soon"},
		{in: StatusStartNow, want: "start now"},
		{in: StatusOverdue, want: "overdue"},
		{in: Status(42), want: "unknown"},
	}

	for _, c := range cases {
		c := c
		t.Run(c.want, func(t *testing.T) {
			if got := c.in.String(); got != c.want {
				t.Fatalf("Status(%d).String() = %q, want %q", int(c.in), got, c.want)
			}
		})
	}
}

// TestStatusSeverityOrder pins the constant order: the sort in the store treats
// a larger Status as more urgent, and the zero value must be the calm one.
func TestStatusSeverityOrder(t *testing.T) {
	if StatusNoDue != 0 {
		t.Fatalf("StatusNoDue = %d, expected the zero value", int(StatusNoDue))
	}
	if !(StatusCalm < StatusSoon && StatusSoon < StatusStartNow && StatusStartNow < StatusOverdue) {
		t.Fatal("statuses are not declared in order of urgency")
	}
}

func validTask() Task {
	due := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	return Task{
		ID:      "T-001",
		Title:   "Prepare the Q3 report",
		Section: "Work",
		Tier:    1,
		From:    "Dana Whitfield",
		Due:     &due,
		Effort:  5,
		Context: "@computer",
		Labels:  []string{"reporting", "finance"},
		Created: time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC),
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Task)
		wantErr error
	}{
		{name: "valid task", mutate: func(*Task) {}},
		{name: "minimal task", mutate: func(tk *Task) {
			tk.Due = nil
			tk.Effort = 0
			tk.Context = ""
			tk.Labels = nil
		}},
		{name: "tier 3", mutate: func(tk *Task) { tk.Tier = 3 }},
		{name: "from self", mutate: func(tk *Task) { tk.From = SelfAssigned }},
		{name: "long identifier", mutate: func(tk *Task) { tk.ID = "T-1234" }},

		{name: "empty identifier", mutate: func(tk *Task) { tk.ID = "" }, wantErr: ErrInvalidID},
		{name: "identifier without prefix", mutate: func(tk *Task) { tk.ID = "001" }, wantErr: ErrInvalidID},
		{name: "identifier with a short number", mutate: func(tk *Task) { tk.ID = "T-1" }, wantErr: ErrInvalidID},
		{name: "lower-case identifier", mutate: func(tk *Task) { tk.ID = "t-001" }, wantErr: ErrInvalidID},
		{name: "identifier with a tail", mutate: func(tk *Task) { tk.ID = "T-001x" }, wantErr: ErrInvalidID},

		{name: "tier zero", mutate: func(tk *Task) { tk.Tier = 0 }, wantErr: ErrInvalidTier},
		{name: "tier too large", mutate: func(tk *Task) { tk.Tier = 4 }, wantErr: ErrInvalidTier},
		{name: "negative tier", mutate: func(tk *Task) { tk.Tier = -1 }, wantErr: ErrInvalidTier},

		{name: "empty title", mutate: func(tk *Task) { tk.Title = "" }, wantErr: ErrEmptyTitle},
		{name: "whitespace title", mutate: func(tk *Task) { tk.Title = "   " }, wantErr: ErrEmptyTitle},

		{name: "empty from", mutate: func(tk *Task) { tk.From = "" }, wantErr: ErrEmptyFrom},
		{name: "whitespace from", mutate: func(tk *Task) { tk.From = "  " }, wantErr: ErrEmptyFrom},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			tk := validTask()
			c.mutate(&tk)

			err := tk.Validate()
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate() returned an error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want %v", c.wantErr)
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("Validate() = %v, want %v", err, c.wantErr)
			}
		})
	}
}

func TestHasDue(t *testing.T) {
	tk := validTask()
	if !tk.HasDue() {
		t.Fatal("HasDue() = false for a task with a deadline")
	}
	tk.Due = nil
	if tk.HasDue() {
		t.Fatal("HasDue() = true for a task without a deadline")
	}
}
