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
		fmt.Fprintln(os.Stderr, "build failed:", err)
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
sections: [Work, Personal, Home, Health, Relationships, Finance]
contexts: ["@town", "@home", "@phone", "@computer", "@work"]
tiers:
  "1": {name: Critical, buffer_min_days: 2, buffer_ratio: 0.5}
  "2": {name: Important,    buffer_min_days: 1, buffer_ratio: 0.25}
  "3": {name: Background,  buffer_min_days: 0, buffer_ratio: 0}
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
		t.Fatalf("running %v: %v", args, err)
	}
	return out.String(), errOut.String(), code
}

// mustRun fails the test if the command did not succeed.
func (w *workspace) mustRun(t *testing.T, args ...string) string {
	t.Helper()
	stdout, stderr, code := w.run(t, args...)
	if code != 0 {
		t.Fatalf("%v: exit code %d, stderr: %s", args, code, stderr)
	}
	return stdout
}

func (w *workspace) read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
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
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Prepare the Q3 report", "--tier=1",
		"--from=Dana Whitfield", "--due=2026-09-05", "--effort=5d", "--section=Work",
		"--context=@computer", "--labels=reporting, finance", "--note=needs the numbers from accounting")
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Collect the documents from the notary", "--tier=1",
		"--from=Helen", "--due=2026-08-29", "--effort=1h", "--section=Personal", "--context=@town",
		"--labels=paperwork, documents")
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Buy Helen a present", "--tier=2",
		"--due=2026-09-01", "--effort=2h", "--section=Personal", "--context=@town", "--labels=relationships")
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Get the vacation approved", "--tier=3",
		"--section=Work", "--context=@work")
}

func TestAddAssignsSequentialIDs(t *testing.T) {
	w := newWorkspace(t)

	for i, want := range []string{"T-001", "T-002", "T-003"} {
		out := w.mustRun(t, "--today=2026-08-27", "add",
			fmt.Sprintf("--title=Task %d", i+1), "--tier=2", "--section=Personal")
		if got := strings.TrimSpace(out); got != want {
			t.Fatalf("add #%d printed %q, want %q", i+1, got, want)
		}
	}

	artifact := w.read(t, w.artifact)
	for _, want := range []string{"`T-001`", "`T-003`", "## Personal", "created:2026-08-27"} {
		if !strings.Contains(artifact, want) {
			t.Errorf("artifact lacks %q:\n%s", want, artifact)
		}
	}
}

func TestAddFieldsRoundTripIntoArtifact(t *testing.T) {
	w := newWorkspace(t)
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Collect the documents", "--tier=1",
		"--from=Helen", "--due=2026-08-29", "--effort=1h", "--section=Personal",
		"--context=@town", "--labels=paperwork, documents", "--note=notary open until 18:00",
		"--note=bring the passport")

	artifact := w.read(t, w.artifact)
	for _, want := range []string{
		"tier:1", "from:Helen", "due:2026-08-29", "effort:1h", "context:@town",
		"labels: paperwork, documents", "> notary open until 18:00", "> bring the passport",
	} {
		if !strings.Contains(artifact, want) {
			t.Errorf("artifact lacks %q:\n%s", want, artifact)
		}
	}
}

func TestAddDefaultsSectionAndFrom(t *testing.T) {
	w := newWorkspace(t)
	w.mustRun(t, "--today=2026-08-27", "add", "--title=No section", "--tier=3")

	artifact := w.read(t, w.artifact)
	if !strings.Contains(artifact, "## Work") {
		t.Errorf("default section is not the first configured one:\n%s", artifact)
	}
	if !strings.Contains(artifact, "from:self") {
		t.Errorf("default from: is not «self»:\n%s", artifact)
	}
}

func TestBriefBuckets(t *testing.T) {
	w := newWorkspace(t)
	w.seed(t)

	out := w.mustRun(t, "--today=2026-09-06", "brief")
	for _, want := range []string{"Overdue:", "Prepare the Q3 report", "Collect the documents from the notary", "Open:"} {
		if !strings.Contains(out, want) {
			t.Errorf("briefing lacks %q:\n%s", want, out)
		}
	}
}

