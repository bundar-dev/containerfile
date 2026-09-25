package containerfile

import (
	"maps"
	"regexp"
	"strconv"
	"strings"
)

var (
	nodePackageFiles = []string{"package.json", "package-lock.json", "npm-shrinkwrap.json", "yarn.lock",
		".yarnrc", ".yarnrc.yml", "pnpm-lock.yaml", "pnpm-workspace.yaml", ".npmrc", "bun.lockb", "bun.lock", "bunfig.toml"}
	nodeStartScripts = []string{"start:prod", "start:production", "start"}
	nodeEntryFiles   = []string{"server.js", "index.js", "app.js", "main.js", "server.mjs", "index.mjs", "app.mjs",
		"src/server.js", "src/index.js", "src/app.js"}
	nodeIgnore = []string{"node_modules", ".npm", ".pnpm-store", ".yarn/cache", ".next", ".nuxt", ".output",
		".svelte-kit", ".astro", "coverage"}

	// Lockfiles win over the packageManager field; npm is the fallback.
	nodeLockfiles = []struct {
		files []string
		pm    string
	}{
		{[]string{"bun.lockb", "bun.lock"}, "bun"},
		{[]string{"pnpm-lock.yaml"}, "pnpm"},
		{[]string{"yarn.lock"}, "yarn"},
		{[]string{"package-lock.json", "npm-shrinkwrap.json"}, "npm"},
	}

	// Frameworks and the dependencies that identify them, checked in order.
	nodeFrameworks = []struct {
		name string
		deps []string
	}{
		{"nextjs", []string{"next"}},
		{"nuxt", []string{"nuxt"}},
		{"remix", []string{"@remix-run/node", "@remix-run/serve", "remix"}},
		{"react-router", []string{"@react-router/serve", "@react-router/node"}},
		{"sveltekit", []string{"@sveltejs/kit"}},
		{"astro", []string{"astro"}},
		{"nestjs", []string{"@nestjs/core"}},
		{"vite", []string{"vite"}},
		{"create-react-app", []string{"react-scripts"}},
	}

	nextStandaloneRe = regexp.MustCompile("output\\s*:\\s*[\"'`]standalone[\"'`]")
	pnpmLockRe       = regexp.MustCompile(`lockfileVersion:\s*'?(\d+)`)
	nodePortRe       = regexp.MustCompile(`(?:PORT\s*(?:\|\||\?\?)\s*|\.listen\(\s*|port\s*[:=]\s*)(\d{4,5})\b`)
)

// nodeScanner handles package.json projects on Node.js or Bun, detecting
// the package manager from lockfiles and common frameworks. Client-side
// SPAs are built and served by nginx.
type nodeScanner struct{}

func (nodeScanner) Name() string { return "node" }

func (nodeScanner) Render(p *Plan) (string, error) { return renderTemplate("node", p) }

// nodeApp is what a framework decides about the final image.
type nodeApp struct {
	framework  string
	port       int
	env        map[string]string
	prune      bool
	standalone bool
	staticDir  string
	runnerArgs []string // run with node/bun directly
}

func (nodeScanner) Detect(src *Source, forced bool) (*Plan, bool) {
	if !src.File("package.json") && !forced {
		return nil, false
	}
	pkg := src.ReadJSON("package.json")
	if pkg == nil {
		pkg = map[string]any{}
	}
	deps := maps.Clone(jsonMap(pkg, "devDependencies"))
	maps.Copy(deps, jsonMap(pkg, "dependencies"))
	scripts := jsonMap(pkg, "scripts")
	pm := nodePackageManager(src, pkg)
	runtime := "node"
	if pm == "bun" {
		runtime = "bun"
	}
	app := nodeFramework(src, deps, scripts)

	plan := newPlan(runtime)
	plan.Framework = app.framework
	plan.PackageManager = pm
	plan.Port = app.port
	plan.Env = app.env
	plan.Cmd = nodeCommand(app, src, pkg, pm, runtime)
	plan.Ignore = nodeIgnore
	plan.SetVersion(runtime, nodeVersion(src, pkg, runtime))

	_, prisma := deps["@prisma/client"]
	_, prismaCLI := deps["prisma"]
	build := ""
	if _, ok := scripts["build"]; ok {
		build = nodeRunScript(pm, "build")
	}
	prune := ""
	if app.prune {
		prune = nodePrune[pm]
	}
	workspaces := nodeWorkspaces(src, pkg)
	buildPackages := []string{"build-essential", "pkg-config", "python-is-python3"}
	if prisma || prismaCLI {
		buildPackages = append(buildPackages, "openssl")
	}
	user := "node"
	if runtime == "bun" {
		user = "bun"
	}
	plan.Assigns = map[string]any{
		"bun":           runtime == "bun",
		"user":          user,
		"workspaces":    workspaces,
		"packageFiles":  existing(src, nodePackageFiles),
		"pmSetup":       nodePMSetup(pm, pkg, src),
		"install":       nodeInstall(pm, src),
		"build":         build,
		"prisma":        prisma || prismaCLI,
		"prismaDir":     src.IsDir("prisma"),
		"buildPackages": buildPackages,
		"exec":          nodeExec[pm],
		"prune":         prune,
		"staticDir":     app.staticDir,
		"outputs":       nodeOutputs(app, src),
	}

	_, adapterNode := deps["@sveltejs/adapter-node"]
	plan.Note(workspaces, "Workspaces/monorepo detected; the whole repo is copied before install.")
	plan.Note(app.framework == "nextjs" && !app.standalone,
		"Set `output: \"standalone\"` in next.config for a much smaller image.")
	plan.Note(app.framework == "sveltekit" && app.staticDir == "" && !adapterNode,
		"SvelteKit without @sveltejs/adapter-node; install it so `node build` works.")
	plan.Note(pm == "yarn-berry",
		"Yarn Berry: dev dependencies are not pruned; consider `yarn workspaces focus --production`.")
	plan.Note(app.staticDir == "" && len(plan.Cmd) == 0,
		"No start script, main file or known framework found; set CMD manually.")
	return plan, true
}

