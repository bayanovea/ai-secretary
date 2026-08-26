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
	return task.Task{ID: id, Title: strings.TrimSpace("Дело " + id), Section: section, Tier: tier, From: task.SelfAssigned}
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
			name: "пустой документ",
			doc:  docOf(),
			want: "T-001",
		},
		{
			name: "раздел без задач",
			doc:  docOf(Section{Name: "Работа"}),
			want: "T-001",
		},
		{
			name: "продолжает нумерацию",
			doc: docOf(Section{Name: "Работа", Tasks: []task.Task{
				open("T-001", "Работа", 1), open("T-002", "Работа", 2),
			}}),
			want: "T-003",
		},
		{
			// Дырки не переиспользуются: T-002 могла уехать в архив, и новая
			// задача с её номером сделала бы старую ссылку ложной.
			name: "пропуск не заполняется",
			doc: docOf(Section{Name: "Работа", Tasks: []task.Task{
				open("T-001", "Работа", 1), open("T-003", "Работа", 2),
			}}),
			want: "T-004",
		},
		{
			name: "учитывает выполненные",
			doc: docOf(
				Section{Name: "Работа", Tasks: []task.Task{open("T-002", "Работа", 1)}},
				Section{Name: DoneSection, Tasks: []task.Task{{ID: "T-009", Title: "Старое", Section: DoneSection, Done: true}}},
			),
			want: "T-010",
		},
		{
			name: "номер шире трёх цифр",
			doc: docOf(Section{Name: "Работа", Tasks: []task.Task{
				open("T-999", "Работа", 1),
			}}),
			want: "T-1000",
		},
		{
			name: "чужие идентификаторы игнорируются",
			doc: docOf(Section{Name: "Работа", Tasks: []task.Task{
				{ID: "заметка", Title: "Не задача", Section: "Работа"},
			}}),
			want: "T-001",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.doc.Add(open("", "Работа", 2), nil)
			if err != nil {
				t.Fatalf("Add() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("Add() = %q, ожидалось %q", got, tc.want)
			}
			if _, ok := tc.doc.Find(got); !ok {
				t.Errorf("задача %s не найдена в документе после Add", got)
			}
		})
	}
}

func TestAddPlacesTaskInSection(t *testing.T) {
	doc := docOf(
		Section{Name: "Работа", Tasks: []task.Task{open("T-001", "Работа", 1)}},
		Section{Name: DoneSection},
	)

	// Новый раздел есть в конфиге, но отсутствует в файле — создаётся, причём
	// перед «Сделано».
	if _, err := doc.Add(open("", "Здоровье", 3), nil); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	var names []string
	for _, s := range doc.Sections {
		names = append(names, s.Name)
	}
	want := []string{"Работа", "Здоровье", DoneSection}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("разделы = %v, ожидалось %v", names, want)
	}
	if got := ids(doc.Sections[1].Tasks); !reflect.DeepEqual(got, []string{"T-002"}) {
		t.Errorf("задачи раздела Здоровье = %v", got)
	}

	// Существующий раздел пополняется в конец.
	if _, err := doc.Add(open("", "Работа", 2), nil); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if got := ids(doc.Sections[0].Tasks); !reflect.DeepEqual(got, []string{"T-001", "T-003"}) {
		t.Errorf("задачи раздела Работа = %v", got)
	}
}

func TestAddNormalisesDates(t *testing.T) {
	doc := docOf()
	in := open("", "Личное", 2)
	due := time.Date(2026, 9, 1, 23, 30, 0, 0, time.FixedZone("MSK", 3*3600))
	in.Due, in.Created = &due, due.AddDate(0, 0, -1)

	id, err := doc.Add(in, nil)
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	got, _ := doc.Find(id)
	if want := date(t, "2026-09-01"); !got.Due.Equal(want) {
		t.Errorf("срок = %v, ожидалось %v", got.Due, want)
	}
	if want := date(t, "2026-08-31"); !got.Created.Equal(want) {
		t.Errorf("создано = %v, ожидалось %v", got.Created, want)
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
			name: "раздела нет в конфиге",
			doc:  docOf(),
			in:   open("", "Гараж", 2),
			want: ErrUnknownSection,
		},
		{
			name: "раздел не указан",
			doc:  docOf(),
			in:   open("", "", 2),
			want: ErrUnknownSection,
		},
		{
			name: "идентификатор занят",
			doc:  docOf(Section{Name: "Работа", Tasks: []task.Task{open("T-001", "Работа", 1)}}),
			in:   open("T-001", "Работа", 2),
			want: ErrDuplicateID,
		},
		{
			name: "некорректный тир",
			doc:  docOf(),
			in:   open("", "Работа", 7),
			want: task.ErrInvalidTier,
		},
		{
			name: "не указано, от кого",
			doc:  docOf(),
			in:   task.Task{Title: "Без автора", Section: "Работа", Tier: 2},
			want: task.ErrEmptyFrom,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := len(tc.doc.Tasks())
			id, err := tc.doc.Add(tc.in, nil)
			if err == nil {
				t.Fatalf("Add() = %q, ожидалась ошибка", id)
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("Add() error = %v, ожидалось %v", err, tc.want)
			}
			if after := len(tc.doc.Tasks()); after != before {
				t.Errorf("документ изменён при ошибке: было %d задач, стало %d", before, after)
			}
		})
	}
}

