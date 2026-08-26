// Package brief assembles the daily report: what is overdue, what has to be
// started today, what is coming, which duties are worth doing in one go, and
// what Alfred would rather not leave unsaid.
//
// The package decides *what* is reported and in which order; the voice package
// decides how each line sounds. That split is why Text takes a persona and why
// nothing here contains a Russian sentence about a duty — only the structural
// headings that hold the sentences apart.
//
// Nothing here reads a clock. Build is handed a `now`, so the same artifact on
// the same day always produces the same briefing, byte for byte — which is what
// makes the golden test meaningful and what the future cron job needs in order
// not to nag twice about the same thing.
package brief

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bayanovea/secretary/internal/config"
	"github.com/bayanovea/secretary/internal/grouping"
	"github.com/bayanovea/secretary/internal/mdstore"
	"github.com/bayanovea/secretary/internal/schedule"
	"github.com/bayanovea/secretary/internal/task"
	"github.com/bayanovea/secretary/internal/voice"
)

// dateLayout is the machine form of a date in the JSON output. It matches the
// artifact's so that a consumer never has to know which of the two it is
// reading.
const dateLayout = mdstore.DateLayout

// Complaint is a flag raised about a duty, paired with the duty it is about.
type Complaint struct {
	Flag schedule.Flag
	Task task.Task
}

// Briefing is the report as data: three buckets of duties by urgency, the
// batches worth doing together, the remarks, and how many open duties there are
// per tier.
//
// A duty appears in at most one of Overdue, StartNow and Soon — the status it
// has today decides which — and duties that are calm or have no deadline appear
// in none of them. They are still counted in Counts and can still be grouped or
// complained about.
type Briefing struct {
	Now        time.Time
	Overdue    []task.Task
	StartNow   []task.Task
	Soon       []task.Task
	Groups     []grouping.Group
	Complaints []Complaint
	// Counts is the number of open duties per tier, keyed by tier number.
	// Every configured tier is present, including the ones at zero.
	Counts map[int]int

	// cfg is kept so that Text can name a tier and JSON can recompute a latest
	// start date without the caller having to hand the configuration in twice.
	cfg *config.Config
}

// Build reads the document as of now and sorts it into the report.
//
// Completed duties are ignored throughout: they are history, and a briefing is
// about what is still owed.
func Build(doc *mdstore.Document, now time.Time, cfg *config.Config) Briefing {
	if cfg == nil {
		cfg = config.Default()
	}

	b := Briefing{Now: now, Counts: map[int]int{}, cfg: cfg}
	for tier := task.MinTier; tier <= task.MaxTier; tier++ {
		b.Counts[tier] = 0
	}

	var open []task.Task
	for _, t := range doc.Tasks() {
		if t.Done {
			continue
		}
		open = append(open, t)
		if t.Tier >= task.MinTier && t.Tier <= task.MaxTier {
			b.Counts[t.Tier]++
		}

		switch schedule.Status(t, now, cfg) {
		case task.StatusOverdue:
			b.Overdue = append(b.Overdue, t)
		case task.StatusStartNow:
			b.StartNow = append(b.StartNow, t)
		case task.StatusSoon:
			b.Soon = append(b.Soon, t)
		}

		for _, f := range schedule.Flags(t, now, cfg) {
			b.Complaints = append(b.Complaints, Complaint{Flag: f, Task: t})
		}
	}

	sortTasks(b.Overdue)
	sortTasks(b.StartNow)
	sortTasks(b.Soon)
	sortComplaints(b.Complaints)
	b.Groups = grouping.Build(open, cfg)
	return b
}

// IsClear reports whether there is nothing to raise: nothing overdue, nothing
// to start now or shortly, and no remarks.
//
// Batches do not count. A suggestion to combine two errands is a convenience,
// not a demand on the day, so a day whose only content is a batch is still a
// clear one.
func (b Briefing) IsClear() bool {
	return len(b.Overdue) == 0 && len(b.StartNow) == 0 &&
		len(b.Soon) == 0 && len(b.Complaints) == 0
}

// Total is the number of open duties across every tier.
func (b Briefing) Total() int {
	total := 0
	for _, n := range b.Counts {
		total += n
	}
	return total
}