func existing(src *Source, files []string) []string {
	var out []string
	for _, f := range files {
		if src.File(f) {
			out = append(out, f)
		}
	}
	return out
}

func nodePackageManager(src *Source, pkg map[string]any) string {
	field := jsonString(pkg, "packageManager")
	pm, _, _ := strings.Cut(field, "@")
	for _, lock := range nodeLockfiles {
		if src.File(lock.files...) {
			pm = lock.pm
			break
		}
	}
	switch pm {
	case "yarn":
		if src.File(".yarnrc.yml") || (strings.HasPrefix(field, "yarn@") && Major(strings.TrimPrefix(field, "yarn@")) >= 2) {
			return "yarn-berry"
		}
		return "yarn"
	case "npm", "pnpm", "bun":
		return pm
	default:
		return "npm"
	}
}

func nodePMSetup(pm string, pkg map[string]any, src *Source) string {
	switch pm {
	case "pnpm":
		version := "latest"
		if v, ok := strings.CutPrefix(jsonString(pkg, "packageManager"), "pnpm@"); ok {
			version, _, _ = strings.Cut(v, "+")
		} else if m := pnpmLockRe.FindStringSubmatch(src.ReadString("pnpm-lock.yaml")); m != nil {
			version = map[string]string{"5": "7", "6": "8"}[m[1]]
			if version == "" {
				version = "latest"
			}
		}
		return "RUN npm install -g pnpm@" + version
	case "yarn-berry":
		return "RUN npm install -g corepack && corepack enable"
	default:
		return ""
	}
}

func nodeInstall(pm string, src *Source) string {
	switch pm {
	case "pnpm":
		return "pnpm install --frozen-lockfile --prod=false"
	case "yarn":
		return "yarn install --frozen-lockfile --production=false"
	case "yarn-berry":
		return "yarn install --immutable"
	case "bun":
		if src.File("bun.lockb", "bun.lock") {
			return "bun install --frozen-lockfile"
		}
		return "bun install"
	default:
		if src.File("package-lock.json", "npm-shrinkwrap.json") {
			return "npm ci --include=dev"
		}
		return "npm install --include=dev"
	}
}

var (
	nodePrune = map[string]string{
		"npm":  "npm prune --omit=dev",
		"pnpm": "pnpm prune --prod",
		"yarn": "yarn install --production=true --frozen-lockfile",
		"bun":  "rm -rf node_modules && bun install --production",
	}
	nodeExec = map[string]string{"npm": "npx", "pnpm": "pnpm exec", "bun": "bunx", "yarn": "yarn", "yarn-berry": "yarn"}
)

func nodePMBinary(pm string) string { return strings.TrimSuffix(pm, "-berry") }

func nodeRunScript(pm, script string) string { return nodePMBinary(pm) + " run " + script }

func nodeWorkspaces(src *Source, pkg map[string]any) bool {
	_, ok := pkg["workspaces"]
	return ok || src.File("pnpm-workspace.yaml")
}

