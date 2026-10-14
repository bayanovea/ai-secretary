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
// «Approve the budget» is tier 2, due 2026-09-10, effort 2d, so its buffer is
// max(1, 0.25·2) = 1 day, its lead is 3 days, and its latest start is 2026-09-07
// — three days before the deadline, which is the whole point of the feature.
func acceptanceWorkspace(t *testing.T) *workspace {
	t.Helper()
	w := newWorkspace(t)

	w.mustRun(t, "--today=2026-08-27", "add", "--title=Approve the budget", "--tier=2",
		"--from=Dana Whitfield", "--due=2026-09-10", "--effort=2d", "--section=Work",
		"--context=@computer", "--labels=finance")
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Collect the documents from the notary", "--tier=1",
		"--from=Helen", "--due=2026-09-04", "--effort=2h", "--section=Personal",
		"--context=@town", "--labels=paperwork, documents", "--note=bring the passport")
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Buy Helen a present", "--tier=2",
		"--from=self", "--due=2026-09-12", "--effort=1h", "--section=Personal",
		"--context=@town", "--labels=relationships")
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Clear out the attic", "--tier=3",
		"--from=self", "--section=Home", "--context=@home")
	return w
}

// briefBlock returns the lines of one section of a briefing — everything from
// its heading to the next blank line. Assertions about a bucket have to be
// scoped to that bucket, or «overdue» somewhere in the file passes for
// «overdue» in the task under test.
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

	t.Run("three tiers", func(t *testing.T) {
		for tier, want := range map[string]string{
			"1": "Collect the documents from the notary",
			"2": "Approve the budget",
			"3": "Clear out the attic",
		} {
			out := w.mustRun(t, "--today=2026-08-27", "list", "--tier="+tier)
			if !strings.Contains(out, want) {
				t.Errorf("list --tier=%s did not show %q:\n%s", tier, want, out)
			}
		}
		artifact := w.read(t, w.artifact)
		for _, want := range []string{"tier:1", "tier:2", "tier:3"} {
			if !strings.Contains(artifact, want) {
				t.Errorf("artifact does not store %q:\n%s", want, artifact)
			}
		}
	})

	t.Run("who the duty is from", func(t *testing.T) {
		artifact := w.read(t, w.artifact)
		for _, want := range []string{"from:Helen", "from:Dana Whitfield", "from:self"} {
			if !strings.Contains(artifact, want) {
				t.Errorf("artifact does not store %q:\n%s", want, artifact)
			}
		}
		if out := w.mustRun(t, "--today=2026-08-27", "list"); !strings.Contains(out, "from Helen") {
			t.Errorf("list does not show the assignor:\n%s", out)
		}
	})

	t.Run("deadline and latest start date", func(t *testing.T) {
		// The day before the latest start — «soon», on the day — «start
		// now», while the deadline is still ahead: the nudge comes early.
		early := w.mustRun(t, "--today=2026-09-06", "list", "--tier=2", "--section=Work")
		if !strings.Contains(early, "soon") {
			t.Errorf("the day before the latest start the status is not «soon»:\n%s", early)
		}

		out := w.mustRun(t, "--today=2026-09-07", "brief")
		if !strings.Contains(briefBlock(out, "Start today:"), "Approve the budget") {
			t.Errorf("the briefing did not raise the task on its latest start day:\n%s", out)
		}
		// The nudge comes early, not after the fact: 2026-09-10 is still ahead.
		if strings.Contains(briefBlock(out, "Overdue:"), "Approve the budget") {
			t.Errorf("a task with a future deadline was declared overdue:\n%s", out)
		}
	})

	t.Run("sections and labels", func(t *testing.T) {
		section := w.mustRun(t, "--today=2026-08-27", "list", "--section=Personal")
		if !strings.Contains(section, "Collect the documents from the notary") ||
			strings.Contains(section, "Approve the budget") {
			t.Errorf("section filter did not work:\n%s", section)
		}
		label := w.mustRun(t, "--today=2026-08-27", "list", "--label=paperwork")
		if !strings.Contains(label, "Collect the documents from the notary") ||
			strings.Contains(label, "Buy Helen a present") {
			t.Errorf("label filter did not work:\n%s", label)
		}
	})

	t.Run("«while you are at it» groups", func(t *testing.T) {
		out := w.mustRun(t, "--today=2026-09-02", "brief")
		if !strings.Contains(out, "While you are at it:") || !strings.Contains(out, "@town") {
			t.Errorf("the briefing did not propose one trip to town:\n%s", out)
		}
		for _, want := range []string{"Collect the documents from the notary", "Buy Helen a present"} {
			if !strings.Contains(out, want) {
				t.Errorf("group lacks %q:\n%s", want, out)
			}
		}
	})

	t.Run("Alfred's voice", func(t *testing.T) {
		out := w.mustRun(t, "--today=2026-09-07", "brief")
		if !strings.Contains(out, "sir") {
			t.Errorf("the briefing does not sound like Alfred:\n%s", out)
		}
		// Same date, same wording: no math/rand.
		if again := w.mustRun(t, "--today=2026-09-07", "brief"); again != out {
			t.Errorf("the briefing is not reproducible:\n--- first ---\n%s\n--- second ---\n%s", out, again)
		}
	})

	t.Run("free text", func(t *testing.T) {
		// Free-text parsing lives in the skill, not in the binary: check that
		// it describes the «phrase → flags» step and is obliged to call brief.
		skill := readSkill(t)
		for _, want := range []string{"secretary add", "secretary brief", "--title", "--tier", "--from", "--due"} {
			if !strings.Contains(skill, want) {
				t.Errorf("the skill does not describe %q", want)
			}
		}
	})
}

