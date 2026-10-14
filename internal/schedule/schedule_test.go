package schedule

import (
	"testing"
	"time"

	"github.com/bayanovea/secretary/internal/config"
	"github.com/bayanovea/secretary/internal/task"
)

// date parses a `YYYY-MM-DD` test fixture.
func date(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("date %q: %v", s, err)
	}
	return d
}

// due is the pointer form the Task field wants.
func due(t *testing.T, s string) *time.Time {
	t.Helper()
	d := date(t, s)
	return &d
}

// effort parses an artifact-form effort.
func effort(t *testing.T, s string) task.Effort {
	t.Helper()
	e, err := task.ParseEffort(s)
	if err != nil {
		t.Fatalf("effort %q: %v", s, err)
	}
	return e
}

func TestLatestStart(t *testing.T) {
	tests := []struct {
		name   string
		tier   int
		dueOn  string
		effort string
		want   string // "" means nil
	}{
		// tier 1: buffer = max(2d, 50% of effort).
		{name: "tier 1, minimum buffer exceeds the share", tier: 1, dueOn: "2026-09-10", effort: "1d", want: "2026-09-07"},
		{name: "tier 1, share exceeds the minimum", tier: 1, dueOn: "2026-09-10", effort: "10d", want: "2026-08-26"},
		{name: "tier 1, share equals the minimum", tier: 1, dueOn: "2026-09-10", effort: "4d", want: "2026-09-04"},
		// tier 2: buffer = max(1d, 25% of effort).
		{name: "tier 2, minimum buffer exceeds the share", tier: 2, dueOn: "2026-09-10", effort: "2d", want: "2026-09-07"},
		{name: "tier 2, share exceeds the minimum", tier: 2, dueOn: "2026-09-10", effort: "8d", want: "2026-08-31"},
		// tier 3: no buffer at all.
		{name: "tier 3, no buffer", tier: 3, dueOn: "2026-09-10", effort: "3d", want: "2026-09-07"},
		{name: "tier 3, an hour of work takes a calendar day", tier: 3, dueOn: "2026-09-10", effort: "1h", want: "2026-09-09"},
		// Hour-sized efforts round up together with the buffer.
		{name: "tier 1, an hour of work", tier: 1, dueOn: "2026-08-29", effort: "1h", want: "2026-08-26"},
		{name: "tier 2, four hours", tier: 2, dueOn: "2026-09-10", effort: "4h", want: "2026-09-08"},
		// Weeks count as 7 days.
		{name: "tier 2, a week", tier: 2, dueOn: "2026-09-10", effort: "1w", want: "2026-09-01"},
	}

	cfg := config.Default()
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			tk := task.Task{ID: "T-001", Title: "Task", From: "self", Tier: tt.tier,
				Due: due(t, tt.dueOn), Effort: effort(t, tt.effort)}

			got := LatestStart(tk, cfg)
			if got == nil {
				t.Fatalf("LatestStart() = nil, want %s", tt.want)
			}
			if formatted := got.Format("2006-01-02"); formatted != tt.want {
				t.Errorf("LatestStart() = %s, want %s", formatted, tt.want)
			}
		})
	}
}

func TestLatestStartAbsent(t *testing.T) {
	tests := []struct {
		name string
		tk   task.Task
	}{
		{name: "no deadline", tk: task.Task{Tier: 1, Effort: effort(t, "5d")}},
		{name: "no effort", tk: task.Task{Tier: 1, Due: due(t, "2026-09-10")}},
		{name: "no deadline and no effort", tk: task.Task{Tier: 1}},
		{name: "zero effort", tk: task.Task{Tier: 1, Due: due(t, "2026-09-10"), Effort: effort(t, "0h")}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := LatestStart(tt.tk, config.Default()); got != nil {
				t.Errorf("LatestStart() = %s, want nil", got.Format("2006-01-02"))
			}
		})
	}
}

