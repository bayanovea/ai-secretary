package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/bayanovea/secretary/internal/task"
)

func date(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

// TestHumanDue pins the wording of the deadline column: it is the part of the
// output a person actually reads, and «tomorrow» must not silently become a date.
func TestHumanDue(t *testing.T) {
	now := date("2026-08-28")
	cases := []struct {
		due  string
		want string
	}{
		{"", "—"},
		{"2026-08-28", "today"},
		{"2026-08-29", "tomorrow"},
		{"2026-08-27", "yesterday"},
		{"2026-08-31", "Mon 31 Aug"},
		{"2026-09-15", "15 Sep"},
		{"2027-01-09", "9 Jan 2027"},
	}
	for _, tc := range cases {
		var due *time.Time
		if tc.due != "" {
			d := date(tc.due)
			due = &d
		}
		if got := humanDue(due, now); got != tc.want {
			t.Errorf("humanDue(%q) = %q, want %q", tc.due, got, tc.want)
		}
	}
}

// TestColorEnabled: a pipe gets no escapes, which is what keeps every other test
// — and the skill, which reads the output — looking at plain text.
func TestColorEnabled(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	for _, tc := range []struct {
		mode string
		want bool
	}{{"auto", false}, {"never", false}, {"always", true}} {
		got, err := colorEnabled(tc.mode, &bytes.Buffer{})
		if err != nil {
			t.Fatalf("--color=%s: %v", tc.mode, err)
		}
		if got != tc.want {
			t.Errorf("--color=%s into a pipe: %v, want %v", tc.mode, got, tc.want)
		}
	}
	if _, err := colorEnabled("pink", &bytes.Buffer{}); err == nil {
		t.Error("unknown colour mode accepted")
	}
}

func TestStatusGlyphAndTierMark(t *testing.T) {
	if statusGlyph(task.StatusOverdue) == statusGlyph(task.StatusSoon) {
		t.Error("overdue and «soon» are marked the same")
	}
	if tierMark(1) == tierMark(2) || tierMark(3) != "" {
		t.Errorf("tiers are indistinguishable without colour: %q %q %q", tierMark(1), tierMark(2), tierMark(3))
	}
}
