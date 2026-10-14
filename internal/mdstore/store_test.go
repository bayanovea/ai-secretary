package mdstore

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bayanovea/secretary/internal/config"
	"github.com/bayanovea/secretary/internal/task"
)

// open builds a minimal valid open task.
func open(id, section string, tier int) task.Task {
	return task.Task{ID: id, Title: strings.TrimSpace("Duty " + id), Section: section, Tier: tier, From: task.SelfAssigned}
}

// docOf assembles a document from sections given as name → tasks, in order.
func docOf(sections ...Section) *Document {
	return &Document{Preamble: []string{DefaultPreamble}, Sections: sections}
}

func ids(tasks []task.Task) []string {
	out := make([]string, len(tasks))
	for i, t := range tasks {
		out[i] = t.ID
	}
	return out
}

func TestAddAssignsNextFreeID(t *testing.T) {
	cases := []struct {
		name string
		doc  *Document
		want string
	}{
		{
			name: "empty document",
			doc:  docOf(),
			want: "T-001",
		},
		{
			name: "section without tasks",
			doc:  docOf(Section{Name: "Work"}),
			want: "T-001",
		},
		{
			name: "continues the numbering",
			doc: docOf(Section{Name: "Work", Tasks: []task.Task{
				open("T-001", "Work", 1), open("T-002", "Work", 2),
			}}),
			want: "T-003",
		},
		{
			// Gaps are not reused: T-002 may have gone to the archive, and a new
			// task with its number would make the old reference lie.
			name: "gap is not filled",
			doc: docOf(Section{Name: "Work", Tasks: []task.Task{
				open("T-001", "Work", 1), open("T-003", "Work", 2),
			}}),
			want: "T-004",
		},
		{
			name: "counts completed ones",
			doc: docOf(
				Section{Name: "Work", Tasks: []task.Task{open("T-002", "Work", 1)}},
				Section{Name: DoneSection, Tasks: []task.Task{{ID: "T-009", Title: "Old one", Section: DoneSection, Done: true}}},
			),
			want: "T-010",
		},
		{
			name: "number wider than three digits",
			doc: docOf(Section{Name: "Work", Tasks: []task.Task{
				open("T-999", "Work", 1),
			}}),
			want: "T-1000",
		},
		{
			name: "foreign identifiers are ignored",
			doc: docOf(Section{Name: "Work", Tasks: []task.Task{
				{ID: "note", Title: "Not a task", Section: "Work"},
			}}),
			want: "T-001",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.doc.Add(open("", "Work", 2), nil)
			if err != nil {
				t.Fatalf("Add() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("Add() = %q, want %q", got, tc.want)
			}
			if _, ok := tc.doc.Find(got); !ok {
				t.Errorf("task %s not found in the document after Add", got)
			}
		})
	}
}

func TestAddPlacesTaskInSection(t *testing.T) {
	doc := docOf(
		Section{Name: "Work", Tasks: []task.Task{open("T-001", "Work", 1)}},
		Section{Name: DoneSection},
	)

	// A section known to the config but absent from the file is created, and
	// before «Done».
	if _, err := doc.Add(open("", "Health", 3), nil); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	var names []string
	for _, s := range doc.Sections {
		names = append(names, s.Name)
	}
	want := []string{"Work", "Health", DoneSection}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("sections = %v, want %v", names, want)
	}
	if got := ids(doc.Sections[1].Tasks); !reflect.DeepEqual(got, []string{"T-002"}) {
		t.Errorf("tasks in Health = %v", got)
	}

	// An existing section grows at the end.
	if _, err := doc.Add(open("", "Work", 2), nil); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if got := ids(doc.Sections[0].Tasks); !reflect.DeepEqual(got, []string{"T-001", "T-003"}) {
		t.Errorf("tasks in Work = %v", got)
	}
}

func TestAddNormalisesDates(t *testing.T) {
	doc := docOf()
	in := open("", "Personal", 2)
	due := time.Date(2026, 9, 1, 23, 30, 0, 0, time.FixedZone("MSK", 3*3600))
	in.Due, in.Created = &due, due.AddDate(0, 0, -1)

	id, err := doc.Add(in, nil)
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	got, _ := doc.Find(id)
	if want := date(t, "2026-09-01"); !got.Due.Equal(want) {
		t.Errorf("due = %v, want %v", got.Due, want)
	}
	if want := date(t, "2026-08-31"); !got.Created.Equal(want) {
		t.Errorf("created = %v, want %v", got.Created, want)
	}
}

