package grouping

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/bayanovea/secretary/internal/task"
)

// mk builds a duty with only the fields grouping looks at.
func mk(id string, tier int, from, context string, labels ...string) task.Task {
	return task.Task{
		ID:      id,
		Title:   "задача " + id,
		Tier:    tier,
		From:    from,
		Context: context,
		Labels:  labels,
	}
}

// summary renders the groups as comparable strings: kind, key and the ids.
func summary(groups []Group) []string {
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		ids := make([]string, 0, len(g.Tasks))
		for _, t := range g.Tasks {
			ids = append(ids, t.ID)
		}
		out = append(out, fmt.Sprintf("%s/%s:%v", g.Kind, g.Key, ids))
	}
	return out
}

func TestBuild(t *testing.T) {
	tests := []struct {
		name  string
		tasks []task.Task
		want  []string
	}{
		{
			name: "два дела в одном месте — группа по контексту",
			tasks: []task.Task{
				mk("T-001", 1, "Марина", "@город"),
				mk("T-002", 2, "себя", "@город"),
			},
			want: []string{"контекст/@город:[T-001 T-002]"},
		},
		{
			name: "два дела от одного человека — группа по от",
			tasks: []task.Task{
				mk("T-001", 2, "Марина", ""),
				mk("T-002", 2, "Марина", "@дом"),
			},
			want: []string{"от/Марина:[T-001 T-002]"},
		},
		{
			name: "три дела с общей меткой — группа по метке",
			tasks: []task.Task{
				mk("T-001", 2, "себя", "", "переезд"),
				mk("T-002", 2, "себя", "", "переезд"),
				mk("T-003", 1, "себя", "", "переезд"),
			},
			want: []string{"метка/переезд:[T-003 T-001 T-002]"},
		},
		{
			name: "две метки группу не образуют",
			tasks: []task.Task{
				mk("T-001", 2, "себя", "", "переезд"),
				mk("T-002", 2, "себя", "", "переезд"),
			},
			want: nil,
		},
		{
			name: "от:себя не группирует",
			tasks: []task.Task{
				mk("T-001", 2, "себя", ""),
				mk("T-002", 1, "себя", ""),
				mk("T-003", 2, "себя", ""),
			},
			want: nil,
		},
		{
			name: "одиночные ключи группы не дают",
			tasks: []task.Task{
				mk("T-001", 1, "Марина", "@город", "переезд"),
				mk("T-002", 2, "Анна", "@дом", "работа"),
			},
			want: nil,
		},
		{
			name: "пустые контекст и от игнорируются",
			tasks: []task.Task{
				mk("T-001", 1, "", ""),
				mk("T-002", 2, "", ""),
			},
			want: nil,
		},
		{
			name: "выполненные дела не группируются",
			tasks: []task.Task{
				done(mk("T-001", 1, "Марина", "@город")),
				mk("T-002", 2, "себя", "@город"),
			},
			want: nil,
		},
		{
			name: "контекст важнее человека",
			tasks: []task.Task{
				mk("T-001", 1, "Марина", "@город"),
				mk("T-002", 2, "Марина", "@город"),
			},
			want: []string{"контекст/@город:[T-001 T-002]"},
		},
		{
			name: "группа с тир-1 идёт раньше группы без него",
			tasks: []task.Task{
				mk("T-001", 2, "себя", "@дом"),
				mk("T-002", 2, "себя", "@дом"),
				mk("T-003", 1, "себя", "@город"),
				mk("T-004", 3, "себя", "@город"),
			},
			want: []string{
				"контекст/@город:[T-003 T-004]",
				"контекст/@дом:[T-001 T-002]",
			},
		},
		{
			name: "при равной важности больше группа идёт первой",
			tasks: []task.Task{
				mk("T-001", 2, "себя", "@дом"),
				mk("T-002", 2, "себя", "@дом"),
				mk("T-003", 2, "себя", "@город"),
				mk("T-004", 2, "себя", "@город"),
				mk("T-005", 2, "себя", "@город"),
			},
			want: []string{
				"контекст/@город:[T-003 T-004 T-005]",
				"контекст/@дом:[T-001 T-002]",
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := summary(Build(tc.tasks, nil))
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Build() = %v, ожидалось %v", got, tc.want)
			}
		})
	}
}

// done marks a duty completed.
func done(t task.Task) task.Task {
	t.Done = true
	return t
}

func TestBuildBackgroundThreshold(t *testing.T) {
	tests := []struct {
		name  string
		tasks []task.Task
		want  []string
	}{
		{
			name: "две фоновые задачи группы не образуют",
			tasks: []task.Task{
				mk("T-001", 3, "себя", "@город"),
				mk("T-002", 3, "себя", "@город"),
			},
			want: nil,
		},
		{
			name: "три фоновые задачи образуют группу",
			tasks: []task.Task{
				mk("T-001", 3, "себя", "@город"),
				mk("T-002", 3, "себя", "@город"),
				mk("T-003", 3, "себя", "@город"),
			},
			want: []string{"контекст/@город:[T-001 T-002 T-003]"},
		},
		{
			name: "две задачи с одной не фоновой образуют группу",
			tasks: []task.Task{
				mk("T-001", 3, "себя", "@город"),
				mk("T-002", 2, "себя", "@город"),
			},
			want: []string{"контекст/@город:[T-002 T-001]"},
		},
		{
			name: "две фоновые задачи от одного человека группы не образуют",
			tasks: []task.Task{
				mk("T-001", 3, "Марина", ""),
				mk("T-002", 3, "Марина", ""),
			},
			want: nil,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := summary(Build(tc.tasks, nil))
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Build() = %v, ожидалось %v", got, tc.want)
			}
		})
	}
}

