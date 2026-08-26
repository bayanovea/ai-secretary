package schedule

import (
	"testing"
	"time"

	"github.com/bayanovea/secretary/internal/config"
	"github.com/bayanovea/secretary/internal/task"
)

// date parses a `YYYY-MM-DD` test fixture.
func date(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("дата %q: %v", s, err)
	}
	return d
}

// due is the pointer form the Task field wants.
func due(t *testing.T, s string) *time.Time {
	t.Helper()
	d := date(t, s)
	return &d
}

// effort parses an artifact-form объём.
func effort(t *testing.T, s string) task.Effort {
	t.Helper()
	e, err := task.ParseEffort(s)
	if err != nil {
		t.Fatalf("объём %q: %v", s, err)
	}
	return e
}

func TestLatestStart(t *testing.T) {
	tests := []struct {
		name   string
		tier   int
		dueOn  string
		effort string
		want   string // "" means nil
	}{
		// тир 1: buffer = max(2д, 50% объёма).
		{name: "тир 1, минимальный буфер больше доли", tier: 1, dueOn: "2026-09-10", effort: "1д", want: "2026-09-07"},
		{name: "тир 1, доля больше минимума", tier: 1, dueOn: "2026-09-10", effort: "10д", want: "2026-08-26"},
		{name: "тир 1, доля ровно равна минимуму", tier: 1, dueOn: "2026-09-10", effort: "4д", want: "2026-09-04"},
		// тир 2: buffer = max(1д, 25% объёма).
		{name: "тир 2, минимальный буфер больше доли", tier: 2, dueOn: "2026-09-10", effort: "2д", want: "2026-09-07"},
		{name: "тир 2, доля больше минимума", tier: 2, dueOn: "2026-09-10", effort: "8д", want: "2026-08-31"},
		// тир 3: буфера нет вовсе.
		{name: "тир 3, без буфера", tier: 3, dueOn: "2026-09-10", effort: "3д", want: "2026-09-07"},
		{name: "тир 3, час работы занимает день календаря", tier: 3, dueOn: "2026-09-10", effort: "1ч", want: "2026-09-09"},
		// Часовые объёмы округляются вверх вместе с буфером.
		{name: "тир 1, час работы", tier: 1, dueOn: "2026-08-29", effort: "1ч", want: "2026-08-26"},
		{name: "тир 2, четыре часа", tier: 2, dueOn: "2026-09-10", effort: "4ч", want: "2026-09-08"},
		// Недели считаются как 7 дней.
		{name: "тир 2, неделя", tier: 2, dueOn: "2026-09-10", effort: "1н", want: "2026-09-01"},
	}

	cfg := config.Default()
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			tk := task.Task{ID: "T-001", Title: "Задача", From: "себя", Tier: tt.tier,
				Due: due(t, tt.dueOn), Effort: effort(t, tt.effort)}

			got := LatestStart(tk, cfg)
			if got == nil {
				t.Fatalf("LatestStart() = nil, ожидалось %s", tt.want)
			}
			if formatted := got.Format("2006-01-02"); formatted != tt.want {
				t.Errorf("LatestStart() = %s, ожидалось %s", formatted, tt.want)
			}
		})
	}
}

func TestLatestStartAbsent(t *testing.T) {
	tests := []struct {
		name string
		tk   task.Task
	}{
		{name: "без срока", tk: task.Task{Tier: 1, Effort: effort(t, "5д")}},
		{name: "без объёма", tk: task.Task{Tier: 1, Due: due(t, "2026-09-10")}},
		{name: "без срока и объёма", tk: task.Task{Tier: 1}},
		{name: "нулевой объём", tk: task.Task{Tier: 1, Due: due(t, "2026-09-10"), Effort: effort(t, "0ч")}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := LatestStart(tt.tk, config.Default()); got != nil {
				t.Errorf("LatestStart() = %s, ожидался nil", got.Format("2006-01-02"))
			}
		})
	}
}

// TestLatestStartNilConfig checks the documented fallback to the defaults.
func TestLatestStartNilConfig(t *testing.T) {
	tk := task.Task{Tier: 1, Due: due(t, "2026-09-10"), Effort: effort(t, "1д")}

	got, want := LatestStart(tk, nil), LatestStart(tk, config.Default())
	if got == nil || want == nil || !got.Equal(*want) {
		t.Errorf("LatestStart(nil cfg) = %v, ожидалось %v", got, want)
	}
}