func TestBriefAllClearOnEmptyArtifact(t *testing.T) {
	w := newWorkspace(t)

	out := w.mustRun(t, "--today=2026-08-27", "brief")
	if strings.Contains(out, "Overdue:") || strings.Contains(out, "Start today:") {
		t.Errorf("an empty artifact must demand nothing:\n%s", out)
	}
	if !strings.Contains(out, "Open:") {
		t.Errorf("briefing without the totals line:\n%s", out)
	}
}

func TestBriefJSON(t *testing.T) {
	w := newWorkspace(t)
	w.seed(t)

	out := w.mustRun(t, "--today=2026-09-06", "brief", "--json")

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("brief --json does not parse: %v\n%s", err, out)
	}
	for _, key := range []string{"date", "all_clear", "total", "counts", "overdue", "start_now", "soon", "groups", "complaints"} {
		if _, ok := parsed[key]; !ok {
			t.Errorf("JSON lacks key %q: %s", key, out)
		}
	}
	if parsed["date"] != "2026-09-06" {
		t.Errorf("date = %v, want 2026-09-06", parsed["date"])
	}
	if overdue, ok := parsed["overdue"].([]interface{}); !ok || len(overdue) == 0 {
		t.Errorf("want overdue tasks: %s", out)
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
			name:    "everything",
			args:    []string{"list"},
			want:    []string{"T-001", "T-002", "T-003", "T-004"},
			notWant: nil,
		},
		{
			name:    "by tier",
			args:    []string{"list", "--tier=1"},
			want:    []string{"T-001", "T-002"},
			notWant: []string{"T-003", "T-004"},
		},
		{
			name:    "by section",
			args:    []string{"list", "--section=Personal"},
			want:    []string{"T-002", "T-003"},
			notWant: []string{"T-001", "T-004"},
		},
		{
			name:    "by label",
			args:    []string{"list", "--label=paperwork"},
			want:    []string{"T-002"},
			notWant: []string{"T-001", "T-003", "T-004"},
		},
		{
			name:    "by context",
			args:    []string{"list", "--context=@town"},
			want:    []string{"T-002", "T-003"},
			notWant: []string{"T-001", "T-004"},
		},
		{
			name:    "empty result",
			args:    []string{"list", "--label=nosuch"},
			want:    []string{"Nothing found"},
			notWant: []string{"T-001"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := w.mustRun(t, append([]string{"--today=2026-08-27"}, tc.args...)...)
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(out, notWant) {
					t.Errorf("output unexpectedly contains %q:\n%s", notWant, out)
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
		t.Errorf("done is silent about the task: %q", out)
	}

	artifact := w.read(t, w.artifact)
	if !strings.Contains(artifact, "## Done") {
		t.Errorf("no Done section:\n%s", artifact)
	}
	if !strings.Contains(artifact, "- [x] `T-002`") || !strings.Contains(artifact, "done:2026-08-28") {
		t.Errorf("task not marked done:\n%s", artifact)
	}

	if _, _, code := w.run(t, "--today=2026-08-28", "done", "T-002"); code == 0 {
		t.Error("a second done must fail")
	}
}

func TestDeferMovesDeadlineAndCounts(t *testing.T) {
	w := newWorkspace(t)
	w.seed(t)

	out := w.mustRun(t, "--today=2026-08-27", "defer", "T-002", "--due=2026-09-10")
	if !strings.Contains(out, "2026-09-10") || !strings.Contains(out, "deferrals: 1") {
		t.Errorf("defer did not report the deferral: %q", out)
	}

	w.mustRun(t, "--today=2026-08-27", "defer", "T-002", "--due=2026-09-20")
	artifact := w.read(t, w.artifact)
	if !strings.Contains(artifact, "due:2026-09-20") || !strings.Contains(artifact, "deferrals:2") {
		t.Errorf("second deferral not written:\n%s", artifact)
	}

	// Moving a deadline backwards is a typo, not a deferral.
	if _, _, code := w.run(t, "--today=2026-08-27", "defer", "T-002", "--due=2026-09-01"); code == 0 {
		t.Error("deferring to an earlier date must fail")
	}
}

func TestCheckReportsIncompleteAndSucceeds(t *testing.T) {
	w := newWorkspace(t)
	w.mustRun(t, "--today=2026-08-27", "add", "--title=Critical without a deadline", "--tier=1",
		"--from=The boss", "--section=Work")

	out := w.mustRun(t, "--today=2026-08-27", "check")
	if !strings.Contains(out, "Artifact parses") {
		t.Errorf("check did not report: %q", out)
	}
	if !strings.Contains(out, "Incomplete data: T-001") {
		t.Errorf("check missed a tier-1 without a deadline: %q", out)
	}
}

func TestCheckFailsOnUnparsableArtifact(t *testing.T) {
	w := newWorkspace(t)
	// A task outside any section: the file does not parse.
	w.write(t, w.artifact, "# Duties\n\n- [ ] `T-001` **Nobody's task**\n  tier:2 · from:self\n")

	_, stderr, code := w.run(t, "--today=2026-08-27", "check")
	if code == 0 {
		t.Fatalf("check on a broken artifact must return non-zero, stderr: %q", stderr)
	}
	if !strings.Contains(stderr, "secretary:") {
		t.Errorf("error without an explanation: %q", stderr)
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
		t.Errorf("fmt is not idempotent:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

func TestFmtPreservesHandEdits(t *testing.T) {
	w := newWorkspace(t)
	w.seed(t)
	w.mustRun(t, "--today=2026-08-27", "fmt")

	before := w.read(t, w.artifact)
	// A hand edit: a foreign hint and extra blank lines.
	edited := strings.Replace(before, "## Personal", "\n\n## Personal", 1)
	w.write(t, w.artifact, edited)

	w.mustRun(t, "--today=2026-08-27", "fmt")
	if after := w.read(t, w.artifact); after != before {
		t.Errorf("the hand edit lost data:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}

func TestArchiveMovesOldDoneTasks(t *testing.T) {
	w := newWorkspace(t)
	w.seed(t)
	w.mustRun(t, "--today=2026-08-28", "done", "T-002")
	w.mustRun(t, "--today=2026-10-01", "done", "T-003")

	out := w.mustRun(t, "--today=2026-10-05", "archive")
	if !strings.Contains(out, "Archived") {
		t.Fatalf("archive moved nothing: %q", out)
	}

	artifact := w.read(t, w.artifact)
	if strings.Contains(artifact, "`T-002`") {
		t.Errorf("an old completed task stayed in the artifact:\n%s", artifact)
	}
	if !strings.Contains(artifact, "`T-003`") {
		t.Errorf("a fresh completed task must not go to the archive:\n%s", artifact)
	}

	archive := w.read(t, w.archive)
	if !strings.Contains(archive, "`T-002`") || !strings.Contains(archive, "## Done") {
		t.Errorf("the archive did not receive the task:\n%s", archive)
	}
}

func TestArchiveWithNothingToMove(t *testing.T) {
	w := newWorkspace(t)
	w.seed(t)

	out := w.mustRun(t, "--today=2026-08-27", "archive")
	if !strings.Contains(out, "Nothing to archive") {
		t.Errorf("archive must say there is nothing to move: %q", out)
	}
	if _, err := os.Stat(w.archive); err == nil {
		t.Error("archive must not create a file when there is nothing to move")
	}
}

func TestErrorPaths(t *testing.T) {
	cases := []struct {
		name string
		args []string
		// broken seeds an artifact that does not parse.
		broken bool
	}{
		{name: "no command", args: []string{}},
		{name: "unknown command", args: []string{"pet-the-cat"}},
		{name: "add without title", args: []string{"add", "--tier=2"}},
		{name: "add with an unknown section", args: []string{"add", "--title=X", "--tier=2", "--section=Hobbies"}},
		{name: "add with a foreign tier", args: []string{"add", "--title=X", "--tier=7"}},
		{name: "add with a broken date", args: []string{"add", "--title=X", "--due=tomorrow"}},
		{name: "add with a broken effort", args: []string{"add", "--title=X", "--effort=five"}},
		{name: "done without an identifier", args: []string{"done"}},
		{name: "done with an unknown id", args: []string{"done", "T-999"}},
		{name: "defer without --due", args: []string{"defer", "T-001"}},
		{name: "defer with an unknown id", args: []string{"defer", "T-999", "--due=2026-12-01"}},
		{name: "unknown flag", args: []string{"brief", "--loudly"}},
		{name: "broken --today", args: []string{"brief", "--today=yesterday"}},
		{name: "broken artifact", args: []string{"brief"}, broken: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorkspace(t)
			if tc.broken {
				w.write(t, w.artifact, "# Duties\n\n## Work\n\n- [ ] `T-001` **Task**\n  tier:nine · from:self\n")
			} else {
				w.mustRun(t, "--today=2026-08-27", "add", "--title=Anchor task", "--tier=2", "--section=Personal")
			}

			_, stderr, code := w.run(t, tc.args...)
			if code == 0 {
				t.Fatalf("%v must exit non-zero", tc.args)
			}
			if strings.TrimSpace(stderr) == "" {
				t.Errorf("%v exited silently", tc.args)
			}
		})
	}
}

func TestGlobalFlagsWorkBeforeAndAfterSubcommand(t *testing.T) {
	w := newWorkspace(t)
	other := filepath.Join(w.dir, "other.md")

	w.mustRun(t, "--artifact="+other, "--today=2026-08-27", "add", "--title=Into another file", "--tier=2", "--section=Personal")
	w.mustRun(t, "add", "--artifact="+other, "--today=2026-08-27", "--title=And one more", "--tier=2", "--section=Personal")

	if _, err := os.Stat(w.artifact); err == nil {
		t.Error("--artifact ignored: the default file was written")
	}
	content := w.read(t, other)
	for _, want := range []string{"`T-001`", "`T-002`"} {
		if !strings.Contains(content, want) {
			t.Errorf("%s lacks %q:\n%s", other, want, content)
		}
	}
}

func TestMissingConfigFallsBackToDefaults(t *testing.T) {
	w := newWorkspace(t)
	if err := os.Remove(filepath.Join(w.dir, "secretary.yml")); err != nil {
		t.Fatal(err)
	}

	w.mustRun(t, "--today=2026-08-27", "add", "--title=No configuration", "--tier=2", "--section=Personal")
	// The default artifact is docs/duties.md relative to the working directory.
	if _, err := os.Stat(filepath.Join(w.dir, "docs", "duties.md")); err != nil {
		t.Fatalf("without a configuration the default artifact was not created: %v", err)
	}
}

// TestHomeEnv checks that SECRETARY_HOME moves the configuration and the
// artifact out of the working directory: the way a private installation keeps
// its real duties away from a checkout of the repository.
func TestHomeEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv(homeEnv, home)
	if err := os.WriteFile(filepath.Join(home, "secretary.yml"), []byte("artifact: duties.md\narchive: archive.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	if code := run([]string{"--today=2026-08-27", "add", "--title=At home", "--tier=2", "--section=Personal"}, &stdout, &stderr); code != 0 {
		t.Fatalf("add under %s: exit code %d, stderr: %s", homeEnv, code, stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(home, "duties.md"))
	if err != nil {
		t.Fatalf("artifact not created under %s: %v", homeEnv, err)
	}
	if !strings.Contains(string(data), "At home") {
		t.Errorf("artifact under %s lacks the duty:\n%s", homeEnv, data)
	}
	if _, err := os.Stat("duties.md"); err == nil {
		t.Error("the artifact was also written into the working directory")
	}

	// An explicit --artifact is relative to the working directory, not home.
	other := filepath.Join(t.TempDir(), "other.md")
	stdout.Reset()
	if code := run([]string{"--artifact=" + other, "--today=2026-08-27", "add", "--title=Elsewhere", "--tier=2", "--section=Personal"}, &stdout, &stderr); code != 0 {
		t.Fatalf("add with --artifact: exit code %d, stderr: %s", code, stderr.String())
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("--artifact ignored under %s: %v", homeEnv, err)
	}
}
