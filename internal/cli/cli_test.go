package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func goProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{"go.mod": "module x\n\ngo 1.22\n", "main.go": ""} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Run(args, "1.2.3", &out, &errOut)
	return code, out.String(), errOut.String()
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func contains(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(text, w) {
			t.Errorf("missing %q in:\n%s", w, text)
		}
	}
}

func TestWritesDockerfileByDefault(t *testing.T) {
	dir := goProject(t)
	code, out, _ := runCLI(t, "--dir", dir)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	contains(t, read(t, filepath.Join(dir, "Dockerfile")), "golang")
	contains(t, read(t, filepath.Join(dir, ".dockerignore")), ".git")
	contains(t, out, "* create", "Detected go (go 1.22) via go modules on port 8080")
}

func TestDefaultsToWorkingDirectory(t *testing.T) {
	dir := goProject(t)
	t.Chdir(dir)
	if code, out, _ := runCLI(t); code != 0 {
		t.Fatalf("exit %d", code)
	} else {
		contains(t, out, "* create Dockerfile")
	}
}

func TestContainerfileOut(t *testing.T) {
	dir := goProject(t)
	runCLI(t, "-d", dir, "-o", "Containerfile")
	for _, f := range []string{"Containerfile", ".containerignore"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s not written", f)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "Dockerfile")); err == nil {
		t.Error("Dockerfile should not be written")
	}

	abs := filepath.Join(t.TempDir(), "deploy", "Containerfile.prod")
	runCLI(t, "--dir", dir, "--out", abs, "--no-ignore")
	contains(t, read(t, abs), "golang")
}

func TestStdout(t *testing.T) {
	dir := goProject(t)
	code, out, errOut := runCLI(t, "--dir", dir, "--out", "stdout")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	contains(t, out, "FROM docker.io/library/golang")
	contains(t, errOut, "Detected go")
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("stdout mode wrote files: %v", entries)
	}
}

func TestExistingFiles(t *testing.T) {
	dir := goProject(t)
	path := filepath.Join(dir, "Dockerfile")
	os.WriteFile(path, []byte("FROM custom\n"), 0o644)

	_, out, _ := runCLI(t, "--dir", dir, "--no-ignore")
	contains(t, out, "* skip")
	if read(t, path) != "FROM custom\n" {
		t.Error("file was overwritten without --force")
	}
	if _, err := os.Stat(filepath.Join(dir, ".dockerignore")); err == nil {
		t.Error("--no-ignore wrote .dockerignore")
	}

	_, out, _ = runCLI(t, "--dir", dir, "--force", "--port", "9090")
	contains(t, out, "* overwrite")
	contains(t, read(t, path), "EXPOSE 9090")

	_, out, _ = runCLI(t, "--dir", dir, "--port", "9090")
	contains(t, out, "* identical")
}

func TestWriteErrors(t *testing.T) {
	dir := goProject(t)
	os.WriteFile(filepath.Join(dir, "blocker"), nil, 0o644)
	code, _, errOut := runCLI(t, "--dir", dir, "--out", "blocker/Dockerfile")
	if code != 1 {
		t.Errorf("exit %d", code)
	}
	contains(t, errOut, "error: cannot write")

	readonly := goProject(t)
	os.Mkdir(filepath.Join(readonly, "Dockerfile"), 0o755)
	if code, _, _ := runCLI(t, "--dir", readonly, "--force"); code != 1 {
		t.Errorf("writing over a directory: exit %d", code)
	}

	ignoreBlocked := goProject(t)
	os.Mkdir(filepath.Join(ignoreBlocked, ".dockerignore"), 0o755)
	if code, _, _ := runCLI(t, "--dir", ignoreBlocked, "--force"); code != 1 {
		t.Errorf("ignore over a directory: exit %d", code)
	}
}

func TestPlan(t *testing.T) {
	dir := goProject(t)
	_, out, _ := runCLI(t, "--dir", dir, "--plan", "--runtime", "go")
	contains(t, out, "runtime:         go", "framework:       -", "go:              1.22 (go.mod)",
		"port:            8080", "env:             PORT=8080")
	if _, err := os.Stat(filepath.Join(dir, "Dockerfile")); err == nil {
		t.Error("--plan wrote files")
	}

	_, out, _ = runCLI(t, "--dir", t.TempDir(), "--plan", "--runtime", "deno")
	contains(t, out, "note:            Runs with all permissions")

	_, out, _ = runCLI(t, "--dir", t.TempDir(), "--plan", "--runtime", "static")
	contains(t, out, "package manager: -")

	_, out, _ = runCLI(t, "--dir", t.TempDir(), "--plan", "--runtime", "dotnet")
	contains(t, out, "port:            -")

	_, out, _ = runCLI(t, "--dir", dir, "--plan", "--json")
	var plan map[string]any
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if plan["runtime"] != "go" {
		t.Errorf("json runtime: %v", plan["runtime"])
	}
}

func TestInfoCommands(t *testing.T) {
	_, out, _ := runCLI(t, "--list")
	contains(t, out, "elixir\nruby\n")
	_, out, _ = runCLI(t, "--help")
	contains(t, out, "Usage: containerfile")
	_, out, _ = runCLI(t, "-v")
	contains(t, out, "containerfile 1.2.3")
}

func TestErrors(t *testing.T) {
	cases := map[string][]string{
		"flag provided but not defined": {"--bogus"},
		"unexpected argument(s): foo":   {"foo"},
		"could not detect":              {"--dir", t.TempDir()},
		`unknown runtime "cobol"`:       {"--dir", t.TempDir(), "--runtime", "cobol"},
		"not a directory":               {"--dir", "/definitely/missing"},
	}
	for want, args := range cases {
		code, _, errOut := runCLI(t, args...)
		if code != 1 {
			t.Errorf("%v: exit %d", args, code)
		}
		contains(t, errOut, want)
	}
}

func TestRelative(t *testing.T) {
	if got := relative("/definitely/elsewhere/file"); got != "/definitely/elsewhere/file" {
		t.Errorf("relative outside cwd: %s", got)
	}
}