// TestLatestStartNilConfig checks the documented fallback to the defaults.
func TestLatestStartNilConfig(t *testing.T) {
	tk := task.Task{Tier: 1, Due: due(t, "2026-09-10"), Effort: effort(t, "1d")}

	got, want := LatestStart(tk, nil), LatestStart(tk, config.Default())
	if got == nil || want == nil || !got.Equal(*want) {
		t.Errorf("LatestStart(nil cfg) = %v, want %v", got, want)
	}
}

func TestStatus(t *testing.T) {
	tests := []struct {
		name string
		tk   task.Task
		now  string
		want task.Status
	}{
		{
			name: "no deadline",
			tk:   task.Task{Tier: 2, Effort: effort(t, "1d")},
			now:  "2026-08-27",
			want: task.StatusNoDue,
		},
		{
			name: "overdue",
			tk:   task.Task{Tier: 1, Due: due(t, "2026-08-26"), Effort: effort(t, "1d")},
			now:  "2026-08-27",
			want: task.StatusOverdue,
		},
		{
			name: "deadline exactly today — not yet overdue",
			tk:   task.Task{Tier: 1, Due: due(t, "2026-08-27"), Effort: effort(t, "1d")},
			now:  "2026-08-27",
			want: task.StatusStartNow,
		},
		{
			name: "start day exactly today",
			// effort 1d, tier 1 ⇒ buffer 2d ⇒ start 2026-08-27.
			tk:   task.Task{Tier: 1, Due: due(t, "2026-08-30"), Effort: effort(t, "1d")},
			now:  "2026-08-27",
			want: task.StatusStartNow,
		},
		{
			name: "start day tomorrow — soon",
			tk:   task.Task{Tier: 1, Due: due(t, "2026-08-31"), Effort: effort(t, "1d")},
			now:  "2026-08-27",
			want: task.StatusSoon,
		},
		{
			name: "start day on the window edge",
			// start 2026-08-30 = today + 3 = soon_window_days.
			tk:   task.Task{Tier: 1, Due: due(t, "2026-09-02"), Effort: effort(t, "1d")},
			now:  "2026-08-27",
			want: task.StatusSoon,
		},
		{
			name: "start day one past the window — on track",
			tk:   task.Task{Tier: 1, Due: due(t, "2026-09-03"), Effort: effort(t, "1d")},
			now:  "2026-08-27",
			want: task.StatusCalm,
		},
		{
			name: "on track, distant deadline",
			tk:   task.Task{Tier: 3, Due: due(t, "2026-12-31"), Effort: effort(t, "1d")},
			now:  "2026-08-27",
			want: task.StatusCalm,
		},
		{
			name: "no effort: the deadline stands in for the start day",
			tk:   task.Task{Tier: 2, Due: due(t, "2026-08-29")},
			now:  "2026-08-27",
			want: task.StatusSoon,
		},
		{
			name: "no effort: deadline today",
			tk:   task.Task{Tier: 2, Due: due(t, "2026-08-27")},
			now:  "2026-08-27",
			want: task.StatusStartNow,
		},
		{
			name: "a completed task is on track even past its deadline",
			tk:   task.Task{Tier: 1, Due: due(t, "2026-01-01"), Effort: effort(t, "1d"), Done: true},
			now:  "2026-08-27",
			want: task.StatusCalm,
		},
		{
			name: "time of day does not affect day comparison",
			tk:   task.Task{Tier: 3, Due: due(t, "2026-08-27")},
			now:  "2026-08-27",
			want: task.StatusStartNow,
		},
	}

	cfg := config.Default()
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			now := date(t, tt.now).Add(23 * time.Hour)
			if got := Status(tt.tk, now, cfg); got != tt.want {
				t.Errorf("Status() = %v (%s), want %v (%s)", got, got, tt.want, tt.want)
			}
		})
	}
}

