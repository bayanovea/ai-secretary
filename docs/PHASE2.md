# Phase 2 — the deferred integrations

Phase 1 ships one interface: the Claude Code skill, plus reading `docs/duties.md` by hand. Two more
were designed for and deliberately not built — a **scheduled daily briefing** and a **Telegram bot**.

This document records the seam they both hang off, so that neither of them requires reopening the
logic packages when it is finally built.

## The seam: `brief --json`

Everything a second interface needs is already produced by one command:

```
secretary --today=2026-08-27 brief --json
```

```json
{
  "date": "2026-08-27",
  "all_clear": false,
  "total": 3,
  "counts": {"1": 2, "2": 1, "3": 0},
  "overdue":   [ /* jsonTask */ ],
  "start_now": [ /* jsonTask */ ],
  "soon":      [ /* jsonTask */ ],
  "groups":    [ {"kind": "контекст", "key": "@город", "reason": "…", "tasks": [ /* jsonTask */ ]} ],
  "complaints":[ {"flag": "откладывается", "task": { /* jsonTask */ }} ]
}
```

A `jsonTask` carries the stored fields (`id`, `title`, `section`, `tier`, `from`, `due`, `effort`,
`context`, `labels`, `notes`, `deferrals`, `created`) *and* the computed ones (`status`,
`latest_start`, `days_overdue`, `days_until_start`), so a consumer never recomputes a date and
never has to know the tier buffers.

Two properties make this usable as an integration point rather than a debug dump:

- **Determinism.** No logic package reads a clock; `brief.Build(doc, now, cfg)` takes the day as an
  argument, and the CLI exposes `--today`. The same artifact on the same day yields byte-identical
  output — which is what lets a scheduler tell "nothing changed" from "something new is on fire",
  and lets a bot avoid nagging twice about the same duty.
- **Alfred's prose is separate from the data.** `brief --json` is the facts; `brief` (no flag) is
  the same briefing spoken by `internal/voice`. An integration picks one or renders both.

## Packages reused unchanged

Both integrations are a new `main`, or a new goroutine beside the existing one. Nothing below needs
editing:

| Package | Role in phase 2 |
|---|---|
| `internal/config` | reads `secretary.yml`; a bot process loads it once at start |
| `internal/task` | the `Task`/`Status` model and `объём` parsing |
| `internal/mdstore` | `Store.Load`/`Save` over the artifact, plus `Document.Add/Done/Defer/Sort` — the atomic rename already makes concurrent writers safe enough for one bot |
| `internal/schedule` | `LatestStart`, `Status`, `Flags` |
| `internal/grouping` | `Build` — the "заодно" batches |
| `internal/voice` | `Get("alfred")` — the persona is data, so a second persona is a new file here and nothing else |
| `internal/brief` | `Build` → `Briefing`, then `.Text(persona)` or `.JSON()` |

The only phase-1 component a second interface does **not** reuse is `.claude/skills/secretary/`,
because it is the Claude-Code-shaped half of the free-text intake. A bot needs its own intake — see
below.

## Integration A — scheduled daily briefing

**What it is.** Once a morning, run the briefing and deliver it somewhere the user will see it
(file, email, notification, a chat message).

**Shape.** A cron entry is enough; no new Go code is strictly required.

```cron
0 8 * * *  cd ~/AI/secretary && secretary brief >> docs/briefings.log 2>&1
```

**What to decide when building it.**

- *Silence on an all-clear day.* `all_clear` in the JSON says whether anything is worth a
  notification. A daily message that is usually "ничего не горит" trains the user to ignore it;
  prefer sending only when `all_clear` is false, and let the user pull the quiet days themselves.
- *Repetition.* A duty that stays in `start_now` for four days produces four identical briefings.
  Keep the previous run's JSON and diff on task ids, so the message can lead with what is new.
- *The date is an input.* `--today` means a missed run can be replayed for the day it missed.

**Reuses:** the binary as-is. Only if the delivery needs to be in-process (an SMTP send, a push
API) does it become a small `main` calling `brief.Build(…).Text(persona)`.

## Integration B — Telegram bot

**What it is.** The same secretary, reachable from a phone: the user writes «надо до пятницы отдать
документы нотариусу», the bot files it and answers in Alfred's voice.

**Shape.**

```
Telegram update ─► intake (LLM: free text → fields) ─► mdstore.Document.Add ─► Store.Save
                                                   └─► brief.Build ─► .Text(alfred) ─► reply
```

**The one genuinely new part is the intake.** In phase 1 the classifier is Claude Code itself,
driven by `SKILL.md`. A bot has no Claude Code, so it needs an equivalent: an Anthropic API call
whose system prompt is the classification half of `SKILL.md` and whose tool schema is the `add`
flag set. Keep that prompt derived from `SKILL.md` rather than forked from it — the tier heuristics,
the ask-vs-assume rule and the relative-date rule must not drift between the two front ends.

**What to decide when building it.**

- *Who is the user.* The artifact has no notion of accounts. Bind the bot to a single Telegram
  chat id and refuse everything else; multi-user means one artifact per user, which is a config
  path per chat, not a schema change.
- *Writes are serialised.* `Store` holds no state between calls — it re-parses on every command —
  and `Save` renames a temp file over the artifact. Serialise the bot's own handlers with a mutex
  and two concurrent messages cannot interleave a read-modify-write.
- *Confirmation before writing.* The skill states its assumptions in the reply («Записал в Личное,
  тир 1, от Марины, срок 28 августа, сэр»). A bot should do the same, and offer an inline button to
  correct the tier or the date — which is `defer` and a re-`add`, not a new command.
- *Ids in a chat.* Briefing text carries no ids on purpose. For «сделал» the bot resolves the title
  against `brief --json` / `list` the same way the skill does.

**Reuses:** every package in the table above, plus the artifact and `secretary.yml` untouched. A
bot and the CLI can run against the same file at the same time.
