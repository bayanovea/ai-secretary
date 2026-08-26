package main

import (
	"strings"
	"testing"
)

// This file is the acceptance pass: one test per promise made in the plan's
// Overview, plus the edge cases and the hand-editing guarantee. The unit tests
// under internal/… prove each piece works; these prove the assembled binary
// keeps the promises a user was made, and they are what a later refactor has to
// answer to.

// acceptanceWorkspace seeds the duties the requirement tests read. The dates are
// chosen so that the advance-start nudge can be observed on an exact day:
// «Согласовать смету» is тир 2, срок 2026-09-10, объём 2д, so its buffer is
// max(1, 0.25·2) = 1 day, its lead is 3 days, and its latest start is 2026-09-07
// — three days before the deadline, which is the whole point of the feature.
func acceptanceWorkspace(t *testing.T) *workspace {
	t.Helper()
	w := newWorkspace(t)

	w.mustRun(t, "--today=2026-08-27", "add", "--title=Согласовать смету", "--tier=2",
		"--from=Анна Петрова", "--due=2026-09-10", "--effort=2д", "--section=Работа",
		"--context=@компьютер", "--labels=финансы")
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Забрать документы у нотариуса", "--tier=1",
		"--from=Марина", "--due=2026-09-04", "--effort=2ч", "--section=Личное",
		"--context=@город", "--labels=переезд, документы", "--note=взять паспорт")
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Купить подарок Марине", "--tier=2",
		"--from=себя", "--due=2026-09-12", "--effort=1ч", "--section=Личное",
		"--context=@город", "--labels=отношения")
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Разобрать антресоль", "--tier=3",
		"--from=себя", "--section=Дом", "--context=@дом")
	return w
}

// briefBlock returns the lines of one section of a briefing — everything from
// its heading to the next blank line. Assertions about a bucket have to be
// scoped to that bucket, or «просрочено» somewhere in the file passes for
// «просрочено» in the task under test.
func briefBlock(out, heading string) string {
	start := strings.Index(out, heading)
	if start < 0 {
		return ""
	}
	rest := out[start+len(heading):]
	if end := strings.Index(rest, "\n\n"); end >= 0 {
		return rest[:end]
	}
	return rest
}

