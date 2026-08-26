# AI Secretary "Alfred" — duty & responsibility tracker

## Overview

A personal secretary that keeps track of duties and responsibilities so they don't get lost.

**Core interaction**: the user writes free-form text (in Russian) describing something they must do. The
secretary figures out where it belongs — section, tier, who assigned it, deadline, labels, context — and
files it into a human-readable Markdown artifact. The user can also just open that artifact and read it.

**What it solves**:
- duties arrive as unstructured thoughts and get lost before they are ever written down
- important things are noticed too late to start them in time
- errands that could have been done in one trip get done in three

**Key capabilities**:
1. **Free-text intake** — "надо до пятницы отдать документы нотариусу, это от Марины" becomes a structured task
2. **3 tiers** — критично / важно / фоновое
3. **Provenance** — every task records who it came from (`от:`)
4. **Deadlines + effort** — and from those, a computed *latest start date*, so the secretary can push you to
   begin in advance rather than warn you when it is already too late
5. **Sections + labels** — sections are the "where in life" axis (Работа, Личное, Дом…), labels are the
   free cross-cutting axis (отношения, переезд, финансы…)
6. **Nudging** — Alfred tells you what must be started *today*, what is overdue, and what you have quietly
   postponed three times
7. **Grouping** — proposes batches of tasks that are convenient to do together (same context/place, same
   person, same errand)

**Persona**: **Альфред Пенниуорт** — the butler who has known you since you were small. Addresses you as
«сэр», is dry about trivia, and is the only one willing to say out loud that you are overextending yourself.
The persona is a data-driven layer (`internal/voice`), so other personas can be added later without touching
logic.

**Interface phases**:
- **Phase 1 (this plan)**: Claude Code skill + reading `docs/duties.md` directly
- **Phase 2 (later, not built here)**: scheduled daily briefing, then a Telegram bot — both plug into the
  same Go core with no redesign

## Context (from discovery)

- **Greenfield**: The repository is an empty git repo — `main` with zero commits, no files
- **No existing stack, README, or CLAUDE.md** — every convention in this plan is being established, not followed
- **Toolchain**: `go1.19.10 darwin/arm64` at `/usr/local/go/bin/go`. `golangci-lint` is **not** installed
- ⚠️ **Go 1.19 is EOL (2023)**. It has generics but *not* the `slices`/`maps` stdlib packages (1.21+) nor
  `min`/`max` builtins (1.21+). This plan targets `go 1.19` in `go.mod` and avoids those. Upgrading
  (`brew install go`) is recommended but not required — see Post-Completion
- **Dependencies**: only `gopkg.in/yaml.v3` for config. CLI subcommands use stdlib `flag` — no cobra
- **Related patterns found**: none — nothing to be consistent with yet

## Development Approach

- **testing approach**: **Regular (code first, then tests in the same task)**
- complete each task fully before moving to the next
- make small, focused changes
- **CRITICAL: every task MUST include new/updated tests** for code changes in that task
  - tests are not optional - they are a required part of the checklist
  - write unit tests for new functions/methods
  - write unit tests for modified functions/methods
  - add new test cases for new code paths
  - update existing test cases if behavior changes
  - tests cover both success and error scenarios
- **CRITICAL: all tests must pass before starting next task** - no exceptions
- **CRITICAL: update this plan file when scope changes during implementation**
- run tests after each change
- maintain backward compatibility

## Testing Strategy

The user has stated they will not read the tests. Tests therefore exist purely as an **automated pass/fail
gate for AI agents** — they must require zero human judgement to interpret. Chosen accordingly:

- **single gate**: `go test ./...` — one command, exit code is the whole verdict
- **table-driven unit tests** — Go standard idiom, one `struct` slice of cases per function. Best signal-per-line
  for pure logic (`schedule`, `grouping`, parsing)
- **golden-file tests** for the Markdown artifact (`internal/mdstore/testdata/golden/*.md`)
  - an agent can regenerate with `go test ./... -update` and `git diff` the result to *see* the format change
  - catches artifact-format regressions that prose instructions cannot
