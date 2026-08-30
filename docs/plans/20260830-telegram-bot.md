# Telegram-бот над тем же артефактом

## Overview

Второй фронтенд к тому же секретарю: пишешь боту с телефона — дело попадает в `docs/duties.md`;
просишь список — приходит `list`; утром приходит сводка. Три сценария из запроса:

1. **«Забрать шорты из доставки до среды»** → бот заводит дело и отвечает голосом Альфреда,
   проговаривая, что он предположил (раздел, тир, срок).
2. **«Что у меня по делам»** → бот отвечает списком.
3. **Утро** → бот сам присылает сводку за сегодня, если есть что сказать.

**Архитектуру менять не придётся** — это главный вывод разведки, и он снимает опасение из запроса:

- Telegram-бот работает через **long polling**: процесс сам ходит на `api.telegram.org`. Никакого
  входящего порта, домена, TLS и белого IP. Ноутбук за NAT — нормальная площадка.
- Хранилище остаётся markdown-файлом. `docs/PHASE2.md` уже спроектировал этот шов: бот — это
  «новый `main`», а `mdstore`, `schedule`, `grouping`, `brief`, `voice`, `config` переиспользуются
  **без единой правки**. Атомарная запись (temp + rename) уже на месте; CLI и бот могут работать по
  одному файлу одновременно.
- Единственная реально новая часть — интейк: свободный текст → поля. В phase 1 это Claude Code
  через `SKILL.md`. Бот вызывает **тот же `SKILL.md`** через `claude -p` подпроцессом, поэтому
  второго классификатора не заводится и эвристики тиров/дат не могут разойтись между фронтендами.

Этап 1 (эта задача) — бот на ноуте. Этап 2 (отдельная задача, вне scope) — git-синк и деплой на
VPS; он добавляется обёрткой вокруг процесса и кода бота не касается.

## Context (from discovery)

- **Проект**: Go CLI (`cmd/secretary`) над `docs/duties.md`; одна внешняя зависимость — `yaml.v3`;
  go 1.19.
- **Шов уже описан**: `docs/PHASE2.md`, раздел «Integration B — Telegram bot». План следует ему.
- **API, которое переиспользуется как есть**:
  - `config.Load(path) (*Config, error)` — отсутствующий файл не ошибка
  - `mdstore.NewStore(path, cfg)`, `Store.Load() (*Document, error)`, `Store.Save(doc, now) error`
  - `Document.Add(task.Task, cfg) (string, error)` — возвращает присвоенный id
  - `brief.Build(doc, now, cfg) Briefing`, `Briefing.Text(voice.Persona) string`, `.JSON()`,
    `.IsClear() bool`
  - `voice.Get(name) (Persona, error)`
- **Дисциплина времени**: ни один пакет под `internal/` не читает часы; `time.Now()` живёт ровно в
  одном месте — `cmd/secretary/main.go:resolveToday`. Новый бинарь обязан соблюдать то же правило:
  часы только в `cmd/secretary-bot`, все решения — чистые функции от переданного `now`.
- **Форматирование `list`**: живёт в `cmd/secretary/format.go` (не в `internal/`), поэтому бот его
  не импортирует — см. решение 5 в Solution Overview.
- **Тесты**: golden-файлы в `internal/mdstore` и `internal/brief`, `internal/` держится ≥80%
  покрытия, гейт — `go test ./... && go vet ./...`.

## Development Approach

- **testing approach**: Regular (код, потом тесты в той же задаче)
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
- maintain backward compatibility: `cmd/secretary` и артефакт не меняются, `brief --json` остаётся
  стабильным контрактом

## Testing Strategy

- **unit tests**: обязательны в каждой задаче (см. Development Approach)
- **никакой сети и никаких подпроцессов в тестах**:
  - Telegram-клиент тестируется против `httptest.Server` — транспорт подменяется базовым URL
  - интейк тестируется через интерфейс `Classifier` с фейковой реализацией; настоящий `claude -p`
    вызывается только вручную (см. Post-Completion)
- **детерминизм**: все тесты передают фиксированный `now`; ни один тест не читает часы. Это то же
  правило, что делает golden-файлы осмысленными.
