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
		t.Fatalf("bad date in test %q: %v", s, err)
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
		Preamble: []string{"# Duties"},
		Sections: []Section{
			{
				Name: "Work",
				Tasks: []task.Task{
					{
						ID:        "T-001",
						Title:     "Prepare the Q3 report",
						Section:   "Work",
						Tier:      1,
						From:      "Dana Whitfield",
						Due:       ptr(date(t, "2026-09-05")),
						Effort:    task.Effort(5),
						Context:   "@computer",
						Labels:    []string{"reporting", "finance"},
						Notes:     []string{"needs the numbers from accounting", "they are always late"},
						Deferrals: 1,
						Created:   date(t, "2026-08-20"),
					},
					{
						ID:      "T-004",
						Title:   "Get the vacation approved",
						Section: "Work",
						Tier:    2,
						From:    task.SelfAssigned,
						Created: date(t, "2026-08-25"),
					},
				},
			},
			{
				Name: "Personal",
				Tasks: []task.Task{
					{
						ID:      "T-002",
						Title:   "Collect the documents from the notary",
						Section: "Personal",
						Tier:    1,
						From:    "Helen",
						Due:     ptr(date(t, "2026-08-29")),
						Effort:  task.Effort(1.0 / task.HoursPerDay),
						Context: "@town",
						Labels:  []string{"paperwork", "documents"},
						Created: date(t, "2026-08-26"),
					},
				},
			},
			{
				Name: "Done",
				Tasks: []task.Task{
					{
						ID:          "T-000",
						Title:       "Pay the insurance",
						Section:     "Done",
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
				" tier=" + itoa(t.Tier) + " from=" + t.From + " due=" + due +
				" effort=" + t.Effort.String() + " context=" + t.Context +
				" labels=" + strings.Join(t.Labels, ",") +
				" notes=" + strings.Join(t.Notes, "/") +
				" deferrals=" + itoa(t.Deferrals) +
				" created=" + t.Created.Format(DateLayout) +
				" done=" + done)
		}
	}
	return b.String()
}

func itoa(n int) string { return string(rune('0' + n%10)) }

func TestParseDropsGeneratedLines(t *testing.T) {
	doc := mustParse(t, `# Duties

<!-- secretary:v1 -->
> Updated: 2026-08-27 · At your service, sir.

## Work

- [ ] `+"`T-001`"+` **Report**
  tier:1 · from:self · due:2026-09-05 · effort:1d
  🔴 start by 2026-09-02
  ⏰ soon
  🟡 attention
  💤 stale
`)
	if got := len(doc.Preamble); got != 1 {
		t.Fatalf("preamble = %q, want only the heading", doc.Preamble)
	}
	if got := doc.Sections[0].Tasks[0].Notes; got != nil {
		t.Errorf("glyph lines leaked into notes: %q", got)
	}
}

func TestParseIgnoresUnknownKeys(t *testing.T) {
	doc := mustParse(t, "## Work\n\n- [ ] `T-001` **Report**\n  tier:2 · from:self · mood:cheerful · thing:1\n")
	got := doc.Sections[0].Tasks[0]
	if got.Tier != 2 || got.From != task.SelfAssigned {
		t.Errorf("known fields lost: %+v", got)
	}
}

func TestParseTolerantSpacing(t *testing.T) {
	doc := mustParse(t, "##   Work\n\n-  [ ]   `T-001`   **Report**\n   tier:1  ·  from:Helen  ·  due:2026-09-05\n   labels:  paperwork ,  documents ,\n")
	if len(doc.Sections) != 1 || doc.Sections[0].Name != "Work" {
		t.Fatalf("sections = %+v", doc.Sections)
	}
	got := doc.Sections[0].Tasks[0]
	if got.From != "Helen" || got.Due == nil {
		t.Errorf("fields = %+v", got)
	}
	if want := []string{"paperwork", "documents"}; !reflect.DeepEqual(got.Labels, want) {
		t.Errorf("labels = %q, want %q", got.Labels, want)
	}
}

func TestParseEmptyInput(t *testing.T) {
	doc := mustParse(t, "")
	if len(doc.Sections) != 0 || doc.Preamble != nil {
		t.Errorf("empty input gave %+v", doc)
	}
}

func TestParseSectionWithoutTasks(t *testing.T) {
	doc := mustParse(t, "# Duties\n\n## Work\n\n## Personal\n")
	if len(doc.Sections) != 2 || len(doc.Sections[0].Tasks) != 0 {
		t.Errorf("sections = %+v", doc.Sections)
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
			name:     "tier out of range",
			input:    "## Work\n\n- [ ] `T-001` **Report**\n  tier:5 · from:self\n",
			wantLine: 4,
			wantMsg:  "tier 5",
		},
		{
			name:     "tier not a number",
			input:    "## Work\n\n- [ ] `T-001` **Report**\n  tier:high · from:self\n",
			wantLine: 4,
			wantMsg:  "not a number",
		},
		{
			name:     "bad date",
			input:    "## Work\n\n- [ ] `T-001` **Report**\n  tier:1 · from:self · due:31-12-2026\n",
			wantLine: 4,
			wantMsg:  "YYYY-MM-DD",
		},
		{
			name:     "invalid effort",
			input:    "## Work\n\n- [ ] `T-001` **Report**\n  tier:1 · from:self · effort:five\n",
			wantLine: 4,
			wantMsg:  "effort",
		},
		{
			name:     "field without a value",
			input:    "## Work\n\n- [ ] `T-001` **Report**\n  tier:1 · some text\n",
			wantLine: 4,
			wantMsg:  "key:value",
		},
		{
			name:     "task outside a section",
			input:    "# Duties\n\n- [ ] `T-001` **Report**\n  tier:1 · from:self\n",
			wantLine: 3,
			wantMsg:  "outside any section",
		},
		{
			name:     "duplicate identifier",
			input:    "## Work\n\n- [ ] `T-001` **Report**\n  tier:1 · from:self\n\n- [ ] `T-001` **Second**\n  tier:1 · from:self\n",
			wantLine: 6,
			wantMsg:  "duplicate identifier",
		},
		{
			name:     "line outside a task",
			input:    "## Work\n\n  tier:1 · from:self\n",
			wantLine: 3,
			wantMsg:  "outside any task",
		},
		{
			name:     "invalid identifier",
			input:    "## Work\n\n- [ ] `X1` **Report**\n  tier:1 · from:self\n",
			wantLine: 3,
			wantMsg:  "identifier",
		},
		{
			name:     "no tier",
			input:    "## Work\n\n- [ ] `T-001` **Report**\n  from:self\n",
			wantLine: 3,
			wantMsg:  "tier",
		},
		{
			name:     "no from",
			input:    "## Work\n\n- [ ] `T-001` **Report**\n  tier:1\n",
			wantLine: 3,
			wantMsg:  "who the duty is from",
		},
		{
			name:     "deferrals not a number",
			input:    "## Work\n\n- [ ] `T-001` **Report**\n  tier:1 · from:self · deferrals:twice\n",
			wantLine: 4,
			wantMsg:  "deferrals",
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
				t.Errorf("line = %d, want %d (%v)", perr.Line, tc.wantLine, perr)
			}
			if !strings.Contains(perr.Error(), tc.wantMsg) {
				t.Errorf("message = %q, want one containing %q", perr.Error(), tc.wantMsg)
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
