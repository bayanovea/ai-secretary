package brief

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bayanovea/secretary/internal/config"
	"github.com/bayanovea/secretary/internal/mdstore"
	"github.com/bayanovea/secretary/internal/schedule"
	"github.com/bayanovea/secretary/internal/task"
	"github.com/bayanovea/secretary/internal/voice"
)

// updateGolden rewrites the expected briefing instead of comparing against it.
// Run `go test ./internal/brief -update` after an intentional wording change and
// read the diff: the golden file is how Alfred's briefing is reviewed.
var updateGolden = flag.Bool("update", false, "rewrite the golden files in testdata/golden")

// assertGolden compares got against the named golden file, or rewrites it when
// -update is given.
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (regenerate: go test ./internal/brief -update)", path, err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

// today is the fixed «today» every test in this file runs at.
var today = date("2026-08-27")

func date(s string) time.Time {
	d, err := time.Parse(mdstore.DateLayout, s)
	if err != nil {
		panic(err)
	}
	return d
}

func due(s string) *time.Time {
	d := date(s)
	return &d
}

func effort(s string) task.Effort {
	e, err := task.ParseEffort(s)
	if err != nil {
		panic(err)
	}
	return e
}

// fixture is the document every golden and integration assertion in this file
// runs over: one duty per bucket, two batches, and the two remarks Alfred is
// least willing to leave unsaid.
func fixture() *mdstore.Document {
	return &mdstore.Document{
		Preamble: []string{mdstore.DefaultPreamble},
		Sections: []mdstore.Section{
			{Name: "Work", Tasks: []task.Task{
				{
					ID: "T-001", Title: "Prepare the Q3 report", Section: "Work",
					Tier: 1, From: "Dana Whitfield", Due: due("2026-09-05"), Effort: effort("5d"),
					Context: "@computer", Labels: []string{"reporting", "finance"},
					Notes:     []string{"needs the numbers from accounting"},
					Deferrals: 1, Created: date("2026-08-20"),
				},
				{
					ID: "T-004", Title: "Send the invoices", Section: "Work",
					Tier: 2, From: "Dana Whitfield", Due: due("2026-08-24"), Effort: effort("1d"),
					Context: "@work", Created: date("2026-08-10"),
				},
			}},
			{Name: "Personal", Tasks: []task.Task{
				{
					ID: "T-002", Title: "Collect the documents from the notary", Section: "Personal",
					Tier: 1, From: "Helen", Due: due("2026-08-29"), Effort: effort("1h"),
					Context: "@town", Labels: []string{"paperwork", "documents"},
					Created: date("2026-08-26"),
				},
				{
					ID: "T-003", Title: "Buy Helen a present", Section: "Personal",
					Tier: 2, From: "self", Due: due("2026-09-01"), Effort: effort("2h"),
					Context: "@town", Labels: []string{"relationships"},
					Deferrals: 2, Created: date("2026-08-15"),
				},
				{
					ID: "T-006", Title: "Pick up the parcel", Section: "Personal",
					Tier: 3, From: "Helen", Context: "@town", Created: date("2026-08-20"),
				},
			}},
			{Name: "Home", Tasks: []task.Task{
				{
					ID: "T-005", Title: "Sort out the insurance", Section: "Home",
					Tier: 1, From: "self", Created: date("2026-08-01"),
				},
			}},
			{Name: mdstore.DoneSection, Tasks: []task.Task{
				{
					ID: "T-000", Title: "Pay the internet bill", Section: mdstore.DoneSection,
					Done: true, CompletedAt: due("2026-08-20"),
				},
			}},
		},
	}
}

