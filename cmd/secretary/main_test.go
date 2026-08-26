package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// binary is the compiled command, built once for the whole package. Every test
// then runs the real thing — argument parsing, exit codes and all — which is
// the wiring the unit tests in internal/… cannot reach.
var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "secretary-bin")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	binary = filepath.Join(dir, "secretary")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "не собрался:", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// workspace is a temp directory holding a config, an artifact and an archive,
// so that no test touches the repository's own docs/duties.md.
type workspace struct {
	dir      string
	artifact string
	archive  string
}

const configYAML = `persona: alfred
artifact: duties.md
archive: archive.md
sections: [Работа, Личное, Дом, Здоровье, Отношения, Финансы]
contexts: ["@город", "@дом", "@телефон", "@компьютер", "@работа"]
tiers:
  "1": {name: Критично, buffer_min_days: 2, buffer_ratio: 0.5}
  "2": {name: Важно,    buffer_min_days: 1, buffer_ratio: 0.25}
  "3": {name: Фоновое,  buffer_min_days: 0, buffer_ratio: 0}
soon_window_days: 3
stale_days: 14
`

func newWorkspace(t *testing.T) *workspace {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secretary.yml"), []byte(configYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return &workspace{
		dir:      dir,
		artifact: filepath.Join(dir, "duties.md"),
		archive:  filepath.Join(dir, "archive.md"),
	}
}

// run executes the binary in the workspace and returns what the user would see.
func (w *workspace) run(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = w.dir

	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()

	switch e := err.(type) {
	case nil:
	case *exec.ExitError:
		code = e.ExitCode()
	default:
		t.Fatalf("запуск %v: %v", args, err)
	}
	return out.String(), errOut.String(), code
}

// mustRun fails the test if the command did not succeed.
func (w *workspace) mustRun(t *testing.T, args ...string) string {
	t.Helper()
	stdout, stderr, code := w.run(t, args...)
	if code != 0 {
		t.Fatalf("%v: код %d, stderr: %s", args, code, stderr)
	}
	return stdout
}

func (w *workspace) read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("чтение %s: %v", path, err)
	}
	return string(data)
}

func (w *workspace) write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// seed fills the workspace with the four duties every listing test expects.
func (w *workspace) seed(t *testing.T) {
	t.Helper()
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Подготовить отчёт за Q3", "--tier=1",
		"--from=Анна Петрова", "--due=2026-09-05", "--effort=5д", "--section=Работа",
		"--context=@компьютер", "--labels=отчётность, финансы", "--note=нужны данные от финотдела")
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Забрать документы у нотариуса", "--tier=1",
		"--from=Марина", "--due=2026-08-29", "--effort=1ч", "--section=Личное", "--context=@город",
		"--labels=переезд, документы")
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Купить подарок Марине", "--tier=2",
		"--due=2026-09-01", "--effort=2ч", "--section=Личное", "--context=@город", "--labels=отношения")
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Согласовать отпуск", "--tier=3",
		"--section=Работа", "--context=@работа")
}

func TestAddAssignsSequentialIDs(t *testing.T) {
	w := newWorkspace(t)

	for i, want := range []string{"T-001", "T-002", "T-003"} {
		out := w.mustRun(t, "--today=2026-08-27", "add",
			fmt.Sprintf("--title=Задача %d", i+1), "--tier=2", "--section=Личное")
		if got := strings.TrimSpace(out); got != want {
			t.Fatalf("add №%d напечатал %q, ожидалось %q", i+1, got, want)
		}
	}

	artifact := w.read(t, w.artifact)
	for _, want := range []string{"`T-001`", "`T-003`", "## Личное", "создано:2026-08-27"} {
		if !strings.Contains(artifact, want) {
			t.Errorf("артефакт не содержит %q:\n%s", want, artifact)
		}
	}
}

func TestAddFieldsRoundTripIntoArtifact(t *testing.T) {
	w := newWorkspace(t)
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Забрать документы", "--tier=1",
		"--from=Марина", "--due=2026-08-29", "--effort=1ч", "--section=Личное",
		"--context=@город", "--labels=переезд, документы", "--note=нотариус до 18:00",
		"--note=взять паспорт")

	artifact := w.read(t, w.artifact)
	for _, want := range []string{
		"тир:1", "от:Марина", "срок:2026-08-29", "объём:1ч", "контекст:@город",
		"метки: переезд, документы", "> нотариус до 18:00", "> взять паспорт",
	} {
		if !strings.Contains(artifact, want) {
			t.Errorf("артефакт не содержит %q:\n%s", want, artifact)
		}
	}
}

