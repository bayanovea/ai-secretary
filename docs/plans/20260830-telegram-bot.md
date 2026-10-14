# Telegram bot over the same artifact

## Overview

A second frontend to the same secretary: you message the bot from your phone and the duty lands in
`docs/duties.md`; you ask for a list and `list` comes back; in the morning the briefing arrives.
Three scenarios from the request:

1. **"Pick up the parcel by Wednesday"** → the bot creates the duty and replies in Alfred's voice,
   stating what it assumed (section, tier, due date).
2. **"What is on my list"** → the bot replies with the list.
3. **Morning** → the bot sends today's briefing on its own, if there is something to say.

**The architecture does not need to change** — this is the main finding of discovery, and it removes
the concern raised in the request:

- A Telegram bot works through **long polling**: the process itself calls `api.telegram.org`. No
  inbound port, domain, TLS or public IP. A laptop behind NAT is a fine host.
- Storage stays a markdown file. `docs/PHASE2.md` already designed this seam: the bot is a
  "new `main`", and `mdstore`, `schedule`, `grouping`, `brief`, `voice`, `config` are reused
  **without a single change**. Atomic writes (temp + rename) are already in place; the CLI and the
  bot can work on the same file at the same time.
- The only really new part is intake: free text → fields. In phase 1 that is Claude Code through
  `SKILL.md`. The bot calls **the same `SKILL.md`** through a `claude -p` subprocess, so no second
  classifier appears and the tier/date heuristics cannot drift apart between frontends.

Stage 1 (this task) — the bot on the laptop. Stage 2 (a separate task, out of scope) — git sync and
deployment to a VPS; it is added as a wrapper around the process and does not touch the bot code.

## Context (from discovery)

- **Project**: Go CLI (`cmd/secretary`) over `docs/duties.md`; one external dependency — `yaml.v3`;
  go 1.19.
- **The seam is already described**: `docs/PHASE2.md`, section "Integration B — Telegram bot". The
  plan follows it.
- **API reused as is**:
  - `config.Load(path) (*Config, error)` — a missing file is not an error
  - `mdstore.NewStore(path, cfg)`, `Store.Load() (*Document, error)`, `Store.Save(doc, now) error`
  - `Document.Add(task.Task, cfg) (string, error)` — returns the assigned id
  - `brief.Build(doc, now, cfg) Briefing`, `Briefing.Text(voice.Persona) string`, `.JSON()`,
    `.IsClear() bool`
  - `voice.Get(name) (Persona, error)`
- **Time discipline**: no package under `internal/` reads a clock; `time.Now()` lives in exactly one
  place — `cmd/secretary/main.go:resolveToday`. The new binary must follow the same rule: the clock
  only in `cmd/secretary-bot`, every decision is a pure function of the `now` passed in.
- **`list` formatting**: lives in `cmd/secretary/format.go` (not in `internal/`), so the bot does not
  import it — see decision 5 in Solution Overview.
- **Tests**: golden files in `internal/mdstore` and `internal/brief`, `internal/` is kept at ≥80%
  coverage, the gate is `go test ./... && go vet ./...`.

## Development Approach

- **testing approach**: Regular (code first, then tests in the same task)
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
- maintain backward compatibility: `cmd/secretary` and the artifact do not change, `brief --json`
  stays a stable contract

## Testing Strategy

- **unit tests**: required in every task (see Development Approach)
- **no network and no subprocesses in tests**:
  - the Telegram client is tested against an `httptest.Server` — the transport is swapped through
    the base URL
  - intake is tested through the `Classifier` interface with a fake implementation; the real
    `claude -p` is called only by hand (see Post-Completion)
- **determinism**: every test passes a fixed `now`; no test reads the clock. This is the same rule
  that makes the golden files meaningful.