- **round-trip property test**: `parse(render(parse(x))) == parse(x)` — the single most valuable test in the
  project. It guarantees the CLI never mangles a hand-edited artifact
- **CLI integration tests** — build the binary in `TestMain`, run subcommands against a temp-dir artifact,
  assert on stdout + resulting file. Covers wiring that unit tests miss
- **determinism is mandatory**: no `time.Now()` inside any logic package. A `now time.Time` is passed in, and
  the CLI exposes a hidden `--today=YYYY-MM-DD` flag for tests. No `math/rand` — Alfred's phrase variation is
  chosen by a hash of (task id + date), so it varies day to day but is reproducible in tests
- **no e2e/UI tests** — there is no UI

## Progress Tracking

- mark completed items with `[x]` immediately when done
- add newly discovered tasks with ➕ prefix
- document issues/blockers with ⚠️ prefix
- update plan if implementation deviates from original scope
- keep plan in sync with actual work done

## Solution Overview

### Architecture

```
                  free text (Russian)
                          │
              ┌───────────▼────────────┐
              │  .claude/skills/       │   the ONLY fuzzy component:
              │  secretary/SKILL.md    │   text ──▶ structured fields
              └───────────┬────────────┘
                          │  secretary add --title … --tier 1 --from … --due …
              ┌───────────▼────────────┐
              │   cmd/secretary (CLI)  │
              └───────────┬────────────┘
                          │
   ┌──────────┬───────────┼───────────┬────────────┬─────────┐
   │ config   │ mdstore   │ schedule  │ grouping   │ voice   │ brief
   │ (yaml)   │ parse/    │ start-by, │ "do these  │ Alfred  │ assemble
   │          │ render    │ status    │  together" │ phrases │ briefing
   └──────────┴─────┬─────┴───────────┴────────────┴─────────┴─────────┘
                    │
            ┌───────▼────────┐
            │ docs/duties.md │  ◀── the artifact; also hand-editable
            └────────────────┘
```

### Key design decisions

| Decision | Rationale |
|---|---|
| **Markdown is the source of truth**, not a DB | The user reads and edits it directly. A DB would make the artifact a second-class generated view. Cost: a real parser + a round-trip guarantee (hence the property test) |
| **Deterministic logic in Go, fuzzy classification in the skill** | Date math and "what's overdue" must not vary run-to-run. Natural-language understanding is the one thing only the LLM can do |
| **Latest-start-date is computed, never stored** | It is a pure function of (deadline, effort, tier). Storing it would let it go stale after an edit. It is *rendered* into the artifact as a hint line, then discarded on the next parse |
| **Generated lines are ephemeral** | Any line beginning with a status glyph (⏰ 🔴 🟡 💤) is dropped by the parser and regenerated by the renderer. Keeps the artifact informative without corrupting round-trips |
| **Persona as data (`internal/voice`)** | Alfred's phrasing lives in one table. Adding a second persona later is a data change, not a refactor |
| **Contexts drive grouping** | `контекст:@город` / `@телефон` / `@компьютер` / `@дом` — GTD-style. Grouping by shared context is what makes "do these in one trip" actually correct, rather than guessing from labels alone |
| **Phase-2 seams documented now, not built** | `brief` already emits machine-readable JSON with `--json`; a cron job or Telegram bot consumes the same core. YAGNI on the bot itself |

### Tiers

| Tier | Name | Meaning | Buffer before latest start |
|---|---|---|---|
| **1** | Критично | Real consequences if missed — money, people, legal, trust | `max(2 дня, 50% объёма)` |
| **2** | Важно | Must happen, but slipping it is survivable | `max(1 день, 25% объёма)` |
| **3** | Фоновое | Would be good; no one is harmed if it waits | none |

## Technical Details

### Artifact format — `docs/duties.md`