func TestAddErrors(t *testing.T) {
	cases := []struct {
		name string
		doc  *Document
		in   task.Task
		want error
	}{
		{
			name: "section not in config",
			doc:  docOf(),
			in:   open("", "Garage", 2),
			want: ErrUnknownSection,
		},
		{
			name: "section not given",
			doc:  docOf(),
			in:   open("", "", 2),
			want: ErrUnknownSection,
		},
		{
			name: "identifier taken",
			doc:  docOf(Section{Name: "Work", Tasks: []task.Task{open("T-001", "Work", 1)}}),
			in:   open("T-001", "Work", 2),
			want: ErrDuplicateID,
		},
		{
			name: "invalid tier",
			doc:  docOf(),
			in:   open("", "Work", 7),
			want: task.ErrInvalidTier,
		},
		{
			name: "from not given",
			doc:  docOf(),
			in:   task.Task{Title: "No author", Section: "Work", Tier: 2},
			want: task.ErrEmptyFrom,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := len(tc.doc.Tasks())
			id, err := tc.doc.Add(tc.in, nil)
			if err == nil {
				t.Fatalf("Add() = %q, want an error", id)
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("Add() error = %v, want %v", err, tc.want)
			}
			if after := len(tc.doc.Tasks()); after != before {
				t.Errorf("document changed on error: had %d tasks, now %d", before, after)
			}
		})
	}
}

func TestDone(t *testing.T) {
	doc := docOf(Section{Name: "Work", Tasks: []task.Task{
		open("T-001", "Work", 1), open("T-002", "Work", 2),
	}})
	now := time.Date(2026, 8, 27, 18, 45, 0, 0, time.UTC)

	if err := doc.Done("T-001", now); err != nil {
		t.Fatalf("Done() error = %v", err)
	}

	if got := ids(doc.Sections[0].Tasks); !reflect.DeepEqual(got, []string{"T-002"}) {
		t.Errorf("after Done, Work = %v, want [T-002]", got)
	}
	done := doc.sectionIndex(DoneSection)
	if done < 0 {
		t.Fatalf("section %q not created", DoneSection)
	}
	if done != len(doc.Sections)-1 {
		t.Errorf("section %q is not last: %d of %d", DoneSection, done, len(doc.Sections))
	}
	got, ok := doc.Find("T-001")
	if !ok {
		t.Fatalf("task T-001 lost")
	}
	if !got.Done {
		t.Error("task not marked done")
	}
	if got.Section != DoneSection {
		t.Errorf("task section = %q, want %q", got.Section, DoneSection)
	}
	if want := date(t, "2026-08-27"); got.CompletedAt == nil || !got.CompletedAt.Equal(want) {
		t.Errorf("done = %v, want %v", got.CompletedAt, want)
	}
}

func TestDoneErrors(t *testing.T) {
	now := date(t, "2026-08-27")
	doc := docOf(Section{Name: "Work", Tasks: []task.Task{open("T-001", "Work", 1)}})

	if err := doc.Done("T-404", now); !errors.Is(err, ErrUnknownID) {
		t.Errorf("Done(unknown) error = %v, want %v", err, ErrUnknownID)
	}
	if err := doc.Done("T-001", now); err != nil {
		t.Fatalf("Done() error = %v", err)
	}
	if err := doc.Done("T-001", now); !errors.Is(err, ErrAlreadyDone) {
		t.Errorf("second Done error = %v, want %v", err, ErrAlreadyDone)
	}
}

func TestDefer(t *testing.T) {
	due := date(t, "2026-09-01")

	t.Run("moves the deadline and counts the deferral", func(t *testing.T) {
		task1 := open("T-001", "Work", 1)
		task1.Due, task1.Deferrals = &due, 1
		doc := docOf(Section{Name: "Work", Tasks: []task.Task{task1}})

		if err := doc.Defer("T-001", date(t, "2026-09-10")); err != nil {
			t.Fatalf("Defer() error = %v", err)
		}
		got, _ := doc.Find("T-001")
		if want := date(t, "2026-09-10"); !got.Due.Equal(want) {
			t.Errorf("due = %v, want %v", got.Due, want)
		}
		if got.Deferrals != 2 {
			t.Errorf("deferrals = %d, want 2", got.Deferrals)
		}
	})

	t.Run("a first deadline is not a deferral", func(t *testing.T) {
		doc := docOf(Section{Name: "Work", Tasks: []task.Task{open("T-001", "Work", 2)}})

		if err := doc.Defer("T-001", date(t, "2026-09-10")); err != nil {
			t.Fatalf("Defer() error = %v", err)
		}
		got, _ := doc.Find("T-001")
		if want := date(t, "2026-09-10"); got.Due == nil || !got.Due.Equal(want) {
			t.Errorf("due = %v, want %v", got.Due, want)
		}
		if got.Deferrals != 0 {
			t.Errorf("deferrals = %d, want 0", got.Deferrals)
		}
	})

	t.Run("time of day is dropped", func(t *testing.T) {
		doc := docOf(Section{Name: "Work", Tasks: []task.Task{open("T-001", "Work", 2)}})
		noon := time.Date(2026, 9, 10, 12, 0, 0, 0, time.FixedZone("MSK", 3*3600))

		if err := doc.Defer("T-001", noon); err != nil {
			t.Fatalf("Defer() error = %v", err)
		}
		got, _ := doc.Find("T-001")
		if want := date(t, "2026-09-10"); !got.Due.Equal(want) {
			t.Errorf("due = %v, want %v", got.Due, want)
		}
	})
}

