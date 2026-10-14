package mdstore

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bayanovea/secretary/internal/config"
	"github.com/bayanovea/secretary/internal/schedule"
	"github.com/bayanovea/secretary/internal/task"
)

// DefaultPreamble opens an artifact this package creates from nothing.
const DefaultPreamble = "# Duties"

// idFormat is the shape of an assigned identifier. Wider numbers keep their own
// width, so T-1000 follows T-999 without renumbering anything.
const idFormat = "T-%03d"

// idRe reads the number out of an identifier.
var idRe = regexp.MustCompile(`^T-(\d+)$`)

// Mutation failures, one per rule, so the CLI can tell them apart and say
// something useful.
var (
	ErrUnknownSection = errors.New("unknown section")
	ErrUnknownID      = errors.New("no such task")
	ErrDuplicateID    = errors.New("identifier already taken")
	ErrNotLater       = errors.New("new deadline is not later than the old one")
	ErrAlreadyDone    = errors.New("task already done")
	ErrUnknownStep    = errors.New("no such step")
)

// Store is the artifact on disk: a path, and the configuration that says how to
// render it.
//
// It owns no state between calls — Load parses the file afresh and Save writes
// the whole of it back — so two processes cannot hold divergent views of the
// artifact, and a hand edit made between two commands is picked up rather than
// overwritten from a stale copy.
type Store struct {
	Path string
	Cfg  *config.Config
}

// NewStore returns a store over path. A nil cfg means the defaults.
func NewStore(path string, cfg *config.Config) *Store {
	if cfg == nil {
		cfg = config.Default()
	}
	return &Store{Path: path, Cfg: cfg}
}

// Load reads and parses the artifact.
//
// A file that is not there yet is not an error: the first `add` on a fresh
// checkout gets an empty document with the standard heading, and writes a
// complete artifact. Anything else that goes wrong reading the file — and any
// parse failure — is reported, because guessing at a damaged artifact is worse
// than refusing to touch it.
func (s *Store) Load() (*Document, error) {
	f, err := os.Open(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Document{Preamble: []string{DefaultPreamble}}, nil
		}
		return nil, fmt.Errorf("mdstore: open %s: %w", s.Path, err)
	}
	defer f.Close()

	doc, err := Parse(f)
	if err != nil {
		return nil, fmt.Errorf("mdstore: %s: %w", s.Path, err)
	}
	return doc, nil
}

// Save renders doc and replaces the artifact with it.
//
// The write goes to a temporary file in the same directory and is then renamed
// over the target, so a crash mid-write leaves the previous artifact intact.
// The user's duties are the one thing here that cannot be recomputed.
func (s *Store) Save(doc *Document, now time.Time) error {
	var buf bytes.Buffer
	if err := Render(&buf, doc, now, s.Cfg); err != nil {
		return err
	}

	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mdstore: create %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(s.Path)+".tmp*")
	if err != nil {
		return fmt.Errorf("mdstore: temporary file in %s: %w", dir, err)
	}
	name := tmp.Name()
	defer os.Remove(name) // no-op once the rename has succeeded

	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return fmt.Errorf("mdstore: write %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("mdstore: close %s: %w", name, err)
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return fmt.Errorf("mdstore: permissions %s: %w", name, err)
	}
	if err := os.Rename(name, s.Path); err != nil {
		return fmt.Errorf("mdstore: replace %s: %w", s.Path, err)
	}
	return nil
}

// Add files t under its section and returns the identifier it was given.
//
// ⚠️ The plan wrote this as `Document.Add(t task.Task) (string, error)`. The
// config argument is needed to answer "is this a real section?", and Sort takes
// one for the same kind of reason, so it is passed rather than stored on the
// Document — a Document comes out of Parse, which has no configuration to give
// it.
//
// A section that is configured but absent from the file is created; a section
// that is not in the configuration is refused, because the alternative is a
// typo quietly inventing a seventh area of your life.
func (d *Document) Add(t task.Task, cfg *config.Config) (string, error) {
	if cfg == nil {
		cfg = config.Default()
	}

	trim(&t)
	if !cfg.HasSection(t.Section) {
		return "", fmt.Errorf("%w %q: allowed: %s", ErrUnknownSection, t.Section, strings.Join(cfg.Sections, ", "))
	}

	if t.ID == "" {
		t.ID = d.nextID()
	} else if _, _, found := d.find(t.ID); found {
		return "", fmt.Errorf("%w: %s", ErrDuplicateID, t.ID)
	}

	t.Due = dayPtr(t.Due)
	if !t.Created.IsZero() {
		t.Created = day(t.Created)
	}
	if err := t.Validate(); err != nil {
		return "", err
	}

	idx := d.sectionIndex(t.Section)
	if idx < 0 {
		idx = d.addSection(t.Section)
	}
	d.Sections[idx].Tasks = append(d.Sections[idx].Tasks, t)
	return t.ID, nil
}