// TestAcceptanceEdgeCases covers the states the artifact can legitimately be in
// and the ones it must refuse.
func TestAcceptanceEdgeCases(t *testing.T) {
	t.Run("no deadline", func(t *testing.T) {
		w := newWorkspace(t)
		w.mustRun(t, "--today=2026-08-27", "add", "--title=Clear out the garage some day",
			"--tier=3", "--section=Home")

		if out := w.mustRun(t, "--today=2026-08-27", "list"); !strings.Contains(out, "no deadline") {
			t.Errorf("a task without a deadline is shown differently:\n%s", out)
		}
		out := w.mustRun(t, "--today=2026-08-27", "brief")
		for _, unwanted := range []string{"Overdue:", "Start today:", "Soon:"} {
			if strings.Contains(out, unwanted) {
				t.Errorf("a task without a deadline landed in %q:\n%s", unwanted, out)
			}
		}
	})

	t.Run("no effort", func(t *testing.T) {
		w := newWorkspace(t)
		w.mustRun(t, "--today=2026-08-27", "add", "--title=Hand in the report", "--tier=1",
			"--from=The boss", "--due=2026-08-28", "--section=Work")

		// Without an effort there is no latest start date, and the deadline takes its place.
		if out := w.mustRun(t, "--today=2026-08-28", "brief"); !strings.Contains(out, "Start today:") {
			t.Errorf("without an effort the deadline did not stand in for the start date:\n%s", out)
		}
		if out := w.mustRun(t, "--today=2026-08-27", "check"); !strings.Contains(out, "Incomplete data: T-001") {
			t.Errorf("check missed a tier-1 without an effort:\n%s", out)
		}
	})

	t.Run("unknown section", func(t *testing.T) {
		w := newWorkspace(t)
		_, stderr, code := w.run(t, "--today=2026-08-27", "add", "--title=Wash the car",
			"--tier=3", "--section=Garage")
		if code == 0 {
			t.Fatal("add into an unconfigured section must fail")
		}
		if !strings.Contains(stderr, "Garage") {
			t.Errorf("the error does not name the section: %q", stderr)
		}

		// But a section already written by hand does not break the file: check
		// reports it and returns zero.
		w.write(t, w.artifact, "# Duties\n\n## Garage\n\n- [ ] `T-007` **Change the oil**\n"+
			"  tier:3 · from:self · created:2026-08-01\n\n## Done\n")
		out := w.mustRun(t, "--today=2026-08-27", "check")
		if !strings.Contains(out, "Sections outside the configuration: Garage") {
			t.Errorf("check did not flag the foreign section:\n%s", out)
		}
	})

	t.Run("duplicate identifiers", func(t *testing.T) {
		w := newWorkspace(t)
		w.write(t, w.artifact, "# Duties\n\n## Work\n\n"+
			"- [ ] `T-001` **First**\n  tier:2 · from:self · created:2026-08-01\n\n"+
			"- [ ] `T-001` **Second**\n  tier:2 · from:self · created:2026-08-02\n")

		_, stderr, code := w.run(t, "--today=2026-08-27", "check")
		if code == 0 {
			t.Fatal("a duplicate identifier must break parsing")
		}
		if !strings.Contains(stderr, "T-001") {
			t.Errorf("the error does not name the duplicate: %q", stderr)
		}
	})

	t.Run("empty artifact", func(t *testing.T) {
		w := newWorkspace(t)
		w.write(t, w.artifact, "# Duties\n\n<!-- secretary:v1 -->\n\n## Work\n\n## Done\n")

		w.mustRun(t, "--today=2026-08-27", "check")
		w.mustRun(t, "--today=2026-08-27", "fmt")
		w.mustRun(t, "--today=2026-08-27", "archive")
		if out := w.mustRun(t, "--today=2026-08-27", "list"); !strings.Contains(out, "Nothing found") {
			t.Errorf("an empty list is silent differently:\n%s", out)
		}
		if out := w.mustRun(t, "--today=2026-08-27", "brief"); strings.Contains(out, "Overdue:") {
			t.Errorf("an empty briefing is not empty:\n%s", out)
		}
	})

	t.Run("only completed", func(t *testing.T) {
		w := newWorkspace(t)
		w.mustRun(t, "--today=2026-08-27", "add", "--title=Pay the insurance", "--tier=2",
			"--due=2026-08-20", "--section=Finance")
		w.mustRun(t, "--today=2026-08-27", "done", "T-001")

		if out := w.mustRun(t, "--today=2026-08-27", "list"); !strings.Contains(out, "Nothing found") {
			t.Errorf("a completed duty landed in list:\n%s", out)
		}
		out := w.mustRun(t, "--today=2026-08-27", "brief")
		if strings.Contains(out, "Overdue:") {
			t.Errorf("a completed duty with a past deadline was reported overdue:\n%s", out)
		}
		if !strings.Contains(w.read(t, w.artifact), "done:2026-08-27") {
			t.Error("completion date not written")
		}
	})
}