- **e2e**: the project has no UI tests, no Playwright/Cypress — the section does not apply.
- **acceptance**: in `cmd/secretary-bot/main_test.go` — the scenario "update → artifact changed →
  reply sent" on a fake transport and a fake classifier, modelled on
  `cmd/secretary/acceptance_test.go`.

## Progress Tracking

- mark completed items with `[x]` immediately when done
- add newly discovered tasks with ➕ prefix
- document issues/blockers with ⚠️ prefix
- update plan if implementation deviates from original scope
- keep plan in sync with actual work done

## Solution Overview

```
Telegram ──long poll──► cmd/secretary-bot ──► internal/bot.Handle(now, …)
                              │                        │
                         (clock here)                  ├─► internal/intake  ─► claude -p + SKILL.md
                              │                        │                       → Intent{Add|Brief|List}
                         brief ticker                  ├─► mdstore.Store.Load/Save
                              │                        ├─► brief.Build(…).Text(alfred)
                              └────────────────────────┴─► internal/telegram.SendMessage
```

**Decision 1 — a separate binary `cmd/secretary-bot`.** Not a `secretary bot` subcommand: a daemon
has a different lifecycle, different secrets and a different set of failures. `docs/PHASE2.md` calls
it a "new `main`". `cmd/secretary` is not touched at all.

**Decision 2 — our own Telegram client on `net/http`.** `getUpdates` + `sendMessage` are two calls
and two structs. The project lives with one dependency, and a second one is not worth adding for two
endpoints. The base URL is a struct field, so `httptest` replaces the whole transport.

**Decision 3 — intake through `claude -p` with the same `SKILL.md`.** The path to the skill is a
config key (`skill:`); the file is read at startup and placed into the system prompt in full.
`SKILL.md` is **not edited**: the bot-specific addendum (return only JSON, do not run tools) lives in
`internal/intake/prompt.go`. So both frontends have exactly one source of heuristics — which is what
`docs/PHASE2.md` requires ("keep that prompt derived from `SKILL.md` rather than forked from it").

**Decision 4 — the clock only in `cmd/secretary-bot`.** `bot.Handle` and `bot.ShouldPush` take
`now time.Time`. This is not ceremony: this is how the morning briefing is tested without waiting
for the morning, and it is the same rule that is already in force in `cmd/secretary`.

**Decision 5 — the bot serves the "list" through a `brief --json`-like assembly, not through
`format.go`.** The column layout of `list` lives in `cmd/secretary/format.go` and is tuned for a
terminal (widths, ANSI colour). Telegram does not need it, and a `main` package cannot be imported.
The bot renders its own flat list from `doc.Tasks()` + `schedule` — a short renderer in
`internal/bot/render.go`. Duplication here is cheaper than coupling: the terminal and the chat have
different width constraints.

**Decision 6 — one chat, everything else is rejected.** The artifact knows nothing about accounts.
`chat_id` is in the config; an update from any other chat is logged and silently ignored. The token
comes only from the `TELEGRAM_BOT_TOKEN` environment variable; it is not in `secretary.yml` (the
file is in git).

**Decision 7 — writes are serialized with a mutex.** `Store` keeps no state between calls and
re-reads the file every time, but two messages in a row must not interleave a read-modify-write.
One `sync.Mutex` around the whole load→mutate→save cycle.

**Decision 8 — the briefing runs inside the process, with repeat protection.** A goroutine asks the
pure function `ShouldPush(now, at, lastPushed)` once a minute. The date of the last send is stored
in a small JSON file next to the artifact, so a bot restart does not send the briefing a second time,
and a laptop that wakes up at 11:00 still gets today's morning briefing. With `all_clear` and
`quiet_when_clear: true` the briefing is not sent — a daily "nothing is urgent" teaches you to
ignore it.

## Technical Details

### Config: new `telegram:` block

`internal/config` is extended with one struct; every field has a default, a missing block is not an
error, exactly as now.