func TestStatus(t *testing.T) {
	tests := []struct {
		name string
		tk   task.Task
		now  string
		want task.Status
	}{
		{
			name: "без срока",
			tk:   task.Task{Tier: 2, Effort: effort(t, "1д")},
			now:  "2026-08-27",
			want: task.StatusNoDue,
		},
		{
			name: "просрочено",
			tk:   task.Task{Tier: 1, Due: due(t, "2026-08-26"), Effort: effort(t, "1д")},
			now:  "2026-08-27",
			want: task.StatusOverdue,
		},
		{
			name: "срок ровно сегодня — ещё не просрочено",
			tk:   task.Task{Tier: 1, Due: due(t, "2026-08-27"), Effort: effort(t, "1д")},
			now:  "2026-08-27",
			want: task.StatusStartNow,
		},
		{
			name: "день старта ровно сегодня",
			// объём 1д, тир 1 ⇒ buffer 2д ⇒ старт 2026-08-27.
			tk:   task.Task{Tier: 1, Due: due(t, "2026-08-30"), Effort: effort(t, "1д")},
			now:  "2026-08-27",
			want: task.StatusStartNow,
		},
		{
			name: "день старта завтра — скоро",
			tk:   task.Task{Tier: 1, Due: due(t, "2026-08-31"), Effort: effort(t, "1д")},
			now:  "2026-08-27",
			want: task.StatusSoon,
		},
		{
			name: "день старта на границе окна",
			// старт 2026-08-30 = сегодня + 3 = soon_window_days.
			tk:   task.Task{Tier: 1, Due: due(t, "2026-09-02"), Effort: effort(t, "1д")},
			now:  "2026-08-27",
			want: task.StatusSoon,
		},
		{
			name: "день старта на день позже окна — спокойно",
			tk:   task.Task{Tier: 1, Due: due(t, "2026-09-03"), Effort: effort(t, "1д")},
			now:  "2026-08-27",
			want: task.StatusCalm,
		},
		{
			name: "спокойно, далёкий срок",
			tk:   task.Task{Tier: 3, Due: due(t, "2026-12-31"), Effort: effort(t, "1д")},
			now:  "2026-08-27",
			want: task.StatusCalm,
		},
		{
			name: "без объёма: срок заменяет день старта",
			tk:   task.Task{Tier: 2, Due: due(t, "2026-08-29")},
			now:  "2026-08-27",
			want: task.StatusSoon,
		},
		{
			name: "без объёма: срок сегодня",
			tk:   task.Task{Tier: 2, Due: due(t, "2026-08-27")},
			now:  "2026-08-27",
			want: task.StatusStartNow,
		},
		{
			name: "выполненная задача спокойна, даже с прошедшим сроком",
			tk:   task.Task{Tier: 1, Due: due(t, "2026-01-01"), Effort: effort(t, "1д"), Done: true},
			now:  "2026-08-27",
			want: task.StatusCalm,
		},
		{
			name: "время суток не влияет на сравнение дней",
			tk:   task.Task{Tier: 3, Due: due(t, "2026-08-27")},
			now:  "2026-08-27",
			want: task.StatusStartNow,
		},
	}

	cfg := config.Default()
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			now := date(t, tt.now).Add(23 * time.Hour)
			if got := Status(tt.tk, now, cfg); got != tt.want {
				t.Errorf("Status() = %v (%s), ожидалось %v (%s)", got, got, tt.want, tt.want)
			}
		})
	}
}