func TestFlags(t *testing.T) {
	const now = "2026-08-27"
	fresh := date(t, "2026-08-20")
	stale := date(t, "2026-08-01")

	tests := []struct {
		name string
		tk   task.Task
		want []Flag
	}{
		{
			name: "complete data — no flags",
			tk:   task.Task{Tier: 1, Due: due(t, "2026-09-10"), Effort: effort(t, "1d"), Created: fresh},
			want: nil,
		},
		{
			name: "tier 1 without a deadline",
			tk:   task.Task{Tier: 1, Effort: effort(t, "1d"), Created: fresh},
			want: []Flag{FlagIncomplete},
		},
		{
			name: "tier 1 without an effort",
			tk:   task.Task{Tier: 1, Due: due(t, "2026-09-10"), Created: fresh},
			want: []Flag{FlagIncomplete},
		},
		{
			name: "tier 2 without an effort — not our case",
			tk:   task.Task{Tier: 2, Due: due(t, "2026-09-10"), Created: fresh},
			want: nil,
		},
		{
			name: "two deferrals",
			tk:   task.Task{Tier: 2, Due: due(t, "2026-09-10"), Effort: effort(t, "1d"), Deferrals: 2, Created: fresh},
			want: []Flag{FlagPostponed},
		},
		{
			name: "one deferral — stay quiet",
			tk:   task.Task{Tier: 2, Due: due(t, "2026-09-10"), Effort: effort(t, "1d"), Deferrals: 1, Created: fresh},
			want: nil,
		},
		{
			name: "tier 2 without a deadline, stale",
			tk:   task.Task{Tier: 2, Effort: effort(t, "1d"), Created: stale},
			want: []Flag{FlagForgotten},
		},
		{
			name: "tier 3 without a deadline, stale — background may",
			tk:   task.Task{Tier: 3, Effort: effort(t, "1d"), Created: stale},
			want: nil,
		},
		{
			name: "stale but has a deadline — not forgotten",
			tk:   task.Task{Tier: 2, Due: due(t, "2026-09-10"), Effort: effort(t, "1d"), Created: stale},
			want: nil,
		},
		{
			name: "exactly stale_days old — not yet forgotten",
			tk:   task.Task{Tier: 2, Effort: effort(t, "1d"), Created: date(t, "2026-08-13")},
			want: nil,
		},
		{
			name: "no creation date — never counted as forgotten",
			tk:   task.Task{Tier: 2, Effort: effort(t, "1d")},
			want: nil,
		},
		{
			name: "all three at once, in constant order",
			tk:   task.Task{Tier: 1, Deferrals: 3, Created: stale},
			want: []Flag{FlagIncomplete, FlagPostponed, FlagForgotten},
		},
		{
			name: "a completed task stays quiet",
			tk:   task.Task{Tier: 1, Deferrals: 3, Created: stale, Done: true},
			want: nil,
		},
	}

	cfg := config.Default()
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := Flags(tt.tk, date(t, now), cfg)
			if len(got) != len(tt.want) {
				t.Fatalf("Flags() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("Flags() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestHas(t *testing.T) {
	flags := []Flag{FlagIncomplete, FlagForgotten}
	if !Has(flags, FlagForgotten) {
		t.Error("Has(stale) = false, want true")
	}
	if Has(flags, FlagPostponed) {
		t.Error("Has(deferred) = true, want false")
	}
	if Has(nil, FlagIncomplete) {
		t.Error("Has(nil) = true, want false")
	}
}

func TestFlagString(t *testing.T) {
	tests := []struct {
		flag Flag
		want string
	}{
		{FlagIncomplete, "incomplete data"},
		{FlagPostponed, "deferred"},
		{FlagForgotten, "stale"},
		{Flag(42), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.flag.String(); got != tt.want {
			t.Errorf("Flag(%d).String() = %q, want %q", tt.flag, got, tt.want)
		}
	}
}

// TestSeverityOrder is what the sort order depends on: louder statuses rank
// strictly higher.
func TestSeverityOrder(t *testing.T) {
	order := []task.Status{
		task.StatusNoDue,
		task.StatusCalm,
		task.StatusSoon,
		task.StatusStartNow,
		task.StatusOverdue,
	}

	for i := 1; i < len(order); i++ {
		if Severity(order[i]) <= Severity(order[i-1]) {
			t.Errorf("Severity(%s) = %d is not greater than Severity(%s) = %d",
				order[i], Severity(order[i]), order[i-1], Severity(order[i-1]))
		}
	}
}
