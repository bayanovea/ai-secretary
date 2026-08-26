package main

import (
	"os"
	"testing"

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

	w := newWorkspace(t)
	w.write(t, w.artifact, string(original))

	w.mustRun(t, "--today=2026-08-27", "check")
	w.mustRun(t, "--today=2026-08-27", "brief")
	w.mustRun(t, "--today=2026-08-27", "fmt")

	if got := w.read(t, w.artifact); got != string(original) {
		t.Errorf("fmt переписал пустой артефакт:\n--- было ---\n%s\n--- стало ---\n%s", original, got)
	}
}