// TestAcceptanceOverviewRequirements walks the Overview's key capabilities one
// by one against the real binary.
func TestAcceptanceOverviewRequirements(t *testing.T) {
	w := acceptanceWorkspace(t)

	t.Run("три тира", func(t *testing.T) {
		for tier, want := range map[string]string{
			"1": "Забрать документы у нотариуса",
			"2": "Согласовать смету",
			"3": "Разобрать антресоль",
		} {
			out := w.mustRun(t, "--today=2026-08-27", "list", "--tier="+tier)
			if !strings.Contains(out, want) {
				t.Errorf("list --tier=%s не показал %q:\n%s", tier, want, out)
			}
		}
		artifact := w.read(t, w.artifact)
		for _, want := range []string{"тир:1", "тир:2", "тир:3"} {
			if !strings.Contains(artifact, want) {
				t.Errorf("артефакт не хранит %q:\n%s", want, artifact)
			}
		}
	})

	t.Run("от кого задача", func(t *testing.T) {
		artifact := w.read(t, w.artifact)
		for _, want := range []string{"от:Марина", "от:Анна Петрова", "от:себя"} {
			if !strings.Contains(artifact, want) {
				t.Errorf("артефакт не хранит %q:\n%s", want, artifact)
			}
		}
		if out := w.mustRun(t, "--today=2026-08-27", "list"); !strings.Contains(out, "от Марина") {
			t.Errorf("list не показывает заказчика:\n%s", out)
		}
	})

	t.Run("срок и дата последнего старта", func(t *testing.T) {
		// За день до последнего старта — «скоро», в самый день — «пора
		// начинать», и срок при этом ещё впереди: подгоняют заранее.
		early := w.mustRun(t, "--today=2026-09-06", "list", "--tier=2", "--section=Работа")
		if !strings.Contains(early, "скоро") {
			t.Errorf("за день до последнего старта статус не «скоро»:\n%s", early)
		}

		out := w.mustRun(t, "--today=2026-09-07", "brief")
		if !strings.Contains(briefBlock(out, "Начинать сегодня:"), "Согласовать смету") {
			t.Errorf("сводка не подняла задачу в день последнего старта:\n%s", out)
		}
		// Подгоняют заранее, а не постфактум: срок 2026-09-10 ещё впереди.
		if strings.Contains(briefBlock(out, "Просрочено:"), "Согласовать смету") {
			t.Errorf("задача с будущим сроком объявлена просроченной:\n%s", out)
		}
	})

	t.Run("разделы и метки", func(t *testing.T) {
		section := w.mustRun(t, "--today=2026-08-27", "list", "--section=Личное")
		if !strings.Contains(section, "Забрать документы у нотариуса") ||
			strings.Contains(section, "Согласовать смету") {
			t.Errorf("фильтр по разделу не сработал:\n%s", section)
		}
		label := w.mustRun(t, "--today=2026-08-27", "list", "--label=переезд")
		if !strings.Contains(label, "Забрать документы у нотариуса") ||
			strings.Contains(label, "Купить подарок Марине") {
			t.Errorf("фильтр по метке не сработал:\n%s", label)
		}
	})

	t.Run("группы «заодно»", func(t *testing.T) {
		out := w.mustRun(t, "--today=2026-09-02", "brief")
		if !strings.Contains(out, "Заодно:") || !strings.Contains(out, "@город") {
			t.Errorf("сводка не предложила общий выход в город:\n%s", out)
		}
		for _, want := range []string{"Забрать документы у нотариуса", "Купить подарок Марине"} {
			if !strings.Contains(out, want) {
				t.Errorf("в группе нет %q:\n%s", want, out)
			}
		}
	})

	t.Run("голос Альфреда", func(t *testing.T) {
		out := w.mustRun(t, "--today=2026-09-07", "brief")
		if !strings.Contains(out, "сэр") {
			t.Errorf("сводка звучит не по-альфредовски:\n%s", out)
		}
		// Одна и та же дата — одна и та же формулировка: без math/rand.
		if again := w.mustRun(t, "--today=2026-09-07", "brief"); again != out {
			t.Errorf("сводка не воспроизводима:\n--- раз ---\n%s\n--- два ---\n%s", out, again)
		}
	})

	t.Run("свободный текст", func(t *testing.T) {
		// Разбор свободного текста живёт в навыке, а не в бинарнике: проверяем,
		// что он описывает переход «фраза → флаги» и обязан звать brief.
		skill := readSkill(t)
		for _, want := range []string{"secretary add", "secretary brief", "--title", "--tier", "--from", "--due"} {
			if !strings.Contains(skill, want) {
				t.Errorf("навык не описывает %q", want)
			}
		}
	})
}