- **e2e**: UI-тестов в проекте нет, Playwright/Cypress отсутствуют — раздел неприменим.
- **acceptance**: в `cmd/secretary-bot/main_test.go` — сценарий «апдейт → артефакт изменился →
  отправлен ответ» на фейковом транспорте и фейковом классификаторе, по образцу
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
                         (часы здесь)                  ├─► internal/intake  ─► claude -p + SKILL.md
                              │                        │                       → Intent{Add|Brief|List}
                         тикер сводки                  ├─► mdstore.Store.Load/Save
                              │                        ├─► brief.Build(…).Text(alfred)
                              └────────────────────────┴─► internal/telegram.SendMessage
```

**Решение 1 — отдельный бинарь `cmd/secretary-bot`.** Не подкоманда `secretary bot`: у демона
другой жизненный цикл, другие секреты и другой набор отказов. `docs/PHASE2.md` называет это «новым
`main`». `cmd/secretary` не трогается вовсе.

**Решение 2 — свой Telegram-клиент на `net/http`.** `getUpdates` + `sendMessage` — это два вызова и
две структуры. Проект живёт с одной зависимостью, и ради двух эндпоинтов её вторую заводить не
стоит. Базовый URL — поле структуры, поэтому `httptest` подменяет весь транспорт.

**Решение 3 — интейк через `claude -p` с тем же `SKILL.md`.** Путь к скиллу — ключ конфига
(`skill:`), файл читается на старте и кладётся в системный промпт целиком. `SKILL.md` **не
редактируется**: бот-специфичная приписка (верни только JSON, инструменты не запускай) живёт в
`internal/intake/prompt.go`. Так у обоих фронтендов ровно один источник эвристик — то, чего требует
`docs/PHASE2.md` («keep that prompt derived from `SKILL.md` rather than forked from it»).

**Решение 4 — часы только в `cmd/secretary-bot`.** `bot.Handle` и `bot.ShouldPush` принимают
`now time.Time`. Это не церемония: именно так утренняя сводка тестируется без ожидания утра, и это
то же правило, что уже действует в `cmd/secretary`.

**Решение 5 — «список» бот отдаёт через `brief --json`-подобную сборку, а не через `format.go`.**
Колоночная раскладка `list` живёт в `cmd/secretary/format.go` и заточена под терминал (ширины,
ANSI-цвет). В Telegram она не нужна и импортировать `main`-пакет нельзя. Бот рендерит свой плоский
список из `doc.Tasks()` + `schedule` — короткий рендер в `internal/bot/render.go`. Дублирование
здесь дешевле связывания: у терминала и у чата разные ограничения по ширине.

**Решение 6 — один чат, всё остальное отвергается.** Артефакт не знает про аккаунты. `chat_id` в
конфиге; апдейт из любого другого чата логируется и молча игнорируется. Токен — только из
переменной окружения `TELEGRAM_BOT_TOKEN`, в `secretary.yml` его нет (файл в git).

**Решение 7 — записи сериализованы мьютексом.** `Store` не держит состояния между вызовами и
перечитывает файл каждый раз, но два сообщения подряд не должны переплести read-modify-write.
Один `sync.Mutex` на весь цикл load→mutate→save.

**Решение 8 — сводка внутри процесса, с защитой от повтора.** Горутина раз в минуту спрашивает
чистую функцию `ShouldPush(now, at, lastPushed)`. Дата последней отправки хранится в маленьком
JSON-файле рядом с артефактом, поэтому перезапуск бота не рассылает сводку второй раз, а ноутбук,
проснувшийся в 11:00, всё-таки получит утреннюю сводку за сегодня. При `all_clear` и
`quiet_when_clear: true` сводка не шлётся — ежедневное «ничего не горит» приучает её игнорировать.

## Technical Details

### Конфиг: новый блок `telegram:`

`internal/config` расширяется одной структурой; все поля имеют значения по умолчанию, отсутствие
блока — не ошибка, ровно как сейчас.

```yaml
# secretary.yml
skill: .claude/skills/secretary/SKILL.md   # источник промпта для интейка
telegram:
  chat_id: 0                # обязателен для бота; 0 = бот откажется стартовать
  brief_at: "08:00"         # локальное время утренней сводки
  quiet_when_clear: true    # не слать сводку в спокойный день
  poll_timeout_sec: 30      # long polling
  state: docs/.bot-state.json
  claude_bin: claude        # чем вызывать интейк
  claude_timeout_sec: 60
