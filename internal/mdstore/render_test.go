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
		t.Fatalf("Parse() error = %v\n--- input ---\n%s", err, s)
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
		t.Errorf("parse(render(parse(x))) != parse(x)\n--- got ---\n%#v\n--- want ---\n%#v", second, first)
	}
}

// TestRoundTripUnusualSpacing feeds Render a document parsed from a file nobody
// would generate: ragged indentation, extra blank lines, a deeper heading and
// fields spaced by hand.
func TestRoundTripUnusualSpacing(t *testing.T) {
	const messy = "# Duties\n" +
		"\n" +
		"\n" +
		"Read top to bottom.\n" +
		"\n" +
		"###   Work\n" +
		"-  [ ]   `T-001`   **Prepare the report**\n" +
		"      tier:1   ·   from:Dana Whitfield·due:2026-09-05 ·effort:5d·   context:@computer\n" +
		"   labels:   reporting ,  finance ,\n" +
		"\t> needs the numbers from accounting\n" +
		"🔴 start by 2026-08-27\n" +
		"\n" +
		"\n" +
		"- [x] `T-000` **Pay the insurance**  ·  done:2026-08-20\n" +
		"\n"

	first := parseString(t, messy)
	second := parseString(t, render(t, first, date(t, "2026-08-27")))

	if !reflect.DeepEqual(second, first) {
		t.Errorf("messy formatting did not survive the round-trip\n--- got ---\n%#v\n--- want ---\n%#v", second, first)
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
		t.Errorf("rendering twice changed the file\n--- first ---\n%s\n--- second ---\n%s", once, twice)
	}
}

func TestRenderOmitsAbsentFields(t *testing.T) {
	doc := parseString(t, "# Duties\n\n## Work\n\n- [ ] `T-004` **Get the vacation approved**\n  tier:2 · from:self\n")
	got := render(t, doc, date(t, "2026-08-27"))

	for _, key := range []string{keyDue, keyEffort, keyContext, keyDeferrals, keyCreated, labelsPrefix} {
		if strings.Contains(got, key+":") {
			t.Errorf("absent field %q appeared in the output:\n%s", key, got)
		}
	}
	if want := "  " + keyTier + ":2 · " + keyFrom + ":self\n"; !strings.Contains(got, want) {
		t.Errorf("field line = %q, want substring %q", got, want)
	}
}

func TestRenderMarkerAndUpdatedLine(t *testing.T) {
	doc := parseString(t, "# Duties\n\n## Work\n")
	got := render(t, doc, date(t, "2026-08-27"))

	if !strings.Contains(got, Marker) {
		t.Errorf("marker %q missing from the output:\n%s", Marker, got)
	}
	if want := updatedPrefix + " 2026-08-27 · " + greeting; !strings.Contains(got, want) {
		t.Errorf("line %q missing from the output:\n%s", want, got)
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
			name:   "start now",
			fields: "tier:1 · from:self · due:2026-09-05 · effort:5d",
			now:    "2026-08-29",
			want:   "🔴 start by 2026-08-28",
		},
		{
			name:   "soon",
			fields: "tier:2 · from:self · due:2026-09-05 · effort:1d",
			now:    "2026-09-01",
			want:   "⏰ start by 2026-09-03",
		},
		{
			name:   "on track",
			fields: "tier:3 · from:self · due:2026-12-31 · effort:1d",
			now:    "2026-08-27",
			want:   "",
		},
		{
			name:   "no deadline",
			fields: "tier:1 · from:self · effort:5d",
			now:    "2026-08-27",
			want:   "",
		},
		{
			// Without an effort there is no start date, so the hint names the deadline.
			name:   "no effort",
			fields: "tier:1 · from:self · due:2026-08-28",
			now:    "2026-08-27",
			want:   "⏰ due 2026-08-28",
		},
		{
			name:   "overdue",
			fields: "tier:1 · from:self · due:2026-08-20 · effort:1d",
			now:    "2026-08-27",
			want:   "🔴 overdue · due was 2026-08-20",
		},
		{
			name:   "stale",
			fields: "tier:2 · from:self · created:2026-08-01",
			now:    "2026-08-27",
			want:   "💤 stale · created 2026-08-01",
		},
		{
			name:   "stale but background — stay quiet",
			fields: "tier:3 · from:self · created:2026-08-01",
			now:    "2026-08-27",
			want:   "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			doc := parseString(t, "# Duties\n\n## Work\n\n- [ ] `T-001` **Task**\n  "+tt.fields+"\n")
			got := render(t, doc, date(t, tt.now))

			hint := hintOf(got)
			if hint != tt.want {
				t.Errorf("hint = %q, want %q\n--- output ---\n%s", hint, tt.want, got)
			}
		})
	}
}

func TestRenderNilDocument(t *testing.T) {
	var buf strings.Builder
	if err := Render(&buf, nil, time.Time{}, nil); err == nil {
		t.Error("Render(nil) error = nil, want an error")
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
