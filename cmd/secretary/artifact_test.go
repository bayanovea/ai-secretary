package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bayanovea/secretary/internal/config"
	"github.com/bayanovea/secretary/internal/mdstore"
)

// The repository's own artifact and configuration. Every other test works in a
// temp directory; these two files are the ones a fresh checkout actually starts
// from, so they get a test of their own.
const (
	repoConfigPath   = "../../secretary.yml"
	repoArtifactPath = "../../docs/duties.md"
)

// TestSeededArtifactParses guards the empty-artifact path: a fresh checkout must
// be readable, and it must offer every section the configuration names, or the
// first `add --section Здоровье` lands in a heading nobody wrote.
func TestSeededArtifactParses(t *testing.T) {
	cfg, err := config.Load(repoConfigPath)
	if err != nil {
		t.Fatalf("конфигурация не читается: %v", err)
	}

	f, err := os.Open(repoArtifactPath)
	if err != nil {
		t.Fatalf("артефакт не открывается: %v", err)
	}
	defer f.Close()

	doc, err := mdstore.Parse(f)
	if err != nil {
		t.Fatalf("артефакт не разбирается: %v", err)
	}

	present := make(map[string]bool, len(doc.Sections))
	for _, section := range doc.Sections {
		present[section.Name] = true
	}
	for _, name := range cfg.Sections {
		if !present[name] {
			t.Errorf("в артефакте нет раздела %q из secretary.yml", name)
		}
	}
	if !present[mdstore.DoneSection] {
		t.Errorf("в артефакте нет раздела %q", mdstore.DoneSection)
	}
}

// TestSeededArtifactSurvivesRoundTrip is the hand-editing guarantee applied to
// the shipped file: `secretary fmt` over it must not move a byte.
func TestSeededArtifactSurvivesRoundTrip(t *testing.T) {
	original, err := os.ReadFile(repoArtifactPath)
	if err != nil {
		t.Fatalf("артефакт не читается: %v", err)
	}

	// The day is taken from the artifact itself rather than fixed here: the hint
	// lines and the «Обновлено» line are generated from the date of the last
	// write, so replaying any other day is guaranteed to rewrite the file and
	// would fail the test for a reason that has nothing to do with the format.
	today := "--today=" + updatedDate(t, string(original))

	w := newWorkspace(t)
	w.write(t, w.artifact, string(original))

	w.mustRun(t, today, "check")
	w.mustRun(t, today, "brief")
	w.mustRun(t, today, "fmt")

	if got := w.read(t, w.artifact); got != string(original) {
		t.Errorf("fmt переписал пустой артефакт:\n--- было ---\n%s\n--- стало ---\n%s", original, got)
	}
}

// updatedDate reads the date off the generated «Обновлено» line.
func updatedDate(t *testing.T, artifact string) string {
	t.Helper()
	for _, line := range strings.Split(artifact, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "> Обновлено:") {
			rest := strings.TrimPrefix(trimmed, "> Обновлено:")
			date := strings.TrimSpace(strings.SplitN(rest, "·", 2)[0])
			if _, err := time.Parse(mdstore.DateLayout, date); err != nil {
				t.Fatalf("в строке «Обновлено» не дата: %q", date)
			}
			return date
		}
	}
	t.Fatal("в артефакте нет строки «Обновлено»")
	return ""
}
