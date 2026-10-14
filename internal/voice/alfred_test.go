package voice

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bayanovea/ai-secretary/internal/grouping"
	"github.com/bayanovea/ai-secretary/internal/schedule"
	"github.com/bayanovea/ai-secretary/internal/task"
)

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func sampleTask() task.Task {
	due := day("2026-09-05")
	return task.Task{
		ID:        "T-001",
		Title:     "Prepare the Q3 report",
		Section:   "Work",
		Tier:      1,
		From:      "Dana Whitfield",
		Due:       &due,
		Effort:    5,
		Context:   "@computer",
		Labels:    []string{"reporting", "finance"},
		Deferrals: 2,
		Created:   day("2026-08-01"),
	}
}

func sampleGroup(kind grouping.Kind, key string) grouping.Group {
	a := sampleTask()
	b := sampleTask()
	b.ID = "T-002"
	b.Title = "Collect the documents from the notary"
	return grouping.Group{Kind: kind, Key: key, Tasks: []task.Task{a, b}, Reason: "test"}
}

// everyPhrase calls every Persona method once, labelled, so the non-empty and
// determinism tests can iterate over the whole interface instead of listing it
// twice.
func everyPhrase(p Persona) map[string]string {
	t := sampleTask()
	forgotten := sampleTask()
	forgotten.Due = nil

	return map[string]string{
		"Greeting":             p.Greeting(day("2026-08-27")),
		"Overdue":              p.Overdue(t, 3),
		"StartNow":             p.StartNow(t),
		"Soon":                 p.Soon(t, 2),
		"GroupIntro/context":   p.GroupIntro(sampleGroup(grouping.KindContext, "@town")),
		"GroupIntro/person":    p.GroupIntro(sampleGroup(grouping.KindPerson, "Helen")),
		"GroupIntro/label":     p.GroupIntro(sampleGroup(grouping.KindLabel, "paperwork")),
		"Complaint/incomplete": p.Complaint(schedule.FlagIncomplete, t),
		"Complaint/postponed":  p.Complaint(schedule.FlagPostponed, t),
		"Complaint/forgotten":  p.Complaint(schedule.FlagForgotten, forgotten),
		"Complaint/unknown":    p.Complaint(schedule.Flag(42), t),
		"AllClear":             p.AllClear(),
		"Signoff":              p.Signoff(),
	}
}

func TestAlfredEveryMethodSpeaks(t *testing.T) {
	for name, phrase := range everyPhrase(Alfred{}) {
		if strings.TrimSpace(phrase) == "" {
			t.Errorf("%s: empty phrase", name)
		}
		// A template with the wrong number of arguments shows up as %!s(...)
		// or %!(EXTRA ...) rather than as a failure, so it is checked here.
		if strings.Contains(phrase, "%!") || strings.Contains(phrase, "%s") {
			t.Errorf("%s: unfilled template: %q", name, phrase)
		}
		if !strings.Contains(strings.ToLower(phrase), "sir") {
			t.Errorf("%s: Alfred must address the user as sir: %q", name, phrase)
		}
	}
}

func TestAlfredDeterministic(t *testing.T) {
	first, second := everyPhrase(Alfred{}), everyPhrase(Alfred{})
	for name, want := range first {
		if got := second[name]; got != want {
			t.Errorf("%s: two outputs differ:\n%q\n%q", name, want, got)
		}
	}
}

func TestAlfredDeterministicAcrossInstances(t *testing.T) {
	a, err := Get("alfred")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	b, err := Get("alfred")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	for name, want := range everyPhrase(a) {
		if got := everyPhrase(b)[name]; got != want {
			t.Errorf("%s: different instances speak differently:\n%q\n%q", name, want, got)
		}
	}
}

func TestAlfredVariesOverTime(t *testing.T) {
	alfred := Alfred{}
	sample := sampleTask()

	cases := []struct {
		name  string
		speak func(i int) string
	}{
		{
			name:  "Greeting by day",
			speak: func(i int) string { return alfred.Greeting(day("2026-08-01").AddDate(0, 0, i)) },
		},
		{
			name:  "Overdue by day count",
			speak: func(i int) string { return alfred.Overdue(sample, i+1) },
		},
		{
			name:  "Soon by day count",
			speak: func(i int) string { return alfred.Soon(sample, i+1) },
		},
		{
			name: "StartNow by task",
			speak: func(i int) string {
				other := sample
				other.ID = "T-10" + string(rune('0'+i%10))
				return alfred.StartNow(other)
			},
		},
		{
			name: "Complaint by task",
			speak: func(i int) string {
				other := sample
				other.ID = "T-20" + string(rune('0'+i%10))
				return alfred.Complaint(schedule.FlagIncomplete, other)
			},
		},
		{
			name: "GroupIntro by key",
			speak: func(i int) string {
				return alfred.GroupIntro(sampleGroup(grouping.KindContext, "@place"+string(rune('a'+i%20))))
			},
		},
	}

	for _, c := range cases {
		distinct := make(map[string]bool)
		for i := 0; i < 20; i++ {
			distinct[c.speak(i)] = true
		}
		if len(distinct) < 2 {
			t.Errorf("%s: no new wording in 20 attempts", c.name)
		}
	}
}

