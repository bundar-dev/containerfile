package containerfile

import "strings"

// denoScanner handles deno.json / deno.lock projects. It runs the start
// task when defined, otherwise the first known entry file.
type denoScanner struct{}

func (denoScanner) Name() string { return "deno" }

func (denoScanner) Render(p *Plan) (string, error) { return renderTemplate("deno", p) }

func (denoScanner) Detect(src *Source, forced bool) (*Plan, bool) {
	if !src.File("deno.json", "deno.jsonc", "deno.lock") && !forced {
		return nil, false
	}
	config := src.ReadJSON("deno.json")
	tasks := jsonMap(config, "tasks")
	entry := src.FirstFile("main.ts", "server.ts", "index.ts", "app.ts", "mod.ts", "main.js", "server.js")

	plan := newPlan("deno")
	plan.PackageManager = "deno"
	plan.Port = 8000
	plan.Env["PORT"] = "8000"
	plan.Ignore = []string{"node_modules", "_fresh"}
	for key := range jsonMap(config, "imports") {
		if strings.HasPrefix(key, "$fresh") || key == "fresh" {
			plan.Framework = "fresh"
		}
	}
	switch {
	case tasks["start"] != nil:
		plan.Cmd = []string{"deno", "task", "start"}
	case entry != "":
		plan.Cmd = []string{"deno", "run", "-A", entry}
	}
	_, build := tasks["build"]
	plan.Assigns = map[string]any{"entry": entry, "build": build}

	v := ToolVersion(src, "deno")
	if !v.Found() {
		v = Default("latest")
	}
	plan.SetVersion("deno", v)
	plan.Note(true, "Runs with all permissions (-A); tighten flags such as --allow-net for production.")
	plan.Note(len(plan.Cmd) == 0, "No start task or entry file found; set CMD manually.")
	return plan, true
}