func TestDone(t *testing.T) {
	doc := docOf(Section{Name: "Работа", Tasks: []task.Task{
		open("T-001", "Работа", 1), open("T-002", "Работа", 2),
	}})
	now := time.Date(2026, 8, 27, 18, 45, 0, 0, time.UTC)

	if err := doc.Done("T-001", now); err != nil {
		t.Fatalf("Done() error = %v", err)
	}

	if got := ids(doc.Sections[0].Tasks); !reflect.DeepEqual(got, []string{"T-002"}) {
		t.Errorf("после Done в разделе Работа = %v, ожидалось [T-002]", got)
	}
	done := doc.sectionIndex(DoneSection)
	if done < 0 {
		t.Fatalf("раздел %q не создан", DoneSection)
	}
	if done != len(doc.Sections)-1 {
		t.Errorf("раздел %q не последний: %d из %d", DoneSection, done, len(doc.Sections))
	}
	got, ok := doc.Find("T-001")
	if !ok {
		t.Fatalf("задача T-001 потеряна")
	}
	if !got.Done {
		t.Error("задача не помечена выполненной")
	}
	if got.Section != DoneSection {
		t.Errorf("раздел задачи = %q, ожидалось %q", got.Section, DoneSection)
	}
	if want := date(t, "2026-08-27"); got.CompletedAt == nil || !got.CompletedAt.Equal(want) {
		t.Errorf("выполнено = %v, ожидалось %v", got.CompletedAt, want)
	}
}

func TestDoneErrors(t *testing.T) {
	now := date(t, "2026-08-27")
	doc := docOf(Section{Name: "Работа", Tasks: []task.Task{open("T-001", "Работа", 1)}})

	if err := doc.Done("T-404", now); !errors.Is(err, ErrUnknownID) {
		t.Errorf("Done(неизвестный) error = %v, ожидалось %v", err, ErrUnknownID)
	}
	if err := doc.Done("T-001", now); err != nil {
		t.Fatalf("Done() error = %v", err)
	}
	if err := doc.Done("T-001", now); !errors.Is(err, ErrAlreadyDone) {
		t.Errorf("повторный Done error = %v, ожидалось %v", err, ErrAlreadyDone)
	}
}

func TestDefer(t *testing.T) {
	due := date(t, "2026-09-01")

	t.Run("сдвигает срок и считает перенос", func(t *testing.T) {
		task1 := open("T-001", "Работа", 1)
		task1.Due, task1.Deferrals = &due, 1
		doc := docOf(Section{Name: "Работа", Tasks: []task.Task{task1}})

		if err := doc.Defer("T-001", date(t, "2026-09-10")); err != nil {
			t.Fatalf("Defer() error = %v", err)
		}
		got, _ := doc.Find("T-001")
		if want := date(t, "2026-09-10"); !got.Due.Equal(want) {
			t.Errorf("срок = %v, ожидалось %v", got.Due, want)
		}
		if got.Deferrals != 2 {
			t.Errorf("переносов = %d, ожидалось 2", got.Deferrals)
		}
	})

	t.Run("первый срок не считается переносом", func(t *testing.T) {
		doc := docOf(Section{Name: "Работа", Tasks: []task.Task{open("T-001", "Работа", 2)}})

		if err := doc.Defer("T-001", date(t, "2026-09-10")); err != nil {
			t.Fatalf("Defer() error = %v", err)
		}
		got, _ := doc.Find("T-001")
		if want := date(t, "2026-09-10"); got.Due == nil || !got.Due.Equal(want) {
			t.Errorf("срок = %v, ожидалось %v", got.Due, want)
		}
		if got.Deferrals != 0 {
			t.Errorf("переносов = %d, ожидалось 0", got.Deferrals)
		}
	})

	t.Run("время суток отбрасывается", func(t *testing.T) {
		doc := docOf(Section{Name: "Работа", Tasks: []task.Task{open("T-001", "Работа", 2)}})
		noon := time.Date(2026, 9, 10, 12, 0, 0, 0, time.FixedZone("MSK", 3*3600))

		if err := doc.Defer("T-001", noon); err != nil {
			t.Fatalf("Defer() error = %v", err)
		}
		got, _ := doc.Find("T-001")
		if want := date(t, "2026-09-10"); !got.Due.Equal(want) {
			t.Errorf("срок = %v, ожидалось %v", got.Due, want)
		}
	})
}

