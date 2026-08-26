package main

import (
	"flag"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bayanovea/secretary/internal/brief"
	"github.com/bayanovea/secretary/internal/mdstore"
	"github.com/bayanovea/secretary/internal/schedule"
	"github.com/bayanovea/secretary/internal/task"
)

// command is one subcommand. Flags are registered in one phase and read in
// another because the global flags — `--config`, `--today` — have to be parsed
// out of the same argument list before the store can be opened.
type command interface {
	name() string
	summary() string
	register(fs *flag.FlagSet)
	run(a *app, args []string) error
}

// commands lists the subcommands in the order they appear in the help.
func commands() []command {
	return []command{
		&addCmd{}, &briefCmd{}, &listCmd{}, &doneCmd{},
		&deferCmd{}, &fmtCmd{}, &checkCmd{}, &archiveCmd{},
	}
}

func lookup(name string) command {
	for _, c := range commands() {
		if c.name() == name {
			return c
		}
	}
	return nil
}

// stringList collects a repeatable flag, which is how `--note` takes more than
// one line without inventing a separator that a note might itself contain.
type stringList []string

func (l *stringList) String() string { return strings.Join(*l, "; ") }

func (l *stringList) Set(v string) error {
	if v = strings.TrimSpace(v); v != "" {
		*l = append(*l, v)
	}
	return nil
}

// ── add ───────────────────────────────────────────────────────────────────────

type addCmd struct {
	title   string
	tier    int
	from    string
	due     string
	effort  string
	section string
	labels  string
	context string
	notes   stringList
}

func (c *addCmd) name() string    { return "add" }
func (c *addCmd) summary() string { return "записать новую обязанность" }

func (c *addCmd) register(fs *flag.FlagSet) {
	fs.StringVar(&c.title, "title", "", "название (обязательно)")
	fs.IntVar(&c.tier, "tier", 2, "тир: 1 критично, 2 важно, 3 фоновое")
	fs.StringVar(&c.from, "from", task.SelfAssigned, "от кого задача")
	fs.StringVar(&c.due, "due", "", "срок, ГГГГ-ММ-ДД")
	fs.StringVar(&c.effort, "effort", "", "объём: Nч, Nд или Nн")
	fs.StringVar(&c.section, "section", "", "раздел (по умолчанию первый из конфигурации)")
	fs.StringVar(&c.labels, "labels", "", "метки через запятую")
	fs.StringVar(&c.context, "context", "", "контекст, например @город")
	fs.Var(&c.notes, "note", "заметка (можно повторять)")
}

func (c *addCmd) run(a *app, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("%w: add не принимает позиционных аргументов (%s)", errUsage, strings.Join(args, " "))
	}
	if strings.TrimSpace(c.title) == "" {
		return fmt.Errorf("%w: add требует --title", errUsage)
	}

	t := task.Task{
		Title:   c.title,
		Tier:    c.tier,
		From:    c.from,
		Section: c.section,
		Context: c.context,
		Labels:  splitLabels(c.labels),
		Notes:   c.notes,
		Created: a.now,
	}
	if t.Section == "" {
		t.Section = defaultSection(a)
	}

	if c.due != "" {
		due, err := parseDate(c.due, "--due")
		if err != nil {
			return err
		}
		t.Due = &due
	}
	if c.effort != "" {
		effort, err := task.ParseEffort(c.effort)
		if err != nil {
			return err
		}
		t.Effort = effort
	}

	doc, err := a.load()
	if err != nil {
		return err
	}
	id, err := doc.Add(t, a.cfg)
	if err != nil {
		return err
	}
	if err := a.save(doc); err != nil {
		return err
	}

	a.printf("%s\n", id)
	return nil
}

// defaultSection is where a duty goes when the caller did not say. The first
// configured section is used rather than a hard-coded name so that a user who
// reorders `sections:` gets the answer they expect.
func defaultSection(a *app) string {
	if len(a.cfg.Sections) > 0 {
		return a.cfg.Sections[0]
	}
	return ""
}

// ── brief ─────────────────────────────────────────────────────────────────────

type briefCmd struct {
	json bool
}

