// Golden-file helpers.
//
// ⚠️ The plan called this file `golden.go`. It is a test file instead: the
// helper registers a `-update` flag, and a non-test file doing that would add
// the flag to the production binary.
package mdstore

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// updateGolden rewrites the expected files instead of comparing against them.
// Run `go test ./internal/mdstore -update` after an intentional format change and read the
// diff: the golden files are how the artifact format is reviewed.
var updateGolden = flag.Bool("update", false, "rewrite the golden files in testdata/golden")

// goldenPath locates a golden file by its base name.
func goldenPath(name string) string {
	return filepath.Join("testdata", "golden", name)
}

// assertGolden compares got against the named golden file, or rewrites it when
// -update is given.
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := goldenPath(name)

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (regenerate: go test ./internal/mdstore -update)", path, err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}