// TestAcceptanceEdgeCases covers the states the artifact can legitimately be in
// and the ones it must refuse.
func TestAcceptanceEdgeCases(t *testing.T) {
	t.Run("без срока", func(t *testing.T) {
		w := newWorkspace(t)
		w.mustRun(t, "--today=2026-08-27", "add", "--title=Когда-нибудь разобрать гараж",
			"--tier=3", "--section=Дом")

		if out := w.mustRun(t, "--today=2026-08-27", "list"); !strings.Contains(out, "без срока") {
			t.Errorf("задача без срока показана иначе:\n%s", out)
		}
		out := w.mustRun(t, "--today=2026-08-27", "brief")
		for _, unwanted := range []string{"Просрочено:", "Начинать сегодня:", "Скоро:"} {
			if strings.Contains(out, unwanted) {
				t.Errorf("задача без срока попала в %q:\n%s", unwanted, out)
			}
		}
	})

	t.Run("без объёма", func(t *testing.T) {
		w := newWorkspace(t)
		w.mustRun(t, "--today=2026-08-27", "add", "--title=Сдать отчёт", "--tier=1",
			"--from=Босс", "--due=2026-08-28", "--section=Работа")

		// Без объёма даты последнего старта нет, и её место занимает срок.
		if out := w.mustRun(t, "--today=2026-08-28", "brief"); !strings.Contains(out, "Начинать сегодня:") {
			t.Errorf("без объёма срок не подменил дату старта:\n%s", out)
		}
		if out := w.mustRun(t, "--today=2026-08-27", "check"); !strings.Contains(out, "Неполные данные: T-001") {
			t.Errorf("check не заметил тир-1 без объёма:\n%s", out)
		}
	})

	t.Run("неизвестный раздел", func(t *testing.T) {
		w := newWorkspace(t)
		_, stderr, code := w.run(t, "--today=2026-08-27", "add", "--title=Помыть машину",
			"--tier=3", "--section=Гараж")
		if code == 0 {
			t.Fatal("add в раздел вне конфигурации должен падать")
		}
		if !strings.Contains(stderr, "Гараж") {
			t.Errorf("ошибка не называет раздел: %q", stderr)
		}

		// Но раздел, уже написанный руками, файл не ломает: check о нём
		// сообщает и возвращает ноль.
		w.write(t, w.artifact, "# Обязанности\n\n## Гараж\n\n- [ ] `T-007` **Поменять масло**\n"+
			"  тир:3 · от:себя · создано:2026-08-01\n\n## Сделано\n")
		out := w.mustRun(t, "--today=2026-08-27", "check")
		if !strings.Contains(out, "Разделы вне конфигурации: Гараж") {
			t.Errorf("check не отметил чужой раздел:\n%s", out)
		}
	})

	t.Run("повторяющиеся идентификаторы", func(t *testing.T) {
		w := newWorkspace(t)
		w.write(t, w.artifact, "# Обязанности\n\n## Работа\n\n"+
			"- [ ] `T-001` **Первая**\n  тир:2 · от:себя · создано:2026-08-01\n\n"+
			"- [ ] `T-001` **Вторая**\n  тир:2 · от:себя · создано:2026-08-02\n")

		_, stderr, code := w.run(t, "--today=2026-08-27", "check")
		if code == 0 {
			t.Fatal("дубль идентификатора должен ломать разбор")
		}
		if !strings.Contains(stderr, "T-001") {
			t.Errorf("ошибка не называет дубль: %q", stderr)
		}
	})

	t.Run("пустой артефакт", func(t *testing.T) {
		w := newWorkspace(t)
		w.write(t, w.artifact, "# Обязанности\n\n<!-- secretary:v1 -->\n\n## Работа\n\n## Сделано\n")

		w.mustRun(t, "--today=2026-08-27", "check")
		w.mustRun(t, "--today=2026-08-27", "fmt")
		w.mustRun(t, "--today=2026-08-27", "archive")
		if out := w.mustRun(t, "--today=2026-08-27", "list"); !strings.Contains(out, "Ничего не найдено") {
			t.Errorf("пустой list молчит иначе:\n%s", out)
		}
		if out := w.mustRun(t, "--today=2026-08-27", "brief"); strings.Contains(out, "Просрочено:") {
			t.Errorf("пустая сводка не пуста:\n%s", out)
		}
	})

	t.Run("только сделанное", func(t *testing.T) {
		w := newWorkspace(t)
		w.mustRun(t, "--today=2026-08-27", "add", "--title=Оплатить страховку", "--tier=2",
			"--due=2026-08-20", "--section=Финансы")
		w.mustRun(t, "--today=2026-08-27", "done", "T-001")

		if out := w.mustRun(t, "--today=2026-08-27", "list"); !strings.Contains(out, "Ничего не найдено") {
			t.Errorf("сделанное попало в list:\n%s", out)
		}
		out := w.mustRun(t, "--today=2026-08-27", "brief")
		if strings.Contains(out, "Просрочено:") {
			t.Errorf("сделанное с прошедшим сроком отчитано как просроченное:\n%s", out)
		}
		if !strings.Contains(w.read(t, w.artifact), "выполнено:2026-08-27") {
			t.Error("дата выполнения не записана")
		}
	})
}