```markdown
# Обязанности

<!-- secretary:v1 -->
> Обновлено: 2026-08-27 · К вашим услугам, сэр.

## Работа

- [ ] `T-001` **Подготовить отчёт за Q3**
  тир:1 · от:Анна Петрова · срок:2026-09-05 · объём:5д · контекст:@компьютер
  метки: отчётность, финансы
  > нужны данные от финотдела, они обычно тянут
  🔴 начать не позже 2026-08-27 — то есть сегодня, сэр.

- [ ] `T-004` **Согласовать отпуск**
  тир:2 · от:себя · срок:2026-09-15 · объём:1ч · контекст:@работа
  метки: отпуск

## Личное

- [ ] `T-002` **Забрать документы у нотариуса**
  тир:1 · от:Марина · срок:2026-08-29 · объём:1ч · контекст:@город
  метки: переезд, документы
  ⏰ начать не позже 2026-08-28

- [ ] `T-003` **Купить подарок Марине**
  тир:2 · от:себя · срок:2026-09-01 · объём:2ч · контекст:@город
  метки: отношения
  переносов:2

## Сделано

- [x] `T-000` **Оплатить страховку** · выполнено:2026-08-20
```

**Field grammar** (line 2 of each task, ` · `-separated `ключ:значение`):

| Key | Required | Format | Notes |
|---|---|---|---|
| `тир` | yes | `1` \| `2` \| `3` | |
| `от` | yes | free text | `себя` when self-assigned |
| `срок` | no | `YYYY-MM-DD` | absent ⇒ status `без срока` |
| `объём` | no | `Nч` \| `Nд` \| `Nн` | hours / days / weeks. Needed to compute latest start |
| `контекст` | no | `@слово` | primary grouping key |
| `переносов` | no | integer | auto-incremented when `срок` is pushed back |
| `создано` | auto | `YYYY-MM-DD` | written on add, used for staleness |

`метки:` is its own line (comma-separated). `>` lines are free-text notes. Glyph lines (🔴 ⏰ 🟡 💤) are
generated and dropped on parse.

### Config — `secretary.yml`

```yaml
persona: alfred
artifact: docs/duties.md
archive: docs/duties-archive.md
sections: [Работа, Личное, Дом, Здоровье, Отношения, Финансы]
contexts: ["@город", "@дом", "@телефон", "@компьютер", "@работа"]
tiers:
  "1": {name: Критично, buffer_min_days: 2, buffer_ratio: 0.5}
  "2": {name: Важно,    buffer_min_days: 1, buffer_ratio: 0.25}
  "3": {name: Фоновое,  buffer_min_days: 0, buffer_ratio: 0}
soon_window_days: 3
stale_days: 14
```

### Status computation (`internal/schedule`)

```
latest_start = срок − объём − buffer(тир)

status:
  просрочено     срок < today
  пора начинать  latest_start <= today
  скоро          latest_start <= today + soon_window_days
  спокойно       otherwise
  без срока      срок absent
```

Additional flags Alfred raises:
- `неполные данные` — тир 1 with no `срок` or no `объём` (he cannot compute when to start)
- `откладывается` — `переносов >= 2`
- `забыто` — тир 1–2, `создано` older than `stale_days`, no deadline

### Grouping (`internal/grouping`)

Over open tasks only, in priority order:
1. **shared `контекст`** — ≥2 tasks ⇒ a group ("вы всё равно едете в город")
2. **shared `от`** (person, excluding `себя`) — ≥2 tasks ⇒ a group ("раз уж вы говорите с Мариной")
3. **shared label** — ≥3 tasks ⇒ a group (weaker signal, higher bar)

A task appears in at most one group (first match wins). Groups consisting only of tier-3 tasks require ≥3
members. Groups are sorted by max tier then size, both descending. Fully deterministic.

### CLI surface (`cmd/secretary`)