func TestDeferErrors(t *testing.T) {
	due := date(t, "2026-09-01")
	withDue := func() *Document {
		t1 := open("T-001", "Работа", 1)
		t1.Due = &due
		return docOf(Section{Name: "Работа", Tasks: []task.Task{t1}})
	}

	cases := []struct {
		name   string
		doc    *Document
		id     string
		newDue time.Time
		want   error
	}{
		{name: "нет такой задачи", doc: withDue(), id: "T-404", newDue: date(t, "2026-09-10"), want: ErrUnknownID},
		{name: "срок раньше прежнего", doc: withDue(), id: "T-001", newDue: date(t, "2026-08-25"), want: ErrNotLater},
		{name: "тот же срок", doc: withDue(), id: "T-001", newDue: date(t, "2026-09-01"), want: ErrNotLater},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.doc.Defer(tc.id, tc.newDue)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Defer() error = %v, ожидалось %v", err, tc.want)
			}
			if got, ok := tc.doc.Find("T-001"); ok {
				if !got.Due.Equal(due) {
					t.Errorf("срок изменён при ошибке: %v", got.Due)
				}
				if got.Deferrals != 0 {
					t.Errorf("переносов увеличено при ошибке: %d", got.Deferrals)
				}
			}
		})
	}

	t.Run("выполненную не переносят", func(t *testing.T) {
		doc := docOf(Section{Name: DoneSection, Tasks: []task.Task{
			{ID: "T-001", Title: "Готово", Section: DoneSection, Done: true},
		}})
		if err := doc.Defer("T-001", date(t, "2026-09-10")); !errors.Is(err, ErrAlreadyDone) {
			t.Errorf("Defer(выполненная) error = %v, ожидалось %v", err, ErrAlreadyDone)
		}
	})
}

// sortFixture builds one section whose tasks exercise every sort key: тир,
// severity, срок и идентификатор.
func sortFixture(t *testing.T) []task.Task {
	t.Helper()

	overdue := open("T-010", "Работа", 1)
	overdue.Due = ptr(date(t, "2026-08-20"))

	calm := open("T-011", "Работа", 1)
	calm.Due, calm.Effort = ptr(date(t, "2026-09-30")), task.Effort(1)

	soon := open("T-012", "Работа", 2)
	soon.Due = ptr(date(t, "2026-08-29"))

	noDueLate := open("T-013", "Работа", 2)
	noDueEarly := open("T-009", "Работа", 2)

	startNow := open("T-014", "Работа", 3)
	startNow.Due = ptr(date(t, "2026-08-27"))

	completed := task.Task{ID: "T-000", Title: "Готово", Section: "Работа", Done: true}

	return []task.Task{noDueLate, completed, startNow, soon, overdue, noDueEarly, calm}
}

func TestSortOrder(t *testing.T) {
	now := date(t, "2026-08-27")
	doc := docOf(Section{Name: "Работа", Tasks: sortFixture(t)})

	doc.Sort(now, config.Default())

	// тир 1: просрочено, затем спокойно. тир 2: скоро, затем два без срока по
	// идентификатору. тир 3: пора начинать. Без тира — выполненная — в конце.
	want := []string{"T-010", "T-011", "T-012", "T-009", "T-013", "T-014", "T-000"}
	if got := ids(doc.Sections[0].Tasks); !reflect.DeepEqual(got, want) {
		t.Errorf("порядок = %v, ожидалось %v", got, want)
	}
}