```yaml
# secretary.yml
skill: .claude/skills/secretary/SKILL.md   # prompt source for intake
telegram:
  chat_id: 0                # required for the bot; 0 = the bot refuses to start
  brief_at: "08:00"         # local time of the morning briefing
  quiet_when_clear: true    # do not send the briefing on a quiet day
  poll_timeout_sec: 30      # long polling
  state: docs/.bot-state.json
  claude_bin: claude        # what to call for intake
  claude_timeout_sec: 60
```

The token is `TELEGRAM_BOT_TOKEN` from the environment. A missing token or `chat_id: 0` is a refusal
at startup with a clear, actionable message and exit code `2`.

### Intake contract

`claude -p` receives the system prompt `SKILL.md` + the addendum and must return **only** JSON:

```json
{
  "action": "add" | "brief" | "list" | "unclear",
  "reply": "Filed under Personal, tier 2, due 2 September, sir.",
  "task": {
    "title": "Pick up the parcel",
    "tier": 2,
    "section": "Personal",
    "from": "self",
    "due": "2026-09-02",
    "effort": "1h",
    "context": "@town",
    "labels": ["delivery"],
    "notes": ["..."]
  },
  "filter": { "section": "…", "tier": 0, "label": "…", "context": "@…" }
}
```

- `task` is present only with `action: "add"`, `filter` only with `"list"` (all fields are
  optional).
- `unclear` — the bot forwards `reply` as is and writes nothing. The "ask-vs-assume" rule from
  `SKILL.md` applies here too: in a chat a clarifying question is cheaper than a wrong entry.
- The prompt receives today's date and the list of sections and contexts from the config — otherwise
  it cannot resolve relative dates ("by Wednesday") or the section.
- Claude's answer is parsed strictly: invalid JSON, an unknown `action`, a tier outside 1..3 or a
  date not in `mdstore.DateLayout` → an error; the bot replies "I did not catch that, sir" and
  **writes nothing to the artifact**. A silent guess is worse than a refusal here — the same rule
  as in the artifact parser.

### Processing flow for one message

```
Update → chat_id matches? ─no→ ignore (log)
       → mutex.Lock
       → intake.Classify(text, now, cfg) → Intent
       → switch Intent.Action:
           add:   Store.Load → doc.Add → doc.Sort → Store.Save
                  → brief.Build(doc, now, cfg).Text(alfred)
                  → reply = Intent.Reply + "\n\n" + briefing
           brief: Store.Load → brief.Build → .Text(alfred)
           list:  Store.Load → render.List(doc, now, cfg, filter)
           unclear: reply = Intent.Reply
       → mutex.Unlock
       → telegram.SendMessage(chat_id, reply)
```

`add` always replies with both the confirmation and a fresh briefing — the same rule as in
`SKILL.md` ("Always `secretary brief` after `add`"): a new duty can make another one urgent.

### Package split

| Package | Responsibility | Clock |
|---|---|---|
| `internal/telegram` | `getUpdates`/`sendMessage`, `Update`/`Message` types, backoff | no |
| `internal/intake` | `Classifier` interface + `claude -p` implementation, prompt assembly, JSON parsing | no (`now` argument) |
| `internal/bot` | `Handle` dispatcher, list renderer, `ShouldPush`, state file | no (`now` argument) |
| `cmd/secretary-bot` | config, token, `time.Now()`, polling loop, ticker, exit codes | **yes, the only place** |

## What Goes Where

- **Implementation Steps** (`[ ]` checkboxes): everything done in this repository — code, tests,
  documentation.
- **Post-Completion** (no checkboxes): registering the bot with @BotFather, getting the token and
  chat_id, the first live run with the real `claude -p`, installing under launchd, stage 2 (VPS).

## Implementation Steps

### Task 1: `telegram:` block and `skill:` key in the config

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `secretary.yml`

- [ ] add a `Telegram` struct to `config.go` with fields `ChatID`, `BriefAt`, `QuietWhenClear`,
      `PollTimeoutSec`, `State`, `ClaudeBin`, `ClaudeTimeoutSec` and yaml tags