| Command | Purpose |
|---|---|
| `secretary add --title … --tier N --from … [--due …] [--effort …] [--section …] [--labels …] [--context …] [--note …]` | file a new task; prints the assigned id |
| `secretary brief [--json]` | Alfred's briefing: overdue, start-now, soon, groups, complaints |
| `secretary list [--tier N] [--section S] [--label L] [--context C]` | filtered listing |
| `secretary done <id>` | move to `## Сделано` with `выполнено:` date |
| `secretary defer <id> --due YYYY-MM-DD` | change deadline, increment `переносов` |
| `secretary fmt` | reparse, recompute hints, re-sort, rewrite artifact |
| `secretary check` | validate the artifact parses; report incomplete tasks; non-zero exit on error |
| `secretary archive` | flush `## Сделано` entries older than 30 days into the archive file |

Global hidden flag `--today=YYYY-MM-DD` for deterministic tests.

### Sorting (applied by `fmt` and after every mutation)

Within a section: tier asc → status severity desc → deadline asc (absent last) → id asc.

## What Goes Where

- **Implementation Steps** (`[ ]` checkboxes): tasks achievable within this codebase - code changes, tests, documentation updates
- **Post-Completion** (no checkboxes): items requiring external action - manual testing, changes in consuming projects, deployment configs, third-party verifications

## Implementation Steps

### Task 1: Repo skeleton and config loading

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `secretary.yml`
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [x] `go mod init github.com/bayanovea/secretary` with `go 1.19`; add `gopkg.in/yaml.v3`
- [x] create `.gitignore` (`/secretary`, `*.test`, `.DS_Store`)
- [x] create `secretary.yml` with the schema from Technical Details (sections, contexts, tiers, windows)
- [x] implement `config.Load(path string) (*Config, error)` with defaults applied when the file or a field is absent
- [x] implement `Config.Tier(n int) TierConfig` and `Config.HasSection(name string) bool`
- [x] write table-driven tests for `Load`: full file, partial file (defaults filled), missing file (defaults), malformed YAML (error)
- [x] write tests for `Tier` (valid 1-3, out-of-range returns error/zero) and `HasSection`
- [x] run `go test ./...` - must pass before task 2

### Task 2: Task model, tier and status types

**Files:**
- Create: `internal/task/task.go`
- Create: `internal/task/effort.go`
- Create: `internal/task/task_test.go`
- Create: `internal/task/effort_test.go`

- [x] define `Task` struct: `ID, Title, Section, Tier, From, Due *time.Time, Effort Effort, Context, Labels []string, Notes []string, Deferrals int, Created time.Time, Done bool, CompletedAt *time.Time`
- [x] define `Status` enum (`StatusOverdue`, `StatusStartNow`, `StatusSoon`, `StatusCalm`, `StatusNoDue`) with `String()` returning the Russian label
- [x] implement `Effort` as a duration-in-days value with `ParseEffort(s string) (Effort, error)` handling `Nч`/`Nд`/`Nн` (hours→fraction of a day, weeks→7 days) and `Effort.String()` round-tripping back
- [x] implement `Task.Validate() error` — id format, tier in 1..3, non-empty title and `от`
- [x] write table-driven tests for `ParseEffort` (all three units, decimals, zero, negative→error, garbage→error, empty→error)
- [x] write tests for `Effort.String()` round-trip against `ParseEffort`
- [x] write tests for `Validate` (valid case + each individual failure)
- [x] run `go test ./...` - must pass before task 3

### Task 3: Markdown parser

**Files:**
- Create: `internal/mdstore/parse.go`
- Create: `internal/mdstore/parse_test.go`
- Create: `internal/mdstore/testdata/sample.md`