func TestAlfredMentionsTheFacts(t *testing.T) {
	alfred := Alfred{}
	sample := sampleTask()
	forgotten := sampleTask()
	forgotten.Due = nil

	cases := []struct {
		name   string
		phrase string
		want   []string
	}{
		{"Greeting contains the date", alfred.Greeting(day("2026-08-27")), []string{"27 August"}},
		{"Overdue contains the title and the count", alfred.Overdue(sample, 3), []string{sample.Title, "3 days"}},
		{"Overdue inflects 1 day", alfred.Overdue(sample, 1), []string{"1 day"}},
		{"Overdue inflects 11 days", alfred.Overdue(sample, 11), []string{"11 days"}},
		{"StartNow contains the title", alfred.StartNow(sample), []string{sample.Title}},
		{"Soon contains the title and the count", alfred.Soon(sample, 2), []string{sample.Title, "2 days"}},
		{"GroupIntro contains the key", alfred.GroupIntro(sampleGroup(grouping.KindContext, "@town")), []string{"@town"}},
		{"GroupIntro by person", alfred.GroupIntro(sampleGroup(grouping.KindPerson, "Helen")), []string{"Helen"}},
		{"GroupIntro by label", alfred.GroupIntro(sampleGroup(grouping.KindLabel, "paperwork")), []string{"paperwork"}},
		{"Complaint about deferrals", alfred.Complaint(schedule.FlagPostponed, sample), []string{sample.Title, "2 times"}},
		{"Complaint about a forgotten duty", alfred.Complaint(schedule.FlagForgotten, forgotten), []string{forgotten.Title, "1 August"}},
		{"Complaint about incomplete data", alfred.Complaint(schedule.FlagIncomplete, sample), []string{sample.Title}},
	}

	for _, c := range cases {
		for _, want := range c.want {
			if !strings.Contains(c.phrase, want) {
				t.Errorf("%s: %q does not contain %q", c.name, c.phrase, want)
			}
		}
	}
}

func TestGet(t *testing.T) {
	cases := []struct {
		name    string
		persona string
		wantErr bool
	}{
		{"known persona", "alfred", false},
		{"case does not matter", "Alfred", false},
		{"whitespace is trimmed", "  alfred  ", false},
		{"unknown persona", "batman", true},
		{"empty name", "", true},
		{"whitespace only", "   ", true},
	}

	for _, c := range cases {
		p, err := Get(c.persona)
		switch {
		case c.wantErr && err == nil:
			t.Errorf("%s: want an error, got persona %T", c.name, p)
		case c.wantErr && !errors.Is(err, ErrUnknownPersona):
			t.Errorf("%s: want ErrUnknownPersona, got %v", c.name, err)
		case !c.wantErr && err != nil:
			t.Errorf("%s: unexpected error %v", c.name, err)
		case !c.wantErr && p == nil:
			t.Errorf("%s: persona not created", c.name)
		}
	}
}

func TestNames(t *testing.T) {
	names := Names()
	if len(names) == 0 {
		t.Fatal("persona registry is empty")
	}
	found := false
	for _, n := range names {
		if n == "alfred" {
			found = true
		}
	}
	if !found {
		t.Errorf("alfred missing from %v", names)
	}
}

func TestPick(t *testing.T) {
	variants := []string{"a", "b", "c"}

	if got := pick(nil, "seed"); got != "" {
		t.Errorf("pick with no variants returned %q", got)
	}
	if got := pick([]string{"only one"}, "seed"); got != "only one" {
		t.Errorf("pick with one variant returned %q", got)
	}
	if a, b := pick(variants, "x", "y"), pick(variants, "x", "y"); a != b {
		t.Errorf("pick is not deterministic: %q != %q", a, b)
	}
	// The separator keeps ("ab","c") from colliding with ("a","bc"); the two
	// need not differ in outcome, but they must hash differently, which the
	// distinctness over a range below exercises.
	distinct := make(map[string]bool)
	for i := 0; i < 30; i++ {
		distinct[pick(variants, "seed", string(rune('a'+i)))] = true
	}
	if len(distinct) != len(variants) {
		t.Errorf("pick covered %d variants of %d", len(distinct), len(variants))
	}
}

func TestPlural(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0 days"},
		{1, "1 day"},
		{2, "2 days"},
		{11, "11 days"},
		{21, "21 days"},
		{101, "101 days"},
		{-1, "-1 day"},
	}
	for _, c := range cases {
		if got := plural(c.n, "day", "days"); got != c.want {
			t.Errorf("plural(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestRussianDate(t *testing.T) {
	cases := []struct {
		date time.Time
		want string
	}{
		{day("2026-01-01"), "1 January"},
		{day("2026-08-27"), "27 August"},
		{day("2026-12-31"), "31 December"},
		{time.Time{}, "an unknown date"},
	}
	for _, c := range cases {
		if got := spokenDate(c.date); got != c.want {
			t.Errorf("spokenDate(%v) = %q, want %q", c.date, got, c.want)
		}
	}
}

func TestDueKey(t *testing.T) {
	withDue := sampleTask()
	if got := dueKey(withDue); got != "2026-09-05" {
		t.Errorf("dueKey with a deadline = %q", got)
	}
	noDue := sampleTask()
	noDue.Due = nil
	if got := dueKey(noDue); got != "no-deadline" {
		t.Errorf("dueKey without a deadline = %q", got)
	}
}

func TestGreetingIgnoresClockTime(t *testing.T) {
	alfred := Alfred{}
	morning := time.Date(2026, 8, 27, 7, 0, 0, 0, time.UTC)
	evening := time.Date(2026, 8, 27, 23, 30, 0, 0, time.UTC)
	if alfred.Greeting(morning) != alfred.Greeting(evening) {
		t.Error("greeting changes within one day")
	}
}