- [ ] add fields `Skill string` and `Telegram Telegram` to `Config`, default-value constants next to
      the existing `Default*`
- [ ] extend `Default()` and the fill-in logic in `Load` so that a missing `telegram:` block and a
      missing `skill:` give defaults, not zero values
- [ ] add `skill:` and a commented-out example `telegram:` block to `secretary.yml`
- [ ] write tests: a full block is read; a missing block gives defaults; a partially filled block is
      completed; broken YAML is an error
- [ ] run `go test ./internal/config/... -cover` — must pass and stay ≥80%

### Task 2: Telegram Bot API client on net/http

**Files:**
- Create: `internal/telegram/client.go`
- Create: `internal/telegram/client_test.go`

- [ ] create `Client` with fields `Token`, `BaseURL` (default `https://api.telegram.org`),
      `HTTP *http.Client` — `BaseURL` as a field so that `httptest` replaces the whole transport
- [ ] define the minimal types `Update{UpdateID, Message}` and `Message{MessageID, Chat{ID}, Text}` —
      only the fields the bot actually reads
- [ ] implement `GetUpdates(ctx, offset, timeoutSec) ([]Update, error)` — long polling, parsing of
      the `{"ok":bool,"result":…,"description":…}` envelope, `ok:false` → an error with the text
      from `description`
- [ ] implement `SendMessage(ctx, chatID int64, text string) error`; split the text into parts at
      Telegram's 4096-character limit on line boundaries
- [ ] write tests against `httptest.Server`: a successful `GetUpdates` with two updates; an empty
      result; `ok:false`; HTTP 500; `SendMessage` sends the right body; a long text is split into
      two parts
- [ ] run `go test ./internal/telegram/... -cover` — must pass

### Task 3: Intake — `Classifier` and the response parser

**Files:**
- Create: `internal/intake/intake.go`
- Create: `internal/intake/prompt.go`
- Create: `internal/intake/intake_test.go`

- [ ] check the real CLI flags (`claude --help`) and record the chosen invocation form in a
      comment — the plan assumes `-p` + `--output-format json` + a system prompt and disabled
      tools, but the exact flag names are checked against the installed version
- [ ] define `Intent{Action, Reply, Task *TaskFields, Filter *Filter}` and the interface
      `Classifier{ Classify(ctx, text string, now time.Time) (Intent, error) }`
- [ ] in `prompt.go` assemble the system prompt: the contents of `SKILL.md` (read at startup, path
      from the config) + the addendum "return only JSON by the schema, do not run tools" + today's
      date, sections and contexts from the config
- [ ] implement `ParseIntent([]byte) (Intent, error)` strictly: an unknown `action`, a tier outside
      1..3, a date not in `mdstore.DateLayout`, `add` without `title` — all of these are errors, not
      "we will guess"
- [ ] implement `claudeClassifier` — `exec.CommandContext` with the timeout from the config; a
      non-zero exit code and a timeout return an error with stderr in the text
- [ ] write tests **only for the pure parts** (without starting a subprocess): `ParseIntent` on all
      four `action` values, on garbage, on JSON inside a markdown fence, on tier 0 and 4, on a
      malformed date; prompt assembly contains the `SKILL.md` text, the date and the list of sections
- [ ] run `go test ./internal/intake/... -cover` — must pass

### Task 4: `bot.Handle` dispatcher and list renderer

**Files:**
- Create: `internal/bot/bot.go`
- Create: `internal/bot/render.go`
- Create: `internal/bot/bot_test.go`

- [ ] create `Bot` with fields store, cfg, persona, classifier, sender and a `sync.Mutex`; the
      interface `Sender{ SendMessage(ctx, chatID int64, text string) error }` so that tests do not
      touch the network