- [x] implement `Parse(r io.Reader) (*Document, error)` where `Document` holds ordered sections, each with `[]task.Task`, plus a preamble
- [x] parse the task header line: `- [ ] \`T-001\` **Title**` (and `- [x]` with `· выполнено:DATE` for done items)
- [x] parse the ` · `-separated field line into the `Task` struct, ignoring unknown keys rather than failing
- [x] parse `метки:` lines and `>` note lines; **drop generated glyph lines (🔴 ⏰ 🟡 💤) silently**
- [x] return a parse error with line number for a malformed field line or an unparsable date
- [x] create `testdata/sample.md` covering: two sections, all field keys, a done task, a multi-line note, a glyph line, a task with no deadline
- [x] write tests parsing `sample.md` and asserting the full resulting structure
- [x] write error-case tests (bad tier, bad date, bad effort, task line outside any section, duplicate id)
- [x] run `go test ./...` - must pass before task 4

### Task 4: Markdown renderer and round-trip guarantee

**Files:**
- Create: `internal/mdstore/render.go`
- Create: `internal/mdstore/render_test.go`
- Create: `internal/mdstore/golden_test.go` (⚠️ plan said `golden.go`; a non-test file importing `testing` would register `-update` in the production binary)
- Create: `internal/mdstore/testdata/golden/basic.md`

- [x] implement `Render(w io.Writer, doc *Document, now time.Time, cfg *config.Config) error` emitting the exact format from Technical Details
- [x] emit the `<!-- secretary:v1 -->` marker and the `> Обновлено: …` line
- [x] emit generated glyph hint lines from computed status (wired properly in Task 5; a stub mapping is fine here)
- [x] omit absent optional fields entirely rather than writing empty values
- [x] add a `-update` test flag helper in `golden_test.go` (⚠️ renamed from `golden.go`, see Files; regenerate with `go test ./internal/mdstore -update`) to rewrite golden files
- [x] write a golden test rendering a fixed `Document` at a fixed `now` against `testdata/golden/basic.md`
- [x] **write the round-trip property test: `parse(render(parse(sample.md)))` deep-equals `parse(sample.md)`**
- [x] write a test that a hand-written file with unusual-but-legal spacing survives a round-trip
- [x] run `go test ./...` - must pass before task 5

### Task 5: Schedule — latest start date and status

**Files:**
- Create: `internal/schedule/schedule.go`
- Create: `internal/schedule/schedule_test.go`
- Modify: `internal/mdstore/render.go`

- [x] implement `LatestStart(t task.Task, cfg *config.Config) *time.Time` = `срок − объём − buffer(тир)`, nil when deadline or effort is absent
- [x] implement `Status(t task.Task, now time.Time, cfg *config.Config) task.Status` per the rules in Technical Details
- [x] implement `Flags(t task.Task, now time.Time, cfg *config.Config) []Flag` for `неполные данные`, `откладывается`, `забыто`
- [x] implement `Severity(s task.Status) int` used by the sort order
- [x] wire real status into `render.go` glyph lines (🔴 просрочено/пора начинать · ⏰ скоро · 💤 забыто)
- [x] write table-driven tests for `LatestStart` covering tier 1/2/3 buffers, `buffer_ratio` vs `buffer_min_days` whichever is larger, hour-scale efforts, missing deadline, missing effort
- [x] write table-driven tests for `Status` across all five outcomes including exact boundary days (`latest_start == today`, `срок == today`)
- [x] write tests for `Flags` (each flag fires; each does not fire when it should not)
- [x] update the Task 4 golden file for the now-real glyph lines (⚠️ regenerated byte-identical: the sample tasks fall in the same буфер-based buckets the stub computed; the new просрочено/забыто glyphs are covered by `TestRenderHintLines`)
- [x] run `go test ./...` - must pass before task 6

### Task 6: Store — load, mutate, save

**Files:**
- Create: `internal/mdstore/store.go`
- Create: `internal/mdstore/store_test.go`

