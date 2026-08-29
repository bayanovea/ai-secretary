package main

import (
	"flag"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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
		&addCmd{}, &briefCmd{}, &listCmd{}, &stepCmd{}, &doneCmd{},
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
	steps   stringList
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
	fs.Var(&c.steps, "step", "подпункт-чекбокс (можно повторять)")
}

func (c *addCmd) run(a *app, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("%w: add не принимает позиционных аргументов (%s)", errUsage, strings.Join(args, " "))
	}
	if strings.TrimSpace(c.title) == "" {
		return fmt.Errorf("%w: add требует --title", errUsage)
	}

	parsedSteps, err := steps(c.steps)
	if err != nil {
		return err
	}

	t := task.Task{
		Title:   c.title,
		Tier:    c.tier,
		From:    c.from,
		Section: c.section,
		Context: c.context,
		Labels:  splitLabels(c.labels),
		Notes:   c.notes,
		Steps:   parsedSteps,
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
	order   string
	color   string
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
	fs.StringVar(&c.order, "sort", sortByDate, "порядок внутри раздела: date — по срокам, tier — как в артефакте")
	fs.StringVar(&c.color, "color", "auto", "цвет: auto, always или never")
}

// Sort orders accepted by `--sort`. The artifact keeps duties in tier order,
// which is right for a file read top to bottom; a person asking «что у меня»
// reads by date, so that is the default here.
const (
	sortByDate = "date"
	sortByTier = "tier"
)

// titleWidthMax caps the title column so that one long duty cannot push the
// columns of every other duty off the screen.
const titleWidthMax = 46

func (c *listCmd) run(a *app, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("%w: list не принимает позиционных аргументов", errUsage)
	}
	if c.order != sortByDate && c.order != sortByTier {
		return fmt.Errorf("%w: --sort %q: ожидается date или tier", errUsage, c.order)
	}
	color, err := colorEnabled(c.color, a.out)
	if err != nil {
		return err
	}

	doc, err := a.load()
	if err != nil {
		return err
	}

	type group struct {
		name  string
		tasks []task.Task
	}
	var groups []group
	titleWidth := 0
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
		if c.order == sortByDate {
			sort.SliceStable(matched, func(i, j int) bool { return earlierDue(matched[i], matched[j]) })
		}
		for _, t := range matched {
			if w := utf8.RuneCountInString(t.Title); w > titleWidth {
				titleWidth = w
			}
		}
		groups = append(groups, group{name: section.Name, tasks: matched})
	}

	if len(groups) == 0 {
		a.printf("Ничего не найдено, сэр.\n")
		return nil
	}
	if titleWidth > titleWidthMax {
		titleWidth = titleWidthMax
	}

	pal := palette{on: color}
	for i, g := range groups {
		if i > 0 {
			a.printf("\n")
		}
		a.printf("%s\n", pal.bold(g.name))
		for _, t := range g.tasks {
			a.printf("  %s\n", listLine(t, a, pal, titleWidth))
			for _, l := range stepLines(t, a, pal, titleWidth) {
				a.printf("%s\n", l)
			}
		}
	}
	return nil
}