func TestFlags(t *testing.T) {
	const now = "2026-08-27"
	fresh := date(t, "2026-08-20")
	stale := date(t, "2026-08-01")

	tests := []struct {
		name string
		tk   task.Task
		want []Flag
	}{
		{
			name: "полные данные — ни одного флага",
			tk:   task.Task{Tier: 1, Due: due(t, "2026-09-10"), Effort: effort(t, "1д"), Created: fresh},
			want: nil,
		},
		{
			name: "тир 1 без срока",
			tk:   task.Task{Tier: 1, Effort: effort(t, "1д"), Created: fresh},
			want: []Flag{FlagIncomplete},
		},
		{
			name: "тир 1 без объёма",
			tk:   task.Task{Tier: 1, Due: due(t, "2026-09-10"), Created: fresh},
			want: []Flag{FlagIncomplete},
		},
		{
			name: "тир 2 без объёма — не наш случай",
			tk:   task.Task{Tier: 2, Due: due(t, "2026-09-10"), Created: fresh},
			want: nil,
		},
		{
			name: "два переноса",
			tk:   task.Task{Tier: 2, Due: due(t, "2026-09-10"), Effort: effort(t, "1д"), Deferrals: 2, Created: fresh},
			want: []Flag{FlagPostponed},
		},
		{
			name: "один перенос — молчим",
			tk:   task.Task{Tier: 2, Due: due(t, "2026-09-10"), Effort: effort(t, "1д"), Deferrals: 1, Created: fresh},
			want: nil,
		},
		{
			name: "тир 2 без срока, залежалась",
			tk:   task.Task{Tier: 2, Effort: effort(t, "1д"), Created: stale},
			want: []Flag{FlagForgotten},
		},
		{
			name: "тир 3 без срока, залежалась — фоновому можно",
			tk:   task.Task{Tier: 3, Effort: effort(t, "1д"), Created: stale},
			want: nil,
		},
		{
			name: "залежалась, но срок есть — не забыта",
			tk:   task.Task{Tier: 2, Due: due(t, "2026-09-10"), Effort: effort(t, "1д"), Created: stale},
			want: nil,
		},
		{
			name: "ровно stale_days от роду — ещё не забыта",
			tk:   task.Task{Tier: 2, Effort: effort(t, "1д"), Created: date(t, "2026-08-13")},
			want: nil,
		},
		{
			name: "без даты создания забытой не считаем",
			tk:   task.Task{Tier: 2, Effort: effort(t, "1д")},
			want: nil,
		},
		{
			name: "все три сразу, в порядке констант",
			tk:   task.Task{Tier: 1, Deferrals: 3, Created: stale},
			want: []Flag{FlagIncomplete, FlagPostponed, FlagForgotten},
		},
		{
			name: "выполненная задача молчит",
			tk:   task.Task{Tier: 1, Deferrals: 3, Created: stale, Done: true},
			want: nil,
		},
	}

	cfg := config.Default()
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := Flags(tt.tk, date(t, now), cfg)
			if len(got) != len(tt.want) {
				t.Fatalf("Flags() = %v, ожидалось %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("Flags() = %v, ожидалось %v", got, tt.want)
				}
			}
		})
	}
}

func TestHas(t *testing.T) {
	flags := []Flag{FlagIncomplete, FlagForgotten}
	if !Has(flags, FlagForgotten) {
		t.Error("Has(забыто) = false, ожидалось true")
	}
	if Has(flags, FlagPostponed) {
		t.Error("Has(откладывается) = true, ожидалось false")
	}
	if Has(nil, FlagIncomplete) {
		t.Error("Has(nil) = true, ожидалось false")
	}
}

func TestFlagString(t *testing.T) {
	tests := []struct {
		flag Flag
		want string
	}{
		{FlagIncomplete, "неполные данные"},
		{FlagPostponed, "откладывается"},
		{FlagForgotten, "забыто"},
		{Flag(42), "неизвестно"},
	}

	for _, tt := range tests {
		if got := tt.flag.String(); got != tt.want {
			t.Errorf("Flag(%d).String() = %q, ожидалось %q", tt.flag, got, tt.want)
		}
	}
}

// TestSeverityOrder is what the sort order depends on: louder statuses rank
// strictly higher.
func TestSeverityOrder(t *testing.T) {
	order := []task.Status{
		task.StatusNoDue,
		task.StatusCalm,
		task.StatusSoon,
		task.StatusStartNow,
		task.StatusOverdue,
	}

	for i := 1; i < len(order); i++ {
		if Severity(order[i]) <= Severity(order[i-1]) {
			t.Errorf("Severity(%s) = %d не больше Severity(%s) = %d",
				order[i], Severity(order[i]), order[i-1], Severity(order[i-1]))
		}
	}
}