func TestAddDefaultsSectionAndFrom(t *testing.T) {
	w := newWorkspace(t)
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Без раздела", "--tier=3")

	artifact := w.read(t, w.artifact)
	if !strings.Contains(artifact, "## Работа") {
		t.Errorf("раздел по умолчанию не первый из конфигурации:\n%s", artifact)
	}
	if !strings.Contains(artifact, "от:себя") {
		t.Errorf("от: по умолчанию не «себя»:\n%s", artifact)
	}
}

func TestBriefBuckets(t *testing.T) {
	w := newWorkspace(t)
	w.seed(t)

	out := w.mustRun(t, "--today=2026-09-06", "brief")
	for _, want := range []string{"Просрочено:", "Подготовить отчёт за Q3", "Забрать документы у нотариуса", "Открыто:"} {
		if !strings.Contains(out, want) {
			t.Errorf("сводка не содержит %q:\n%s", want, out)
		}
	}
}

func TestBriefAllClearOnEmptyArtifact(t *testing.T) {
	w := newWorkspace(t)

	out := w.mustRun(t, "--today=2026-08-27", "brief")
	if strings.Contains(out, "Просрочено:") || strings.Contains(out, "Начинать сегодня:") {
		t.Errorf("пустой артефакт не должен ничего требовать:\n%s", out)
	}
	if !strings.Contains(out, "Открыто:") {
		t.Errorf("сводка без итоговой строки:\n%s", out)
	}
}

func TestBriefJSON(t *testing.T) {
	w := newWorkspace(t)
	w.seed(t)

	out := w.mustRun(t, "--today=2026-09-06", "brief", "--json")

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("brief --json не разбирается: %v\n%s", err, out)
	}
	for _, key := range []string{"date", "all_clear", "total", "counts", "overdue", "start_now", "soon", "groups", "complaints"} {
		if _, ok := parsed[key]; !ok {
			t.Errorf("в JSON нет ключа %q: %s", key, out)
		}
	}
	if parsed["date"] != "2026-09-06" {
		t.Errorf("date = %v, ожидалось 2026-09-06", parsed["date"])
	}
	if overdue, ok := parsed["overdue"].([]interface{}); !ok || len(overdue) == 0 {
		t.Errorf("ожидались просроченные задачи: %s", out)
	}
}

func TestListFilters(t *testing.T) {
	w := newWorkspace(t)
	w.seed(t)

	cases := []struct {
		name    string
		args    []string
		want    []string
		notWant []string
	}{
		{
			name:    "всё",
			args:    []string{"list"},
			want:    []string{"T-001", "T-002", "T-003", "T-004"},
			notWant: nil,
		},
		{
			name:    "по тиру",
			args:    []string{"list", "--tier=1"},
			want:    []string{"T-001", "T-002"},
			notWant: []string{"T-003", "T-004"},
		},
		{
			name:    "по разделу",
			args:    []string{"list", "--section=Личное"},
			want:    []string{"T-002", "T-003"},
			notWant: []string{"T-001", "T-004"},
		},
		{
			name:    "по метке",
			args:    []string{"list", "--label=переезд"},
			want:    []string{"T-002"},
			notWant: []string{"T-001", "T-003", "T-004"},
		},
		{
			name:    "по контексту",
			args:    []string{"list", "--context=@город"},
			want:    []string{"T-002", "T-003"},
			notWant: []string{"T-001", "T-004"},
		},
		{
			name:    "пустой результат",
			args:    []string{"list", "--label=неттакой"},
			want:    []string{"Ничего не найдено"},
			notWant: []string{"T-001"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := w.mustRun(t, append([]string{"--today=2026-08-27"}, tc.args...)...)
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("вывод не содержит %q:\n%s", want, out)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(out, notWant) {
					t.Errorf("вывод неожиданно содержит %q:\n%s", notWant, out)
				}
			}
		})
	}
}

