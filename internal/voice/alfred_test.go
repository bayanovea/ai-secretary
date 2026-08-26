package voice

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bayanovea/secretary/internal/grouping"
	"github.com/bayanovea/secretary/internal/schedule"
	"github.com/bayanovea/secretary/internal/task"
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
		Title:     "Подготовить отчёт за Q3",
		Section:   "Работа",
		Tier:      1,
		From:      "Анна Петрова",
		Due:       &due,
		Effort:    5,
		Context:   "@компьютер",
		Labels:    []string{"отчётность", "финансы"},
		Deferrals: 2,
		Created:   day("2026-08-01"),
	}
}

func sampleGroup(kind grouping.Kind, key string) grouping.Group {
	a := sampleTask()
	b := sampleTask()
	b.ID = "T-002"
	b.Title = "Забрать документы у нотариуса"
	return grouping.Group{Kind: kind, Key: key, Tasks: []task.Task{a, b}, Reason: "тест"}
}

// everyPhrase calls every Persona method once, labelled, so the non-empty and
// determinism tests can iterate over the whole interface instead of listing it
// twice.
func everyPhrase(p Persona) map[string]string {
	t := sampleTask()
	forgotten := sampleTask()
	forgotten.Due = nil

	return map[string]string{
		"Greeting":           p.Greeting(day("2026-08-27")),
		"Overdue":            p.Overdue(t, 3),
		"StartNow":           p.StartNow(t),
		"Soon":               p.Soon(t, 2),
		"GroupIntro/context": p.GroupIntro(sampleGroup(grouping.KindContext, "@город")),
		"GroupIntro/person":  p.GroupIntro(sampleGroup(grouping.KindPerson, "Марина")),
		"GroupIntro/label":   p.GroupIntro(sampleGroup(grouping.KindLabel, "переезд")),
		"Complaint/неполные": p.Complaint(schedule.FlagIncomplete, t),
		"Complaint/переносы": p.Complaint(schedule.FlagPostponed, t),
		"Complaint/забыто":   p.Complaint(schedule.FlagForgotten, forgotten),
		"Complaint/неизв":    p.Complaint(schedule.Flag(42), t),
		"AllClear":           p.AllClear(),
		"Signoff":            p.Signoff(),
	}
}

func TestAlfredEveryMethodSpeaks(t *testing.T) {
	for name, phrase := range everyPhrase(Alfred{}) {
		if strings.TrimSpace(phrase) == "" {
			t.Errorf("%s: пустая фраза", name)
		}
		// A template with the wrong number of arguments shows up as %!s(...)
		// or %!(EXTRA ...) rather than as a failure, so it is checked here.
		if strings.Contains(phrase, "%!") || strings.Contains(phrase, "%s") {
			t.Errorf("%s: незаполненный шаблон: %q", name, phrase)
		}
		if !strings.Contains(strings.ToLower(phrase), "сэр") {
			t.Errorf("%s: Альфред обязан обращаться к сэру: %q", name, phrase)
		}
	}
}

