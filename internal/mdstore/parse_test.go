package mdstore

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bayanovea/secretary/internal/task"
)

func date(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse(DateLayout, s)
	if err != nil {
		t.Fatalf("некорректная дата в тесте %q: %v", s, err)
	}
	return d
}

func ptr(t time.Time) *time.Time { return &t }

func TestParseSample(t *testing.T) {
	f, err := os.Open("testdata/sample.md")
	if err != nil {
		t.Fatalf("open sample: %v", err)
	}
	defer f.Close()

	doc, err := Parse(f)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	want := &Document{
		Preamble: []string{"# Обязанности"},
		Sections: []Section{
			{
				Name: "Работа",
				Tasks: []task.Task{
					{
						ID:        "T-001",
						Title:     "Подготовить отчёт за Q3",
						Section:   "Работа",
						Tier:      1,
						From:      "Анна Петрова",
						Due:       ptr(date(t, "2026-09-05")),
						Effort:    task.Effort(5),
						Context:   "@компьютер",
						Labels:    []string{"отчётность", "финансы"},
						Notes:     []string{"нужны данные от финотдела", "они обычно тянут до последнего"},
						Deferrals: 1,
						Created:   date(t, "2026-08-20"),
					},
					{
						ID:      "T-004",
						Title:   "Согласовать отпуск",
						Section: "Работа",
						Tier:    2,
						From:    task.SelfAssigned,
						Created: date(t, "2026-08-25"),
					},
				},
			},
			{
				Name: "Личное",
				Tasks: []task.Task{
					{
						ID:      "T-002",
						Title:   "Забрать документы у нотариуса",
						Section: "Личное",
						Tier:    1,
						From:    "Марина",
						Due:     ptr(date(t, "2026-08-29")),
						Effort:  task.Effort(1.0 / task.HoursPerDay),
						Context: "@город",
						Labels:  []string{"переезд", "документы"},
						Created: date(t, "2026-08-26"),
					},
				},
			},
			{
				Name: "Сделано",
				Tasks: []task.Task{
					{
						ID:          "T-000",
						Title:       "Оплатить страховку",
						Section:     "Сделано",
						Done:        true,
						CompletedAt: ptr(date(t, "2026-08-20")),
					},
				},
			},
		},
	}

	if !reflect.DeepEqual(doc, want) {
		t.Errorf("Parse(sample.md) mismatch\n got: %s\nwant: %s", dump(doc), dump(want))
	}
}

// dump renders a document compactly so a failing DeepEqual says what differs.
func dump(d *Document) string {
	var b strings.Builder
	b.WriteString("\npreamble=")
	b.WriteString(strings.Join(d.Preamble, "|"))
	for _, s := range d.Sections {
		b.WriteString("\n## " + s.Name)
		for _, t := range s.Tasks {
			due := "-"
			if t.Due != nil {
				due = t.Due.Format(DateLayout)
			}
			done := "-"
			if t.CompletedAt != nil {
				done = t.CompletedAt.Format(DateLayout)
			}
			b.WriteString("\n  " + t.ID + " " + t.Title +
				" тир=" + itoa(t.Tier) + " от=" + t.From + " срок=" + due +
				" объём=" + t.Effort.String() + " контекст=" + t.Context +
				" метки=" + strings.Join(t.Labels, ",") +
				" заметки=" + strings.Join(t.Notes, "/") +
				" переносов=" + itoa(t.Deferrals) +
				" создано=" + t.Created.Format(DateLayout) +
				" готово=" + done)
		}
	}
	return b.String()
}

func itoa(n int) string { return string(rune('0' + n%10)) }

func TestParseDropsGeneratedLines(t *testing.T) {
	doc := mustParse(t, `# Обязанности

<!-- secretary:v1 -->
> Обновлено: 2026-08-27 · К вашим услугам, сэр.

## Работа

- [ ] `+"`T-001`"+` **Отчёт**
  тир:1 · от:себя · срок:2026-09-05 · объём:1д
  🔴 начать не позже 2026-09-02
  ⏰ скоро
  🟡 внимание
  💤 забыто
`)
	if got := len(doc.Preamble); got != 1 {
		t.Fatalf("preamble = %q, want only the heading", doc.Preamble)
	}
	if got := doc.Sections[0].Tasks[0].Notes; got != nil {
		t.Errorf("глифы попали в заметки: %q", got)
	}
}

func TestParseIgnoresUnknownKeys(t *testing.T) {
	doc := mustParse(t, "## Работа\n\n- [ ] `T-001` **Отчёт**\n  тир:2 · от:себя · настроение:бодрое · нечто:1\n")
	got := doc.Sections[0].Tasks[0]
	if got.Tier != 2 || got.From != task.SelfAssigned {
		t.Errorf("известные поля потеряны: %+v", got)
	}
}

