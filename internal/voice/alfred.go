package voice

import (
	"fmt"
	"strconv"
	"time"

	"github.com/bayanovea/secretary/internal/grouping"
	"github.com/bayanovea/secretary/internal/schedule"
	"github.com/bayanovea/secretary/internal/task"
)

// Alfred is Alfred Pennyworth: the butler who has known you since you were
// small. Formal, dry about trivia, and the only one willing to say out loud
// that you have taken on more than a day holds.
//
// He has no state — the wording depends only on the facts he is handed — so the
// zero value is usable and safe to share.
type Alfred struct{}

// Greeting opens the briefing. The wording turns over daily, so two runs on the
// same day read identically and Monday does not sound like Sunday.
func (a Alfred) Greeting(now time.Time) string {
	variants := []string{
		"Good morning, sir. It is %s; allow me to set out the state of affairs.",
		"Sir. The briefing for %s, if you are in a condition to hear it.",
		"%s, sir. Coffee is served and the calendar is open.",
		"Sir, the state of affairs as of %s. If you please.",
	}
	return fmt.Sprintf(pick(variants, "greeting", dayKey(now)), spokenDate(now))
}

// Overdue reports a duty whose deadline passed days ago. The phrasing follows
// the count, so the same duty sounds worse the longer it is left.
func (a Alfred) Overdue(t task.Task, days int) string {
	variants := []string{
		"«%s» is %s overdue, sir. I would not mention it if it could be hushed up.",
		"The deadline for «%s» passed %s ago, sir. From: %s.",
		"«%s»: %s past due, sir. It only gets more awkward from here.",
		"Sir, «%s» has been waiting %s past its deadline. I suggest we admit it is a task and not an intention.",
	}
	phrase := pick(variants, "overdue", t.ID, strconv.Itoa(days))
	if countPlaceholders(phrase) == 3 {
		return fmt.Sprintf(phrase, t.Title, plural(days, "day", "days"), t.From)
	}
	return fmt.Sprintf(phrase, t.Title, plural(days, "day", "days"))
}

// StartNow reports a duty that has to be started today to be finished on time.
func (a Alfred) StartNow(t task.Task) string {
	variants := []string{
		"«%s» must be started today, sir. Later will not do.",
		"Sir, it is time to begin «%s». Today, that is, not «this week».",
		"«%s»: the last day on which starting still means finishing on time, sir.",
		"If «%s» is not started today, sir, we shall arrive at the deadline with apologies instead of results.",
	}
	return fmt.Sprintf(pick(variants, "startnow", t.ID, dueKey(t)), t.Title)
}

// Soon reports a duty that has to be started in days days.
func (a Alfred) Soon(t task.Task, days int) string {
	variants := []string{
		"«%s» — to be started in %s, sir.",
		"Sir, %s remain before «%s» must begin.",
		"«%s» comes due for a start in %s, sir. Not urgent, but do not forget it either.",
	}
	phrase := pick(variants, "soon", t.ID, strconv.Itoa(days))
	if phrase == variants[1] {
		return fmt.Sprintf(phrase, plural(days, "day", "days"), t.Title)
	}
	return fmt.Sprintf(phrase, t.Title, plural(days, "day", "days"))
}

// GroupIntro introduces a batch of duties worth doing in one go. The signal
// decides the wording: a shared place is a trip, a shared person a conversation,
// a shared label merely a subject.
func (a Alfred) GroupIntro(g grouping.Group) string {
	var variants []string
	switch g.Kind {
	case grouping.KindContext:
		variants = []string{
			"Since you will be at %s anyway, sir, you might as well:",
			"One trip, %s, and %s dealt with at once, sir:",
			"%s, sir: it would be sensible to close all of this in one outing:",
		}
	case grouping.KindPerson:
		variants = []string{
			"Since you will be speaking with %s anyway, sir, you might as well:",
			"One conversation with %s closes %s, sir:",
			"For %s, sir, you have accumulated:",
		}
	default:
		variants = []string{
			"One subject — %s, sir. Convenient to close them together:",
			"Under «%s» there are %s piled up, sir:",
			"%s, sir: since you will be immersed in it anyway:",
		}
	}
	phrase := pick(variants, "group", g.Kind.String(), g.Key, strconv.Itoa(g.Size()))
	if countPlaceholders(phrase) == 2 {
		return fmt.Sprintf(phrase, g.Key, plural(g.Size(), "duty", "duties"))
	}
	return fmt.Sprintf(phrase, g.Key)
}

