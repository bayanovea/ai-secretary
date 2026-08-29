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
// output a person actually reads, and «завтра» must not silently become a date.
func TestHumanDue(t *testing.T) {
	now := date("2026-08-28")
	cases := []struct {
		due  string
		want string
	}{
		{"", "—"},
		{"2026-08-28", "сегодня"},
		{"2026-08-29", "завтра"},
		{"2026-08-27", "вчера"},
		{"2026-08-31", "пн 31 авг"},
		{"2026-09-15", "15 сен"},
		{"2027-01-09", "9 янв 2027"},
	}
	for _, tc := range cases {
		var due *time.Time
		if tc.due != "" {
			d := date(tc.due)
			due = &d
		}
		if got := humanDue(due, now); got != tc.want {
			t.Errorf("humanDue(%q) = %q, ожидалось %q", tc.due, got, tc.want)
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
			t.Errorf("--color=%s в трубу: %v, ожидалось %v", tc.mode, got, tc.want)
		}
	}
	if _, err := colorEnabled("розовый", &bytes.Buffer{}); err == nil {
		t.Error("неизвестный режим цвета принят")
	}
}

func TestStatusGlyphAndTierMark(t *testing.T) {
	if statusGlyph(task.StatusOverdue) == statusGlyph(task.StatusSoon) {
		t.Error("просроченное и «скоро» помечены одинаково")
	}
	if tierMark(1) == tierMark(2) || tierMark(3) != "" {
		t.Errorf("тиры неразличимы без цвета: %q %q %q", tierMark(1), tierMark(2), tierMark(3))
	}
}