// A duty that fits two signals at once must be claimed by exactly one of them.
func TestBuildClaimsEachTaskOnce(t *testing.T) {
	tasks := []task.Task{
		// T-001 could join the @город batch and the Марина batch.
		mk("T-001", 1, "Марина", "@город"),
		mk("T-002", 2, "себя", "@город"),
		mk("T-003", 2, "Марина", "@дом"),
		// Two labels on one duty: it may only join the first of them.
		mk("T-004", 2, "себя", "", "переезд", "финансы"),
		mk("T-005", 2, "себя", "", "переезд"),
		mk("T-006", 2, "себя", "", "переезд"),
		mk("T-007", 2, "себя", "", "финансы"),
		mk("T-008", 2, "себя", "", "финансы"),
	}

	groups := Build(tasks, nil)

	seen := make(map[string]string)
	for _, g := range groups {
		for _, dt := range g.Tasks {
			if where, dup := seen[dt.ID]; dup {
				t.Fatalf("задача %s попала и в %s, и в %s", dt.ID, where, g.Key)
			}
			seen[dt.ID] = g.Key
		}
	}

	if got := seen["T-001"]; got != "@город" {
		t.Errorf("T-001 в группе %q, ожидался @город (контекст важнее человека)", got)
	}
	if got := seen["T-004"]; got != "переезд" {
		t.Errorf("T-004 в группе %q, ожидалась переезд (ключи в отсортированном порядке)", got)
	}
	// Марина keeps only T-003 once T-001 is claimed, so no person group forms;
	// финансы keeps only T-007 and T-008, below the label threshold.
	if got, want := summary(groups), []string{"контекст/@город:[T-001 T-002]", "метка/переезд:[T-004 T-005 T-006]"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Build() = %v, ожидалось %v", got, want)
	}
}

// The order the artifact happens to be in must not change the proposals.
func TestBuildDeterministic(t *testing.T) {
	tasks := []task.Task{
		mk("T-001", 1, "Марина", "@город"),
		mk("T-002", 2, "себя", "@город"),
		mk("T-003", 2, "Анна", "@дом", "работа"),
		mk("T-004", 3, "Анна", "@телефон", "работа"),
		mk("T-005", 2, "себя", "", "работа"),
		mk("T-006", 3, "себя", "", "переезд"),
		mk("T-007", 2, "Марина", "@город"),
	}

	want := Build(tasks, nil)
	if len(want) == 0 {
		t.Fatal("фикстура не даёт ни одной группы")
	}

	// Every rotation of the same input is the same set of duties in a different
	// order, which is exactly what a hand-edited artifact looks like.
	for shift := 1; shift < len(tasks); shift++ {
		shuffled := append(append([]task.Task{}, tasks[shift:]...), tasks[:shift]...)
		if got := Build(shuffled, nil); !reflect.DeepEqual(got, want) {
			t.Errorf("сдвиг %d: Build() = %v, ожидалось %v", shift, summary(got), summary(want))
		}
	}

	// And twice over the same input, in case a map iteration leaks through.
	if got := Build(tasks, nil); !reflect.DeepEqual(got, want) {
		t.Errorf("повторный вызов дал %v, ожидалось %v", summary(got), summary(want))
	}
}

func TestKindString(t *testing.T) {
	tests := []struct {
		kind Kind
		want string
	}{
		{KindContext, "контекст"},
		{KindPerson, "от"},
		{KindLabel, "метка"},
		{Kind(99), "неизвестно"},
	}
	for _, tc := range tests {
		if got := tc.kind.String(); got != tc.want {
			t.Errorf("Kind(%d).String() = %q, ожидалось %q", tc.kind, got, tc.want)
		}
	}
}

func TestGroupTopTierAndSize(t *testing.T) {
	g := Group{Tasks: []task.Task{mk("T-001", 3, "себя", "@дом"), mk("T-002", 2, "себя", "@дом")}}
	if got := g.TopTier(); got != 2 {
		t.Errorf("TopTier() = %d, ожидалось 2", got)
	}
	if got := g.Size(); got != 2 {
		t.Errorf("Size() = %d, ожидалось 2", got)
	}
	if got := (Group{}).TopTier(); got != task.MaxTier {
		t.Errorf("TopTier() пустой группы = %d, ожидалось %d", got, task.MaxTier)
	}
}

func TestBuildReason(t *testing.T) {
	groups := Build([]task.Task{
		mk("T-001", 2, "Марина", "@город"),
		mk("T-002", 2, "себя", "@город"),
		mk("T-003", 2, "Анна", ""),
		mk("T-004", 2, "Анна", ""),
		mk("T-005", 2, "себя", "", "переезд"),
		mk("T-006", 2, "себя", "", "переезд"),
		mk("T-007", 2, "себя", "", "переезд"),
	}, nil)

	want := map[string]string{
		"@город":  "общее место: @город",
		"Анна":    "один разговор: Анна",
		"переезд": "общая тема: переезд",
	}
	if len(groups) != len(want) {
		t.Fatalf("получено %d групп, ожидалось %d: %v", len(groups), len(want), summary(groups))
	}
	for _, g := range groups {
		if got := g.Reason; got != want[g.Key] {
			t.Errorf("Reason группы %q = %q, ожидалось %q", g.Key, got, want[g.Key])
		}
	}
}