// trim normalises the free-text fields the way Parse does, so that a value
// typed with a stray space on the command line still comes back from the
// artifact byte-for-byte as it went in.
func trim(t *task.Task) {
	t.Section = strings.TrimSpace(t.Section)
	t.Title = strings.TrimSpace(t.Title)
	t.From = strings.TrimSpace(t.From)
	t.Context = strings.TrimSpace(t.Context)

	labels := t.Labels[:0]
	for _, label := range t.Labels {
		if label = strings.TrimSpace(label); label != "" {
			labels = append(labels, label)
		}
	}
	t.Labels = labels

	steps := t.Steps[:0]
	for _, step := range t.Steps {
		if step.Text = strings.TrimSpace(step.Text); step.Text != "" {
			steps = append(steps, step)
		}
	}
	t.Steps = steps

	notes := t.Notes[:0]
	for _, note := range t.Notes {
		if note = strings.TrimSpace(note); note != "" {
			notes = append(notes, note)
		}
	}
	t.Notes = notes
}

// AddStep appends checkbox sub-items to a duty.
//
// Steps are only added to an open duty: a completed task is a record of what
// happened, and appending to it would rewrite history.
func (d *Document) AddStep(id string, texts ...string) error {
	t, err := d.openTask(id)
	if err != nil {
		return err
	}
	for _, text := range texts {
		if strings.TrimSpace(text) == "" {
			continue
		}
		step, err := ParseStep(text)
		if err != nil {
			return err
		}
		t.Steps = append(t.Steps, step)
	}
	return nil
}

// StepFields is the part of a step that carries its own urgency. A zero value
// leaves the step alone; the Clear flags exist because "no tier" and "tier 0"
// have to be distinguishable on a command line.
type StepFields struct {
	Tier      int
	ClearTier bool
	Due       *time.Time
	ClearDue  bool
}

// SetStepFields changes the tier and due date of the n-th step, counting from one.
func (d *Document) SetStepFields(id string, n int, f StepFields) error {
	t, err := d.openTask(id)
	if err != nil {
		return err
	}
	if n < 1 || n > len(t.Steps) {
		return fmt.Errorf("%w: %s: step %d", ErrUnknownStep, id, n)
	}
	step := &t.Steps[n-1]
	switch {
	case f.ClearTier:
		step.Tier = 0
	case f.Tier != 0:
		step.Tier = f.Tier
	}
	switch {
	case f.ClearDue:
		step.Due = nil
	case f.Due != nil:
		due := day(*f.Due)
		step.Due = &due
	}
	return nil
}

// SetStep checks or unchecks the n-th step, counting from one — the number the
// CLI prints, so that what the user reads is what they type back.
func (d *Document) SetStep(id string, n int, done bool) error {
	t, err := d.openTask(id)
	if err != nil {
		return err
	}
	if n < 1 || n > len(t.Steps) {
		return fmt.Errorf("%w: %s %d: only %d steps", ErrUnknownStep, id, n, len(t.Steps))
	}
	t.Steps[n-1].Done = done
	return nil
}

// DropStep removes the n-th step and returns it, so the caller can name what it
// deleted.
func (d *Document) DropStep(id string, n int) (task.Step, error) {
	t, err := d.openTask(id)
	if err != nil {
		return task.Step{}, err
	}
	if n < 1 || n > len(t.Steps) {
		return task.Step{}, fmt.Errorf("%w: %s %d: only %d steps", ErrUnknownStep, id, n, len(t.Steps))
	}
	step := t.Steps[n-1]
	t.Steps = append(t.Steps[:n-1], t.Steps[n:]...)
	return step, nil
}

// openTask returns a pointer to a task that may still be modified.
func (d *Document) openTask(id string) (*task.Task, error) {
	si, ti, found := d.find(id)
	if !found {
		return nil, fmt.Errorf("%w: %s", ErrUnknownID, id)
	}
	t := &d.Sections[si].Tasks[ti]
	if t.Done {
		return nil, fmt.Errorf("%w: %s", ErrAlreadyDone, id)
	}
	return t, nil
}

// Done moves the task into `## Done` and dates it.
//
// The completion date is normalised to a calendar day: the artifact records
// dates, and «done» at half past eleven is still that day's work.
func (d *Document) Done(id string, now time.Time) error {
	si, ti, found := d.find(id)
	if !found {
		return fmt.Errorf("%w: %s", ErrUnknownID, id)
	}

	t := d.Sections[si].Tasks[ti]
	if t.Done {
		return fmt.Errorf("%w: %s", ErrAlreadyDone, id)
	}

	d.Sections[si].Tasks = append(d.Sections[si].Tasks[:ti], d.Sections[si].Tasks[ti+1:]...)

	completed := day(now)
	t.Done, t.CompletedAt, t.Section = true, &completed, DoneSection

	done := d.sectionIndex(DoneSection)
	if done < 0 {
		done = d.addSection(DoneSection)
	}
	d.Sections[done].Tasks = append(d.Sections[done].Tasks, t)
	return nil
}

