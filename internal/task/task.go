// Package task holds the domain model: a single duty, how much work it is, and
// how urgent it has become.
//
// The package is deliberately free of clocks and I/O. Status is a value that
// the schedule package computes from a `now` handed in by the caller, so every
// run over the same artifact on the same day produces the same answer.
package task

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Status is how a task stands relative to its deadline.
type Status int

// The five outcomes. They are ordered by severity, calmest first, so that the
// zero value is the harmless one.
const (
	StatusNoDue Status = iota
	StatusCalm
	StatusSoon
	StatusStartNow
	StatusOverdue
)

// String returns the Russian label used in the artifact and in Alfred's speech.
func (s Status) String() string {
	switch s {
	case StatusNoDue:
		return "без срока"
	case StatusCalm:
		return "спокойно"
	case StatusSoon:
		return "скоро"
	case StatusStartNow:
		return "пора начинать"
	case StatusOverdue:
		return "просрочено"
	default:
		return "неизвестно"
	}
}

// SelfAssigned is the value of `от:` for a duty nobody else handed you. The
// grouping package skips it: "раз уж вы говорите с собой" is not a useful hint.
const SelfAssigned = "себя"

// Validation failures, one per rule, so callers can tell them apart.
var (
	ErrInvalidID   = errors.New("некорректный идентификатор")
	ErrInvalidTier = errors.New("некорректный тир")
	ErrEmptyTitle  = errors.New("пустое название")
	ErrEmptyFrom   = errors.New("не указано, от кого задача")
)

// idPattern matches the `T-001` form assigned by the store.
var idPattern = regexp.MustCompile(`^T-\d{3,}$`)

// Task is one duty as written in the artifact.
//
// Everything the parser can read is here and nothing that it cannot: the latest
// start date and the status are computed on demand from Due, Effort and Tier
// rather than stored, so they cannot go stale after a hand edit.
type Task struct {
	ID      string
	Title   string
	Section string
	Tier    int
	From    string
	// Due is the `срок:`. Absent means the task has no deadline at all, which
	// is a status of its own rather than an infinitely distant one.
	Due     *time.Time
	Effort  Effort
	Context string
	Labels  []string
	// Notes are the free-text `>` lines, one entry per line.
	Notes []string
	// Steps are the checkbox sub-items of the duty, in the order written.
	Steps []Step
	// Deferrals counts how many times the deadline has been pushed back. Alfred
	// starts commenting at two.
	Deferrals   int
	Created     time.Time
	Done        bool
	CompletedAt *time.Time
}

// Validate checks the invariants the rest of the code relies on: a well-formed
// id, a tier in range, and the two fields a duty is meaningless without.
func (t Task) Validate() error {
	if !idPattern.MatchString(t.ID) {
		return fmt.Errorf("%w %q: ожидается T-NNN", ErrInvalidID, t.ID)
	}
	if t.Tier < MinTier || t.Tier > MaxTier {
		return fmt.Errorf("%w %d в задаче %s: ожидается 1..3", ErrInvalidTier, t.Tier, t.ID)
	}
	if strings.TrimSpace(t.Title) == "" {
		return fmt.Errorf("%w в задаче %s", ErrEmptyTitle, t.ID)
	}
	if strings.TrimSpace(t.From) == "" {
		return fmt.Errorf("%w %s", ErrEmptyFrom, t.ID)
	}
	return nil
}

// HasDue reports whether a deadline is set.
func (t Task) HasDue() bool { return t.Due != nil }

// Tier bounds, mirrored from the config package so that the domain model does
// not depend on configuration to know what a legal tier is.
const (
	MinTier = 1
	MaxTier = 3
)

// Step is one checkbox inside a duty: «позвонить», «отвезти вещи». Steps are
// the parts of a single errand, not duties in their own right — they have no
// identifier and no effort, and they never appear in a brief on their own.
//
// Tier and Due are optional and independent of the parent: the call that opens
// an errand is often more urgent than the errand's own deadline. Both are zero
// for the ordinary step, which is most of them.
type Step struct {
	Text string
	Done bool
	Tier int
	Due  *time.Time
}

// HasDue reports whether the step carries a deadline of its own.
func (s Step) HasDue() bool { return s.Due != nil }

// StepsDone counts the checked steps.
func (t Task) StepsDone() int {
	n := 0
	for _, s := range t.Steps {
		if s.Done {
			n++
		}
	}
	return n
}

// StepsLeft counts the unchecked ones.
func (t Task) StepsLeft() int { return len(t.Steps) - t.StepsDone() }

// Progress renders the step count as `2/3`, or "" for a duty without steps —
// which is most of them, and must not grow a column for it.
func (t Task) Progress() string {
	if len(t.Steps) == 0 {
		return ""
	}
	return fmt.Sprintf("%d/%d", t.StepsDone(), len(t.Steps))
}