func (c *briefCmd) name() string { return "brief" }
func (c *briefCmd) summary() string {
	return "сводка: просрочено, начинать сегодня, скоро"
}

func (c *briefCmd) register(fs *flag.FlagSet) {
	fs.BoolVar(&c.json, "json", false, "выдать сводку в JSON")
}

func (c *briefCmd) run(a *app, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("%w: brief не принимает позиционных аргументов", errUsage)
	}

	doc, err := a.load()
	if err != nil {
		return err
	}
	b := brief.Build(doc, a.now, a.cfg)

	if c.json {
		data, err := b.JSON()
		if err != nil {
			return err
		}
		a.printf("%s\n", data)
		return nil
	}

	p, err := a.persona()
	if err != nil {
		return err
	}
	a.printf("%s", b.Text(p))
	return nil
}

// ── list ──────────────────────────────────────────────────────────────────────

type listCmd struct {
	tier    int
	section string
	label   string
	context string
}

func (c *listCmd) name() string { return "list" }
func (c *listCmd) summary() string {
	return "показать открытые обязанности с фильтрами"
}

func (c *listCmd) register(fs *flag.FlagSet) {
	fs.IntVar(&c.tier, "tier", 0, "только этот тир")
	fs.StringVar(&c.section, "section", "", "только этот раздел")
	fs.StringVar(&c.label, "label", "", "только с этой меткой")
	fs.StringVar(&c.context, "context", "", "только с этим контекстом")
}

func (c *listCmd) run(a *app, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("%w: list не принимает позиционных аргументов", errUsage)
	}

	doc, err := a.load()
	if err != nil {
		return err
	}

	found := 0
	for _, section := range doc.Sections {
		var matched []task.Task
		for _, t := range section.Tasks {
			if !t.Done && c.matches(t, section.Name) {
				matched = append(matched, t)
			}
		}
		if len(matched) == 0 {
			continue
		}
		if found > 0 {
			a.printf("\n")
		}
		a.printf("%s\n", section.Name)
		for _, t := range matched {
			a.printf("  · %s\n", listLine(t, a))
		}
		found += len(matched)
	}

	if found == 0 {
		a.printf("Ничего не найдено, сэр.\n")
	}
	return nil
}

func (c *listCmd) matches(t task.Task, section string) bool {
	if c.tier != 0 && t.Tier != c.tier {
		return false
	}
	if c.section != "" && !strings.EqualFold(section, c.section) {
		return false
	}
	if c.context != "" && !strings.EqualFold(t.Context, c.context) {
		return false
	}
	if c.label != "" {
		for _, l := range t.Labels {
			if strings.EqualFold(l, c.label) {
				return true
			}
		}
		return false
	}
	return true
}

// listLine is one duty on one line: enough to recognise it and to see where it
// stands, without repeating what the artifact already renders in full.
func listLine(t task.Task, a *app) string {
	parts := []string{
		fmt.Sprintf("%s «%s»", t.ID, t.Title),
		fmt.Sprintf("тир %d", t.Tier),
		"от " + t.From,
	}
	if t.Due != nil {
		parts = append(parts, "срок "+t.Due.Format(mdstore.DateLayout))
	}
	if !t.Effort.IsZero() {
		parts = append(parts, "объём "+t.Effort.String())
	}
	if t.Context != "" {
		parts = append(parts, t.Context)
	}
	if len(t.Labels) > 0 {
		parts = append(parts, strings.Join(t.Labels, ", "))
	}
	parts = append(parts, schedule.Status(t, a.now, a.cfg).String())
	return strings.Join(parts, " · ")
}

// ── done ──────────────────────────────────────────────────────────────────────

type doneCmd struct{}

func (c *doneCmd) name() string { return "done" }
func (c *doneCmd) summary() string {
	return "отметить обязанность выполненной"
}
func (c *doneCmd) register(fs *flag.FlagSet) {}

func (c *doneCmd) run(a *app, args []string) error {
	id, err := oneID("done", args)
	if err != nil {
		return err
	}

	doc, err := a.load()
	if err != nil {
		return err
	}
	t, _ := doc.Find(id)
	if err := doc.Done(id, a.now); err != nil {
		return err
	}
	if err := a.save(doc); err != nil {
		return err
	}

	a.printf("Отмечено выполненным: %s «%s».\n", id, t.Title)
	return nil
}