func TestDoneMovesTaskAndIsNotRepeatable(t *testing.T) {
	w := newWorkspace(t)
	w.seed(t)

	out := w.mustRun(t, "--today=2026-08-28", "done", "T-002")
	if !strings.Contains(out, "T-002") {
		t.Errorf("done молчит про задачу: %q", out)
	}

	artifact := w.read(t, w.artifact)
	if !strings.Contains(artifact, "## Сделано") {
		t.Errorf("нет раздела Сделано:\n%s", artifact)
	}
	if !strings.Contains(artifact, "- [x] `T-002`") || !strings.Contains(artifact, "выполнено:2026-08-28") {
		t.Errorf("задача не отмечена выполненной:\n%s", artifact)
	}

	if _, _, code := w.run(t, "--today=2026-08-28", "done", "T-002"); code == 0 {
		t.Error("повторный done должен завершиться ошибкой")
	}
}

func TestDeferMovesDeadlineAndCounts(t *testing.T) {
	w := newWorkspace(t)
	w.seed(t)

	out := w.mustRun(t, "--today=2026-08-27", "defer", "T-002", "--due=2026-09-10")
	if !strings.Contains(out, "2026-09-10") || !strings.Contains(out, "переносов: 1") {
		t.Errorf("defer не отчитался о переносе: %q", out)
	}

	w.mustRun(t, "--today=2026-08-27", "defer", "T-002", "--due=2026-09-20")
	artifact := w.read(t, w.artifact)
	if !strings.Contains(artifact, "срок:2026-09-20") || !strings.Contains(artifact, "переносов:2") {
		t.Errorf("второй перенос не записан:\n%s", artifact)
	}

	// Двигать срок назад — не перенос, а опечатка.
	if _, _, code := w.run(t, "--today=2026-08-27", "defer", "T-002", "--due=2026-09-01"); code == 0 {
		t.Error("перенос на более раннюю дату должен завершиться ошибкой")
	}
}

func TestCheckReportsIncompleteAndSucceeds(t *testing.T) {
	w := newWorkspace(t)
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Критично без срока", "--tier=1",
		"--from=Босс", "--section=Работа")

	out := w.mustRun(t, "--today=2026-08-27", "check")
	if !strings.Contains(out, "Артефакт читается") {
		t.Errorf("check не отчитался: %q", out)
	}
	if !strings.Contains(out, "Неполные данные: T-001") {
		t.Errorf("check не заметил тир-1 без срока: %q", out)
	}
}

func TestCheckFailsOnUnparsableArtifact(t *testing.T) {
	w := newWorkspace(t)
	// Задача вне раздела: файл не разбирается.
	w.write(t, w.artifact, "# Обязанности\n\n- [ ] `T-001` **Ничья задача**\n  тир:2 · от:себя\n")

	_, stderr, code := w.run(t, "--today=2026-08-27", "check")
	if code == 0 {
		t.Fatalf("check на битом артефакте должен вернуть ненулевой код, stderr: %q", stderr)
	}
	if !strings.Contains(stderr, "secretary:") {
		t.Errorf("ошибка без объяснения: %q", stderr)
	}
}

func TestFmtIsIdempotentAfterAdd(t *testing.T) {
	w := newWorkspace(t)
	w.seed(t)

	w.mustRun(t, "--today=2026-08-27", "fmt")
	first := w.read(t, w.artifact)
	w.mustRun(t, "--today=2026-08-27", "fmt")
	second := w.read(t, w.artifact)

	if first != second {
		t.Errorf("fmt не идемпотентен:\n--- первый ---\n%s\n--- второй ---\n%s", first, second)
	}
}

func TestFmtPreservesHandEdits(t *testing.T) {
	w := newWorkspace(t)
	w.seed(t)
	w.mustRun(t, "--today=2026-08-27", "fmt")

	before := w.read(t, w.artifact)
	// Ручная правка: чужая подсказка и лишние пустые строки.
	edited := strings.Replace(before, "## Личное", "\n\n## Личное", 1)
	w.write(t, w.artifact, edited)

	w.mustRun(t, "--today=2026-08-27", "fmt")
	if after := w.read(t, w.artifact); after != before {
		t.Errorf("ручная правка потеряла данные:\n--- было ---\n%s\n--- стало ---\n%s", before, after)
	}
}

func TestArchiveMovesOldDoneTasks(t *testing.T) {
	w := newWorkspace(t)
	w.seed(t)
	w.mustRun(t, "--today=2026-08-28", "done", "T-002")
	w.mustRun(t, "--today=2026-10-01", "done", "T-003")

	out := w.mustRun(t, "--today=2026-10-05", "archive")
	if !strings.Contains(out, "Перенесено в архив") {
		t.Fatalf("archive ничего не перенёс: %q", out)
	}

	artifact := w.read(t, w.artifact)
	if strings.Contains(artifact, "`T-002`") {
		t.Errorf("старая выполненная задача осталась в артефакте:\n%s", artifact)
	}
	if !strings.Contains(artifact, "`T-003`") {
		t.Errorf("свежая выполненная задача не должна уезжать в архив:\n%s", artifact)
	}

	archive := w.read(t, w.archive)
	if !strings.Contains(archive, "`T-002`") || !strings.Contains(archive, "## Сделано") {
		t.Errorf("архив не получил задачу:\n%s", archive)
	}
}

