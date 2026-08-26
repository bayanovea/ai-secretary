package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// skillPath is the Claude Code skill that turns free-form Russian into the
// flags this command takes. It lives outside the Go tree, but it is the only
// documentation of the CLI that is read by something which cannot ask
// questions, so it is worth a test that it has not drifted.
const skillPath = "../../.claude/skills/secretary/SKILL.md"

func TestSkillDocumentsEverySubcommand(t *testing.T) {
	text := readSkill(t)
	for _, c := range commands() {
		if !strings.Contains(text, "secretary "+c.name()) {
			t.Errorf("навык не описывает команду %q", c.name())
		}
	}
}

// TestSkillDocumentsAddFlags guards the classification contract: a flag the
// skill never mentions is a field that silently stops being filled in.
func TestSkillDocumentsAddFlags(t *testing.T) {
	text := readSkill(t)
	for _, flag := range []string{"--title", "--tier", "--from", "--due", "--effort", "--section", "--labels", "--context", "--note"} {
		if !strings.Contains(text, flag) {
			t.Errorf("навык не описывает флаг %q", flag)
		}
	}
}

// TestSkillFrontmatter checks the header Claude Code needs to load the skill at
// all: a name it can be invoked by, and a description it can match on.
func TestSkillFrontmatter(t *testing.T) {
	lines := strings.Split(readSkill(t), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		t.Fatal("навык начинается не с фронтматтера")
	}
	end := -1
	for i, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			end = i + 1
			break
		}
	}
	if end < 0 {
		t.Fatal("фронтматтер не закрыт")
	}

	front := strings.Join(lines[1:end], "\n")
	if !strings.Contains(front, "name: secretary") {
		t.Error("во фронтматтере нет name: secretary")
	}
	if !strings.Contains(front, "description:") {
		t.Error("во фронтматтере нет description")
	}
}

func readSkill(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.FromSlash(skillPath))
	if err != nil {
		t.Fatalf("навык не читается: %v", err)
	}
	return string(data)
}