- [x] implement `Store` wrapping a file path + config, with `Load() (*Document, error)` and `Save(*Document, now) error` (write to temp file, then atomic rename)
- [x] implement `Document.Add(t task.Task, cfg *config.Config) (string, error)` (⚠️ takes `cfg`, like `Sort`: the section check needs it and a `Document` comes out of `Parse`, which has no config to store) assigning the next free `T-NNN` id (⚠️ gaps are not reused — an archived id must not come back pointing at a different duty) and placing the task in its section (creating the section if it is new but listed in config; error if not in config)
- [x] implement `Document.Done(id string, now time.Time) error` — move to `## Сделано`, set `выполнено:`
- [x] implement `Document.Defer(id string, newDue time.Time) error` — set new deadline, increment `переносов`, error if the new date is not later than the old one (⚠️ a task that had no deadline gets one without incrementing: nothing was postponed)
- [x] implement `Document.Sort(now, cfg)` applying the sort order from Technical Details
- [x] write tests for `Add` (id allocation including gaps and an empty document, unknown section error)
- [x] write tests for `Done` and `Defer` (success, unknown id, deferring to an earlier date)
- [x] write a test for `Sort` determinism and correct ordering across tiers/statuses
- [x] write a test that `Save` then `Load` yields an equal document
- [x] run `go test ./...` - must pass before task 7

### Task 7: Grouping — "do these together"

**Files:**
- Create: `internal/grouping/grouping.go`
- Create: `internal/grouping/grouping_test.go`

- [x] implement `Group` struct: `Kind` (context/person/label), `Key`, `Tasks []task.Task`, `Reason string`
- [x] implement `Build(tasks []task.Task, cfg *config.Config) []Group` following the priority order: context (≥2) → person excluding `себя` (≥2) → label (≥3)
- [x] ensure each task lands in at most one group (first match wins, iterating in a deterministic key order)
- [x] require ≥3 members for groups whose tasks are all tier 3
- [x] sort groups by max tier asc, then size desc, then key asc
- [x] write table-driven tests: context grouping, person grouping, label grouping, `себя` excluded, single-task keys produce no group
- [x] write tests for the tier-3-only threshold and for a task eligible for two groups landing in exactly one
- [x] write a determinism test (same input, shuffled order ⇒ identical output)
- [x] run `go test ./...` - must pass before task 8
- ⚠️ «max tier asc» реализовано как важность: группа сортируется по наименьшему номеру тира (тир 1 — самый важный) по возрастанию, затем размер по убыванию, затем ключ

### Task 8: Alfred's voice

**Files:**
- Create: `internal/voice/voice.go`
- Create: `internal/voice/alfred.go`
- Create: `internal/voice/alfred_test.go`

- [x] define `Persona` interface: `Greeting(now) string`, `Overdue(t, days) string`, `StartNow(t) string`, `Soon(t, days) string`, `GroupIntro(g) string`, `Complaint(f Flag, t) string`, `AllClear() string`, `Signoff() string` (⚠️ `Flag` is `schedule.Flag`, `g` is `grouping.Group` — voice imports both, no cycle)
- [x] implement the Alfred persona in Russian — dry, formal, «сэр», concerned about overextension. Several phrasings per situation
- [x] implement deterministic variant selection: `fnv32(taskID + date) % len(variants)` — varies by day, reproducible in tests. **No `math/rand`**
- [x] implement `voice.Get(name string) (Persona, error)` so config `persona:` selects the implementation and an unknown name is an error
- [x] write tests asserting determinism (same id+date ⇒ same phrase, twice)
- [x] write tests asserting variation (different dates over a window produce >1 distinct phrase)
- [x] write tests that every method returns non-empty text for a representative task, and that `Get` errors on an unknown persona
- [x] run `go test ./...` - must pass before task 9
- ⚠️ `AllClear()` и `Signoff()` не имеют варьирования: интерфейс не передаёт им ни задачи, ни даты, а ритуальная фраза дворецкого и должна быть одной и той же

### Task 9: Briefing assembly

**Files:**
- Create: `internal/brief/brief.go`
- Create: `internal/brief/brief_test.go`
- Create: `internal/brief/testdata/golden/brief_ru.txt`