// earlierDue is the date order: nearest deadline first, undated duties last —
// they are the ones with nothing to be late for — then by tier and by id so that
// the order is total and two runs on the same day agree.
func earlierDue(a, b task.Task) bool {
	if (a.Due == nil) != (b.Due == nil) {
		return b.Due == nil
	}
	if a.Due != nil && !a.Due.Equal(*b.Due) {
		return a.Due.Before(*b.Due)
	}
	if a.Tier != b.Tier {
		return a.Tier < b.Tier
	}
	return a.ID < b.ID
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

// listLine is one duty on one line, laid out in columns so that a column can be
// read down the page: what it is, how it stands, when it is due, how much work
// it is, where it is done. Everything a person does not scan for — who asked,
// the labels, the remarks, the identifier — is dimmed into the tail.
func listLine(t task.Task, a *app, pal palette, titleWidth int) string {
	status := schedule.Status(t, a.now, a.cfg)

	effort := ""
	if !t.Effort.IsZero() {
		effort = t.Effort.String()
	}

	var tail []string
	if t.From != "" && t.From != "себя" {
		tail = append(tail, "от "+t.From)
	}
	if p := t.Progress(); p != "" {
		tail = append(tail, "подпункты "+p)
	}
	if len(t.Labels) > 0 {
		tail = append(tail, strings.Join(t.Labels, ", "))
	}
	for _, f := range schedule.Flags(t, a.now, a.cfg) {
		tail = append(tail, f.String())
	}
	tail = append(tail, t.ID)

	line := statusGlyph(status) + " " +
		pal.tierColor(t.Tier, padLeft(tierMark(t.Tier), 2)) + " " +
		pal.tierColor(t.Tier, pad(truncate(t.Title, titleWidth), titleWidth)) + "  " +
		pal.statusColor(status, pad(status.String(), 13)) + " " +
		pad(humanDue(t.Due, a.now), 11) +
		padLeft(effort, 4) + "  " +
		pad(t.Context, 11) + " " +
		pal.dim(strings.Join(tail, " · "))

	return strings.TrimRight(line, " ")
}

// ── step ──────────────────────────────────────────────────────────────────────

// stepCmd manages the checkboxes inside one duty. Called with an identifier and
// nothing else it prints them, numbered, which is the numbering `--check` and
// `--drop` expect.
type stepCmd struct {
	add     stringList
	check   int
	uncheck int
	drop    int
	set     int
	tier    string
	due     string
}

func (c *stepCmd) name() string    { return "step" }
func (c *stepCmd) summary() string { return "подпункты внутри обязанности" }

func (c *stepCmd) register(fs *flag.FlagSet) {
	fs.Var(&c.add, "add", "добавить подпункт (можно повторять)")
	fs.IntVar(&c.check, "check", 0, "отметить N-й подпункт выполненным")
	fs.IntVar(&c.uncheck, "uncheck", 0, "снять отметку с N-го подпункта")
	fs.IntVar(&c.drop, "drop", 0, "удалить N-й подпункт")
	fs.IntVar(&c.set, "set", 0, "править N-й подпункт: --tier, --due")
	fs.StringVar(&c.tier, "tier", "", "тир подпункта при --set: 1..3 или «-», чтобы снять")
	fs.StringVar(&c.due, "due", "", "срок подпункта при --set: ГГГГ-ММ-ДД или «-», чтобы снять")
}

func (c *stepCmd) run(a *app, args []string) error {
	id, err := oneID("step", args)
	if err != nil {
		return err
	}

	doc, err := a.load()
	if err != nil {
		return err
	}

	// Deletion runs last so that the numbers named by --check and --drop in one
	// call both refer to the list the user was looking at.
	var lines []string
	if len(c.add) > 0 {
		if err := doc.AddStep(id, c.add...); err != nil {
			return err
		}
		lines = append(lines, fmt.Sprintf("Добавлено подпунктов: %d.", len(c.add)))
	}
	if c.set != 0 {
		fields, err := c.stepFields()
		if err != nil {
			return err
		}
		if err := doc.SetStepFields(id, c.set, fields); err != nil {
			return err
		}
		t, _ := doc.Find(id)
		lines = append(lines, fmt.Sprintf("Изменён подпункт: %d. %s", c.set, stepTail(t.Steps[c.set-1])))
	}
	for _, m := range []struct {
		n    int
		done bool
		verb string
	}{{c.check, true, "Выполнено"}, {c.uncheck, false, "Снята отметка"}} {
		if m.n == 0 {
			continue
		}
		if err := doc.SetStep(id, m.n, m.done); err != nil {
			return err
		}
		t, _ := doc.Find(id)
		lines = append(lines, fmt.Sprintf("%s: %d. %s", m.verb, m.n, t.Steps[m.n-1].Text))
	}
	if c.drop != 0 {
		step, err := doc.DropStep(id, c.drop)
		if err != nil {
			return err
		}
		lines = append(lines, fmt.Sprintf("Удалён подпункт: %s", step.Text))
	}

	if len(lines) > 0 {
		if err := a.save(doc); err != nil {
			return err
		}
	}

	t, ok := doc.Find(id)
	if !ok {
		return fmt.Errorf("%w: %s", mdstore.ErrUnknownID, id)
	}
	for _, line := range lines {
		a.printf("%s\n", line)
	}

	a.printf("%s «%s»", t.ID, t.Title)
	if len(t.Steps) > 0 {
		a.printf(" · %s", t.Progress())
	}
	a.printf("\n")
	if len(t.Steps) == 0 {
		a.printf("Подпунктов нет, сэр.\n")
		return nil
	}
	for i, step := range t.Steps {
		box := " "
		if step.Done {
			box = "x"
		}
		a.printf("  %d. [%s] %s\n", i+1, box, stepTail(step))
	}
	if t.StepsLeft() == 0 && !t.Done {
		a.printf("Все подпункты закрыты — можно отмечать: secretary done %s\n", t.ID)
	}
	return nil
}

// stepFields reads the --tier/--due pair of `step --set`. An empty flag leaves
// the field alone; «-» removes it — a distinction a bare number cannot make.
func (c *stepCmd) stepFields() (mdstore.StepFields, error) {
	var f mdstore.StepFields
	if c.tier == "" && c.due == "" {
		return f, fmt.Errorf("%w: step --set требует --tier или --due", errUsage)
	}
	switch {
	case c.tier == "-":
		f.ClearTier = true
	case c.tier != "":
		tier, err := strconv.Atoi(c.tier)
		if err != nil || tier < task.MinTier || tier > task.MaxTier {
			return f, fmt.Errorf("%w: --tier %q: ожидается 1..3 или «-»", errUsage, c.tier)
		}
		f.Tier = tier
	}
	switch {
	case c.due == "-":
		f.ClearDue = true
	case c.due != "":
		due, err := parseDate(c.due, "--due")
		if err != nil {
			return f, err
		}
		f.Due = &due
	}
	return f, nil
}

// stepTail is a step as it is read back to the user: the text, then whatever
// urgency of its own it carries.
func stepTail(s task.Step) string {
	out := s.Text
	if s.Tier != 0 {
		out += " · тир " + strconv.Itoa(s.Tier)
	}
	if s.Due != nil {
		out += " · срок " + s.Due.Format(mdstore.DateLayout)
	}
	return out
}

// steps turns the repeatable `--step` values into unchecked sub-items. The text
// is read by the artifact's own parser, so `--step "позвонить · срок:2026-09-01"`
// on the command line means what the same line means in the file.
func steps(texts []string) ([]task.Step, error) {
	var out []task.Step
	for _, text := range texts {
		if strings.TrimSpace(text) == "" {
			continue
		}
		step, err := mdstore.ParseStep(text)
		if err != nil {
			return nil, err
		}
		out = append(out, step)
	}
	return out, nil
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
	if left := t.StepsLeft(); left > 0 {
		a.printf("Незакрытых подпунктов осталось: %d — они уходят в «Сделано» как есть.\n", left)
	}
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
