package grouping

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/bayanovea/ai-secretary/internal/task"
)

// mk builds a duty with only the fields grouping looks at.
func mk(id string, tier int, from, context string, labels ...string) task.Task {
	return task.Task{
		ID:      id,
		Title:   "task " + id,
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
			name: "two duties in one place — a context group",
			tasks: []task.Task{
				mk("T-001", 1, "Helen", "@town"),
				mk("T-002", 2, "self", "@town"),
			},
			want: []string{"context/@town:[T-001 T-002]"},
		},
		{
			name: "two duties from one person — a from group",
			tasks: []task.Task{
				mk("T-001", 2, "Helen", ""),
				mk("T-002", 2, "Helen", "@home"),
			},
			want: []string{"from/Helen:[T-001 T-002]"},
		},
		{
			name: "three duties with one label — a label group",
			tasks: []task.Task{
				mk("T-001", 2, "self", "", "paperwork"),
				mk("T-002", 2, "self", "", "paperwork"),
				mk("T-003", 1, "self", "", "paperwork"),
			},
			want: []string{"label/paperwork:[T-003 T-001 T-002]"},
		},
		{
			name: "two labels make no group",
			tasks: []task.Task{
				mk("T-001", 2, "self", "", "paperwork"),
				mk("T-002", 2, "self", "", "paperwork"),
			},
			want: nil,
		},
		{
			name: "from:self does not group",
			tasks: []task.Task{
				mk("T-001", 2, "self", ""),
				mk("T-002", 1, "self", ""),
				mk("T-003", 2, "self", ""),
			},
			want: nil,
		},
		{
			name: "lone keys make no group",
			tasks: []task.Task{
				mk("T-001", 1, "Helen", "@town", "paperwork"),
				mk("T-002", 2, "Anna", "@home", "work"),
			},
			want: nil,
		},
		{
			name: "empty context and from are ignored",
			tasks: []task.Task{
				mk("T-001", 1, "", ""),
				mk("T-002", 2, "", ""),
			},
			want: nil,
		},
		{
			name: "completed duties are not grouped",
			tasks: []task.Task{
				done(mk("T-001", 1, "Helen", "@town")),
				mk("T-002", 2, "self", "@town"),
			},
			want: nil,
		},
		{
			name: "context beats person",
			tasks: []task.Task{
				mk("T-001", 1, "Helen", "@town"),
				mk("T-002", 2, "Helen", "@town"),
			},
			want: []string{"context/@town:[T-001 T-002]"},
		},
		{
			name: "a group with a tier-1 duty comes before one without",
			tasks: []task.Task{
				mk("T-001", 2, "self", "@home"),
				mk("T-002", 2, "self", "@home"),
				mk("T-003", 1, "self", "@town"),
				mk("T-004", 3, "self", "@town"),
			},
			want: []string{
				"context/@town:[T-003 T-004]",
				"context/@home:[T-001 T-002]",
			},
		},
		{
			name: "at equal importance the larger group comes first",
			tasks: []task.Task{
				mk("T-001", 2, "self", "@home"),
				mk("T-002", 2, "self", "@home"),
				mk("T-003", 2, "self", "@town"),
				mk("T-004", 2, "self", "@town"),
				mk("T-005", 2, "self", "@town"),
			},
			want: []string{
				"context/@town:[T-003 T-004 T-005]",
				"context/@home:[T-001 T-002]",
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
				t.Errorf("Build() = %v, want %v", got, tc.want)
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
			name: "two background duties make no group",
			tasks: []task.Task{
				mk("T-001", 3, "self", "@town"),
				mk("T-002", 3, "self", "@town"),
			},
			want: nil,
		},
		{
			name: "three background duties make a group",
			tasks: []task.Task{
				mk("T-001", 3, "self", "@town"),
				mk("T-002", 3, "self", "@town"),
				mk("T-003", 3, "self", "@town"),
			},
			want: []string{"context/@town:[T-001 T-002 T-003]"},
		},
		{
			name: "two duties with one non-background make a group",
			tasks: []task.Task{
				mk("T-001", 3, "self", "@town"),
				mk("T-002", 2, "self", "@town"),
			},
			want: []string{"context/@town:[T-002 T-001]"},
		},
		{
			name: "two background duties from one person make no group",
			tasks: []task.Task{
				mk("T-001", 3, "Helen", ""),
				mk("T-002", 3, "Helen", ""),
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
				t.Errorf("Build() = %v, want %v", got, tc.want)
			}
		})
	}
}

