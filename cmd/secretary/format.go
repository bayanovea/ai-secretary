package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bayanovea/secretary/internal/task"
)

// The list is read with the eyes, not parsed: a glyph says how loud the duty is,
// the columns line up so that a scan down the page compares like with like, and
// the identifier — which carries no meaning for a person — is dimmed to the end
// of the line, where the skill can still find it to resolve «закрой отчёт».
//
// The glyphs are the artifact's own vocabulary (see mdstore.hintLine), so a duty
// looks the same in `list` as it does in docs/duties.md.
func statusGlyph(s task.Status) string {
	switch s {
	case task.StatusOverdue:
		return "🔴"
	case task.StatusStartNow:
		return "⏰"
	case task.StatusSoon:
		return "🟡"
	default:
		return "💤"
	}
}

// tierMark is the tier for eyes that get no colour — a pipe, a log, a terminal
// with NO_COLOR. Two marks shout, one is a duty, none is a wish.
func tierMark(tier int) string {
	switch tier {
	case 1:
		return "!!"
	case 2:
		return "!"
	default:
		return ""
	}
}

var (
	monthsShort   = [...]string{"янв", "фев", "мар", "апр", "мая", "июн", "июл", "авг", "сен", "окт", "ноя", "дек"}
	weekdaysShort = [...]string{"вс", "пн", "вт", "ср", "чт", "пт", "сб"}
)

// humanDue is the deadline as a person would say it. The status column already
// names the state («просрочено», «без срока»), so this column only ever answers
// «когда»: an absolute date is unreadable at a glance, «завтра» is not.
//
// A weekday is added inside the coming week because that is the horizon a person
// plans in; beyond it the day of the week stops helping.
func humanDue(due *time.Time, now time.Time) string {
	if due == nil {
		return "—"
	}
	d := *due
	switch days := int(day(d).Sub(day(now)).Hours() / 24); {
	case days == 0:
		return "сегодня"
	case days == 1:
		return "завтра"
	case days == -1:
		return "вчера"
	case days > 1 && days <= 6:
		return fmt.Sprintf("%s %d %s", weekdaysShort[int(d.Weekday())], d.Day(), monthsShort[int(d.Month())-1])
	}
	out := fmt.Sprintf("%d %s", d.Day(), monthsShort[int(d.Month())-1])
	if d.Year() != now.Year() {
		out += fmt.Sprintf(" %d", d.Year())
	}
	return out
}

// palette paints the output when it goes to a terminal and stays silent when it
// goes into a pipe — which is what keeps the golden and acceptance tests reading
// plain text while a person gets colour.
type palette struct{ on bool }

func (p palette) wrap(code, s string) string {
	if !p.on || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (p palette) bold(s string) string { return p.wrap("1", s) }
func (p palette) dim(s string) string  { return p.wrap("2", s) }

// tierColor gives importance a visual weight: critical duties are red and bold,
// important ones plain, background ones greyed out so the eye slides past them.
func (p palette) tierColor(tier int, s string) string {
	switch tier {
	case 1:
		return p.wrap("1;31", s)
	case 2:
		return s
	default:
		return p.wrap("2", s)
	}
}

func (p palette) statusColor(st task.Status, s string) string {
	switch st {
	case task.StatusOverdue:
		return p.wrap("1;31", s)
	case task.StatusStartNow:
		return p.wrap("33", s)
	case task.StatusSoon:
		return p.wrap("36", s)
	default:
		return p.dim(s)
	}
}

// colorEnabled decides whether ANSI escapes are appropriate. `never`/`always`
// are for scripts and for piping into `less -R`; `auto` asks the writer whether
// it is a terminal and obeys NO_COLOR.
func colorEnabled(mode string, w io.Writer) (bool, error) {
	switch mode {
	case "never":
		return false, nil
	case "always":
		return true, nil
	case "", "auto":
		if os.Getenv("NO_COLOR") != "" {
			return false, nil
		}
		f, ok := w.(*os.File)
		if !ok {
			return false, nil
		}
		info, err := f.Stat()
		if err != nil {
			return false, nil
		}
		return info.Mode()&os.ModeCharDevice != 0, nil
	default:
		return false, fmt.Errorf("%w: --color %q: ожидается auto, always или never", errUsage, mode)
	}
}

// pad widens s to n display cells, counting runes — every letter the artifact
// holds is one cell wide, so a rune count is the column width.
func pad(s string, n int) string {
	if w := utf8.RuneCountInString(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

func padLeft(s string, n int) string {
	if w := utf8.RuneCountInString(s); w < n {
		return strings.Repeat(" ", n-w) + s
	}
	return s
}

// truncate keeps a runaway title from breaking the column grid for everyone else.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// stepIndent puts the checklist under the title of its duty, past the glyph and
// the tier mark, so the eye reads it as belonging to the line above.
const stepIndent = "        "

// stepLines renders a duty's checklist below it. The numbers are the ones
// `secretary step --check N` expects, so a list is enough to act from; a checked
// step is dimmed but still says `[x]`, because the pipe reading this output —
// the skill — never sees the colour.
//
// A step's own tier and срок are laid out so that its date falls in the same
// column as the dates of the duties above and below it: a deadline is read down
// the page, and a step that is due sooner than its parent has to be visible in
// that scan. titleWidth is the parent grid's title column, which is what puts
// the two columns in line.
func stepLines(t task.Task, a *app, pal palette, titleWidth int) []string {
	var out []string
	for i, s := range t.Steps {
		box := "[ ]"
		if s.Done {
			box = "[x]"
		}
		line := fmt.Sprintf("%s%d. %s %s", stepIndent, i+1, box, truncate(s.Text, titleWidth+stepTextPad-1))

		if s.Tier != 0 || s.Due != nil {
			line = pad(line, len(stepIndent)+stepPrefixWidth+titleWidth+stepTextPad) +
				pal.tierColor(s.Tier, padLeft(tierMark(s.Tier), 2)) + " " +
				humanDue(s.Due, a.now)
		}
		if s.Done {
			line = pal.dim(line)
		}
		out = append(out, strings.TrimRight(line, " "))
	}
	return out
}

// The step line's own geometry: `1. [ ] ` before the text, and the text column
// runs a little wider than the parent's title because it starts further right.
const (
	stepPrefixWidth = 7
	stepTextPad     = 6
)