// Text renders the briefing the way p would say it.
//
// A nil persona falls back to Alfred, so a caller that has not resolved the
// config's `persona:` yet still gets a readable report rather than a panic.
func (b Briefing) Text(p voice.Persona) string {
	if p == nil {
		p = voice.Alfred{}
	}
	cfg := b.config()

	var out []string
	out = append(out, p.Greeting(b.Now))

	if b.IsClear() {
		out = append(out, "", p.AllClear())
	} else {
		out = appendBlock(out, "Просрочено:", b.Overdue, func(t task.Task) string {
			return p.Overdue(t, b.DaysOverdue(t))
		})
		out = appendBlock(out, "Начинать сегодня:", b.StartNow, func(t task.Task) string {
			return p.StartNow(t)
		})
		out = appendBlock(out, "Скоро:", b.Soon, func(t task.Task) string {
			return p.Soon(t, b.DaysUntilStart(t))
		})
	}

	if len(b.Groups) > 0 {
		out = append(out, "", "Заодно:")
		for _, g := range b.Groups {
			out = append(out, "  "+p.GroupIntro(g))
			for _, t := range g.Tasks {
				out = append(out, "    — "+taskLine(t))
			}
		}
	}

	if len(b.Complaints) > 0 {
		out = append(out, "", "Отдельно:")
		for _, c := range b.Complaints {
			out = append(out, "  · "+p.Complaint(c.Flag, c.Task))
		}
	}

	out = append(out, "", "Открыто: "+countsLine(b.Counts, cfg))
	out = append(out, p.Signoff())
	return strings.Join(out, "\n") + "\n"
}

// appendBlock adds a heading and one line per duty, or nothing at all when the
// bucket is empty — an empty heading reads as an oversight rather than as good
// news.
func appendBlock(out []string, heading string, tasks []task.Task, say func(task.Task) string) []string {
	if len(tasks) == 0 {
		return out
	}
	out = append(out, "", heading)
	for _, t := range tasks {
		out = append(out, "  · "+say(t))
	}
	return out
}

// taskLine is the compact reference used inside a batch, where the duty has
// already been introduced by the group and only needs identifying.
func taskLine(t task.Task) string {
	line := fmt.Sprintf("%s «%s» · тир %d", t.ID, t.Title, t.Tier)
	if t.Due != nil {
		line += " · срок " + t.Due.Format(dateLayout)
	}
	return line
}

// countsLine renders the per-tier tally using the configured tier names.
func countsLine(counts map[int]int, cfg *config.Config) string {
	parts := make([]string, 0, task.MaxTier)
	for tier := task.MinTier; tier <= task.MaxTier; tier++ {
		name := cfg.Tier(tier).Name
		if name == "" {
			name = fmt.Sprintf("тир %d", tier)
		}
		parts = append(parts, fmt.Sprintf("%s — %d", name, counts[tier]))
	}
	return strings.Join(parts, " · ") + "."
}

// DaysOverdue is how many whole days have passed since the deadline. A duty
// without one is never in the overdue bucket, so the answer is zero.
func (b Briefing) DaysOverdue(t task.Task) int {
	if t.Due == nil {
		return 0
	}
	return daysBetween(*t.Due, b.Now)
}

// DaysUntilStart is how many whole days remain before the duty has to be
// started. Without an effort estimate there is no computed start date, so the
// deadline itself stands in for it, exactly as schedule.Status does.
func (b Briefing) DaysUntilStart(t task.Task) int {
	start := schedule.LatestStart(t, b.config())
	if start == nil {
		start = t.Due
	}
	if start == nil {
		return 0
	}
	return daysBetween(b.Now, *start)
}

// config returns the configuration the briefing was built with, or the defaults
// for a Briefing assembled by hand in a test.
func (b Briefing) config() *config.Config {
	if b.cfg == nil {
		return config.Default()
	}
	return b.cfg
}

// JSON is the machine-readable briefing: the form the scheduled job and the
// Telegram bot of phase 2 consume instead of parsing Alfred's prose.
//
// It is a separate shape from the Go structs on purpose. Dates come out as
// `YYYY-MM-DD` rather than as timestamps, statuses and flags as their Russian
// labels, and every bucket is present even when empty, so a consumer can index
// into the document without checking for nulls first.
func (b Briefing) JSON() ([]byte, error) {
	out := jsonBriefing{
		Date:       b.Now.Format(dateLayout),
		AllClear:   b.IsClear(),
		Total:      b.Total(),
		Counts:     b.Counts,
		Overdue:    b.jsonTasks(b.Overdue),
		StartNow:   b.jsonTasks(b.StartNow),
		Soon:       b.jsonTasks(b.Soon),
		Groups:     make([]jsonGroup, 0, len(b.Groups)),
		Complaints: make([]jsonComplaint, 0, len(b.Complaints)),
	}
	if out.Counts == nil {
		out.Counts = map[int]int{}
	}
	for _, g := range b.Groups {
		out.Groups = append(out.Groups, jsonGroup{
			Kind:   g.Kind.String(),
			Key:    g.Key,
			Reason: g.Reason,
			Tasks:  b.jsonTasks(g.Tasks),
		})
	}
	for _, c := range b.Complaints {
		out.Complaints = append(out.Complaints, jsonComplaint{
			Flag: c.Flag.String(),
			Task: b.jsonTask(c.Task),
		})
	}
	return json.MarshalIndent(out, "", "  ")
}