```

Токен — `TELEGRAM_BOT_TOKEN` из окружения. Отсутствие токена или `chat_id: 0` — отказ на старте с
внятным русским сообщением и кодом `2`.

### Контракт интейка

`claude -p` получает системный промпт `SKILL.md` + приписку и должен вернуть **только** JSON:

```json
{
  "action": "add" | "brief" | "list" | "unclear",
  "reply": "Записал в Личное, тир 2, срок 2 сентября, сэр.",
  "task": {
    "title": "Забрать шорты из доставки",
    "tier": 2,
    "section": "Личное",
    "from": "себя",
    "due": "2026-09-02",
    "effort": "1ч",
    "context": "@город",
    "labels": ["доставка"],
    "notes": ["..."]
  },
  "filter": { "section": "…", "tier": 0, "label": "…", "context": "@…" }
}
```

- `task` присутствует только при `action: "add"`, `filter` — только при `"list"` (все поля
  необязательны).
- `unclear` — бот пересылает `reply` как есть и ничего не пишет. Правило «ask-vs-assume» из
  `SKILL.md` действует и здесь: в чате уточняющий вопрос дешевле неверной записи.
- Промпту передаются сегодняшняя дата, список разделов и контекстов из конфига — иначе
  относительные даты («до среды») и раздел он вычислить не может.
- Ответ Claude парсится строго: невалидный JSON, неизвестный `action`, тир вне 1..3 или дата не в
  `mdstore.DateLayout` → ошибка, бот отвечает «Не разобрал, сэр» и **ничего не пишет в артефакт**.
  Молчаливая догадка тут хуже отказа — то же правило, что у парсера артефакта.

### Поток обработки одного сообщения

```
Update → chat_id совпал? ─нет→ игнор (лог)
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

`add` всегда отвечает и подтверждением, и свежей сводкой — то же правило, что у `SKILL.md`
(«Always `secretary brief` after `add`»): новое дело может сделать срочным другое.

### Разбиение по пакетам

| Пакет | Ответственность | Часы |
|---|---|---|
| `internal/telegram` | `getUpdates`/`sendMessage`, типы `Update`/`Message`, backoff | нет |
| `internal/intake` | `Classifier` интерфейс + `claude -p` реализация, сборка промпта, парс JSON | нет (`now` аргумент) |
| `internal/bot` | диспетчер `Handle`, рендер списка, `ShouldPush`, файл состояния | нет (`now` аргумент) |
| `cmd/secretary-bot` | конфиг, токен, `time.Now()`, цикл поллинга, тикер, коды выхода | **да, единственное место** |

## What Goes Where

- **Implementation Steps** (`[ ]` checkboxes): всё, что делается в этом репозитории — код, тесты,
  документация.
- **Post-Completion** (без чекбоксов): регистрация бота в @BotFather, получение токена и chat_id,
  первый живой прогон с настоящим `claude -p`, установка в launchd, этап 2 (VPS).

## Implementation Steps

### Task 1: Блок `telegram:` и ключ `skill:` в конфиге

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `secretary.yml`

- [ ] добавить в `config.go` структуру `Telegram` с полями `ChatID`, `BriefAt`, `QuietWhenClear`,
      `PollTimeoutSec`, `State`, `ClaudeBin`, `ClaudeTimeoutSec` и yaml-тегами
- [ ] добавить поля `Skill string` и `Telegram Telegram` в `Config`, константы значений по умолчанию
      рядом с существующими `Default*`
- [ ] дополнить `Default()` и логику дозаполнения в `Load` так, чтобы отсутствующий блок `telegram:`
      и отсутствующий `skill:` давали дефолты, а не нули
- [ ] добавить `skill:` и закомментированный пример блока `telegram:` в `secretary.yml`
- [ ] написать тесты: полный блок читается; отсутствующий блок даёт дефолты; частично заполненный
      блок дозаполняется; битый YAML — ошибка
