# CLAUDE.md

Conventions this repository already follows. They are load-bearing: each one exists because a test
or a design decision depends on it.

## What this is

A Go CLI (`cmd/secretary`) over a single hand-editable Markdown artifact (`docs/duties.md`). No
database, no server, no state outside the artifact and `secretary.yml`. The fuzzy part — turning
«hand the papers to the notary by Friday» into flags — lives in the Claude Code skill at
`.claude/skills/secretary/SKILL.md`, not in Go.

Package layout, dependencies pointing one way only:

```
task      domain model: a duty, its effort, its status enum. No clock, no I/O.
config    secretary.yml, every field defaulted. A missing file is not an error.
schedule  latest start date and status from (due, effort, tier) and a `now`.
grouping  batches of duties convenient to do in one go.
mdstore   parse and render the artifact; Store adds load/mutate/atomic-save.
voice     Persona interface + alfred.go. Wording only, no decisions.
brief     assembles what is reported and in which order; asks voice how it sounds.
cmd/secretary  flag parsing, one load/mutate/save cycle per subcommand, exit code.
               format.go holds the presentation of `list` — columns, glyphs, colour.
```

## Deterministic time

**No package under `internal/` reads a clock.** `time.Now()` appears exactly once, in
`cmd/secretary/main.go:resolveToday`, and even there it is truncated to a calendar day in UTC so a
command run at midnight behaves like one run at noon.

Everything else takes a `now time.Time` argument. The hidden global flag `--today=YYYY-MM-DD`
overrides it; it is absent from the usage text on purpose (`commandUsage` walks the flag set and
skips flags with an empty usage string) because it exists for tests and for a future cron job
replaying a day, not for daily use.

Consequence: same artifact + same day → byte-identical output. That is what makes the golden tests
meaningful. If you add a function that needs the date, take it as a parameter.

Dates parsed out of the artifact and dates produced by the CLI must compare exactly, so use
`mdstore.DateLayout` and UTC calendar days everywhere.

## Glyph lines are ephemeral

The 🔴 ⏰ 🟡 💤 hint lines, the `<!-- secretary:v1 -->` marker and the `> Updated:` line are
*generated*. The parser drops them; the renderer writes them back from the current date. Never
treat them as input, and never ask a user to edit them — an edited hint disappears on the next
`fmt`.

This is what makes `parse(render(parse(x))) == parse(x)` a fixed point, asserted by the round-trip
test in `internal/mdstore`. Any new rendered decoration must be dropped by the parser in the same
commit, or the round-trip test will catch you.

Parsing is forgiving about spacing and unknown lines, strict about tiers, dates and efforts — a
wrong answer there is worse than an error. Unknown *field keys* on the `tier:… · from:…` line do not
survive `fmt`; that is documented in the README's «Editing by hand» section, so keep the two in sync
if it ever changes.

Writes are atomic: temp file plus rename, so an interrupted command leaves the previous artifact
intact.

## Steps are input, hints are not

A duty may carry checkbox sub-items (`- [ ] call them`, `task.Step`). They are parsed and written
back verbatim — unlike the glyph lines above them, which are regenerated. Both live in the same
task block, so the parser tries `taskHeaderRe` first and only then `stepRe`: a step has neither an
identifier nor a bold title, which is what keeps the two apart.

A step carries an optional `tier:` and `due:` of its own, written at the end of its line after
` · ` — the call that opens an errand is routinely more urgent than the errand's deadline. The
fields are read tail-first and scanning stops at the first segment that is not a known key, so a
`·` inside the text survives; `mdstore.ParseStep` is the single reader, shared by the parser and by
`--step` on the command line, which is what keeps a typed step and a hand-written one identical.

Steps still have no effort, no context and no id, and nothing outside `mdstore` and `format.go`
reads them — `schedule` and `brief` remain deliberately untouched, so a step's due date shows up in
`list` but never raises its voice in the briefing. Anything that has to be *reminded about* is a
task. If that ever changes, `brief` and its golden files change with it, in the same commit.

## Colour is optional

`list` lays its duties out in columns and paints them only when stdout is a terminal and `NO_COLOR`
is unset (`--color always|never` overrides). Everything colour carries is also carried by text —
the status glyph, the `!!`/`!` tier marks — because the two readers who matter most, a pipe and the
Claude Code skill, never see the escapes. Do not let a new column exist in colour only.

## Persona as data

`voice.Persona` is an interface; `alfred.go` is one implementation, selected by name through
`voice.Get` from the `persona:` config key. A second butler is a new file, not a refactor.

Phrasing is **deterministic**: a variant is chosen by an FNV hash of the facts it describes
(usually task id + day), never by `math/rand`. Alfred says the same thing twice on the same day and
something different tomorrow, and tests can assert on either.

Keep the split honest: `brief` decides *what* is said and in which order, `voice` decides *how*. No
Russian sentence about a duty belongs in `internal/brief`; no scheduling decision belongs in
`internal/voice`.

## Golden files

`internal/mdstore` and `internal/brief` each register a `-update` flag and keep expectations in
`testdata/golden`. After an intentional format or wording change:

```
go test ./internal/mdstore -update
go test ./internal/brief -update
git diff        # read the diff — that is the review
```

Regenerating without reading the diff defeats the point of the test.

## Where the data lives

A checkout of this repository is a working example: `secretary.yml` and `docs/duties.md` at the
root are synthetic sample data, and the tests read them (`TestLoadRepoConfig`, the seeded-artifact
tests). Real duties never go into the repository. `SECRETARY_HOME` points the binary at a private
directory holding its own `secretary.yml` and artifact; relative paths in that configuration
resolve against the home, while an explicit `--artifact` stays relative to the working directory.

## Language

Everything is English: code, comments, CLI output, errors, usage text, the artifact format, the
skill and the docs. The artifact's field keys (`tier:`, `due:`, `effort:`) are part of the file
format, so changing one is a format change: parser, renderer, goldens, README and SKILL.md move
in the same commit. Section names, contexts and labels are configuration and free text, so a user
may keep those in any language. Errors are sentences a person can act on, not error codes.

Comments explain *why*, not what the line does. If a comment restates the code, delete it.

## Exit codes

`0` done · `1` refused on the merits (no such duty, artifact unreadable) · `2` called wrong.
Usage failures wrap `errUsage`; `fail` picks the code from it. A script has to be able to tell "I
called it wrong" from "it refused".

## Before committing

```
go test ./...
go vet ./...
```

`golangci-lint` is not installed here; `go vet` is the gate. Logic packages under `internal/` are
kept at ≥80% coverage (`go test ./internal/... -cover`).

## Phase 2

`docs/PHASE2.md` describes the two deferred frontends — a scheduled daily briefing and a Telegram
bot — both built over `brief --json` and `internal/mdstore` without changing them. Keep `brief
--json` a stable contract.