// jsonBriefing is the wire shape of a briefing.
type jsonBriefing struct {
	Date       string          `json:"date"`
	AllClear   bool            `json:"all_clear"`
	Total      int             `json:"total"`
	Counts     map[int]int     `json:"counts"`
	Overdue    []jsonTask      `json:"overdue"`
	StartNow   []jsonTask      `json:"start_now"`
	Soon       []jsonTask      `json:"soon"`
	Groups     []jsonGroup     `json:"groups"`
	Complaints []jsonComplaint `json:"complaints"`
}

// jsonGroup is the wire shape of a batch.
type jsonGroup struct {
	Kind   string     `json:"kind"`
	Key    string     `json:"key"`
	Reason string     `json:"reason"`
	Tasks  []jsonTask `json:"tasks"`
}

// jsonComplaint is the wire shape of a remark.
type jsonComplaint struct {
	Flag string   `json:"flag"`
	Task jsonTask `json:"task"`
}

// jsonTask is the wire shape of a duty. The computed fields — status, latest
// start, the two day counts — are included so that a consumer never has to
// reimplement the schedule package to render a line.
type jsonTask struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	Section        string   `json:"section"`
	Tier           int      `json:"tier"`
	From           string   `json:"from"`
	Due            string   `json:"due,omitempty"`
	Effort         string   `json:"effort,omitempty"`
	Context        string   `json:"context,omitempty"`
	Labels         []string `json:"labels,omitempty"`
	Notes          []string `json:"notes,omitempty"`
	Deferrals      int      `json:"deferrals"`
	Created        string   `json:"created,omitempty"`
	Status         string   `json:"status"`
	LatestStart    string   `json:"latest_start,omitempty"`
	DaysOverdue    int      `json:"days_overdue"`
	DaysUntilStart int      `json:"days_until_start"`
}

func (b Briefing) jsonTasks(tasks []task.Task) []jsonTask {
	out := make([]jsonTask, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, b.jsonTask(t))
	}
	return out
}

func (b Briefing) jsonTask(t task.Task) jsonTask {
	cfg := b.config()
	jt := jsonTask{
		ID:             t.ID,
		Title:          t.Title,
		Section:        t.Section,
		Tier:           t.Tier,
		From:           t.From,
		Context:        t.Context,
		Labels:         t.Labels,
		Notes:          t.Notes,
		Deferrals:      t.Deferrals,
		Status:         schedule.Status(t, b.Now, cfg).String(),
		DaysUntilStart: b.DaysUntilStart(t),
	}
	if t.Due != nil {
		jt.Due = t.Due.Format(dateLayout)
		jt.DaysOverdue = b.DaysOverdue(t)
	}
	if !t.Effort.IsZero() {
		jt.Effort = t.Effort.String()
	}
	if !t.Created.IsZero() {
		jt.Created = t.Created.Format(dateLayout)
	}
	if start := schedule.LatestStart(t, cfg); start != nil {
		jt.LatestStart = start.Format(dateLayout)
	}
	return jt
}

// sortTasks orders a bucket the way the artifact orders a section: most
// important first, then the nearest deadline, then the identifier so that the
// order is total.
func sortTasks(tasks []task.Task) {
	sort.SliceStable(tasks, func(i, j int) bool {
		a, b := tasks[i], tasks[j]
		if a.Tier != b.Tier {
			return a.Tier < b.Tier
		}
		if a.Due != nil && b.Due != nil && !a.Due.Equal(*b.Due) {
			return a.Due.Before(*b.Due)
		}
		if (a.Due == nil) != (b.Due == nil) {
			return b.Due == nil
		}
		return a.ID < b.ID
	})
}

// sortComplaints orders the remarks by the duty they are about, then by the
// flag, so the same artifact always complains in the same order.
func sortComplaints(complaints []Complaint) {
	sort.SliceStable(complaints, func(i, j int) bool {
		a, b := complaints[i], complaints[j]
		if a.Task.Tier != b.Task.Tier {
			return a.Task.Tier < b.Task.Tier
		}
		if a.Task.ID != b.Task.ID {
			return a.Task.ID < b.Task.ID
		}
		return a.Flag < b.Flag
	})
}

// dayOf drops the clock time and the zone, so a comparison is between calendar
// days however the caller built its `now`.
func dayOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// daysBetween counts whole calendar days from a to b; negative when b is first.
func daysBetween(a, b time.Time) int {
	return int(dayOf(b).Sub(dayOf(a)).Hours() / 24)
}