- [x] implement `Build(doc *Document, now time.Time, cfg *config.Config) Briefing` — a structured value: `Overdue, StartNow, Soon []task.Task`, `Groups []grouping.Group`, `Complaints []Complaint`, `Counts map[int]int`
- [x] implement `Briefing.Text(p voice.Persona) string` — Alfred's rendered briefing
- [x] implement `Briefing.JSON() ([]byte, error)` — the machine-readable form the future Telegram bot and cron job consume
- [x] handle the empty case (nothing due, nothing overdue) via `Persona.AllClear()`
- [x] write a golden test for `Text` at a fixed `now` over a fixed document against `testdata/golden/brief_ru.txt`
- [x] write tests for `Build` bucketing (a task lands in exactly one of overdue/start-now/soon) and for the all-clear path
- [x] write a test that `JSON` output unmarshals and contains every bucket
- [x] run `go test ./...` - must pass before task 10
- ➕ добавлены `IsClear`, `Total`, `DaysOverdue`, `DaysUntilStart` — сводка сама считает дни, чтобы CLI и будущий бот не повторяли арифметику `schedule`
- ⚠️ структурные заголовки («Просрочено:», «Заодно:») принадлежат `brief`, а не персоне: интерфейс `Persona` даёт только фразы о задачах

### Task 10: CLI wiring

**Files:**
- Create: `cmd/secretary/main.go`
- Create: `cmd/secretary/commands.go`
- Create: `cmd/secretary/main_test.go`
- Create: `Makefile`

- [x] implement subcommand dispatch over stdlib `flag` for: `add`, `brief`, `list`, `done`, `defer`, `fmt`, `check`, `archive`
- [x] implement the global `--config`, `--artifact` and hidden `--today=YYYY-MM-DD` flags (the latter defaulting to the real clock)
- [x] implement `add` printing the assigned id, and `check` exiting non-zero when the artifact fails to parse
- [x] implement `archive` moving `## Сделано` entries older than 30 days into `archive:` from config
- [x] add a `Makefile` with `build`, `test`, `install` targets
- [x] write integration tests building the binary once in `TestMain` and running each subcommand against a temp-dir artifact with a fixed `--today`
- [x] write integration tests for error paths (unknown subcommand, unknown id, unknown section, unparsable artifact ⇒ non-zero exit)
- [x] write a test that `add` followed by `fmt` twice is idempotent at the byte level
- [x] run `go test ./...` - must pass before task 11
- ➕ `parseInterspersed` — флаги после позиционного аргумента: `secretary defer T-002 --due=…` пишется именно так, и stdlib `flag` сам это не разбирает
- ➕ глобальные флаги регистрируются дважды (до и после имени команды), поэтому `--today` работает в обеих позициях
- ➕ выходные коды разделены: 2 — неверный вызов, 1 — отказ по существу
- ⚠️ `add --section` по умолчанию берёт первый раздел из конфигурации (`Работа`); навык из задачи 11 всегда указывает раздел явно
- ⚠️ текст сводки не содержит идентификаторов (так говорит персона из задачи 8) — id берутся из `secretary list` или `brief --json`
- ⚠️ `archive` пишет архивный файл раньше основного: сбой между записями оставляет задачу в двух местах, а не нигде

### Task 11: Claude Code skill — the free-text intake

**Files:**
- Create: `.claude/skills/secretary/SKILL.md`