func TestAlfredDeterministic(t *testing.T) {
	first, second := everyPhrase(Alfred{}), everyPhrase(Alfred{})
	for name, want := range first {
		if got := second[name]; got != want {
			t.Errorf("%s: две выдачи различаются:\n%q\n%q", name, want, got)
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
			t.Errorf("%s: разные экземпляры говорят по-разному:\n%q\n%q", name, want, got)
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
			name:  "Greeting по дням",
			speak: func(i int) string { return alfred.Greeting(day("2026-08-01").AddDate(0, 0, i)) },
		},
		{
			name:  "Overdue по числу дней",
			speak: func(i int) string { return alfred.Overdue(sample, i+1) },
		},
		{
			name:  "Soon по числу дней",
			speak: func(i int) string { return alfred.Soon(sample, i+1) },
		},
		{
			name: "StartNow по задачам",
			speak: func(i int) string {
				other := sample
				other.ID = "T-10" + string(rune('0'+i%10))
				return alfred.StartNow(other)
			},
		},
		{
			name: "Complaint по задачам",
			speak: func(i int) string {
				other := sample
				other.ID = "T-20" + string(rune('0'+i%10))
				return alfred.Complaint(schedule.FlagIncomplete, other)
			},
		},
		{
			name: "GroupIntro по ключам",
			speak: func(i int) string {
				return alfred.GroupIntro(sampleGroup(grouping.KindContext, "@место"+string(rune('a'+i%20))))
			},
		},
	}

	for _, c := range cases {
		distinct := make(map[string]bool)
		for i := 0; i < 20; i++ {
			distinct[c.speak(i)] = true
		}
		if len(distinct) < 2 {
			t.Errorf("%s: за 20 попыток ни одной новой формулировки", c.name)
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
		{"Greeting содержит дату", alfred.Greeting(day("2026-08-27")), []string{"27 августа"}},
		{"Overdue содержит название и срок", alfred.Overdue(sample, 3), []string{sample.Title, "3 дня"}},
		{"Overdue склоняет 1 день", alfred.Overdue(sample, 1), []string{"1 день"}},
		{"Overdue склоняет 11 дней", alfred.Overdue(sample, 11), []string{"11 дней"}},
		{"StartNow содержит название", alfred.StartNow(sample), []string{sample.Title}},
		{"Soon содержит название и срок", alfred.Soon(sample, 2), []string{sample.Title, "2 дня"}},
		{"GroupIntro содержит ключ", alfred.GroupIntro(sampleGroup(grouping.KindContext, "@город")), []string{"@город"}},
		{"GroupIntro по человеку", alfred.GroupIntro(sampleGroup(grouping.KindPerson, "Марина")), []string{"Марина"}},
		{"GroupIntro по метке", alfred.GroupIntro(sampleGroup(grouping.KindLabel, "переезд")), []string{"переезд"}},
		{"Complaint о переносах", alfred.Complaint(schedule.FlagPostponed, sample), []string{sample.Title, "2 раза"}},
		{"Complaint о забытом", alfred.Complaint(schedule.FlagForgotten, forgotten), []string{forgotten.Title, "1 августа"}},
		{"Complaint о неполных данных", alfred.Complaint(schedule.FlagIncomplete, sample), []string{sample.Title}},
	}

	for _, c := range cases {
		for _, want := range c.want {
			if !strings.Contains(c.phrase, want) {
				t.Errorf("%s: %q не содержит %q", c.name, c.phrase, want)
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
		{"известная персона", "alfred", false},
		{"регистр не важен", "Alfred", false},
		{"пробелы обрезаются", "  alfred  ", false},
		{"неизвестная персона", "batman", true},
		{"пустое имя", "", true},
		{"только пробелы", "   ", true},
	}

	for _, c := range cases {
		p, err := Get(c.persona)
		switch {
		case c.wantErr && err == nil:
			t.Errorf("%s: ожидалась ошибка, получена персона %T", c.name, p)
		case c.wantErr && !errors.Is(err, ErrUnknownPersona):
			t.Errorf("%s: ожидалась ErrUnknownPersona, получено %v", c.name, err)
		case !c.wantErr && err != nil:
			t.Errorf("%s: неожиданная ошибка %v", c.name, err)
		case !c.wantErr && p == nil:
			t.Errorf("%s: персона не создана", c.name)
		}
	}
}

func TestNames(t *testing.T) {
	names := Names()
	if len(names) == 0 {
		t.Fatal("реестр персон пуст")
	}
	found := false
	for _, n := range names {
		if n == "alfred" {
			found = true
		}
	}
	if !found {
		t.Errorf("alfred отсутствует в %v", names)
	}
}

func TestPick(t *testing.T) {
	variants := []string{"а", "б", "в"}

	if got := pick(nil, "seed"); got != "" {
		t.Errorf("pick без вариантов вернул %q", got)
	}
	if got := pick([]string{"единственный"}, "seed"); got != "единственный" {
		t.Errorf("pick одного варианта вернул %q", got)
	}
	if a, b := pick(variants, "x", "y"), pick(variants, "x", "y"); a != b {
		t.Errorf("pick недетерминирован: %q != %q", a, b)
	}
	// The separator keeps ("ab","c") from colliding with ("a","bc"); the two
	// need not differ in outcome, but they must hash differently, which the
	// distinctness over a range below exercises.
	distinct := make(map[string]bool)
	for i := 0; i < 30; i++ {
		distinct[pick(variants, "seed", string(rune('a'+i)))] = true
	}
	if len(distinct) != len(variants) {
		t.Errorf("pick покрыл %d вариантов из %d", len(distinct), len(variants))
	}
}

func TestPlural(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0 дней"},
		{1, "1 день"},
		{2, "2 дня"},
		{4, "4 дня"},
		{5, "5 дней"},
		{11, "11 дней"},
		{12, "12 дней"},
		{14, "14 дней"},
		{21, "21 день"},
		{22, "22 дня"},
		{25, "25 дней"},
		{101, "101 день"},
		{111, "111 дней"},
		{-1, "-1 день"},
	}
	for _, c := range cases {
		if got := plural(c.n, "день", "дня", "дней"); got != c.want {
			t.Errorf("plural(%d) = %q, ожидалось %q", c.n, got, c.want)
		}
	}
}

func TestRussianDate(t *testing.T) {
	cases := []struct {
		date time.Time
		want string
	}{
		{day("2026-01-01"), "1 января"},
		{day("2026-08-27"), "27 августа"},
		{day("2026-12-31"), "31 декабря"},
		{time.Time{}, "неизвестного числа"},
	}
	for _, c := range cases {
		if got := russianDate(c.date); got != c.want {
			t.Errorf("russianDate(%v) = %q, ожидалось %q", c.date, got, c.want)
		}
	}
}

func TestDueKey(t *testing.T) {
	withDue := sampleTask()
	if got := dueKey(withDue); got != "2026-09-05" {
		t.Errorf("dueKey со сроком = %q", got)
	}
	noDue := sampleTask()
	noDue.Due = nil
	if got := dueKey(noDue); got != "без-срока" {
		t.Errorf("dueKey без срока = %q", got)
	}
}

func TestGreetingIgnoresClockTime(t *testing.T) {
	alfred := Alfred{}
	morning := time.Date(2026, 8, 27, 7, 0, 0, 0, time.UTC)
	evening := time.Date(2026, 8, 27, 23, 30, 0, 0, time.UTC)
	if alfred.Greeting(morning) != alfred.Greeting(evening) {
		t.Error("приветствие меняется в течение одного дня")
	}
}