func ids(tasks []task.Task) []string {
	out := make([]string, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, t.ID)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestBuildBuckets(t *testing.T) {
	b := Build(fixture(), today, config.Default())

	cases := []struct {
		name string
		got  []string
		want []string
	}{
		{"overdue", ids(b.Overdue), []string{"T-004"}},
		{"start now", ids(b.StartNow), []string{"T-002"}},
		{"soon", ids(b.Soon), []string{"T-001", "T-003"}},
	}
	for _, c := range cases {
		if !equalStrings(c.got, c.want) {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

// TestBuildBucketsAreDisjoint is the invariant the plan asks for: a duty lands
// in exactly one bucket, never in two and never in none-plus-a-duplicate.
func TestBuildBucketsAreDisjoint(t *testing.T) {
	b := Build(fixture(), today, config.Default())

	seen := map[string]string{}
	for bucket, tasks := range map[string][]task.Task{
		"overdue":   b.Overdue,
		"start now": b.StartNow,
		"soon":      b.Soon,
	} {
		for _, tk := range tasks {
			if other, dup := seen[tk.ID]; dup {
				t.Errorf("%s landed in both %q and %q", tk.ID, other, bucket)
			}
			seen[tk.ID] = bucket
		}
	}
}

// TestBuildBucketPerStatus checks the mapping itself: whatever status the
// schedule package assigns, the duty ends up in the matching bucket and nowhere
// else. Calm and deadline-less duties are reported in no bucket at all.
func TestBuildBucketPerStatus(t *testing.T) {
	cfg := config.Default()
	cases := []struct {
		name   string
		task   task.Task
		bucket string
	}{
		{"overdue", task.Task{ID: "T-101", Title: "Overdue one", Section: "Work", Tier: 2, From: "self", Due: due("2026-08-20"), Effort: effort("1d")}, "overdue"},
		{"start now", task.Task{ID: "T-102", Title: "Time", Section: "Work", Tier: 2, From: "self", Due: due("2026-08-28"), Effort: effort("1d")}, "start now"},
		{"soon", task.Task{ID: "T-103", Title: "Soon", Section: "Work", Tier: 3, From: "self", Due: due("2026-08-30"), Effort: effort("1d")}, "soon"},
		{"on track", task.Task{ID: "T-104", Title: "Calm", Section: "Work", Tier: 3, From: "self", Due: due("2026-12-31"), Effort: effort("1d")}, ""},
		{"no deadline", task.Task{ID: "T-105", Title: "No deadline", Section: "Work", Tier: 2, From: "self"}, ""},
		{"done", task.Task{ID: "T-106", Title: "Finished", Section: mdstore.DoneSection, Tier: 1, From: "self", Due: due("2026-01-01"), Done: true}, ""},
	}

	for _, c := range cases {
		doc := &mdstore.Document{Sections: []mdstore.Section{{Name: "Work", Tasks: []task.Task{c.task}}}}
		b := Build(doc, today, cfg)

		got := ""
		switch {
		case len(b.Overdue) > 0:
			got = "overdue"
		case len(b.StartNow) > 0:
			got = "start now"
		case len(b.Soon) > 0:
			got = "soon"
		}
		if got != c.bucket {
			t.Errorf("%s: landed in %q, want %q", c.name, got, c.bucket)
		}
		if total := len(b.Overdue) + len(b.StartNow) + len(b.Soon); total > 1 {
			t.Errorf("%s: task landed in %d buckets", c.name, total)
		}
	}
}

func TestBuildCounts(t *testing.T) {
	b := Build(fixture(), today, config.Default())

	want := map[int]int{1: 3, 2: 2, 3: 1}
	for tier, n := range want {
		if b.Counts[tier] != n {
			t.Errorf("tier %d: %d, want %d", tier, b.Counts[tier], n)
		}
	}
	if b.Total() != 6 {
		t.Errorf("total open: %d, want 6 (completed do not count)", b.Total())
	}
}

func TestBuildComplaints(t *testing.T) {
	b := Build(fixture(), today, config.Default())

	type pair struct {
		id   string
		flag schedule.Flag
	}
	var got []pair
	for _, c := range b.Complaints {
		got = append(got, pair{c.Task.ID, c.Flag})
	}
	want := []pair{
		{"T-005", schedule.FlagIncomplete},
		{"T-005", schedule.FlagForgotten},
		{"T-003", schedule.FlagPostponed},
	}
	if len(got) != len(want) {
		t.Fatalf("complaints %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("complaint %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestBuildGroups(t *testing.T) {
	b := Build(fixture(), today, config.Default())

	if len(b.Groups) != 2 {
		t.Fatalf("groups %d, want 2: %v", len(b.Groups), b.Groups)
	}
	if b.Groups[0].Key != "@town" || !equalStrings(ids(b.Groups[0].Tasks), []string{"T-002", "T-003", "T-006"}) {
		t.Errorf("first group = %s %v, want @town [T-002 T-003 T-006]", b.Groups[0].Key, ids(b.Groups[0].Tasks))
	}
	if b.Groups[1].Key != "Dana Whitfield" || !equalStrings(ids(b.Groups[1].Tasks), []string{"T-001", "T-004"}) {
		t.Errorf("second group = %s %v, want Dana Whitfield [T-001 T-004]", b.Groups[1].Key, ids(b.Groups[1].Tasks))
	}
}

// TestBuildIgnoresDone guards the one thing a briefing must never do: report a
// finished duty as overdue.
func TestBuildIgnoresDone(t *testing.T) {
	doc := &mdstore.Document{Sections: []mdstore.Section{{
		Name: mdstore.DoneSection,
		Tasks: []task.Task{{
			ID: "T-000", Title: "Pay the internet bill", Section: mdstore.DoneSection,
			Tier: 1, From: "self", Due: due("2026-01-01"), Effort: effort("1d"),
			Done: true, CompletedAt: due("2026-01-01"),
		}},
	}}}

	b := Build(doc, today, config.Default())
	if !b.IsClear() {
		t.Errorf("a completed task landed in the briefing: %+v", b)
	}
	if b.Total() != 0 {
		t.Errorf("total open: %d, want 0", b.Total())
	}
}

func TestAllClear(t *testing.T) {
	cases := []struct {
		name string
		doc  *mdstore.Document
	}{
		{"empty document", &mdstore.Document{Preamble: []string{mdstore.DefaultPreamble}}},
		{"only calm tasks", &mdstore.Document{Sections: []mdstore.Section{{
			Name: "Work",
			Tasks: []task.Task{{
				ID: "T-001", Title: "Calm one", Section: "Work",
				Tier: 3, From: "self", Due: due("2026-12-31"), Effort: effort("1d"),
				Created: date("2026-08-26"),
			}},
		}}}},
	}

	alfred := voice.Alfred{}
	for _, c := range cases {
		b := Build(c.doc, today, config.Default())
		if !b.IsClear() {
			t.Errorf("%s: IsClear = false, want true", c.name)
		}
		text := b.Text(alfred)
		if !strings.Contains(text, alfred.AllClear()) {
			t.Errorf("%s: the text lacks the all-clear phrase:\n%s", c.name, text)
		}
		if !strings.Contains(text, alfred.Signoff()) {
			t.Errorf("%s: the text lacks the sign-off:\n%s", c.name, text)
		}
	}
}

func TestTextGolden(t *testing.T) {
	b := Build(fixture(), today, config.Default())
	assertGolden(t, "brief.txt", b.Text(voice.Alfred{}))
}

// TestTextDeterministic is the property the whole voice package is built for:
// the same artifact on the same day says the same thing.
func TestTextDeterministic(t *testing.T) {
	first := Build(fixture(), today, config.Default()).Text(voice.Alfred{})
	second := Build(fixture(), today, config.Default()).Text(voice.Alfred{})
	if first != second {
		t.Errorf("two runs on one day diverged:\n%s\n---\n%s", first, second)
	}
}

// TestTextNilPersona covers the caller that has not resolved the config's
// `persona:` yet: a report, not a panic.
func TestTextNilPersona(t *testing.T) {
	text := Build(fixture(), today, config.Default()).Text(nil)
	if !strings.Contains(text, voice.Alfred{}.Signoff()) {
		t.Errorf("without a persona the text is not Alfred's:\n%s", text)
	}
}

// TestTextMentionsEveryReportedTask checks the wiring rather than the wording:
// nothing that Build put in a bucket may quietly vanish on the way to the page.
func TestTextMentionsEveryReportedTask(t *testing.T) {
	b := Build(fixture(), today, config.Default())
	text := b.Text(voice.Alfred{})

	var reported []task.Task
	reported = append(reported, b.Overdue...)
	reported = append(reported, b.StartNow...)
	reported = append(reported, b.Soon...)
	for _, c := range b.Complaints {
		reported = append(reported, c.Task)
	}
	for _, rt := range reported {
		if !strings.Contains(text, rt.Title) {
			t.Errorf("«%s» missing from the briefing:\n%s", rt.Title, text)
		}
	}
	for _, g := range b.Groups {
		for _, gt := range g.Tasks {
			if !strings.Contains(text, gt.ID) {
				t.Errorf("%s not mentioned in group %q:\n%s", gt.ID, g.Key, text)
			}
		}
	}
}

func TestJSONHasEveryBucket(t *testing.T) {
	data, err := Build(fixture(), today, config.Default()).JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}

	var out map[string]json.RawMessage
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("JSON decode: %v\n%s", err, data)
	}
	for _, key := range []string{"date", "all_clear", "total", "counts", "overdue", "start_now", "soon", "groups", "complaints"} {
		if _, ok := out[key]; !ok {
			t.Errorf("JSON lacks field %q:\n%s", key, data)
		}
	}
}

func TestJSONRoundTrip(t *testing.T) {
	b := Build(fixture(), today, config.Default())
	data, err := b.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}

	var out struct {
		Date     string `json:"date"`
		AllClear bool   `json:"all_clear"`
		Total    int    `json:"total"`
		Counts   map[string]int
		Overdue  []struct {
			ID          string `json:"id"`
			Status      string `json:"status"`
			Due         string `json:"due"`
			DaysOverdue int    `json:"days_overdue"`
		} `json:"overdue"`
		StartNow []struct {
			ID          string `json:"id"`
			LatestStart string `json:"latest_start"`
		} `json:"start_now"`
		Soon []struct {
			ID             string `json:"id"`
			DaysUntilStart int    `json:"days_until_start"`
		} `json:"soon"`
		Groups []struct {
			Kind  string `json:"kind"`
			Key   string `json:"key"`
			Tasks []struct {
				ID string `json:"id"`
			} `json:"tasks"`
		} `json:"groups"`
		Complaints []struct {
			Flag string `json:"flag"`
			Task struct {
				ID string `json:"id"`
			} `json:"task"`
		} `json:"complaints"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("JSON decode: %v\n%s", err, data)
	}

	if out.Date != "2026-08-27" {
		t.Errorf("date = %q, want 2026-08-27", out.Date)
	}
	if out.AllClear {
		t.Error("all_clear = true despite an overdue task")
	}
	if out.Total != 6 || out.Counts["1"] != 3 {
		t.Errorf("total = %d, counts = %v, want 6 and 3 tier-1 tasks", out.Total, out.Counts)
	}
	if len(out.Overdue) != 1 || out.Overdue[0].ID != "T-004" {
		t.Fatalf("overdue = %+v, want only T-004", out.Overdue)
	}
	if out.Overdue[0].Status != task.StatusOverdue.String() || out.Overdue[0].DaysOverdue != 3 {
		t.Errorf("overdue[0] = %+v, want «overdue», 3 days", out.Overdue[0])
	}
	if len(out.StartNow) != 1 || out.StartNow[0].LatestStart != "2026-08-26" {
		t.Errorf("start_now = %+v, want latest start 2026-08-26", out.StartNow)
	}
	if len(out.Soon) != 2 || out.Soon[0].ID != "T-001" || out.Soon[0].DaysUntilStart != 1 {
		t.Errorf("soon = %+v, want T-001 in a day", out.Soon)
	}
	if len(out.Groups) != 2 || out.Groups[0].Key != "@town" || len(out.Groups[0].Tasks) != 3 {
		t.Errorf("groups = %+v, want a @town group of three tasks", out.Groups)
	}
	if len(out.Complaints) != 3 || out.Complaints[0].Flag != schedule.FlagIncomplete.String() {
		t.Errorf("complaints = %+v, want three, the first one incomplete data", out.Complaints)
	}
}

// TestJSONAllClearEmptyBuckets checks that an empty briefing still has every
// bucket as a list — a consumer indexes into it without a nil check.
func TestJSONAllClearEmptyBuckets(t *testing.T) {
	b := Build(&mdstore.Document{Preamble: []string{mdstore.DefaultPreamble}}, today, config.Default())
	data, err := b.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}

	var out struct {
		AllClear   bool          `json:"all_clear"`
		Overdue    []interface{} `json:"overdue"`
		StartNow   []interface{} `json:"start_now"`
		Soon       []interface{} `json:"soon"`
		Groups     []interface{} `json:"groups"`
		Complaints []interface{} `json:"complaints"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("JSON decode: %v\n%s", err, data)
	}
	if !out.AllClear {
		t.Error("all_clear = false for an empty document")
	}
	if strings.Contains(string(data), "null") {
		t.Errorf("null in an empty briefing:\n%s", data)
	}
	if out.Overdue == nil || out.StartNow == nil || out.Soon == nil || out.Groups == nil || out.Complaints == nil {
		t.Errorf("not every bucket came as a list:\n%s", data)
	}
}

// TestBuildNilConfig covers the caller that has no configuration to give.
func TestBuildNilConfig(t *testing.T) {
	b := Build(fixture(), today, nil)
	if len(b.Overdue) != 1 {
		t.Errorf("with a nil config overdue = %d, want 1", len(b.Overdue))
	}
	if text := b.Text(voice.Alfred{}); text == "" {
		t.Error("with a nil config the text is empty")
	}
}