- [ ] запустить `go test ./internal/config/... -cover` — должно пройти и остаться ≥80%

### Task 2: Клиент Telegram Bot API на net/http

**Files:**
- Create: `internal/telegram/client.go`
- Create: `internal/telegram/client_test.go`

- [ ] создать `Client` с полями `Token`, `BaseURL` (по умолчанию `https://api.telegram.org`),
      `HTTP *http.Client` — `BaseURL` полем, чтобы `httptest` подменял транспорт целиком
- [ ] описать минимальные типы `Update{UpdateID, Message}` и `Message{MessageID, Chat{ID}, Text}` —
      только те поля, которые бот действительно читает
- [ ] реализовать `GetUpdates(ctx, offset, timeoutSec) ([]Update, error)` — long polling, разбор
      конверта `{"ok":bool,"result":…,"description":…}`, `ok:false` → ошибка с текстом из
      `description`
- [ ] реализовать `SendMessage(ctx, chatID int64, text string) error`; резать текст на части по
      лимиту Telegram в 4096 символов по границам строк
- [ ] написать тесты против `httptest.Server`: успешный `GetUpdates` с двумя апдейтами; пустой
      результат; `ok:false`; HTTP 500; `SendMessage` шлёт верное тело; длинный текст режется на две
      части
- [ ] запустить `go test ./internal/telegram/... -cover` — должно пройти

### Task 3: Интейк — `Classifier` и парсер ответа

**Files:**
- Create: `internal/intake/intake.go`
- Create: `internal/intake/prompt.go`
- Create: `internal/intake/intake_test.go`

- [ ] проверить реальные флаги CLI (`claude --help`) и зафиксировать выбранную форму вызова
      комментарием — план предполагает `-p` + `--output-format json` + системный промпт и запрет
      инструментов, но точные имена флагов сверяются с установленной версией
- [ ] описать `Intent{Action, Reply, Task *TaskFields, Filter *Filter}` и интерфейс
      `Classifier{ Classify(ctx, text string, now time.Time) (Intent, error) }`
- [ ] в `prompt.go` собрать системный промпт: содержимое `SKILL.md` (читается на старте, путь из
      конфига) + приписка про «верни только JSON по схеме, инструменты не запускай» + сегодняшняя
      дата, разделы и контексты из конфига
- [ ] реализовать `ParseIntent([]byte) (Intent, error)` строго: неизвестный `action`, тир вне 1..3,
      дата не в `mdstore.DateLayout`, `add` без `title` — всё это ошибки, а не «догадаемся»
- [ ] реализовать `claudeClassifier` — `exec.CommandContext` с таймаутом из конфига; ненулевой код
      выхода и таймаут возвращают ошибку с stderr в тексте
- [ ] написать тесты **только на чистые части** (без запуска подпроцесса): `ParseIntent` на всех
      четырёх `action`, на мусоре, на JSON в markdown-заборе, на тире 0 и 4, на кривой дате;
      сборка промпта содержит текст `SKILL.md`, дату и список разделов
- [ ] запустить `go test ./internal/intake/... -cover` — должно пройти

### Task 4: Диспетчер `bot.Handle` и рендер списка

**Files:**
- Create: `internal/bot/bot.go`
- Create: `internal/bot/render.go`
- Create: `internal/bot/bot_test.go`

- [ ] создать `Bot` с полями store, cfg, persona, classifier, sender и `sync.Mutex`; интерфейс
      `Sender{ SendMessage(ctx, chatID int64, text string) error }`, чтобы тесты не ходили в сеть
- [ ] реализовать `Handle(ctx, upd telegram.Update, now time.Time) error`: отсев чужого `chat_id`,
      мьютекс на весь цикл load→mutate→save, диспетчеризация по `Intent.Action`
- [ ] ветка `add`: собрать `task.Task` из `Intent.Task`, `doc.Add` → `doc.Sort` → `Store.Save`,
      ответ = подтверждение интейка + `brief.Build(doc, now, cfg).Text(persona)`