func TestArchiveWithNothingToMove(t *testing.T) {
	w := newWorkspace(t)
	w.seed(t)

	out := w.mustRun(t, "--today=2026-08-27", "archive")
	if !strings.Contains(out, "нечего") {
		t.Errorf("archive должен сказать, что переносить нечего: %q", out)
	}
	if _, err := os.Stat(w.archive); err == nil {
		t.Error("archive не должен создавать файл, когда переносить нечего")
	}
}

func TestErrorPaths(t *testing.T) {
	cases := []struct {
		name string
		args []string
		// broken seeds an artifact that does not parse.
		broken bool
	}{
		{name: "нет команды", args: []string{}},
		{name: "неизвестная команда", args: []string{"погладить-кота"}},
		{name: "add без title", args: []string{"add", "--tier=2"}},
		{name: "add с неизвестным разделом", args: []string{"add", "--title=Х", "--tier=2", "--section=Хобби"}},
		{name: "add с чужим тиром", args: []string{"add", "--title=Х", "--tier=7"}},
		{name: "add с битой датой", args: []string{"add", "--title=Х", "--due=завтра"}},
		{name: "add с битым объёмом", args: []string{"add", "--title=Х", "--effort=пять"}},
		{name: "done без идентификатора", args: []string{"done"}},
		{name: "done с неизвестным id", args: []string{"done", "T-999"}},
		{name: "defer без --due", args: []string{"defer", "T-001"}},
		{name: "defer с неизвестным id", args: []string{"defer", "T-999", "--due=2026-12-01"}},
		{name: "неизвестный флаг", args: []string{"brief", "--громко"}},
		{name: "битый --today", args: []string{"brief", "--today=вчера"}},
		{name: "битый артефакт", args: []string{"brief"}, broken: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorkspace(t)
			if tc.broken {
				w.write(t, w.artifact, "# Обязанности\n\n## Работа\n\n- [ ] `T-001` **Задача**\n  тир:девять · от:себя\n")
			} else {
				w.mustRun(t, "--today=2026-08-27", "add", "--title=Опорная задача", "--tier=2", "--section=Личное")
			}

			_, stderr, code := w.run(t, tc.args...)
			if code == 0 {
				t.Fatalf("%v должно завершаться ненулевым кодом", tc.args)
			}
			if strings.TrimSpace(stderr) == "" {
				t.Errorf("%v завершилось молча", tc.args)
			}
		})
	}
}

func TestGlobalFlagsWorkBeforeAndAfterSubcommand(t *testing.T) {
	w := newWorkspace(t)
	other := filepath.Join(w.dir, "другое.md")

	w.mustRun(t, "--artifact="+other, "--today=2026-08-27", "add", "--title=В другой файл", "--tier=2", "--section=Личное")
	w.mustRun(t, "add", "--artifact="+other, "--today=2026-08-27", "--title=И ещё одна", "--tier=2", "--section=Личное")

	if _, err := os.Stat(w.artifact); err == nil {
		t.Error("--artifact проигнорирован: записан файл по умолчанию")
	}
	content := w.read(t, other)
	for _, want := range []string{"`T-001`", "`T-002`"} {
		if !strings.Contains(content, want) {
			t.Errorf("в %s нет %q:\n%s", other, want, content)
		}
	}
}

func TestMissingConfigFallsBackToDefaults(t *testing.T) {
	w := newWorkspace(t)
	if err := os.Remove(filepath.Join(w.dir, "secretary.yml")); err != nil {
		t.Fatal(err)
	}

	w.mustRun(t, "--today=2026-08-27", "add", "--title=Без конфигурации", "--tier=2", "--section=Личное")
	// Артефакт по умолчанию — docs/duties.md относительно рабочего каталога.
	if _, err := os.Stat(filepath.Join(w.dir, "docs", "duties.md")); err != nil {
		t.Fatalf("без конфигурации артефакт не создан по умолчанию: %v", err)
	}
}
