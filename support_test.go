package containerfile

import (
	"regexp"
	"testing"
)

func TestVersions(t *testing.T) {
	src := NewSource(project(t, map[string]string{
		".tool-versions": "# comment\nerlang 27.1\nelixir 1.17.3-otp-27\n",
		"mise.toml":      "[env]\nnode = \"ignored\"\n\n[tools]\nnode = \"22.3\"\npython = [\"3.12\", \"3.11\"]\nruby = { version = \"3.3\" }\nbroken = 1\n",
		".nvmrc":         "\nv20.11.0\n",
	}))
	assertEqual(t, ToolVersion(src, "elixir"), Version{"1.17.3-otp-27", ".tool-versions"})
	assertEqual(t, ToolVersion(src, "node"), Version{"22.3", "mise.toml"})
	assertEqual(t, ToolVersion(src, "python"), Version{"3.12", "mise.toml"})
	assertEqual(t, ToolVersion(src, "ruby"), Version{"3.3", "mise.toml"})
	assertEqual(t, ToolVersion(src, "broken"), Version{})
	assertEqual(t, FileVersion(src, ".missing", ".nvmrc"), Version{"20.11.0", ".nvmrc"})
	assertEqual(t, FileVersion(src, ".missing"), Version{})
	assertEqual(t, MatchVersion("x", regexp.MustCompile(`(y)`), "s"), Version{})

	assertEqual(t, Extract("^8.2 || ^8.3"), "8.2")
	assertEqual(t, Extract(">=3.11,<4"), "3.11")
	assertEqual(t, Extract("lts/*"), "")
	assertEqual(t, Take("1.17.3", 2), "1.17")
	assertEqual(t, Take("1", 2), "1")
	assertEqual(t, Major("27.1"), 27)
	assertEqual(t, Major("lts"), -1)
}

func TestTemplateHelpers(t *testing.T) {
	assertEqual(t, Exec([]string{"bin/server", "start"}), `["bin/server", "start"]`)
	assertEqual(t, Quote(`a "b" & <c>`), `"a \"b\" & <c>"`)
	assertEqual(t, Env(map[string]string{"B": "2", "A": "1"}), "ENV A=\"1\" \\\n    B=\"2\"")
	assertEqual(t, Env(nil), "")
	assertEqual(t, AptInstall(nil), "")
	assertContains(t, AptInstall([]string{"b", "a", "b"}), "--no-install-recommends a b &&")
	assertEqual(t, Preprocess("a\n{{if .x}}\nb\n{{end}}\n{{env .e}}\n"), "a\n{{if .x}}b\n{{end}}{{env .e}}\n")
}

func TestSource(t *testing.T) {
	src := NewSource(project(t, map[string]string{
		"a.json": "[1]", "b.json": "{", "c.txt": "hel\x00lo", "d/e.txt": "", "node_modules/x.txt": "",
	}))
	if src.ReadJSON("a.json") != nil || src.ReadJSON("b.json") != nil || src.ReadJSON("missing") != nil {
		t.Error("ReadJSON should return nil for non-objects, invalid and missing files")
	}
	assertEqual(t, src.ReadString("c.txt"), "hello")
	assertEqual(t, src.IsDir("d"), true)
	assertEqual(t, src.File("d"), false)
	assertSlice(t, src.Glob("*.{txt,json}"), "a.json", "b.json", "c.txt")
	assertSlice(t, src.Glob("*/*.txt"), "d/e.txt")
	assertEqual(t, src.GlobContains("*.txt", "hello"), true)
	assertEqual(t, src.Matches(regexp.MustCompile("x"), "missing"), false)
}

func TestPlanSummary(t *testing.T) {
	p := newPlan("x")
	assertEqual(t, p.Summary(), "x")
	p.SetVersion("x", Default("1"))
	p.PackageManager, p.Port, p.Framework = "pm", 80, "fw"
	assertEqual(t, p.Summary(), "fw (x 1) via pm on port 80")
}