- [x] write the skill frontmatter (name `secretary`, description triggering on duty/task capture, briefings, and «что мне сегодня делать»)
- [x] document Alfred's persona and register so conversational replies match the artifact's voice
- [x] specify the classification procedure: free text → `--title --tier --from --due --effort --section --labels --context --note`
- [x] specify tier heuristics (consequences ⇒ 1, obligation without consequence ⇒ 2, wish ⇒ 3) and effort estimation (ask only when a tier-1 task has a deadline but no obvious effort)
- [x] specify the ask-vs-assume rule: **assume and state the assumption; ask only when the deadline or the assignor is genuinely unrecoverable from the text**
- [x] document every CLI invocation the skill may make, and require running `secretary brief` after each `add`
- [x] document the relative-date rule: resolve «до пятницы» / «на следующей неделе» against today's real date before calling the CLI
- [x] add worked examples (a work task from a boss, a personal errand, a vague wish, a task that should trigger a grouping suggestion)
- [x] verify by driving the skill end-to-end in a scratch directory and confirming the artifact matches expectations
- [x] run `go test ./...` - must pass before task 12
- ➕ `cmd/secretary/skill_test.go` — сторож от расхождения: навык обязан упоминать каждую подкоманду и каждый флаг `add`, а фронтматтер — быть валидным
- ⚠️ навык обязан всегда передавать `--section`: без него `add` молча кладёт задачу в первый раздел конфигурации
- ⚠️ текст сводки не содержит идентификаторов, поэтому «закрой отчёт» разрешается через `list`/`brief --json`, и это записано в навыке

### Task 12: Seed the artifact and document the Phase-2 seams

**Files:**
- Create: `docs/duties.md`
- Create: `docs/PHASE2.md`
- Create: `README.md`

- [x] create `docs/duties.md` with the header, marker, and the config's sections as empty headings
- [x] verify `secretary check` and `secretary brief` both succeed against the empty artifact (all-clear path)
- [x] write `README.md`: what it is, install (`make install`), the CLI table, the artifact format, how to edit the artifact by hand safely
- [x] write `docs/PHASE2.md` documenting the two deferred integrations against `brief --json`: a scheduled daily briefing, and a Telegram bot — including which packages they reuse unchanged
- [x] write a test asserting the seeded `docs/duties.md` parses and contains every section from `secretary.yml`
- [x] run `go test ./...` - must pass before task 13

### Task 13: Verify acceptance criteria

- [ ] verify all requirements from Overview are implemented: 3 tiers, `от:`, deadlines, sections, labels, advance-start nudging, grouping, free-text intake, Alfred persona
- [ ] verify edge cases: task with no deadline, no effort, unknown section, duplicate ids, empty artifact, artifact with only done tasks
- [ ] verify hand-editing safety: manually edit `docs/duties.md`, run `secretary fmt`, confirm no data loss
- [ ] run full test suite: `go test ./...`
- [ ] run `go vet ./...`
- [ ] no e2e suite exists for this project (no UI) — confirmed intentionally out of scope
- [ ] verify coverage of the logic packages is ≥80%: `go test ./internal/... -cover`

### Task 14: [Final] Update documentation

- [ ] update `README.md` if the CLI surface drifted during implementation
- [ ] create `CLAUDE.md` capturing the conventions established here (deterministic-time rule, glyph lines are ephemeral, persona-as-data, golden-file workflow with `-update`)
- [ ] move this plan to `docs/plans/completed/`

## Post-Completion

*Items requiring manual intervention or external systems - no checkboxes, informational only*

**Manual verification**:
- live-use for a week with real duties — the real test is whether Alfred's nudges feel useful or naggy;
  tune `soon_window_days` and the tier buffers from that experience
- confirm the grouping suggestions actually match how errands cluster in practice; the context list in
  `secretary.yml` will likely need personal additions
- confirm Alfred's Russian register reads well; phrase variants live in `internal/voice/alfred.go` and are
  cheap to edit

**Toolchain**:
- Go 1.19.10 is EOL. `brew install go` upgrades to a current release; afterwards `go.mod` can be bumped and
  the `slices`/`maps` stdlib packages used. Not required for this plan
- `golangci-lint` is not installed. `brew install golangci-lint` if linting is wanted; `go vet` is the gate
  until then

**Deferred to Phase 2** (documented in `docs/PHASE2.md`, not built here):
- scheduled daily briefing — a cron entry or a Claude Code scheduled agent calling `secretary brief`
- Telegram bot — a new frontend over `internal/brief` + `internal/mdstore`; needs a bot token, a chat-id
  allowlist, and a decision about where the artifact lives when the bot runs on a server (git-synced repo vs.
  a shared volume)
