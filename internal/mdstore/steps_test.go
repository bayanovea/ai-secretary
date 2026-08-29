package mdstore

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/bayanovea/secretary/internal/task"
)

const withSteps = `# Обязанности

## Личное

- [ ] ` + "`T-001`" + ` **Разобраться со складом**
  тир:2 · от:себя · объём:2ч · контекст:@город · создано:2026-08-27
  > условия хранения
  - [x] позвонить
  - [ ] отвезти вещи
`

func TestParseSteps(t *testing.T) {
	doc, err := Parse(strings.NewReader(withSteps))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	got := doc.Sections[0].Tasks[0].Steps
	want := []task.Step{{Text: "позвонить", Done: true}, {Text: "отвезти вещи"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Steps = %+v, want %+v", got, want)
	}
}

// A step must survive parse → render → parse unchanged, glyph lines and all:
// the checkbox is input, not decoration.
func TestStepsRoundTrip(t *testing.T) {
	doc, err := Parse(strings.NewReader(withSteps))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	var buf bytes.Buffer
	if err := Render(&buf, doc, date(t, "2026-08-29"), nil); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	again, err := Parse(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("повторный Parse() error = %v\n%s", err, buf.String())
	}
	if !reflect.DeepEqual(doc, again) {
		t.Errorf("round-trip разошёлся:\n%s", buf.String())
	}
}

func TestStepMutations(t *testing.T) {
	doc, err := Parse(strings.NewReader(withSteps))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if err := doc.AddStep("T-001", "  забрать ключ  ", ""); err != nil {
		t.Fatalf("AddStep() error = %v", err)
	}
	if err := doc.SetStep("T-001", 2, true); err != nil {
		t.Fatalf("SetStep() error = %v", err)
	}
	got, _ := doc.Find("T-001")
	if want := []task.Step{{Text: "позвонить", Done: true}, {Text: "отвезти вещи", Done: true}, {Text: "забрать ключ"}}; !reflect.DeepEqual(got.Steps, want) {
		t.Fatalf("Steps = %+v, want %+v", got.Steps, want)
	}
	if got.Progress() != "2/3" {
		t.Errorf("Progress() = %q, want 2/3", got.Progress())
	}

	dropped, err := doc.DropStep("T-001", 1)
	if err != nil {
		t.Fatalf("DropStep() error = %v", err)
	}
	if dropped.Text != "позвонить" {
		t.Errorf("DropStep() = %q", dropped.Text)
	}
	if got, _ := doc.Find("T-001"); len(got.Steps) != 2 {
		t.Errorf("после DropStep осталось %d подпунктов", len(got.Steps))
	}

	if err := doc.SetStep("T-001", 9, true); err == nil {
		t.Error("SetStep() вне диапазона: ожидалась ошибка")
	}
	if err := doc.AddStep("T-404", "что-нибудь"); err == nil {
		t.Error("AddStep() к несуществующей задаче: ожидалась ошибка")
	}
}

func TestParseRejectsEmptyStep(t *testing.T) {
	broken := strings.Replace(withSteps, "- [x] позвонить", "- [ ]", 1)
	if _, err := Parse(strings.NewReader(broken)); err == nil {
		t.Error("подпункт без текста: ожидалась ошибка")
	}
}

const withStepFields = `# Обязанности

## Личное

- [ ] ` + "`T-001`" + ` **Разобраться со складом**
  тир:2 · от:себя · срок:2026-09-17 · объём:2ч · контекст:@город · создано:2026-08-27
  - [ ] позвонить · тир:1 · срок:2026-09-01
  - [x] отвезти вещи · срок:2026-09-16
  - [ ] сверить опись · вещи, которые не едут
`

// A step carries its own urgency, and the two duties inside one errand rarely
// share a date: the call that opens it is due long before the errand is.
func TestParseStepFields(t *testing.T) {
	doc, err := Parse(strings.NewReader(withStepFields))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	first, second := date(t, "2026-09-01"), date(t, "2026-09-16")
	want := []task.Step{
		{Text: "позвонить", Tier: 1, Due: &first},
		{Text: "отвезти вещи", Done: true, Due: &second},
		// A `·` that is not a known key stays in the text.
		{Text: "сверить опись · вещи, которые не едут"},
	}
	if got := doc.Sections[0].Tasks[0].Steps; !reflect.DeepEqual(got, want) {
		t.Errorf("Steps = %+v, want %+v", got, want)
	}
}

func TestStepFieldsRoundTrip(t *testing.T) {
	doc, err := Parse(strings.NewReader(withStepFields))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	var buf bytes.Buffer
	if err := Render(&buf, doc, date(t, "2026-08-29"), nil); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	again, err := Parse(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("повторный Parse() error = %v\n%s", err, buf.String())
	}
	if !reflect.DeepEqual(doc, again) {
		t.Errorf("round-trip разошёлся:\n%s", buf.String())
	}
}

func TestSetStepFields(t *testing.T) {
	doc, err := Parse(strings.NewReader(withStepFields))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if err := doc.SetStepFields("T-001", 1, StepFields{ClearTier: true, ClearDue: true}); err != nil {
		t.Fatalf("SetStepFields() error = %v", err)
	}
	if s := doc.Sections[0].Tasks[0].Steps[0]; s.Tier != 0 || s.Due != nil {
		t.Errorf("подпункт не очищен: %+v", s)
	}

	due := date(t, "2026-09-20")
	if err := doc.SetStepFields("T-001", 3, StepFields{Tier: 2, Due: &due}); err != nil {
		t.Fatalf("SetStepFields() error = %v", err)
	}
	if s := doc.Sections[0].Tasks[0].Steps[2]; s.Tier != 2 || s.Due == nil || !s.Due.Equal(due) {
		t.Errorf("подпункт не изменён: %+v", s)
	}

	if err := doc.SetStepFields("T-001", 9, StepFields{Tier: 1}); !errors.Is(err, ErrUnknownStep) {
		t.Errorf("SetStepFields(9) = %v, ожидался ErrUnknownStep", err)
	}
}

func TestParseRejectsBadStepFields(t *testing.T) {
	for _, line := range []string{"  - [ ] позвонить · тир:9", "  - [ ] позвонить · срок:вчера", "  - [ ] · срок:2026-09-01"} {
		src := strings.Replace(withSteps, "  - [x] позвонить", line, 1)
		if _, err := Parse(strings.NewReader(src)); err == nil {
			t.Errorf("Parse(%q) прошёл без ошибки", line)
		}
	}
}