func nodeVersion(src *Source, pkg map[string]any, runtime string) Version {
	if runtime == "bun" {
		v := FirstVersion(
			func() Version { return ToolVersion(src, "bun") },
			func() Version {
				if v, ok := strings.CutPrefix(jsonString(pkg, "packageManager"), "bun@"); ok {
					return Version{Take(v, 1), "package.json packageManager"}
				}
				return Version{}
			},
		)
		if !v.Found() {
			return Default("1")
		}
		return v
	}

	v := FirstVersion(
		func() Version { return FileVersion(src, ".nvmrc", ".node-version") },
		func() Version { return ToolVersion(src, "node", "nodejs") },
		func() Version {
			return Version{jsonString(jsonMap(pkg, "engines"), "node"), "package.json engines.node"}
		},
	)
	if major := Take(Extract(v.Value), 1); major != "" {
		return Version{major, v.Source}
	}
	return Default("lts")
}

func nodeFramework(src *Source, deps, scripts map[string]any) nodeApp {
	app := nodeApp{port: 3000, env: map[string]string{}, prune: true}
	for _, f := range nodeFrameworks {
		if hasAnyKey(deps, f.deps) && nodeFrameworkApplies(f.name, scripts) {
			app.framework = f.name
			break
		}
	}

	switch app.framework {
	case "nextjs":
		if src.Matches(nextStandaloneRe, "next.config.js", "next.config.mjs", "next.config.ts", "next.config.cjs") {
			app.standalone, app.prune = true, false
			app.env["HOSTNAME"] = "0.0.0.0"
			app.runnerArgs = []string{"server.js"}
		}
	case "nuxt":
		app.prune = false
		app.runnerArgs = []string{".output/server/index.mjs"}
	case "sveltekit":
		if hasAnyKey(deps, []string{"@sveltejs/adapter-static"}) {
			app.staticDir = "build"
		} else {
			app.runnerArgs = []string{"build"}
		}
	case "astro":
		if hasAnyKey(deps, []string{"@astrojs/node"}) {
			app.port = 4321
			app.env["HOST"] = "0.0.0.0"
			app.runnerArgs = []string{"./dist/server/entry.mjs"}
		} else {
			app.staticDir = "dist"
		}
	case "nestjs":
		app.runnerArgs = []string{"dist/main"}
	case "vite":
		app.staticDir = "dist"
	case "create-react-app":
		app.staticDir = "build"
	case "":
		app.port = nodeDetectPort(src)
	}

	if app.staticDir != "" {
		app.port, app.prune = 8080, false
	} else {
		app.env["PORT"] = strconv.Itoa(app.port)
	}
	return app
}

// SPA tooling only means "static site" when there is no server to start.
func nodeFrameworkApplies(name string, scripts map[string]any) bool {
	switch name {
	case "vite":
		return !hasAnyKey(scripts, nodeStartScripts)
	case "create-react-app":
		return !hasAnyKey(scripts, []string{"start:prod", "start:production"})
	default:
		return true
	}
}

func hasAnyKey(m map[string]any, keys []string) bool {
	for _, k := range keys {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

func nodeCommand(app nodeApp, src *Source, pkg map[string]any, pm, runner string) []string {
	if app.staticDir != "" {
		return []string{}
	}
	if app.runnerArgs != nil {
		return append([]string{runner}, app.runnerArgs...)
	}
	scripts := jsonMap(pkg, "scripts")
	for _, s := range nodeStartScripts {
		if _, ok := scripts[s]; ok {
			return []string{nodePMBinary(pm), "run", s}
		}
	}
	if main := jsonString(pkg, "main"); main != "" && src.File(main) {
		return []string{runner, main}
	}
	if entry := src.FirstFile(nodeEntryFiles...); entry != "" {
		return []string{runner, entry}
	}
	return []string{}
}

func nodeDetectPort(src *Source) int {
	files := append(append([]string{}, nodeEntryFiles...), "src/main.ts", "src/index.ts", "src/server.ts", "server.ts", "index.ts")
	for _, f := range files {
		if m := nodePortRe.FindStringSubmatch(src.ReadString(f)); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n >= 1024 {
				return n
			}
		}
	}
	return 3000
}

type copyStep struct{ From, To string }

// nodeOutputs lists what the final stage copies from the build stage.
func nodeOutputs(app nodeApp, src *Source) []copyStep {
	switch {
	case app.standalone:
		out := []copyStep{{"/app/.next/standalone", "/app"}, {"/app/.next/static", "/app/.next/static"}}
		if src.IsDir("public") {
			out = append(out, copyStep{"/app/public", "/app/public"})
		}
		return out
	case app.framework == "nuxt":
		return []copyStep{{"/app/.output", "/app/.output"}}
	case app.staticDir != "":
		return []copyStep{{"/app/" + app.staticDir, "/usr/share/nginx/html"}}
	default:
		return []copyStep{{"/app", "/app"}}
	}
}