- [ ] implement `Handle(ctx, upd telegram.Update, now time.Time) error`: reject a foreign `chat_id`,
      hold the mutex over the whole load→mutate→save cycle, dispatch on `Intent.Action`
- [ ] `add` branch: build a `task.Task` from `Intent.Task`, `doc.Add` → `doc.Sort` → `Store.Save`,
      reply = intake confirmation + `brief.Build(doc, now, cfg).Text(persona)`
- [ ] `brief` branch: `brief.Build(…).Text(persona)`; `unclear` branch: only `Intent.Reply`
- [ ] in `render.go` implement `List(doc, now, cfg, filter) string` — flat text for the chat
      (status glyph, `!!`/`!`, title, due date), no ANSI and no columns; an empty result gives a
      clear phrase
- [ ] write tests with a fake `Classifier` and a fake `Sender` on a temporary artifact: `add` writes
      the task and replies with a briefing; `brief` does not change the file; `list` filters by
      section and by tier; a foreign `chat_id` neither writes nor replies; a classifier error does
      not corrupt the artifact
- [ ] write a serialization test: two `Handle` calls from different goroutines give two duties and a
      valid file (run under `-race`)
- [ ] run `go test ./internal/bot/... -race -cover` — must pass

### Task 5: Morning briefing schedule and state file

**Files:**
- Create: `internal/bot/schedule.go`
- Create: `internal/bot/schedule_test.go`
- Modify: `internal/bot/bot.go`

- [ ] implement the pure `ShouldPush(now time.Time, at string, lastPushed time.Time) bool` — true
      when `now` is already past `at` on the same calendar day, and `lastPushed` is an earlier day
- [ ] implement `LoadState(path)`/`SaveState(path, date)` over a small JSON (`{"last_push":
      "2026-08-30"}`); a missing file is not an error, the write is atomic (temp + rename), as in
      `mdstore`
- [ ] add `Bot.PushBrief(ctx, now) (sent bool, err error)`: build the briefing, do not send on
      `IsClear()` with `quiet_when_clear`, record the state date after a successful send
- [ ] write `ShouldPush` tests: before the time; after the time on the same day; already sent today;
      a missed push from yesterday (the laptop was asleep); a malformed `at` string — an error at
      startup, not in the loop
- [ ] write state tests: a missing file; broken JSON; a write-and-read round-trip
- [ ] write `PushBrief` tests: a quiet day stays silent with `quiet_when_clear: true` and speaks
      with `false`; a repeated call on the same day does not send a second time
- [ ] run `go test ./internal/bot/... -cover` — must pass

### Task 6: `cmd/secretary-bot` binary — polling loop and clock

**Files:**
- Create: `cmd/secretary-bot/main.go`
- Create: `cmd/secretary-bot/main_test.go`
- Modify: `Makefile`

- [ ] write `main.go` modelled on `cmd/secretary/main.go`: `run(args, stdout, stderr) int`, flags
      `--config`, `--artifact`, exit codes `0`/`1`/`2`, actionable error messages
- [ ] startup checks: `TELEGRAM_BOT_TOKEN` is set, `telegram.chat_id != 0`, `SKILL.md` is readable,
      `brief_at` parses — each with its own clear message and exit code `2`
- [ ] implement the long polling loop: `GetUpdates` keeping the `offset`, exponential backoff on
      network errors, clean shutdown on SIGINT/SIGTERM through `context.CancelFunc`
- [ ] implement the briefing goroutine: a tick once a minute → `ShouldPush(time.Now(), …)` →
      `PushBrief`; **`time.Now()` is called only here and in the polling loop**
- [ ] add build and run targets for the bot to the `Makefile` next to the existing ones
- [ ] write tests: a missing token → exit code `2` and a message; `chat_id: 0` → exit code `2`; an
      unreadable `SKILL.md` → exit code `2`; a malformed `brief_at` → exit code `2`
- [ ] write an acceptance test on a fake Telegram (`httptest`) and a fake classifier: one update
      "buy bread" → the artifact contains the new duty → the sent message contains the
      confirmation; the loop exits on context cancellation
