package main

import (
	"strings"
	"testing"
)

func TestStepAddCheckAndDrop(t *testing.T) {
	w := newWorkspace(t)
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Sort out the storage unit", "--tier=2",
		"--section=Personal", "--context=@town", "--step=call them")

	out := w.mustRun(t, "--today=2026-08-27", "step", "T-001", "--add=drive the boxes over")
	if !strings.Contains(out, "1. [ ] call them") || !strings.Contains(out, "2. [ ] drive the boxes over") {
		t.Fatalf("step did not show the list:\n%s", out)
	}

	out = w.mustRun(t, "--today=2026-08-27", "step", "T-001", "--check=1")
	if !strings.Contains(out, "1. [x] call them") || !strings.Contains(out, "1/2") {
		t.Fatalf("--check did not check the step:\n%s", out)
	}

	// The checkbox lives in the artifact, so a hand edit and a `fmt` see it too.
	if artifact := w.read(t, w.artifact); !strings.Contains(artifact, "  - [x] call them") {
		t.Fatalf("step not written to the artifact:\n%s", artifact)
	}

	out = w.mustRun(t, "--today=2026-08-27", "step", "T-001", "--check=2")
	if !strings.Contains(out, "secretary done T-001") {
		t.Errorf("closed steps did not suggest closing the task:\n%s", out)
	}

	out = w.mustRun(t, "--today=2026-08-27", "step", "T-001", "--drop=1")
	if strings.Contains(out, "call them") && strings.Contains(out, "1. [x] call them") {
		t.Errorf("--drop did not remove the step:\n%s", out)
	}

	if _, _, code := w.run(t, "--today=2026-08-27", "step", "T-001", "--check=9"); code != 1 {
		t.Errorf("missing step: exit code %d, want 1", code)
	}
	if _, _, code := w.run(t, "--today=2026-08-27", "step"); code != 2 {
		t.Errorf("step without an identifier: exit code %d, want 2", code)
	}
}

func TestListShowsStepProgress(t *testing.T) {
	w := newWorkspace(t)
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Sort out the storage unit", "--tier=2",
		"--section=Personal", "--step=call them", "--step=drive the boxes over")
	w.mustRun(t, "--today=2026-08-27", "step", "T-001", "--check=1")

	out := w.mustRun(t, "--today=2026-08-27", "list")
	if !strings.Contains(out, "steps 1/2") {
		t.Errorf("list did not show the progress:\n%s", out)
	}
	if !strings.Contains(out, "1. [x] call them") || !strings.Contains(out, "2. [ ] drive the boxes over") {
		t.Errorf("list did not expand the checklist:\n%s", out)
	}
}

func TestDoneWarnsAboutOpenSteps(t *testing.T) {
	w := newWorkspace(t)
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Sort out the storage unit", "--tier=2",
		"--section=Personal", "--step=call them")

	out := w.mustRun(t, "--today=2026-08-27", "done", "T-001")
	if !strings.Contains(out, "Open steps left") {
		t.Errorf("done was silent about open steps:\n%s", out)
	}
}

func TestStepFieldsThroughCLI(t *testing.T) {
	w := newWorkspace(t)
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Sort out the storage unit", "--tier=2",
		"--section=Personal", "--due=2026-09-17",
		"--step=call them · tier:1 · due:2026-09-01", "--step=drive the boxes over")

	out := w.mustRun(t, "--today=2026-08-27", "list")
	if !strings.Contains(out, "1. [ ] call them") || !strings.Contains(out, "!! Tue 1 Sep") {
		t.Errorf("list did not show the step deadline:\n%s", out)
	}

	w.mustRun(t, "--today=2026-08-27", "step", "T-001", "--set=2", "--due=2026-09-16")
	if out := w.mustRun(t, "--today=2026-08-27", "list"); !strings.Contains(out, "16 Sep") {
		t.Errorf("--set did not set due:\n%s", out)
	}

	w.mustRun(t, "--today=2026-08-27", "step", "T-001", "--set=1", "--tier=-", "--due=-")
	if out := w.mustRun(t, "--today=2026-08-27", "list"); strings.Contains(out, "Tue 1 Sep") {
		t.Errorf("«-» did not clear the step fields:\n%s", out)
	}

	if _, _, code := w.run(t, "--today=2026-08-27", "step", "T-001", "--set=1"); code != 2 {
		t.Errorf("--set without fields: exit code %d, want 2", code)
	}
	if _, _, code := w.run(t, "--today=2026-08-27", "step", "T-001", "--set=1", "--tier=9"); code != 2 {
		t.Errorf("--tier 9: exit code %d, want 2", code)
	}
}
