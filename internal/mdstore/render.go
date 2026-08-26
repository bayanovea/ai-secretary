package mdstore

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/bayanovea/secretary/internal/config"
	"github.com/bayanovea/secretary/internal/schedule"
	"github.com/bayanovea/secretary/internal/task"
)

// greeting closes the generated «Обновлено» line. Alfred's real vocabulary
// arrives with the voice package; this is the one phrase the artifact format
// itself prescribes.
const greeting = "К вашим услугам, сэр."

// indent prefixes every line of a task block below its header.
const indent = "  "

// fieldSep joins `ключ:значение` pairs on the field line.
const fieldSep = " · "

// Render writes doc as an artifact. The output is a fixed point of Parse: what
// it emits, Parse reads back into an equal Document, which is what lets the CLI
// rewrite a hand-edited file without losing anything.
//
// now dates the «Обновлено» line and the generated hint lines. cfg supplies the
// tier buffers those hints are computed from; a nil cfg means the defaults.
func Render(w io.Writer, doc *Document, now time.Time, cfg *config.Config) error {
	if doc == nil {
		return fmt.Errorf("mdstore: нечего записывать: документ пуст")
	}
	if cfg == nil {
		cfg = config.Default()
	}

	var lines []string
	lines = append(lines, doc.Preamble...)
	lines = appendBlank(lines)
	lines = append(lines, Marker, updatedLine(now))

	for _, section := range doc.Sections {
		lines = appendBlank(lines)
		lines = append(lines, "## "+section.Name)
		for _, t := range section.Tasks {
			lines = appendBlank(lines)
			lines = append(lines, taskLines(t, now, cfg)...)
		}
	}

	out := strings.Join(trimBlank(lines), "\n")
	if out == "" {
		return nil
	}
	if _, err := io.WriteString(w, out+"\n"); err != nil {
		return fmt.Errorf("mdstore: запись: %w", err)
	}
	return nil
}

// updatedLine is the generated timestamp of the preamble. Parse drops it, so it
// may change on every write without disturbing a round-trip.
func updatedLine(now time.Time) string {
	return updatedPrefix + " " + now.Format(DateLayout) + fieldSep + greeting
}

// taskLines renders one task: header, fields, labels, notes, hint.
func taskLines(t task.Task, now time.Time, cfg *config.Config) []string {
	box, fields := " ", fieldList(t)
	if t.Done {
		box = "x"
	}

	header := fmt.Sprintf("- [%s] `%s` **%s**", box, t.ID, t.Title)
	// A completed task is a one-line record, so its date rides on the header
	// rather than opening a field line of its own.
	if t.CompletedAt != nil {
		header += fieldSep + keyCompleted + ":" + t.CompletedAt.Format(DateLayout)
	}
	lines := []string{header}

	if len(fields) > 0 {
		lines = append(lines, indent+strings.Join(fields, fieldSep))
	}
	if len(t.Labels) > 0 {
		lines = append(lines, indent+labelsPrefix+" "+strings.Join(t.Labels, ", "))
	}
	for _, note := range t.Notes {
		lines = append(lines, indent+"> "+note)
	}
	if hint := hintLine(t, now, cfg); hint != "" {
		lines = append(lines, indent+hint)
	}
	return lines
}

// fieldList builds the ` · `-separated pairs in a fixed order. An absent value
// is left out entirely rather than written empty: `срок:` with nothing after it
// would read as a deadline the user forgot to fill in.
func fieldList(t task.Task) []string {
	var fields []string
	add := func(key, value string) {
		fields = append(fields, key+":"+value)
	}

	if t.Tier != 0 {
		add(keyTier, strconv.Itoa(t.Tier))
	}
	if t.From != "" {
		add(keyFrom, t.From)
	}
	if t.Due != nil {
		add(keyDue, t.Due.Format(DateLayout))
	}
	if !t.Effort.IsZero() {
		add(keyEffort, t.Effort.String())
	}
	if t.Context != "" {
		add(keyContext, t.Context)
	}
	if t.Deferrals > 0 {
		add(keyDeferrals, strconv.Itoa(t.Deferrals))
	}
	if !t.Created.IsZero() {
		add(keyCreated, t.Created.Format(DateLayout))
	}
	return fields
}

// hintLine is the generated status line: the one part of the artifact that is
// recomputed on every write rather than read back. Parse throws these lines
// away, so their wording can change freely.
//
// Overdue and start-now share the red glyph — both mean "today, sir" — while
// «скоро» is a clock and a forgotten duty is asleep. A task with a deadline but
// no объём has no latest start date, so the hint names the deadline instead.
func hintLine(t task.Task, now time.Time, cfg *config.Config) string {
	if t.Done {
		return ""
	}

	start := schedule.LatestStart(t, cfg)
	byDate := func(glyph string) string {
		if start != nil {
			return glyph + " начать не позже " + start.Format(DateLayout)
		}
		return glyph + " срок " + t.Due.Format(DateLayout)
	}

	switch schedule.Status(t, now, cfg) {
	case task.StatusOverdue:
		return "🔴 просрочено" + fieldSep + "срок был " + t.Due.Format(DateLayout)
	case task.StatusStartNow:
		return byDate("🔴")
	case task.StatusSoon:
		return byDate("⏰")
	}

	if schedule.Has(schedule.Flags(t, now, cfg), schedule.FlagForgotten) {
		return "💤 забыто" + fieldSep + "создано " + t.Created.Format(DateLayout)
	}
	return ""
}

// appendBlank separates blocks with exactly one empty line, and never opens the
// file with one.
func appendBlank(lines []string) []string {
	if len(lines) == 0 || strings.TrimSpace(lines[len(lines)-1]) == "" {
		return lines
	}
	return append(lines, "")
}
