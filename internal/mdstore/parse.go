// Package mdstore reads and writes the Markdown artifact that is the
// secretary's source of truth.
//
// The artifact is meant to be hand-edited, so parsing is forgiving about
// spacing and about keys it does not know, and strict about the few things a
// wrong answer would be worse than an error: tiers, dates and efforts. Lines
// the renderer generates — the version marker, the «Updated» line and the
// status glyph hints — are dropped here and regenerated on write, which is what
// makes parse → render → parse a fixed point.
package mdstore

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bayanovea/ai-secretary/internal/task"
)

// DateLayout is the `YYYY-MM-DD` form used by every date in the artifact.
const DateLayout = "2006-01-02"

// DoneSection is the section completed tasks are moved into.
const DoneSection = "Done"

// Marker identifies a file this program wrote. It is regenerated on render.
const Marker = "<!-- secretary:v1 -->"

// updatedPrefix starts the generated «Updated» line of the preamble.
const updatedPrefix = "> Updated:"

// Field keys of the ` · `-separated line under a task header.
const (
	keyTier      = "tier"
	keyFrom      = "from"
	keyDue       = "due"
	keyEffort    = "effort"
	keyContext   = "context"
	keyDeferrals = "deferrals"
	keyCreated   = "created"
	keyCompleted = "done"
)

// labelsPrefix starts the `labels:` line.
const labelsPrefix = "labels:"

// glyphs begin the generated hint lines. They carry no information that is not
// recomputable, so the parser throws them away rather than trying to read them.
var glyphs = []string{"🔴", "⏰", "🟡", "💤"}

var (
	sectionRe = regexp.MustCompile(`^#{2,}\s+(.+?)\s*$`)
	// taskHeaderRe is deliberately loose about whitespace: the artifact is
	// hand-edited and an extra space must not lose a duty.
	taskHeaderRe = regexp.MustCompile("^-\\s*\\[([ xX])\\]\\s*`([^`]*)`\\s*\\*\\*(.+?)\\*\\*\\s*(?:·\\s*(.*?))?\\s*$")
	// stepRe matches a checkbox inside a task block. It is tried only after
	// taskHeaderRe has failed, so the two cannot be confused: a step carries
	// neither an identifier nor a bold title.
	stepRe = regexp.MustCompile(`^-\s*\[([ xX])\]\s*(.*)$`)
)

// Section is one `## Heading` block with the tasks filed under it, in the
// order they appear in the file.
type Section struct {
	Name  string
	Tasks []task.Task
}

// Document is a parsed artifact.
//
// Preamble holds the free text before the first section — the `# Duties`
// heading and anything the user added — with the generated marker and
// «Updated» lines removed and surrounding blank lines trimmed, so that it
// survives a round-trip unchanged.
type Document struct {
	Preamble []string
	Sections []Section
}

// Error is a parse failure located at a line of the artifact.
type Error struct {
	Line int
	Msg  string
}

func (e *Error) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Msg) }

func errAt(line int, format string, args ...interface{}) error {
	return &Error{Line: line, Msg: fmt.Sprintf(format, args...)}
}

// pos remembers where a task's header was, so that a rule which can only be
// checked once the whole block has been read still reports a useful line.
type pos struct {
	section int
	index   int
	line    int
}