func TestSortDeterministic(t *testing.T) {
	now := date(t, "2026-08-27")
	cfg := config.Default()

	sorted := func(tasks []task.Task) []string {
		doc := docOf(Section{Name: "Работа", Tasks: tasks})
		doc.Sort(now, cfg)
		return ids(doc.Sections[0].Tasks)
	}

	base := sortFixture(t)
	want := sorted(append([]task.Task(nil), base...))

	// Все перестановки исходного порядка обязаны дать один и тот же результат:
	// последний ключ — уникальный идентификатор, так что порядок полный.
	permute(append([]task.Task(nil), base...), 0, func(p []task.Task) {
		if got := sorted(append([]task.Task(nil), p...)); !reflect.DeepEqual(got, want) {
			t.Fatalf("перестановка %v дала %v, ожидалось %v", ids(p), got, want)
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
		Section{Name: "Личное", Tasks: []task.Task{open("T-002", "Личное", 3)}},
		Section{Name: "Работа", Tasks: []task.Task{open("T-001", "Работа", 1)}},
	)

	doc.Sort(date(t, "2026-08-27"), nil)

	if doc.Sections[0].Name != "Личное" || doc.Sections[1].Name != "Работа" {
		t.Errorf("порядок разделов изменён: %q, %q", doc.Sections[0].Name, doc.Sections[1].Name)
	}
}

// fullDoc is a document using every field the artifact can carry, for the
// save → load equality check.
func fullDoc(t *testing.T) *Document {
	t.Helper()

	report := task.Task{
		ID: "T-001", Title: "Подготовить отчёт", Section: "Работа", Tier: 1,
		From: "Анна Петрова", Due: ptr(date(t, "2026-09-05")), Effort: task.Effort(5),
		Context: "@компьютер", Labels: []string{"отчётность", "финансы"},
		Notes:     []string{"нужны данные от финотдела", "они тянут"},
		Deferrals: 2, Created: date(t, "2026-08-10"),
	}
	gift := task.Task{
		ID: "T-002", Title: "Купить подарок", Section: "Личное", Tier: 3,
		From: task.SelfAssigned, Created: date(t, "2026-08-25"),
	}
	insurance := task.Task{
		ID: "T-000", Title: "Оплатить страховку", Section: DoneSection,
		Done: true, CompletedAt: ptr(date(t, "2026-08-20")),
	}

	return docOf(
		Section{Name: "Работа", Tasks: []task.Task{report}},
		Section{Name: "Личное", Tasks: []task.Task{gift}},
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
		t.Errorf("Load(Save(doc)) != doc\nполучено:  %+v\nожидалось: %+v", got, want)
	}
}

func TestSaveAfterMutations(t *testing.T) {
	now := date(t, "2026-08-27")
	path := filepath.Join(t.TempDir(), "duties.md")
	store := NewStore(path, config.Default())

	doc, err := store.Load()
	if err != nil {
		t.Fatalf("Load() пустого артефакта error = %v", err)
	}
	in := open("", "Личное", 2)
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
		t.Errorf("Load(Save(doc)) != doc\nполучено:  %+v\nожидалось: %+v", reloaded, doc)
	}
	got, ok := reloaded.Find(id)
	if !ok {
		t.Fatalf("задача %s не сохранилась", id)
	}
	if want := date(t, "2026-09-08"); !got.Due.Equal(want) {
		t.Errorf("срок = %v, ожидалось %v", got.Due, want)
	}
	if got.Deferrals != 1 {
		t.Errorf("переносов = %d, ожидалось 1", got.Deferrals)
	}
}

func TestSaveReplacesFileAndLeavesNoLeftovers(t *testing.T) {
	now := date(t, "2026-08-27")
	dir := t.TempDir()
	path := filepath.Join(dir, "duties.md")
	store := NewStore(path, nil)

	if err := os.WriteFile(path, []byte("# Старое содержимое\n"), 0o644); err != nil {
		t.Fatalf("подготовка файла: %v", err)
	}
	if err := store.Save(fullDoc(t), now); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("чтение %s: %v", path, err)
	}
	if strings.Contains(string(data), "Старое содержимое") {
		t.Error("прежнее содержимое не заменено")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("чтение каталога: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "duties.md" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("в каталоге остались лишние файлы: %v", names)
	}
}

func TestLoadMissingFile(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "нет.md"), nil)

	doc, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := &Document{Preamble: []string{DefaultPreamble}}
	if !reflect.DeepEqual(doc, want) {
		t.Errorf("Load() = %+v, ожидалось %+v", doc, want)
	}
}

func TestLoadBrokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duties.md")
	broken := "# Обязанности\n\n## Работа\n\n- [ ] `T-001` **Задача**\n  тир:9 · от:себя\n"
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatalf("подготовка файла: %v", err)
	}

	if _, err := NewStore(path, nil).Load(); err == nil {
		t.Fatal("Load() = nil, ожидалась ошибка разбора")
	}
}
