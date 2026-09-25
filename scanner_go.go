package containerfile

import (
	"path"
	"regexp"
	"strings"
)

var (
	goCgoModules = []string{"github.com/mattn/go-sqlite3", "github.com/confluentinc/confluent-kafka-go",
		"gopkg.in/gographics/imagick"}
	goDirectiveRe = regexp.MustCompile(`(?m)^go\s+(\d+\.\d+)`)
	goModuleRe    = regexp.MustCompile(`(?m)^module\s+(\S+)`)
)

// goScanner handles Go modules. It builds a static binary and runs it on
// distroless; projects using cgo get a glibc-based runtime.
type goScanner struct{}

func (goScanner) Name() string { return "go" }

func (goScanner) Render(p *Plan) (string, error) { return renderTemplate("go", p) }

func (goScanner) Detect(src *Source, forced bool) (*Plan, bool) {
	if !src.File("go.mod") && !forced {
		return nil, false
	}
	gomod := src.ReadString("go.mod")
	pkg, many := goMainPackage(src, gomod)
	cgo := false
	for _, m := range goCgoModules {
		cgo = cgo || strings.Contains(gomod, m)
	}

	plan := newPlan("go")
	plan.PackageManager = "go modules"
	plan.Port = 8080
	plan.Env["PORT"] = "8080"
	plan.Cmd = []string{"/app/server"}
	plan.Ignore = []string{"/bin", "/tmp", "*.test", "*.out", "/vendor"}
	plan.Assigns = map[string]any{"package": pkg, "goSum": src.File("go.sum"), "cgo": cgo}

	v := FirstVersion(
		func() Version { return ToolVersion(src, "golang", "go") },
		func() Version { return MatchVersion(gomod, goDirectiveRe, "go.mod") },
	)
	if v.Found() {
		plan.SetVersion("go", Version{Take(v.Value, 2), v.Source})
	} else {
		plan.SetVersion("go", Default("1"))
	}

	plan.Note(many, "Several commands under cmd/; building "+pkg+". Change the go build path if needed.")
	plan.Note(strings.Contains(gomod, "require") && !src.File("go.sum"),
		"No go.sum found; run `go mod tidy` for reproducible builds.")
	return plan, true
}

// goMainPackage picks the package to build: the root when it has main.go,
// else cmd/<name>, preferring the module's base name, server, api or app.
func goMainPackage(src *Source, gomod string) (string, bool) {
	if src.File("main.go") {
		return ".", false
	}
	var dirs []string
	for _, f := range src.Glob("cmd/*/main.go") {
		dirs = append(dirs, path.Dir(f))
	}
	switch len(dirs) {
	case 0:
		return ".", false
	case 1:
		return "./" + dirs[0], false
	}
	module := ""
	if m := goModuleRe.FindStringSubmatch(gomod); m != nil {
		module = path.Base(m[1])
	}
	for _, d := range dirs {
		if containsString([]string{module, "server", "api", "app"}, path.Base(d)) {
			return "./" + d, true
		}
	}
	return "./" + dirs[0], true
}