// A duty that fits two signals at once must be claimed by exactly one of them.
func TestBuildClaimsEachTaskOnce(t *testing.T) {
	tasks := []task.Task{
		// T-001 could join the @town batch and the Helen batch.
		mk("T-001", 1, "Helen", "@town"),
		mk("T-002", 2, "self", "@town"),
		mk("T-003", 2, "Helen", "@home"),
		// Two labels on one duty: it may only join the first of them.
		mk("T-004", 2, "self", "", "paperwork", "taxes"),
		mk("T-005", 2, "self", "", "paperwork"),
		mk("T-006", 2, "self", "", "paperwork"),
		mk("T-007", 2, "self", "", "taxes"),
		mk("T-008", 2, "self", "", "taxes"),
	}

	groups := Build(tasks, nil)

	seen := make(map[string]string)
	for _, g := range groups {
		for _, dt := range g.Tasks {
			if where, dup := seen[dt.ID]; dup {
				t.Fatalf("task %s landed in both %s and %s", dt.ID, where, g.Key)
			}
			seen[dt.ID] = g.Key
		}
	}

	if got := seen["T-001"]; got != "@town" {
		t.Errorf("T-001 in group %q, want @town (context beats person)", got)
	}
	if got := seen["T-004"]; got != "paperwork" {
		t.Errorf("T-004 in group %q, want paperwork (keys in sorted order)", got)
	}
	// Helen keeps only T-003 once T-001 is claimed, so no person group forms;
	// taxes keeps only T-007 and T-008, below the label threshold.
	if got, want := summary(groups), []string{"context/@town:[T-001 T-002]", "label/paperwork:[T-004 T-005 T-006]"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Build() = %v, want %v", got, want)
	}
}

// The order the artifact happens to be in must not change the proposals.
func TestBuildDeterministic(t *testing.T) {
	tasks := []task.Task{
		mk("T-001", 1, "Helen", "@town"),
		mk("T-002", 2, "self", "@town"),
		mk("T-003", 2, "Anna", "@home", "work"),
		mk("T-004", 3, "Anna", "@phone", "work"),
		mk("T-005", 2, "self", "", "work"),
		mk("T-006", 3, "self", "", "paperwork"),
		mk("T-007", 2, "Helen", "@town"),
	}

	want := Build(tasks, nil)
	if len(want) == 0 {
		t.Fatal("fixture yields no groups")
	}

	// Every rotation of the same input is the same set of duties in a different
	// order, which is exactly what a hand-edited artifact looks like.
	for shift := 1; shift < len(tasks); shift++ {
		shuffled := append(append([]task.Task{}, tasks[shift:]...), tasks[:shift]...)
		if got := Build(shuffled, nil); !reflect.DeepEqual(got, want) {
			t.Errorf("shift %d: Build() = %v, want %v", shift, summary(got), summary(want))
		}
	}

	// And twice over the same input, in case a map iteration leaks through.
	if got := Build(tasks, nil); !reflect.DeepEqual(got, want) {
		t.Errorf("second call gave %v, want %v", summary(got), summary(want))
	}
}

func TestKindString(t *testing.T) {
	tests := []struct {
		kind Kind
		want string
	}{
		{KindContext, "context"},
		{KindPerson, "from"},
		{KindLabel, "label"},
		{Kind(99), "unknown"},
	}
	for _, tc := range tests {
		if got := tc.kind.String(); got != tc.want {
			t.Errorf("Kind(%d).String() = %q, want %q", tc.kind, got, tc.want)
		}
	}
}

func TestGroupTopTierAndSize(t *testing.T) {
	g := Group{Tasks: []task.Task{mk("T-001", 3, "self", "@home"), mk("T-002", 2, "self", "@home")}}
	if got := g.TopTier(); got != 2 {
		t.Errorf("TopTier() = %d, want 2", got)
	}
	if got := g.Size(); got != 2 {
		t.Errorf("Size() = %d, want 2", got)
	}
	if got := (Group{}).TopTier(); got != task.MaxTier {
		t.Errorf("TopTier() of an empty group = %d, want %d", got, task.MaxTier)
	}
}

func TestBuildReason(t *testing.T) {
	groups := Build([]task.Task{
		mk("T-001", 2, "Helen", "@town"),
		mk("T-002", 2, "self", "@town"),
		mk("T-003", 2, "Anna", ""),
		mk("T-004", 2, "Anna", ""),
		mk("T-005", 2, "self", "", "paperwork"),
		mk("T-006", 2, "self", "", "paperwork"),
		mk("T-007", 2, "self", "", "paperwork"),
	}, nil)

	want := map[string]string{
		"@town":     "same place: @town",
		"Anna":      "one conversation: Anna",
		"paperwork": "same theme: paperwork",
	}
	if len(groups) != len(want) {
		t.Fatalf("got %d groups, want %d: %v", len(groups), len(want), summary(groups))
	}
	for _, g := range groups {
		if got := g.Reason; got != want[g.Key] {
			t.Errorf("Reason of group %q = %q, want %q", g.Key, got, want[g.Key])
		}
	}
}
