package main

import (
	"strings"
	"testing"
)

func TestStepAddCheckAndDrop(t *testing.T) {
	w := newWorkspace(t)
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Разобраться со складом", "--tier=2",
		"--section=Личное", "--context=@город", "--step=позвонить")

	out := w.mustRun(t, "--today=2026-08-27", "step", "T-001", "--add=отвезти вещи")
	if !strings.Contains(out, "1. [ ] позвонить") || !strings.Contains(out, "2. [ ] отвезти вещи") {
		t.Fatalf("step не показал список:\n%s", out)
	}

	out = w.mustRun(t, "--today=2026-08-27", "step", "T-001", "--check=1")
	if !strings.Contains(out, "1. [x] позвонить") || !strings.Contains(out, "1/2") {
		t.Fatalf("--check не отметил подпункт:\n%s", out)
	}

	// The checkbox lives in the artifact, so a hand edit and a `fmt` see it too.
	if artifact := w.read(t, w.artifact); !strings.Contains(artifact, "  - [x] позвонить") {
		t.Fatalf("подпункт не записан в артефакт:\n%s", artifact)
	}

	out = w.mustRun(t, "--today=2026-08-27", "step", "T-001", "--check=2")
	if !strings.Contains(out, "secretary done T-001") {
		t.Errorf("закрытые подпункты не предложили закрыть задачу:\n%s", out)
	}

	out = w.mustRun(t, "--today=2026-08-27", "step", "T-001", "--drop=1")
	if strings.Contains(out, "позвонить") && strings.Contains(out, "1. [x] позвонить") {
		t.Errorf("--drop не удалил подпункт:\n%s", out)
	}

	if _, _, code := w.run(t, "--today=2026-08-27", "step", "T-001", "--check=9"); code != 1 {
		t.Errorf("несуществующий подпункт: код %d, ожидался 1", code)
	}
	if _, _, code := w.run(t, "--today=2026-08-27", "step"); code != 2 {
		t.Errorf("step без идентификатора: код %d, ожидался 2", code)
	}
}

func TestListShowsStepProgress(t *testing.T) {
	w := newWorkspace(t)
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Разобраться со складом", "--tier=2",
		"--section=Личное", "--step=позвонить", "--step=отвезти вещи")
	w.mustRun(t, "--today=2026-08-27", "step", "T-001", "--check=1")

	out := w.mustRun(t, "--today=2026-08-27", "list")
	if !strings.Contains(out, "подпункты 1/2") {
		t.Errorf("list не показал прогресс:\n%s", out)
	}
	if !strings.Contains(out, "1. [x] позвонить") || !strings.Contains(out, "2. [ ] отвезти вещи") {
		t.Errorf("list не развернул чек-лист:\n%s", out)
	}
}

func TestDoneWarnsAboutOpenSteps(t *testing.T) {
	w := newWorkspace(t)
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Разобраться со складом", "--tier=2",
		"--section=Личное", "--step=позвонить")

	out := w.mustRun(t, "--today=2026-08-27", "done", "T-001")
	if !strings.Contains(out, "Незакрытых подпунктов") {
		t.Errorf("done промолчал о незакрытых подпунктах:\n%s", out)
	}
}

func TestStepFieldsThroughCLI(t *testing.T) {
	w := newWorkspace(t)
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Разобраться со складом", "--tier=2",
		"--section=Личное", "--due=2026-09-17",
		"--step=позвонить · тир:1 · срок:2026-09-01", "--step=отвезти вещи")

	out := w.mustRun(t, "--today=2026-08-27", "list")
	if !strings.Contains(out, "1. [ ] позвонить") || !strings.Contains(out, "!! вт 1 сен") {
		t.Errorf("list не показал срок подпункта:\n%s", out)
	}

	w.mustRun(t, "--today=2026-08-27", "step", "T-001", "--set=2", "--due=2026-09-16")
	if out := w.mustRun(t, "--today=2026-08-27", "list"); !strings.Contains(out, "16 сен") {
		t.Errorf("--set не проставил срок:\n%s", out)
	}

	w.mustRun(t, "--today=2026-08-27", "step", "T-001", "--set=1", "--tier=-", "--due=-")
	if out := w.mustRun(t, "--today=2026-08-27", "list"); strings.Contains(out, "вт 1 сен") {
		t.Errorf("«-» не снял поля подпункта:\n%s", out)
	}

	if _, _, code := w.run(t, "--today=2026-08-27", "step", "T-001", "--set=1"); code != 2 {
		t.Errorf("--set без полей: код %d, ожидался 2", code)
	}
	if _, _, code := w.run(t, "--today=2026-08-27", "step", "T-001", "--set=1", "--tier=9"); code != 2 {
		t.Errorf("--tier 9: код %d, ожидался 2", code)
	}
}