- [ ] ветка `brief`: `brief.Build(…).Text(persona)`; ветка `unclear`: только `Intent.Reply`
- [ ] в `render.go` реализовать `List(doc, now, cfg, filter) string` — плоский текст для чата
      (глиф статуса, `!!`/`!`, заголовок, срок), без ANSI и без колонок; пустой результат даёт
      внятную русскую фразу
- [ ] написать тесты с фейковым `Classifier` и фейковым `Sender` на временном артефакте: `add`
      пишет задачу и отвечает сводкой; `brief` не меняет файл; `list` фильтрует по разделу и по
      тиру; чужой `chat_id` не пишет и не отвечает; ошибка классификатора не портит артефакт
- [ ] написать тест на сериализацию: два `Handle` из разных горутин дают два дела и валидный файл
      (прогнать под `-race`)
- [ ] запустить `go test ./internal/bot/... -race -cover` — должно пройти

### Task 5: Расписание утренней сводки и файл состояния

**Files:**
- Create: `internal/bot/schedule.go`
- Create: `internal/bot/schedule_test.go`
- Modify: `internal/bot/bot.go`

- [ ] реализовать чистую `ShouldPush(now time.Time, at string, lastPushed time.Time) bool` — true,
      когда `now` в тот же календарный день уже прошёл `at`, а `lastPushed` — более ранний день
- [ ] реализовать `LoadState(path)`/`SaveState(path, date)` над маленьким JSON (`{"last_push":
      "2026-08-30"}`); отсутствующий файл — не ошибка, запись атомарная (temp + rename), как в
      `mdstore`
- [ ] добавить `Bot.PushBrief(ctx, now) (sent bool, err error)`: собрать сводку, при `IsClear()` и
      `quiet_when_clear` не слать, после успешной отправки записать дату состояния
- [ ] написать тесты `ShouldPush`: до времени; после времени в тот же день; уже отправлено сегодня;
      пропущенный вчерашний пуш (ноут спал); кривая строка `at` — ошибка на старте, не в цикле
- [ ] написать тесты состояния: отсутствующий файл; битый JSON; round-trip записи и чтения
- [ ] написать тесты `PushBrief`: спокойный день молчит при `quiet_when_clear: true` и говорит при
      `false`; повторный вызов в тот же день не шлёт второй раз
- [ ] запустить `go test ./internal/bot/... -cover` — должно пройти

### Task 6: Бинарь `cmd/secretary-bot` — цикл поллинга и часы

**Files:**
- Create: `cmd/secretary-bot/main.go`
- Create: `cmd/secretary-bot/main_test.go`
- Modify: `Makefile`

- [ ] написать `main.go` по образцу `cmd/secretary/main.go`: `run(args, stdout, stderr) int`, флаги
      `--config`, `--artifact`, коды выхода `0`/`1`/`2`, русские сообщения об ошибках
- [ ] проверки на старте: `TELEGRAM_BOT_TOKEN` задан, `telegram.chat_id != 0`, `SKILL.md` читается,
      `brief_at` разбирается — каждая с отдельным внятным сообщением и кодом `2`
- [ ] реализовать цикл long polling: `GetUpdates` с сохранением `offset`, экспоненциальный backoff
      на сетевых ошибках, корректное завершение по SIGINT/SIGTERM через `context.CancelFunc`
- [ ] реализовать горутину сводки: тик раз в минуту → `ShouldPush(time.Now(), …)` → `PushBrief`;
      **`time.Now()` вызывается только здесь и в цикле поллинга**
- [ ] добавить в `Makefile` цели сборки и запуска бота рядом с существующими
- [ ] написать тесты: отсутствие токена → код `2` и сообщение; `chat_id: 0` → код `2`; нечитаемый
      `SKILL.md` → код `2`; кривой `brief_at` → код `2`
- [ ] написать acceptance-тест на фейковом Telegram (`httptest`) и фейковом классификаторе: один
      апдейт «купить хлеб» → артефакт содержит новое дело → в отправленном сообщении есть
      подтверждение; цикл завершается по отмене контекста
- [ ] запустить `go test ./cmd/... -race` — должно пройти

### Task 7: Verify acceptance criteria