// ── defer ─────────────────────────────────────────────────────────────────────

type deferCmd struct {
	due string
}

func (c *deferCmd) name() string    { return "defer" }
func (c *deferCmd) summary() string { return "перенести срок" }

func (c *deferCmd) register(fs *flag.FlagSet) {
	fs.StringVar(&c.due, "due", "", "новый срок, ГГГГ-ММ-ДД (обязательно)")
}

func (c *deferCmd) run(a *app, args []string) error {
	id, err := oneID("defer", args)
	if err != nil {
		return err
	}
	if strings.TrimSpace(c.due) == "" {
		return fmt.Errorf("%w: defer требует --due", errUsage)
	}
	due, err := parseDate(c.due, "--due")
	if err != nil {
		return err
	}

	doc, err := a.load()
	if err != nil {
		return err
	}
	if err := doc.Defer(id, due); err != nil {
		return err
	}
	t, _ := doc.Find(id)
	if err := a.save(doc); err != nil {
		return err
	}

	a.printf("Срок перенесён: %s «%s» → %s (переносов: %d).\n",
		id, t.Title, due.Format(mdstore.DateLayout), t.Deferrals)
	return nil
}

// ── fmt ───────────────────────────────────────────────────────────────────────

type fmtCmd struct{}

func (c *fmtCmd) name() string { return "fmt" }
func (c *fmtCmd) summary() string {
	return "перечитать, пересчитать подсказки и переписать артефакт"
}
func (c *fmtCmd) register(fs *flag.FlagSet) {}

func (c *fmtCmd) run(a *app, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("%w: fmt не принимает позиционных аргументов", errUsage)
	}

	doc, err := a.load()
	if err != nil {
		return err
	}
	if err := a.save(doc); err != nil {
		return err
	}

	a.printf("Приведено в порядок: %s\n", a.store.Path)
	return nil
}

// ── check ─────────────────────────────────────────────────────────────────────

type checkCmd struct{}

func (c *checkCmd) name() string { return "check" }
func (c *checkCmd) summary() string {
	return "проверить, что артефакт читается"
}
func (c *checkCmd) register(fs *flag.FlagSet) {}

// run reports on the artifact without touching it.
//
// Only a parse failure is an error: a duty with missing data is a remark, not a
// broken file, and a `check` that exited non-zero over an unfinished tier-1
// task would make the command useless in a pre-commit hook.
func (c *checkCmd) run(a *app, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("%w: check не принимает позиционных аргументов", errUsage)
	}

	doc, err := a.load()
	if err != nil {
		return err
	}

	open, incomplete := 0, []string{}
	for _, t := range doc.Tasks() {
		if t.Done {
			continue
		}
		open++
		if schedule.Has(schedule.Flags(t, a.now, a.cfg), schedule.FlagIncomplete) {
			incomplete = append(incomplete, fmt.Sprintf("%s «%s»", t.ID, t.Title))
		}
	}

	var unknown []string
	for _, section := range doc.Sections {
		if section.Name != mdstore.DoneSection && !a.cfg.HasSection(section.Name) {
			unknown = append(unknown, section.Name)
		}
	}

	a.printf("Артефакт читается: %s · открытых обязанностей: %d.\n", a.store.Path, open)
	if len(unknown) > 0 {
		a.printf("Разделы вне конфигурации: %s\n", strings.Join(unknown, ", "))
	}
	for _, line := range incomplete {
		a.printf("Неполные данные: %s — тир 1 без срока или объёма.\n", line)
	}
	return nil
}

// ── archive ───────────────────────────────────────────────────────────────────

// archiveAfterDays is how long a completed duty stays in the main artifact. A
// month is long enough to still answer «когда я это сделал?» and short enough
// that `## Сделано` does not grow past the duties themselves.
const archiveAfterDays = 30

type archiveCmd struct{}