- [ ] run `go test ./cmd/... -race` — must pass

### Task 7: Verify acceptance criteria

- [ ] check the three scenarios from the Overview on a fake transport: adding a duty, requesting
      the list, the morning briefing
- [ ] check the edge cases: a foreign `chat_id`, an invalid Claude response, Telegram unavailable,
      an empty artifact, a message longer than 4096 characters
- [ ] make sure `cmd/secretary` and `docs/duties.md` did not change: `git diff` over
      `cmd/secretary/` and `internal/{mdstore,brief,schedule,grouping,voice,task}` is empty
- [ ] make sure `time.Now()` appears exactly in `cmd/secretary/main.go` and
      `cmd/secretary-bot/main.go`: `grep -rn "time.Now()" --include=*.go . | grep -v _test.go`
- [ ] run the full suite: `go test ./... && go vet ./...`
- [ ] check coverage: `go test ./internal/... -cover` — every package ≥80%

### Task 8: [Final] Update documentation

- [ ] add a section about the bot to `README.md`: the environment variable, the config block, how
      to run it, what the bot understands
- [ ] update `docs/PHASE2.md`: mark "Integration B" as implemented, record that intake went through
      `claude -p` with the same `SKILL.md`, not through the Anthropic API
- [ ] update `CLAUDE.md`: new packages in the dependency diagram, the rule "clock in `cmd/`, not in
      `internal/`" extends to the second binary, `SKILL.md` is the single source of heuristics for
      both frontends
- [ ] move this plan to `docs/plans/completed/`

## Post-Completion

*Requires manual steps or external systems — no checkboxes, for reference*

**Registration and first run** (required before real use):

- create the bot with `@BotFather`, get the token, put it into the environment
  (`TELEGRAM_BOT_TOKEN`), **not into `secretary.yml`** — the file is in git
- find your `chat_id`: message the bot and look at `getUpdates`, or use `@userinfobot`
- put `chat_id` into `secretary.yml`, check that foreign chats are really rejected
- first live run with the real `claude -p`: intake is the only part the unit tests do not touch,
  because the subprocess is not started in tests. Run it by hand on 5–10 real phrases ("hand the
  documents to the notary by Friday", "Dana asked for the report", "what is urgent") and compare
  the result with what the same text gives through Claude Code — a discrepancy means the addendum
  in `prompt.go` fights `SKILL.md`
- check speed: `claude -p` answers in seconds, not milliseconds. If the pause is annoying, send
  "typing…" (`sendChatAction`) before calling intake

**Installing as a service**:

- a launchd plist on the laptop (`KeepAlive`, working directory — the repository root, environment
  with the token)
- check behaviour when the laptop sleeps: long polling must survive the disconnect and reconnect
  with backoff, and a missed morning briefing must arrive on wake-up

**Stage 2 — VPS (a separate task, out of scope for this plan)**:

- a private git repository as the sync layer: the bot does commit+push after every write and pull
  before every read; on the laptop — ordinary pull/push
- deployment: the binary + `claude` in PATH on the server (intake through `claude -p` requires an
  installed and authorized Claude Code) — if that turns out to be inconvenient, `internal/intake`
  swaps one `Classifier` implementation for a call to the Anthropic API, without touching `bot` and
  `telegram`
- merge conflicts: the artifact is sorted deterministically, but editing in two places at once still
  produces a conflict — a strategy is needed (for example, the bot always does `pull --rebase`
  before writing)

**The obvious continuation, deliberately left out of scope**:

- `done` and `defer` from the chat. The `Intent.Action` enumeration leaves room for them, but the
  request was about adding, listing and the briefing — extending it on our own initiative is not
  right. When needed: one branch in the `switch`, a call to `doc.Done(id, now)`, and resolving a
  title to an id the same way `SKILL.md` does.