// Parse reads an artifact. It returns the first error it meets rather than
// collecting them: a file that does not parse is not one to guess at.
func Parse(r io.Reader) (*Document, error) {
	doc := &Document{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	seen := make(map[string]int)
	var positions []pos
	secIdx, taskIdx, lineNo := -1, -1, 0

	for sc.Scan() {
		lineNo++
		raw := sc.Text()
		line := strings.TrimSpace(raw)

		if isGlyphLine(line) || line == Marker {
			continue
		}
		if secIdx < 0 && strings.HasPrefix(line, updatedPrefix) {
			continue
		}

		if m := sectionRe.FindStringSubmatch(line); m != nil {
			doc.Sections = append(doc.Sections, Section{Name: m[1]})
			secIdx, taskIdx = len(doc.Sections)-1, -1
			continue
		}

		if m := taskHeaderRe.FindStringSubmatch(line); m != nil {
			if secIdx < 0 {
				return nil, errAt(lineNo, "task %q outside any section", m[2])
			}
			t := task.Task{
				ID:      strings.TrimSpace(m[2]),
				Title:   strings.TrimSpace(m[3]),
				Section: doc.Sections[secIdx].Name,
				Done:    m[1] != " ",
			}
			if first, dup := seen[t.ID]; dup {
				return nil, errAt(lineNo, "duplicate identifier %s, already seen on line %d", t.ID, first)
			}
			seen[t.ID] = lineNo
			if rest := strings.TrimSpace(m[4]); rest != "" {
				if err := applyFields(&t, rest, lineNo); err != nil {
					return nil, err
				}
			}
			doc.Sections[secIdx].Tasks = append(doc.Sections[secIdx].Tasks, t)
			taskIdx = len(doc.Sections[secIdx].Tasks) - 1
			positions = append(positions, pos{section: secIdx, index: taskIdx, line: lineNo})
			continue
		}

		if secIdx < 0 {
			doc.Preamble = append(doc.Preamble, raw)
			continue
		}
		if line == "" {
			continue
		}
		if taskIdx < 0 {
			return nil, errAt(lineNo, "line %q outside any task", line)
		}

		cur := &doc.Sections[secIdx].Tasks[taskIdx]
		switch {
		case strings.HasPrefix(line, labelsPrefix):
			cur.Labels = append(cur.Labels, splitLabels(strings.TrimPrefix(line, labelsPrefix))...)
		case strings.HasPrefix(line, ">"):
			cur.Notes = append(cur.Notes, strings.TrimSpace(strings.TrimPrefix(line, ">")))
		default:
			if m := stepRe.FindStringSubmatch(line); m != nil {
				step, err := parseStep(strings.TrimSpace(m[2]), lineNo)
				if err != nil {
					return nil, fmt.Errorf("%w (task %s)", err, cur.ID)
				}
				step.Done = m[1] != " "
				cur.Steps = append(cur.Steps, step)
				continue
			}
			if err := applyFields(cur, line, lineNo); err != nil {
				return nil, err
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("mdstore: read: %w", err)
	}

	doc.Preamble = trimBlank(doc.Preamble)

	for _, p := range positions {
		if err := check(doc.Sections[p.section].Tasks[p.index], p.line); err != nil {
			return nil, err
		}
	}
	return doc, nil
}

// applyFields reads one ` · `-separated line of `key:value` pairs into t.
// Unknown keys are ignored — a future version of the format, or a note the user
// invented, must not make the file unreadable — but a known key with a value
// that cannot be read is an error.
func applyFields(t *task.Task, line string, lineNo int) error {
	for _, field := range strings.Split(line, "·") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		key, value, ok := strings.Cut(field, ":")
		if !ok {
			return errAt(lineNo, "field %q has no value: expected key:value", field)
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)

		switch key {
		case keyTier:
			tier, err := strconv.Atoi(value)
			if err != nil {
				return errAt(lineNo, "tier %q: not a number", value)
			}
			if tier < task.MinTier || tier > task.MaxTier {
				return errAt(lineNo, "tier %d: expected 1..3", tier)
			}
			t.Tier = tier
		case keyFrom:
			t.From = value
		case keyDue:
			due, err := parseDate(value, keyDue, lineNo)
			if err != nil {
				return err
			}
			t.Due = &due
		case keyEffort:
			effort, err := task.ParseEffort(value)
			if err != nil {
				return errAt(lineNo, "%v", err)
			}
			t.Effort = effort
		case keyContext:
			t.Context = value
		case keyDeferrals:
			n, err := strconv.Atoi(value)
			if err != nil {
				return errAt(lineNo, "deferrals %q: not a number", value)
			}
			if n < 0 {
				return errAt(lineNo, "deferrals %d: negative", n)
			}
			t.Deferrals = n
		case keyCreated:
			created, err := parseDate(value, keyCreated, lineNo)
			if err != nil {
				return err
			}
			t.Created = created
		case keyCompleted:
			completed, err := parseDate(value, keyCompleted, lineNo)
			if err != nil {
				return err
			}
			t.CompletedAt = &completed
		}
	}
	return nil
}

// check validates a task once its whole block has been read. Completed tasks
// are held to a lower standard: they are history, and the artifact keeps them
// as a one-line record without a tier or a from.
func check(t task.Task, line int) error {
	if t.Done {
		if t.ID == "" || t.Title == "" {
			return errAt(line, "completed task without an identifier or a title")
		}
		return nil
	}
	if err := t.Validate(); err != nil {
		return errAt(line, "%v", err)
	}
	return nil
}

func parseDate(value, key string, lineNo int) (time.Time, error) {
	d, err := time.Parse(DateLayout, value)
	if err != nil {
		return time.Time{}, errAt(lineNo, "%s %q: expected YYYY-MM-DD", key, value)
	}
	return d, nil
}

// splitLabels reads the comma-separated `labels:` line, dropping empties so that
// a trailing comma is harmless.
func splitLabels(s string) []string {
	var labels []string
	for _, part := range strings.Split(s, ",") {
		if label := strings.TrimSpace(part); label != "" {
			labels = append(labels, label)
		}
	}
	return labels
}

func isGlyphLine(line string) bool {
	for _, g := range glyphs {
		if strings.HasPrefix(line, g) {
			return true
		}
	}
	return false
}

// trimBlank removes leading and trailing blank lines, so that how many empty
// lines the user left around the heading does not change the parsed value.
func trimBlank(lines []string) []string {
	start, end := 0, len(lines)
	for start < end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	if start == end {
		return nil
	}
	return lines[start:end]
}

// parseStep reads the text of a checkbox and the optional `· tier:1 · due:…`
// pairs a step may carry. Only *trailing* known keys are taken as fields, and
// scanning stops at the first segment that is not one: a step whose text itself
// contains a `·` keeps it, which is the same forgiveness the task line gets for
// unknown keys.
func parseStep(text string, lineNo int) (task.Step, error) {
	var step task.Step
	for {
		head, tail, ok := cutLast(text, "·")
		if !ok {
			break
		}
		key, value, ok := strings.Cut(strings.TrimSpace(tail), ":")
		if !ok {
			break
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch key {
		case keyTier:
			if step.Tier != 0 {
				return step, errAt(lineNo, "step: tier given twice")
			}
			tier, err := strconv.Atoi(value)
			if err != nil {
				return step, errAt(lineNo, "step: tier %q: not a number", value)
			}
			if tier < task.MinTier || tier > task.MaxTier {
				return step, errAt(lineNo, "step: tier %d: expected 1..3", tier)
			}
			step.Tier = tier
		case keyDue:
			if step.Due != nil {
				return step, errAt(lineNo, "step: due given twice")
			}
			due, err := parseDate(value, keyDue, lineNo)
			if err != nil {
				return step, err
			}
			step.Due = &due
		default:
			// Not a step field — it belongs to the text.
			return finishStep(step, text, lineNo)
		}
		text = strings.TrimSpace(head)
	}
	return finishStep(step, text, lineNo)
}

func finishStep(step task.Step, text string, lineNo int) (task.Step, error) {
	if text == "" {
		return step, errAt(lineNo, "step without text")
	}
	step.Text = text
	return step, nil
}

// cutLast is strings.Cut from the right — the step fields are read tail first.
func cutLast(s, sep string) (before, after string, found bool) {
	if i := strings.LastIndex(s, sep); i >= 0 {
		return s[:i], s[i+len(sep):], true
	}
	return s, "", false
}

// ParseStep turns one hand-written step — «call them · due:2026-09-01» — into
// a Step. It is the same reader the artifact uses, so `--step` on the command
// line and a line typed into the file mean exactly the same thing.
func ParseStep(text string) (task.Step, error) {
	return parseStep(strings.TrimSpace(text), 0)
}