func (c *archiveCmd) name() string { return "archive" }
func (c *archiveCmd) summary() string {
	return "убрать давно выполненное в архивный файл"
}
func (c *archiveCmd) register(fs *flag.FlagSet) {}

func (c *archiveCmd) run(a *app, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("%w: archive не принимает позиционных аргументов", errUsage)
	}
	if strings.TrimSpace(a.cfg.Archive) == "" {
		return fmt.Errorf("%w: в конфигурации не задан archive", errUsage)
	}

	doc, err := a.load()
	if err != nil {
		return err
	}

	// A completed duty with no date is left alone: without one there is no way
	// to tell whether it is old, and losing sight of it is worse than a long
	// «Сделано».
	cutoff := a.now.AddDate(0, 0, -archiveAfterDays)
	var moved []task.Task
	for i := range doc.Sections {
		if doc.Sections[i].Name != mdstore.DoneSection {
			continue
		}
		kept := doc.Sections[i].Tasks[:0]
		for _, t := range doc.Sections[i].Tasks {
			if t.Done && t.CompletedAt != nil && t.CompletedAt.Before(cutoff) {
				moved = append(moved, t)
				continue
			}
			kept = append(kept, t)
		}
		doc.Sections[i].Tasks = kept
	}

	if len(moved) == 0 {
		a.printf("В архив переносить нечего, сэр.\n")
		return nil
	}

	archive := mdstore.NewStore(a.cfg.Archive, a.cfg)
	adoc, err := archive.Load()
	if err != nil {
		return err
	}
	if len(adoc.Sections) == 0 {
		adoc.Preamble = []string{"# Архив обязанностей"}
	}
	appendDone(adoc, moved)
	// The archive is written first: a crash between the two writes then leaves
	// a duty recorded twice, which is recoverable, rather than nowhere.
	if err := archive.Save(adoc, a.now); err != nil {
		return err
	}
	if err := a.save(doc); err != nil {
		return err
	}

	a.printf("Перенесено в архив: %s → %s\n", plural(len(moved)), a.cfg.Archive)
	return nil
}

// appendDone files completed duties under `## Сделано`, creating the section if
// the archive is new, and keeps them in completion order.
func appendDone(doc *mdstore.Document, tasks []task.Task) {
	idx := -1
	for i, section := range doc.Sections {
		if section.Name == mdstore.DoneSection {
			idx = i
			break
		}
	}
	if idx < 0 {
		doc.Sections = append(doc.Sections, mdstore.Section{Name: mdstore.DoneSection})
		idx = len(doc.Sections) - 1
	}

	doc.Sections[idx].Tasks = append(doc.Sections[idx].Tasks, tasks...)
	all := doc.Sections[idx].Tasks
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if a.CompletedAt != nil && b.CompletedAt != nil && !a.CompletedAt.Equal(*b.CompletedAt) {
			return a.CompletedAt.Before(*b.CompletedAt)
		}
		return a.ID < b.ID
	})
}

// plural renders a count of duties with the right Russian ending.
func plural(n int) string {
	word := "задач"
	switch {
	case n%10 == 1 && n%100 != 11:
		word = "задача"
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		word = "задачи"
	}
	return fmt.Sprintf("%d %s", n, word)
}

// ── shared ────────────────────────────────────────────────────────────────────

// oneID reads the single positional identifier the mutating commands take.
func oneID(name string, args []string) (string, error) {
	switch len(args) {
	case 0:
		return "", fmt.Errorf("%w: %s требует идентификатор, например T-001", errUsage, name)
	case 1:
		return strings.TrimSpace(args[0]), nil
	default:
		return "", fmt.Errorf("%w: %s принимает ровно один идентификатор", errUsage, name)
	}
}

// parseDate reads a date in artifact form and normalises it to a calendar day.
func parseDate(value, flagName string) (time.Time, error) {
	t, err := time.Parse(mdstore.DateLayout, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %s %q: ожидается ГГГГ-ММ-ДД", errUsage, flagName, value)
	}
	return day(t), nil
}

// splitLabels reads the comma-separated `--labels` value.
func splitLabels(s string) []string {
	var labels []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			labels = append(labels, part)
		}
	}
	return labels
}
