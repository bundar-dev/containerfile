package containerfile

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Plan is the intermediate representation between scanning and rendering.
// A Scanner inspects a Source and returns a Plan; Render turns it into a
// Containerfile, so a plan can be inspected or tweaked first.
type Plan struct {
	// Scanner renders the plan.
	Scanner Scanner `json:"-"`
	// Runtime is the language family, e.g. "elixir", "node".
	Runtime string `json:"runtime"`
	// Framework is the detected framework, e.g. "phoenix", or "".
	Framework string `json:"framework,omitempty"`
	// Versions holds detected tool versions, e.g. {"elixir": "1.18.4"}.
	Versions map[string]string `json:"versions"`
	// Sources records where each version came from.
	Sources map[string]string `json:"sources"`
	// PackageManager is e.g. "pnpm", "poetry", "bundler".
	PackageManager string `json:"package_manager,omitempty"`
	// Port the app listens on inside the container; 0 means none.
	Port int `json:"port,omitempty"`
	// Env holds runtime environment variables for the final stage.
	Env map[string]string `json:"env"`
	// Cmd is the start command in exec form.
	Cmd []string `json:"cmd"`
	// Assigns holds scanner-specific template values.
	Assigns map[string]any `json:"-"`
	// Ignore lists .dockerignore/.containerignore entries.
	Ignore []string `json:"ignore"`
	// Notes are hints about things the user should check.
	Notes []string `json:"notes"`
}

func newPlan(runtime string) *Plan {
	return &Plan{
		Runtime:  runtime,
		Versions: map[string]string{},
		Sources:  map[string]string{},
		Env:      map[string]string{},
		Cmd:      []string{},
		Assigns:  map[string]any{},
	}
}

// SetVersion records a version and where it came from.
func (p *Plan) SetVersion(tool string, v Version) {
	p.Versions[tool] = v.Value
	p.Sources[tool] = v.Source
}

// Note appends a note when cond is true.
func (p *Plan) Note(cond bool, message string) {
	if cond {
		p.Notes = append(p.Notes, message)
	}
}

// Summary returns a one-line description, e.g.
// "phoenix (elixir 1.18.4, otp 27) via mix on port 4000".
func (p *Plan) Summary() string {
	parts := []string{p.Runtime}
	if p.Framework != "" {
		parts[0] = p.Framework
	}
	if len(p.Versions) > 0 {
		var vs []string
		for _, k := range slices.Sorted(maps.Keys(p.Versions)) {
			vs = append(vs, k+" "+p.Versions[k])
		}
		parts = append(parts, "("+strings.Join(vs, ", ")+")")
	}
	if p.PackageManager != "" {
		parts = append(parts, "via "+p.PackageManager)
	}
	if p.Port != 0 {
		parts = append(parts, fmt.Sprintf("on port %d", p.Port))
	}
	return strings.Join(parts, " ")
}

// templateData merges the plan fields and assigns for templates.
func (p *Plan) templateData() map[string]any {
	data := map[string]any{
		"runtime":        p.Runtime,
		"framework":      p.Framework,
		"versions":       p.Versions,
		"packageManager": p.PackageManager,
		"port":           p.Port,
		"env":            p.Env,
		"cmd":            p.Cmd,
	}
	maps.Copy(data, p.Assigns)
	return data
}