func TestDeferErrors(t *testing.T) {
	due := date(t, "2026-09-01")
	withDue := func() *Document {
		t1 := open("T-001", "Work", 1)
		t1.Due = &due
		return docOf(Section{Name: "Work", Tasks: []task.Task{t1}})
	}

	cases := []struct {
		name   string
		doc    *Document
		id     string
		newDue time.Time
		want   error
	}{
		{name: "no such task", doc: withDue(), id: "T-404", newDue: date(t, "2026-09-10"), want: ErrUnknownID},
		{name: "deadline earlier than before", doc: withDue(), id: "T-001", newDue: date(t, "2026-08-25"), want: ErrNotLater},
		{name: "same deadline", doc: withDue(), id: "T-001", newDue: date(t, "2026-09-01"), want: ErrNotLater},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.doc.Defer(tc.id, tc.newDue)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Defer() error = %v, want %v", err, tc.want)
			}
			if got, ok := tc.doc.Find("T-001"); ok {
				if !got.Due.Equal(due) {
					t.Errorf("deadline changed on error: %v", got.Due)
				}
				if got.Deferrals != 0 {
					t.Errorf("deferrals incremented on error: %d", got.Deferrals)
				}
			}
		})
	}

	t.Run("a completed task is not deferred", func(t *testing.T) {
		doc := docOf(Section{Name: DoneSection, Tasks: []task.Task{
			{ID: "T-001", Title: "Finished", Section: DoneSection, Done: true},
		}})
		if err := doc.Defer("T-001", date(t, "2026-09-10")); !errors.Is(err, ErrAlreadyDone) {
			t.Errorf("Defer(completed) error = %v, want %v", err, ErrAlreadyDone)
		}
	})
}

// sortFixture builds one section whose tasks exercise every sort key: tier,
// severity, due and identifier.
func sortFixture(t *testing.T) []task.Task {
	t.Helper()

	overdue := open("T-010", "Work", 1)
	overdue.Due = ptr(date(t, "2026-08-20"))

	calm := open("T-011", "Work", 1)
	calm.Due, calm.Effort = ptr(date(t, "2026-09-30")), task.Effort(1)

	soon := open("T-012", "Work", 2)
	soon.Due = ptr(date(t, "2026-08-29"))

	noDueLate := open("T-013", "Work", 2)
	noDueEarly := open("T-009", "Work", 2)

	startNow := open("T-014", "Work", 3)
	startNow.Due = ptr(date(t, "2026-08-27"))

	completed := task.Task{ID: "T-000", Title: "Finished", Section: "Work", Done: true}

	return []task.Task{noDueLate, completed, startNow, soon, overdue, noDueEarly, calm}
}