// Complaint voices a flag raised about a duty: the remarks that are not about
// the deadline, and the ones Alfred is least willing to leave unsaid.
func (a Alfred) Complaint(f schedule.Flag, t task.Task) string {
	switch f {
	case schedule.FlagIncomplete:
		variants := []string{
			"«%s» is marked critical, sir, yet has neither a deadline nor an effort. I was not trained to plan by inspiration.",
			"Sir, «%s» is first tier without a deadline or without an effort. Tell me those and I shall tell you when to start.",
			"All that is known about «%s» is that it matters, sir. That is not enough for a schedule.",
		}
		return fmt.Sprintf(pick(variants, "incomplete", t.ID), t.Title)
	case schedule.FlagPostponed:
		variants := []string{
			"«%s» has been deferred %s already, sir. At some point that stops being a schedule and becomes a decision.",
			"Sir, the deadline for «%s» has moved %s. Perhaps the task wants crossing out rather than postponing.",
			"«%s» was put off %s, sir. I am obliged to say so.",
		}
		return fmt.Sprintf(pick(variants, "postponed", t.ID, strconv.Itoa(t.Deferrals)),
			t.Title, plural(t.Deferrals, "time", "times"))
	case schedule.FlagForgotten:
		variants := []string{
			"«%s» has sat without a deadline since %s, sir. I believe we are both pretending to remember it.",
			"Sir, «%s» was written down on %s and has not moved a day since.",
			"«%s» has been waiting since %s, sir. Either a deadline or an honest «no».",
		}
		return fmt.Sprintf(pick(variants, "forgotten", t.ID, dayKey(t.Created)), t.Title, spokenDate(t.Created))
	default:
		variants := []string{
			"Something is amiss with «%s», sir, though I struggle to say precisely what.",
			"«%s» requires your attention, sir, for a reason I shall not presume to name.",
		}
		return fmt.Sprintf(pick(variants, "flag", strconv.Itoa(int(f)), t.ID), t.Title)
	}
}

// AllClear is the report for a day with nothing overdue, nothing to start and
// nothing forgotten.
//
// The interface hands this method nothing to vary on, and that is deliberate:
// the one line the butler repeats verbatim is the one that says everything is
// in order.
func (a Alfred) AllClear() string {
	return "Nothing is on fire and nothing is forgotten, sir. A rare day — dispose of it as you see fit."
}

// Signoff closes the briefing. Like AllClear it is fixed: a ritual phrase is
// more reassuring than a varied one.
func (a Alfred) Signoff() string {
	return "At your service, sir."
}

// dueKey is the seed part standing for a duty's deadline. A duty without one
// still needs a stable seed, hence the explicit word.
func dueKey(t task.Task) string {
	if t.Due == nil {
		return "no-deadline"
	}
	return dayKey(*t.Due)
}

// plural renders a count with the noun form it takes: 1 day, 2 days, and
// -1 day, since English keys the form on the magnitude being exactly one.
func plural(n int, one, many string) string {
	abs := n
	if abs < 0 {
		abs = -abs
	}
	form := many
	if abs == 1 {
		form = one
	}
	return strconv.Itoa(n) + " " + form
}

// spokenDate renders a date the way Alfred would say it aloud: «27 August».
func spokenDate(t time.Time) string {
	if t.IsZero() {
		return "an unknown date"
	}
	return fmt.Sprintf("%d %s", t.Day(), t.Month().String())
}

// countPlaceholders counts the `%s` verbs in a template, so a variant may take
// either the bare title or the title plus a detail without the caller having to
// keep two parallel lists in step.
func countPlaceholders(s string) int {
	n := 0
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '%' && s[i+1] == 's' {
			n++
		}
	}
	return n
}