func TestParseTolerantSpacing(t *testing.T) {
	doc := mustParse(t, "##   Работа\n\n-  [ ]   `T-001`   **Отчёт**\n   тир:1  ·  от:Марина  ·  срок:2026-09-05\n   метки:  переезд ,  документы ,\n")
	if len(doc.Sections) != 1 || doc.Sections[0].Name != "Работа" {
		t.Fatalf("разделы = %+v", doc.Sections)
	}
	got := doc.Sections[0].Tasks[0]
	if got.From != "Марина" || got.Due == nil {
		t.Errorf("поля = %+v", got)
	}
	if want := []string{"переезд", "документы"}; !reflect.DeepEqual(got.Labels, want) {
		t.Errorf("метки = %q, want %q", got.Labels, want)
	}
}

func TestParseEmptyInput(t *testing.T) {
	doc := mustParse(t, "")
	if len(doc.Sections) != 0 || doc.Preamble != nil {
		t.Errorf("пустой ввод дал %+v", doc)
	}
}

func TestParseSectionWithoutTasks(t *testing.T) {
	doc := mustParse(t, "# Обязанности\n\n## Работа\n\n## Личное\n")
	if len(doc.Sections) != 2 || len(doc.Sections[0].Tasks) != 0 {
		t.Errorf("разделы = %+v", doc.Sections)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantLine int
		wantMsg  string
	}{
		{
			name:     "тир вне диапазона",
			input:    "## Работа\n\n- [ ] `T-001` **Отчёт**\n  тир:5 · от:себя\n",
			wantLine: 4,
			wantMsg:  "тир 5",
		},
		{
			name:     "тир не число",
			input:    "## Работа\n\n- [ ] `T-001` **Отчёт**\n  тир:высокий · от:себя\n",
			wantLine: 4,
			wantMsg:  "не число",
		},
		{
			name:     "некорректная дата",
			input:    "## Работа\n\n- [ ] `T-001` **Отчёт**\n  тир:1 · от:себя · срок:31-12-2026\n",
			wantLine: 4,
			wantMsg:  "ГГГГ-ММ-ДД",
		},
		{
			name:     "некорректный объём",
			input:    "## Работа\n\n- [ ] `T-001` **Отчёт**\n  тир:1 · от:себя · объём:пять\n",
			wantLine: 4,
			wantMsg:  "объём",
		},
		{
			name:     "поле без значения",
			input:    "## Работа\n\n- [ ] `T-001` **Отчёт**\n  тир:1 · какая-то строка\n",
			wantLine: 4,
			wantMsg:  "ключ:значение",
		},
		{
			name:     "задача вне раздела",
			input:    "# Обязанности\n\n- [ ] `T-001` **Отчёт**\n  тир:1 · от:себя\n",
			wantLine: 3,
			wantMsg:  "вне раздела",
		},
		{
			name:     "повторный идентификатор",
			input:    "## Работа\n\n- [ ] `T-001` **Отчёт**\n  тир:1 · от:себя\n\n- [ ] `T-001` **Второй**\n  тир:1 · от:себя\n",
			wantLine: 6,
			wantMsg:  "повторный идентификатор",
		},
		{
			name:     "строка вне задачи",
			input:    "## Работа\n\n  тир:1 · от:себя\n",
			wantLine: 3,
			wantMsg:  "вне задачи",
		},
		{
			name:     "некорректный идентификатор",
			input:    "## Работа\n\n- [ ] `X1` **Отчёт**\n  тир:1 · от:себя\n",
			wantLine: 3,
			wantMsg:  "идентификатор",
		},
		{
			name:     "нет тира",
			input:    "## Работа\n\n- [ ] `T-001` **Отчёт**\n  от:себя\n",
			wantLine: 3,
			wantMsg:  "тир",
		},
		{
			name:     "нет от",
			input:    "## Работа\n\n- [ ] `T-001` **Отчёт**\n  тир:1\n",
			wantLine: 3,
			wantMsg:  "от кого",
		},
		{
			name:     "переносов не число",
			input:    "## Работа\n\n- [ ] `T-001` **Отчёт**\n  тир:1 · от:себя · переносов:дважды\n",
			wantLine: 4,
			wantMsg:  "переносов",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tc.input))
			if err == nil {
				t.Fatalf("Parse() = nil error, want %q", tc.wantMsg)
			}
			perr, ok := err.(*Error)
			if !ok {
				t.Fatalf("Parse() error type = %T, want *Error", err)
			}
			if perr.Line != tc.wantLine {
				t.Errorf("строка = %d, want %d (%v)", perr.Line, tc.wantLine, perr)
			}
			if !strings.Contains(perr.Error(), tc.wantMsg) {
				t.Errorf("сообщение = %q, want содержащее %q", perr.Error(), tc.wantMsg)
			}
		})
	}
}

func mustParse(t *testing.T, input string) *Document {
	t.Helper()
	doc, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return doc
}
