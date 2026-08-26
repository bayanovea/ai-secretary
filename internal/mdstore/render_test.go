package mdstore

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// render is the string form of Render, which is what every assertion here wants.
func render(t *testing.T, doc *Document, now time.Time) string {
	t.Helper()
	var buf strings.Builder
	if err := Render(&buf, doc, now, nil); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	return buf.String()
}

func parseString(t *testing.T, s string) *Document {
	t.Helper()
	doc, err := Parse(strings.NewReader(s))
	if err != nil {
		t.Fatalf("Parse() error = %v\n--- вход ---\n%s", err, s)
	}
	return doc
}

func TestRenderGolden(t *testing.T) {
	f, err := os.Open("testdata/sample.md")
	if err != nil {
		t.Fatalf("open sample: %v", err)
	}
	defer f.Close()

	doc, err := Parse(f)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	assertGolden(t, "basic.md", render(t, doc, date(t, "2026-08-27")))
}

// TestRoundTrip is the property the whole artifact design rests on: rewriting a
// file the user has edited by hand must not change what it says.
func TestRoundTrip(t *testing.T) {
	data, err := os.ReadFile("testdata/sample.md")
	if err != nil {
		t.Fatalf("read sample: %v", err)
	}

	first := parseString(t, string(data))
	second := parseString(t, render(t, first, date(t, "2026-08-27")))

	if !reflect.DeepEqual(second, first) {
		t.Errorf("parse(render(parse(x))) != parse(x)\n--- получено ---\n%#v\n--- ожидалось ---\n%#v", second, first)
	}
}

// TestRoundTripUnusualSpacing feeds Render a document parsed from a file nobody
// would generate: ragged indentation, extra blank lines, a deeper heading and
// fields spaced by hand.
func TestRoundTripUnusualSpacing(t *testing.T) {
	const messy = "# Обязанности\n" +
		"\n" +
		"\n" +
		"Читайте сверху вниз.\n" +
		"\n" +
		"###   Работа\n" +
		"-  [ ]   `T-001`   **Подготовить отчёт**\n" +
		"      тир:1   ·   от:Анна Петрова·срок:2026-09-05 ·объём:5д·   контекст:@компьютер\n" +
		"   метки:   отчётность ,  финансы ,\n" +
		"\t> нужны данные от финотдела\n" +
		"🔴 начать не позже 2026-08-27\n" +
		"\n" +
		"\n" +
		"- [x] `T-000` **Оплатить страховку**  ·  выполнено:2026-08-20\n" +
		"\n"

	first := parseString(t, messy)
	second := parseString(t, render(t, first, date(t, "2026-08-27")))

	if !reflect.DeepEqual(second, first) {
		t.Errorf("нестандартное форматирование не пережило round-trip\n--- получено ---\n%#v\n--- ожидалось ---\n%#v", second, first)
	}
}

// TestRenderIdempotent checks that rendering twice at the same `now` is stable
// at the byte level — the guarantee `secretary fmt` needs.
func TestRenderIdempotent(t *testing.T) {
	data, err := os.ReadFile("testdata/sample.md")
	if err != nil {
		t.Fatalf("read sample: %v", err)
	}

	now := date(t, "2026-08-27")
	once := render(t, parseString(t, string(data)), now)
	twice := render(t, parseString(t, once), now)

	if once != twice {
		t.Errorf("повторный рендер изменил файл\n--- первый ---\n%s\n--- второй ---\n%s", once, twice)
	}
}

func TestRenderOmitsAbsentFields(t *testing.T) {
	doc := parseString(t, "# Обязанности\n\n## Работа\n\n- [ ] `T-004` **Согласовать отпуск**\n  тир:2 · от:себя\n")
	got := render(t, doc, date(t, "2026-08-27"))

	for _, key := range []string{keyDue, keyEffort, keyContext, keyDeferrals, keyCreated, labelsPrefix} {
		if strings.Contains(got, key+":") {
			t.Errorf("отсутствующее поле %q попало в вывод:\n%s", key, got)
		}
	}
	if want := "  " + keyTier + ":2 · " + keyFrom + ":себя\n"; !strings.Contains(got, want) {
		t.Errorf("строка полей = %q, ожидалась подстрока %q", got, want)
	}
}

func TestRenderMarkerAndUpdatedLine(t *testing.T) {
	doc := parseString(t, "# Обязанности\n\n## Работа\n")
	got := render(t, doc, date(t, "2026-08-27"))

	if !strings.Contains(got, Marker) {
		t.Errorf("нет маркера %q в выводе:\n%s", Marker, got)
	}
	if want := updatedPrefix + " 2026-08-27 · " + greeting; !strings.Contains(got, want) {
		t.Errorf("нет строки %q в выводе:\n%s", want, got)
	}
}

// TestRenderHintLines pins the stub status mapping the schedule package will
// replace: red once the latest start date has arrived, a clock while it is
// within the soon window, and nothing at all while there is time.
func TestRenderHintLines(t *testing.T) {
	tests := []struct {
		name   string
		fields string
		now    string
		want   string
	}{
		{
			name:   "пора начинать",
			fields: "тир:1 · от:себя · срок:2026-09-05 · объём:5д",
			now:    "2026-08-29",
			want:   "🔴 начать не позже 2026-08-28",
		},
		{
			name:   "скоро",
			fields: "тир:2 · от:себя · срок:2026-09-05 · объём:1д",
			now:    "2026-09-01",
			want:   "⏰ начать не позже 2026-09-03",
		},
		{
			name:   "спокойно",
			fields: "тир:3 · от:себя · срок:2026-12-31 · объём:1д",
			now:    "2026-08-27",
			want:   "",
		},
		{
			name:   "без срока",
			fields: "тир:1 · от:себя · объём:5д",
			now:    "2026-08-27",
			want:   "",
		},
		{
			name:   "без объёма",
			fields: "тир:1 · от:себя · срок:2026-08-28",
			now:    "2026-08-27",
			want:   "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			doc := parseString(t, "# Обязанности\n\n## Работа\n\n- [ ] `T-001` **Задача**\n  "+tt.fields+"\n")
			got := render(t, doc, date(t, tt.now))

			hint := hintOf(got)
			if hint != tt.want {
				t.Errorf("подсказка = %q, ожидалась %q\n--- вывод ---\n%s", hint, tt.want, got)
			}
		})
	}
}

func TestRenderNilDocument(t *testing.T) {
	var buf strings.Builder
	if err := Render(&buf, nil, time.Time{}, nil); err == nil {
		t.Error("Render(nil) error = nil, ожидалась ошибка")
	}
}

// hintOf returns the generated glyph line of a rendered artifact, or "".
func hintOf(rendered string) string {
	for _, line := range strings.Split(rendered, "\n") {
		if line = strings.TrimSpace(line); isGlyphLine(line) {
			return line
		}
	}
	return ""
}