- [ ] проверить три сценария из Overview на фейковом транспорте: запись дела, запрос списка,
      утренняя сводка
- [ ] проверить краевые случаи: чужой `chat_id`, невалидный ответ Claude, недоступный Telegram,
      пустой артефакт, сообщение длиннее 4096 символов
- [ ] убедиться, что `cmd/secretary` и `docs/duties.md` не изменились: `git diff` по
      `cmd/secretary/` и `internal/{mdstore,brief,schedule,grouping,voice,task}` пуст
- [ ] убедиться, что `time.Now()` встречается ровно в `cmd/secretary/main.go` и
      `cmd/secretary-bot/main.go`: `grep -rn "time.Now()" --include=*.go . | grep -v _test.go`
- [ ] запустить полный набор: `go test ./... && go vet ./...`
- [ ] проверить покрытие: `go test ./internal/... -cover` — все пакеты ≥80%

### Task 8: [Final] Update documentation

- [ ] дописать в `README.md` раздел про бота: переменная окружения, блок конфига, запуск, что бот
      понимает
- [ ] обновить `docs/PHASE2.md`: пометить «Integration B» как реализованную, зафиксировать, что
      интейк пошёл через `claude -p` с тем же `SKILL.md`, а не через Anthropic API
- [ ] обновить `CLAUDE.md`: новые пакеты в схеме зависимостей, правило «часы в `cmd/`, не в
      `internal/`» распространяется на второй бинарь, `SKILL.md` — единственный источник эвристик
      для обоих фронтендов
- [ ] переместить этот план в `docs/plans/completed/`

## Post-Completion

*Требует ручных действий или внешних систем — без чекбоксов, справочно*

**Регистрация и первый запуск** (обязательно перед реальным использованием):

- завести бота у `@BotFather`, получить токен, положить в окружение (`TELEGRAM_BOT_TOKEN`), **не в
  `secretary.yml`** — файл в git
- узнать свой `chat_id`: написать боту и посмотреть `getUpdates`, либо через `@userinfobot`
- прописать `chat_id` в `secretary.yml`, проверить, что чужие чаты действительно отвергаются
- первый живой прогон с настоящим `claude -p`: интейк — единственная часть, которую юнит-тесты не
  трогают, потому что подпроцесс в тестах не запускается. Прогнать вручную на 5–10 реальных фразах
  («надо до пятницы отдать документы нотариусу», «Анна попросила отчёт», «что горит») и сверить
  результат с тем, что даёт тот же текст через Claude Code — расхождение означает, что приписка в
  `prompt.go` борется со `SKILL.md`
- проверить скорость: `claude -p` отвечает секунды, не миллисекунды. Если пауза раздражает, послать
  «печатает…» (`sendChatAction`) до вызова интейка

**Установка как сервис**:

- launchd-plist на ноуте (`KeepAlive`, рабочий каталог — корень репозитория, окружение с токеном)
- проверить поведение при засыпании ноутбука: long polling должен пережить разрыв и переподключиться
  backoff'ом, а пропущенная утренняя сводка — прийти при пробуждении

**Этап 2 — VPS (отдельная задача, вне scope этого плана)**:

- приватный git-репозиторий как слой синхронизации: бот делает commit+push после каждой записи и
  pull перед чтением; на ноуте — обычные pull/push
- деплой: бинарь + `claude` в PATH на сервере (интейк через `claude -p` требует установленного и
  авторизованного Claude Code) — если это окажется неудобно, `internal/intake` меняет одну
  реализацию `Classifier` на вызов Anthropic API, не трогая `bot` и `telegram`
- конфликты слияния: артефакт отсортирован детерминированно, но одновременная правка в двух местах
  всё равно даёт конфликт — нужна стратегия (например, бот всегда pull --rebase перед записью)

**Очевидное продолжение, сознательно оставленное за рамками**:

- `done` и `defer` из чата. Перечисление `Intent.Action` оставляет для них место, но запрос был про
  запись, список и сводку — расширять его самовольно не стоит. Когда понадобится: одна ветка в
  `switch`, вызов `doc.Done(id, now)` и разрешение заголовка в id тем же способом, каким это делает
  `SKILL.md`.
