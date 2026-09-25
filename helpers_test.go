package containerfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// project writes files into a temporary directory and returns its path.
func project(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func generate(t *testing.T, files map[string]string, opts ...Options) *Result {
	t.Helper()
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	result, err := Generate(project(t, files), o)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return result
}

func assertContains(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
}

func refuteContains(t *testing.T, text string, unwanted ...string) {
	t.Helper()
	for _, u := range unwanted {
		if strings.Contains(text, u) {
			t.Errorf("unexpected %q in:\n%s", u, text)
		}
	}
}

func assertEqual[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func assertSlice(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") || len(got) != len(want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func hasNote(plan *Plan, substr string) bool {
	for _, n := range plan.Notes {
		if strings.Contains(n, substr) {
			return true
		}
	}
	return false
}