// handEdited is the artifact as a human leaves it: ragged spacing, a stale hint
// line copied by hand, a field key the parser has never heard of, a section
// outside the configuration, and notes that must survive verbatim.
const handEdited = `# Обязанности

Свои заметки сверху, которые Альфреда не касаются.

<!-- secretary:v1 -->
> Обновлено: 2026-08-20 · К вашим услугам, сэр.

## Личное

- [ ] ` + "`T-042`" + ` **Отдать документы нотариусу**
  тир:1 · от:Марина · срок:2026-09-04 · объём:2ч · контекст:@город · создано:2026-08-27 · переносов:1
  метки: переезд, документы
  🔴 подсказка, скопированная руками из вчерашней сводки
  > нотариус до 18:00
  > взять паспорт

- [ ]  ` + "`T-043`" + `  **Купить подарок**
    тир:2 ·  от:себя  ·  срок:2026-09-10  ·  объём:1ч  · контекст:@город · создано:2026-08-20 · неизвестноеполе:чепуха
  метки:   отношения

## Гараж

- [ ] ` + "`T-044`" + ` **Поменять масло**
  тир:3 · от:себя · создано:2026-08-01

## Сделано

- [x] ` + "`T-041`" + ` **Оплатить страховку** · выполнено:2026-08-20
  тир:2 · от:себя · создано:2026-08-01
`

// TestAcceptanceHandEditingIsSafe is the promise the README makes: edit the file
// however you like, run `fmt`, and nothing you wrote is lost.
func TestAcceptanceHandEditingIsSafe(t *testing.T) {
	w := newWorkspace(t)
	w.write(t, w.artifact, handEdited)

	w.mustRun(t, "--today=2026-08-27", "check")
	w.mustRun(t, "--today=2026-08-27", "fmt")
	after := w.read(t, w.artifact)

	// Каждая единица данных пережила приведение в порядок.
	for _, want := range []string{
		"Свои заметки сверху",
		"`T-042`", "**Отдать документы нотариусу**",
		"тир:1", "от:Марина", "срок:2026-09-04", "объём:2ч", "контекст:@город", "переносов:1",
		"метки: переезд, документы", "> нотариус до 18:00", "> взять паспорт",
		"`T-043`", "**Купить подарок**", "метки: отношения",
		"## Гараж", "`T-044`", "**Поменять масло**",
		"## Сделано", "`T-041`", "выполнено:2026-08-20",
	} {
		if !strings.Contains(after, want) {
			t.Errorf("после fmt потеряно %q:\n%s", want, after)
		}
	}

	// Ровно две вещи fmt снимает намеренно: подсказки он пересчитывает сам,
	// а ключей, которых не знает, не хранит. И то и другое описано в README.
	if strings.Contains(after, "скопированная руками") {
		t.Errorf("устаревшая подсказка пережила fmt:\n%s", after)
	}
	if strings.Contains(after, "неизвестноеполе") {
		t.Errorf("неизвестный ключ пережил fmt вопреки README:\n%s", after)
	}

	// Второй прогон не двигает ни байта.
	w.mustRun(t, "--today=2026-08-27", "fmt")
	if again := w.read(t, w.artifact); again != after {
		t.Errorf("fmt не идемпотентен:\n--- раз ---\n%s\n--- два ---\n%s", after, again)
	}

	// И задачи по-прежнему адресуются по идентификатору.
	w.mustRun(t, "--today=2026-08-27", "defer", "T-042", "--due=2026-09-20")
	w.mustRun(t, "--today=2026-08-27", "done", "T-043")
	final := w.read(t, w.artifact)
	if !strings.Contains(final, "срок:2026-09-20") || !strings.Contains(final, "переносов:2") {
		t.Errorf("перенос не применился к правленной руками задаче:\n%s", final)
	}
}