func TestSortOrder(t *testing.T) {
	now := date(t, "2026-08-27")
	doc := docOf(Section{Name: "Work", Tasks: sortFixture(t)})

	doc.Sort(now, config.Default())

	// tier 1: overdue, then on track. tier 2: soon, then two without a deadline by
	// identifier. tier 3: start now. No tier — completed — last.
	want := []string{"T-010", "T-011", "T-012", "T-009", "T-013", "T-014", "T-000"}
	if got := ids(doc.Sections[0].Tasks); !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestSortDeterministic(t *testing.T) {
	now := date(t, "2026-08-27")
	cfg := config.Default()

	sorted := func(tasks []task.Task) []string {
		doc := docOf(Section{Name: "Work", Tasks: tasks})
		doc.Sort(now, cfg)
		return ids(doc.Sections[0].Tasks)
	}

	base := sortFixture(t)
	want := sorted(append([]task.Task(nil), base...))

	// Every permutation of the input order must give the same result:
	// the last key is the unique identifier, so the order is total.
	permute(append([]task.Task(nil), base...), 0, func(p []task.Task) {
		if got := sorted(append([]task.Task(nil), p...)); !reflect.DeepEqual(got, want) {
			t.Fatalf("permutation %v gave %v, want %v", ids(p), got, want)
		}
	})
}

// permute calls fn for every permutation of tasks.
func permute(tasks []task.Task, i int, fn func([]task.Task)) {
	if i == len(tasks) {
		fn(tasks)
		return
	}
	for j := i; j < len(tasks); j++ {
		tasks[i], tasks[j] = tasks[j], tasks[i]
		permute(tasks, i+1, fn)
		tasks[i], tasks[j] = tasks[j], tasks[i]
	}
}

func TestSortLeavesSectionOrder(t *testing.T) {
	doc := docOf(
		Section{Name: "Personal", Tasks: []task.Task{open("T-002", "Personal", 3)}},
		Section{Name: "Work", Tasks: []task.Task{open("T-001", "Work", 1)}},
	)

	doc.Sort(date(t, "2026-08-27"), nil)

	if doc.Sections[0].Name != "Personal" || doc.Sections[1].Name != "Work" {
		t.Errorf("section order changed: %q, %q", doc.Sections[0].Name, doc.Sections[1].Name)
	}
}

// fullDoc is a document using every field the artifact can carry, for the
// save → load equality check.
func fullDoc(t *testing.T) *Document {
	t.Helper()

	report := task.Task{
		ID: "T-001", Title: "Prepare the report", Section: "Work", Tier: 1,
		From: "Dana Whitfield", Due: ptr(date(t, "2026-09-05")), Effort: task.Effort(5),
		Context: "@computer", Labels: []string{"reporting", "finance"},
		Notes:     []string{"needs the numbers from accounting", "they are late"},
		Deferrals: 2, Created: date(t, "2026-08-10"),
	}
	gift := task.Task{
		ID: "T-002", Title: "Buy a present", Section: "Personal", Tier: 3,
		From: task.SelfAssigned, Created: date(t, "2026-08-25"),
	}
	insurance := task.Task{
		ID: "T-000", Title: "Pay the insurance", Section: DoneSection,
		Done: true, CompletedAt: ptr(date(t, "2026-08-20")),
	}

	return docOf(
		Section{Name: "Work", Tasks: []task.Task{report}},
		Section{Name: "Personal", Tasks: []task.Task{gift}},
		Section{Name: DoneSection, Tasks: []task.Task{insurance}},
	)
}

func TestSaveThenLoad(t *testing.T) {
	now := date(t, "2026-08-27")
	path := filepath.Join(t.TempDir(), "docs", "duties.md")
	store := NewStore(path, config.Default())
	want := fullDoc(t)

	if err := store.Save(want, now); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Load(Save(doc)) != doc\ngot:  %+v\nwant: %+v", got, want)
	}
}

func TestSaveAfterMutations(t *testing.T) {
	now := date(t, "2026-08-27")
	path := filepath.Join(t.TempDir(), "duties.md")
	store := NewStore(path, config.Default())

	doc, err := store.Load()
	if err != nil {
		t.Fatalf("Load() of an empty artifact error = %v", err)
	}
	in := open("", "Personal", 2)
	in.Due, in.Created = ptr(date(t, "2026-09-01")), now
	id, err := doc.Add(in, store.Cfg)
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := doc.Defer(id, date(t, "2026-09-08")); err != nil {
		t.Fatalf("Defer() error = %v", err)
	}
	doc.Sort(now, store.Cfg)
	if err := store.Save(doc, now); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	reloaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(reloaded, doc) {
		t.Errorf("Load(Save(doc)) != doc\ngot:  %+v\nwant: %+v", reloaded, doc)
	}
	got, ok := reloaded.Find(id)
	if !ok {
		t.Fatalf("task %s was not saved", id)
	}
	if want := date(t, "2026-09-08"); !got.Due.Equal(want) {
		t.Errorf("due = %v, want %v", got.Due, want)
	}
	if got.Deferrals != 1 {
		t.Errorf("deferrals = %d, want 1", got.Deferrals)
	}
}

func TestSaveReplacesFileAndLeavesNoLeftovers(t *testing.T) {
	now := date(t, "2026-08-27")
	dir := t.TempDir()
	path := filepath.Join(dir, "duties.md")
	store := NewStore(path, nil)

	if err := os.WriteFile(path, []byte("# Old contents\n"), 0o644); err != nil {
		t.Fatalf("preparing the file: %v", err)
	}
	if err := store.Save(fullDoc(t), now); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if strings.Contains(string(data), "Old contents") {
		t.Error("old contents not replaced")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "duties.md" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("stray files left in the directory: %v", names)
	}
}

func TestLoadMissingFile(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "missing.md"), nil)

	doc, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := &Document{Preamble: []string{DefaultPreamble}}
	if !reflect.DeepEqual(doc, want) {
		t.Errorf("Load() = %+v, want %+v", doc, want)
	}
}

func TestLoadBrokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duties.md")
	broken := "# Duties\n\n## Work\n\n- [ ] `T-001` **Task**\n  tier:9 · from:self\n"
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatalf("preparing the file: %v", err)
	}

	if _, err := NewStore(path, nil).Load(); err == nil {
		t.Fatal("Load() = nil, want a parse error")
	}
}