// Defer pushes a deadline back and counts it.
//
// Moving a deadline earlier is not a deferral and is refused: it is far more
// likely to be a typo than a decision, and the counter Alfred reads to say
// «deferred three times already» only means something if it counts one direction.
//
// A task that had no deadline at all is a different act — a first date is being
// set, not a promise broken — so the date is taken and the counter left alone.
func (d *Document) Defer(id string, newDue time.Time) error {
	si, ti, found := d.find(id)
	if !found {
		return fmt.Errorf("%w: %s", ErrUnknownID, id)
	}

	t := &d.Sections[si].Tasks[ti]
	if t.Done {
		return fmt.Errorf("%w: %s", ErrAlreadyDone, id)
	}

	due := day(newDue)
	if t.Due != nil {
		if !due.After(day(*t.Due)) {
			return fmt.Errorf("%w: %s %s ≤ %s", ErrNotLater, id,
				due.Format(DateLayout), t.Due.Format(DateLayout))
		}
		t.Deferrals++
	}
	t.Due = &due
	return nil
}

// Sort orders the tasks inside each section: tier asc → severity of status desc
// → due asc (absent last) → id asc.
//
// Sections keep the order they appear in, so a user who moved one stays moved.
// The final key is the identifier, which is unique, so the order is total: the
// same tasks in any input order come out identical.
//
// Tasks with no tier — completed ones, which the artifact keeps as a one-line
// record — sort after tier 3 rather than before tier 1.
func (d *Document) Sort(now time.Time, cfg *config.Config) {
	if cfg == nil {
		cfg = config.Default()
	}
	for i := range d.Sections {
		tasks := d.Sections[i].Tasks
		sort.SliceStable(tasks, func(a, b int) bool {
			return less(tasks[a], tasks[b], now, cfg)
		})
	}
}

func less(a, b task.Task, now time.Time, cfg *config.Config) bool {
	if ta, tb := tierKey(a), tierKey(b); ta != tb {
		return ta < tb
	}
	sa := schedule.Severity(schedule.Status(a, now, cfg))
	sb := schedule.Severity(schedule.Status(b, now, cfg))
	if sa != sb {
		return sa > sb
	}
	if a.Due != nil && b.Due != nil && !a.Due.Equal(*b.Due) {
		return a.Due.Before(*b.Due)
	}
	if (a.Due == nil) != (b.Due == nil) {
		return b.Due == nil
	}
	return a.ID < b.ID
}

// tierKey maps an unset tier past the last real one.
func tierKey(t task.Task) int {
	if t.Tier < task.MinTier || t.Tier > task.MaxTier {
		return task.MaxTier + 1
	}
	return t.Tier
}

// nextID returns one past the highest number in use.
//
// Gaps are not filled. An identifier is how the user refers to a duty in a
// command and how a note in some other file refers to it; handing T-002 to a
// new task because the old one was archived would make those references point
// at the wrong duty.
func (d *Document) nextID() string {
	max := 0
	for _, section := range d.Sections {
		for _, t := range section.Tasks {
			if m := idRe.FindStringSubmatch(t.ID); m != nil {
				if n, err := strconv.Atoi(m[1]); err == nil && n > max {
					max = n
				}
			}
		}
	}
	return fmt.Sprintf(idFormat, max+1)
}

// find locates a task by identifier.
func (d *Document) find(id string) (section, index int, found bool) {
	for si := range d.Sections {
		for ti := range d.Sections[si].Tasks {
			if d.Sections[si].Tasks[ti].ID == id {
				return si, ti, true
			}
		}
	}
	return 0, 0, false
}

// Find returns a copy of the task with the given identifier.
func (d *Document) Find(id string) (task.Task, bool) {
	si, ti, found := d.find(id)
	if !found {
		return task.Task{}, false
	}
	return d.Sections[si].Tasks[ti], true
}

// Tasks lists every task in the document in file order.
func (d *Document) Tasks() []task.Task {
	var all []task.Task
	for _, section := range d.Sections {
		all = append(all, section.Tasks...)
	}
	return all
}

func (d *Document) sectionIndex(name string) int {
	for i, section := range d.Sections {
		if section.Name == name {
			return i
		}
	}
	return -1
}

// addSection appends a section, keeping `## Done` last: history belongs at
// the bottom of the page, under the duties that are still open.
func (d *Document) addSection(name string) int {
	if done := d.sectionIndex(DoneSection); done >= 0 && name != DoneSection {
		d.Sections = append(d.Sections, Section{})
		copy(d.Sections[done+1:], d.Sections[done:])
		d.Sections[done] = Section{Name: name}
		return done
	}
	d.Sections = append(d.Sections, Section{Name: name})
	return len(d.Sections) - 1
}

// day drops the clock time and the zone, so that a date written by the CLI
// compares and renders exactly like one read back from the artifact.
func day(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func dayPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	d := day(*t)
	return &d
}
