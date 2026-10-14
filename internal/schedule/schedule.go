// Package schedule answers the two questions the artifact cannot store: when a
// duty must be started, and how it stands today.
//
// Both are pure functions of (due, effort, tier) and a `now` handed in by the
// caller. Nothing here reads a clock, so a run over the same artifact on the
// same day always produces the same answer, and a test can put the day wherever
// it likes.
package schedule

import (
	"math"
	"time"

	"github.com/bayanovea/secretary/internal/config"
	"github.com/bayanovea/secretary/internal/task"
)

// DeferralsThreshold is how many postponements it takes before Alfred mentions
// that the duty is being avoided rather than scheduled.
const DeferralsThreshold = 2

// Flag is a remark about a task that is not about its deadline: something is
// missing, or the task has been treated in a way worth pointing out.
type Flag int

// The flags Alfred raises. They are independent of Status — a perfectly calm
// task can still be the one you have postponed three times.
const (
	// FlagIncomplete marks a tier-1 task without a deadline or without an
	// effort estimate: the latest start date cannot be computed, so the one
	// duty that must not slip is the one nobody is watching.
	FlagIncomplete Flag = iota
	// FlagPostponed marks a deadline pushed back DeferralsThreshold times.
	FlagPostponed
	// FlagForgotten marks a tier-1 or tier-2 task with no deadline that has sat
	// in the artifact for longer than stale_days.
	FlagForgotten
)

// String returns the Russian label used in the artifact and in Alfred's speech.
func (f Flag) String() string {
	switch f {
	case FlagIncomplete:
		return "incomplete data"
	case FlagPostponed:
		return "deferred"
	case FlagForgotten:
		return "stale"
	default:
		return "unknown"
	}
}

// LatestStart is the last day the task can be started and still be finished on
// time: `due − effort − buffer(tier)`.
//
// It is nil when either the deadline or the effort is missing — with one of the
// two unknown the date would be a guess, and a guessed start date is worse than
// none. Callers that need to say something anyway fall back to the deadline.
//
// The lead time is rounded up to whole days: part of a day of work still
// occupies a day of the calendar, and half a day started tomorrow is not
// started.
func LatestStart(t task.Task, cfg *config.Config) *time.Time {
	if cfg == nil {
		cfg = config.Default()
	}
	if t.Due == nil || t.Effort.IsZero() {
		return nil
	}

	tier := cfg.Tier(t.Tier)
	buffer := math.Max(float64(tier.BufferMinDays), tier.BufferRatio*t.Effort.Days())
	lead := int(math.Ceil(t.Effort.Days() + buffer))
	start := t.Due.AddDate(0, 0, -lead)
	return &start
}

// Status places the task relative to today:
//
//	overdue        the deadline has passed
//	start now      the latest start day has arrived
//	soon           it arrives within soon_window_days
//	on track       otherwise
//	no deadline    there is no deadline at all
//
// Without an effort estimate there is no latest start date, so the deadline
// itself stands in for it: a task due tomorrow is «start now» even when
// nobody said how long it takes.
//
// A completed task is on track whatever its dates say — it has stopped being a
// duty, and reporting it as overdue would be Alfred nagging about the past.
func Status(t task.Task, now time.Time, cfg *config.Config) task.Status {
	if cfg == nil {
		cfg = config.Default()
	}
	if t.Done {
		return task.StatusCalm
	}
	if t.Due == nil {
		return task.StatusNoDue
	}
	if daysBetween(now, *t.Due) < 0 {
		return task.StatusOverdue
	}

	start := LatestStart(t, cfg)
	if start == nil {
		start = t.Due
	}
	switch days := daysBetween(now, *start); {
	case days <= 0:
		return task.StatusStartNow
	case days <= cfg.SoonWindowDays:
		return task.StatusSoon
	default:
		return task.StatusCalm
	}
}

// Flags lists the remarks that apply to the task today, in the fixed order of
// the Flag constants. A completed task raises none.
func Flags(t task.Task, now time.Time, cfg *config.Config) []Flag {
	if cfg == nil {
		cfg = config.Default()
	}
	if t.Done {
		return nil
	}

	var flags []Flag
	if t.Tier == task.MinTier && (t.Due == nil || t.Effort.IsZero()) {
		flags = append(flags, FlagIncomplete)
	}
	if t.Deferrals >= DeferralsThreshold {
		flags = append(flags, FlagPostponed)
	}
	if t.Tier >= task.MinTier && t.Tier < task.MaxTier &&
		t.Due == nil && !t.Created.IsZero() &&
		daysBetween(t.Created, now) > cfg.StaleDays {
		flags = append(flags, FlagForgotten)
	}
	return flags
}

// Has reports whether flag is present in flags.
func Has(flags []Flag, flag Flag) bool {
	for _, f := range flags {
		if f == flag {
			return true
		}
	}
	return false
}

// Severity ranks statuses for the sort order: the loudest first when sorted
// descending. It mirrors the order of the task.Status constants, but the sort
// depends on the number rather than on the enum's internal layout.
func Severity(s task.Status) int {
	switch s {
	case task.StatusOverdue:
		return 4
	case task.StatusStartNow:
		return 3
	case task.StatusSoon:
		return 2
	case task.StatusCalm:
		return 1
	case task.StatusNoDue:
		return 0
	default:
		return 0
	}
}

// dayOf drops the clock time and the zone, so that a comparison is between
// calendar days however the caller built its `now`.
func dayOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// daysBetween counts whole calendar days from a to b; negative when b is first.
func daysBetween(a, b time.Time) int {
	return int(dayOf(b).Sub(dayOf(a)).Hours() / 24)
}