// handEdited is the artifact as a human leaves it: ragged spacing, a stale hint
// line copied by hand, a field key the parser has never heard of, a section
// outside the configuration, and notes that must survive verbatim.
const handEdited = `# Duties

My own notes up top, none of Alfred's business.

<!-- secretary:v1 -->
> Updated: 2026-08-20 · At your service, sir.

## Personal

- [ ] ` + "`T-042`" + ` **Hand the documents to the notary**
  tier:1 · from:Helen · due:2026-09-04 · effort:2h · context:@town · created:2026-08-27 · deferrals:1
  labels: paperwork, documents
  🔴 a hint copied by hand from yesterday's briefing
  > notary open until 18:00
  > bring the passport

- [ ]  ` + "`T-043`" + `  **Buy a present**
    tier:2 ·  from:self  ·  due:2026-09-10  ·  effort:1h  · context:@town · created:2026-08-20 · unknownfield:nonsense
  labels:   relationships

## Garage

- [ ] ` + "`T-044`" + ` **Change the oil**
  tier:3 · from:self · created:2026-08-01

## Done

- [x] ` + "`T-041`" + ` **Pay the insurance** · done:2026-08-20
  tier:2 · from:self · created:2026-08-01
`

// TestAcceptanceHandEditingIsSafe is the promise the README makes: edit the file
// however you like, run `fmt`, and nothing you wrote is lost.
func TestAcceptanceHandEditingIsSafe(t *testing.T) {
	w := newWorkspace(t)
	w.write(t, w.artifact, handEdited)

	w.mustRun(t, "--today=2026-08-27", "check")
	w.mustRun(t, "--today=2026-08-27", "fmt")
	after := w.read(t, w.artifact)

	// Every piece of data survived the tidy-up.
	for _, want := range []string{
		"My own notes up top",
		"`T-042`", "**Hand the documents to the notary**",
		"tier:1", "from:Helen", "due:2026-09-04", "effort:2h", "context:@town", "deferrals:1",
		"labels: paperwork, documents", "> notary open until 18:00", "> bring the passport",
		"`T-043`", "**Buy a present**", "labels: relationships",
		"## Garage", "`T-044`", "**Change the oil**",
		"## Done", "`T-041`", "done:2026-08-20",
	} {
		if !strings.Contains(after, want) {
			t.Errorf("lost after fmt: %q:\n%s", want, after)
		}
	}

	// Exactly two things fmt drops on purpose: hints, which it recomputes,
	// and keys it does not know. Both are documented in the README.
	if strings.Contains(after, "copied by hand") {
		t.Errorf("a stale hint survived fmt:\n%s", after)
	}
	if strings.Contains(after, "unknownfield") {
		t.Errorf("an unknown key survived fmt despite the README:\n%s", after)
	}

	// A second run moves not a single byte.
	w.mustRun(t, "--today=2026-08-27", "fmt")
	if again := w.read(t, w.artifact); again != after {
		t.Errorf("fmt is not idempotent:\n--- first ---\n%s\n--- second ---\n%s", after, again)
	}

	// And tasks are still addressed by identifier.
	w.mustRun(t, "--today=2026-08-27", "defer", "T-042", "--due=2026-09-20")
	w.mustRun(t, "--today=2026-08-27", "done", "T-043")
	final := w.read(t, w.artifact)
	if !strings.Contains(final, "due:2026-09-20") || !strings.Contains(final, "deferrals:2") {
		t.Errorf("defer did not apply to the hand-edited task:\n%s", final)
	}
}
