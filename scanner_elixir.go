package containerfile

import (
	"path"
	"strconv"
	"strings"
)

// Default versions when a project pins nothing.
const (
	defaultElixir = "1.19"
)

// maxOTP is the newest OTP major each Elixir minor has official images for.
var maxOTP = map[string]string{
	"1.13": "25", "1.14": "26", "1.15": "26", "1.16": "26", "1.17": "27",
	"1.18": "28", "1.19": "28", "1.20": "29",
}

// elixirScanner handles mix projects, with Phoenix and umbrella support.
// It builds a `mix release` in the official elixir image and runs it on
// Debian slim, following `mix phx.gen.release --docker`.
type elixirScanner struct{}

func (elixirScanner) Name() string { return "elixir" }

func (s elixirScanner) Render(p *Plan) (string, error) { return renderTemplate("elixir", p) }

func (elixirScanner) Detect(src *Source, forced bool) (*Plan, bool) {
	if !src.File("mix.exs") && !forced {
		return nil, false
	}

	project := ParseMixFile(src.ReadString("mix.exs"))
	children := elixirChildren(src, project)
	deps := project.Deps
	for _, c := range children {
		deps = append(deps, c.mix.Deps...)
	}
	has := func(dep string) bool { return containsString(deps, dep) }
	phoenix := has("phoenix")
	release := project.App
	if len(project.Releases) > 0 {
		release = project.Releases[0]
	}
	if release == "" {
		release = "app"
	}

	plan := newPlan("elixir")
	plan.PackageManager = "mix"
	if phoenix {
		plan.Framework = "phoenix"
		plan.Env["PHX_SERVER"] = "true"
	}
	if phoenix || has("bandit") || has("plug_cowboy") {
		plan.Port = 4000
		plan.Env["PORT"] = "4000"
	}
	plan.Cmd = []string{"/app/bin/" + release, "start"}
	if src.File("rel/overlays/bin/server") {
		plan.Cmd = []string{"/app/bin/server"}
	}

	elixir, otp := elixirVersions(src)
	plan.SetVersion("elixir", elixir)
	plan.SetVersion("otp", otp)

	web := elixirWeb(src, project, children, phoenix)
	plan.Ignore = elixirIgnore(web.dir)
	plan.Assigns = elixirAssigns(src, project, release, web, otp)

	plan.Note(project.Umbrella && len(project.Releases) == 0,
		"Umbrella project without `releases:` in mix.exs; add one so `mix release` knows what to build.")
	plan.Note(phoenix, "Set SECRET_KEY_BASE (mix phx.gen.secret) and PHX_HOST at runtime.")
	plan.Note(has("ecto_sql"), ectoNote(src))
	plan.Note(web.assets && !web.deploy,
		"Phoenix assets found but no `assets.deploy` alias; static assets will not be built.")
	plan.Note(src.IsDir("native"),
		"`native/` found (Rustler?); add a Rust toolchain and COPY native to the builder stage.")
	return plan, true
}

type elixirChild struct {
	dir string
	mix MixFile
}

func elixirChildren(src *Source, project MixFile) []elixirChild {
	if !project.Umbrella {
		return nil
	}
	var children []elixirChild
	for _, file := range src.Glob("apps/*/mix.exs") {
		children = append(children, elixirChild{path.Dir(file), ParseMixFile(src.ReadString(file))})
	}
	return children
}

type elixirWebApp struct {
	dir, npmInstall       string
	assets, setup, deploy bool
}

// elixirWeb finds where the Phoenix web app lives and how its assets build.
func elixirWeb(src *Source, project MixFile, children []elixirChild, phoenix bool) elixirWebApp {
	web := elixirWebApp{dir: "."}
	aliases := project.Aliases
	for _, c := range children {
		if c.mix.HasDep("phoenix") && src.IsDir(path.Join(c.dir, "assets")) {
			web.dir, aliases = c.dir, c.mix.Aliases
			break
		}
	}
	assetsDir := inDir(web.dir, "assets")
	web.assets = phoenix && src.IsDir(assetsDir)
	web.setup = web.assets && containsString(aliases, "assets.setup")
	web.deploy = web.assets && containsString(aliases, "assets.deploy")
	if web.assets && src.File(path.Join(assetsDir, "package.json")) {
		web.npmInstall = "npm --prefix " + assetsDir + " install"
		if src.File(path.Join(assetsDir, "package-lock.json")) {
			web.npmInstall = "npm --prefix " + assetsDir + " ci"
		}
	}
	return web
}

func elixirAssigns(src *Source, project MixFile, release string, web elixirWebApp, otp Version) map[string]any {
	var configFiles []string
	for _, f := range []string{"config/config.exs", "config/prod.exs"} {
		if src.File(f) {
			configFiles = append(configFiles, f)
		}
	}
	releaseArg := ""
	if len(project.Releases) > 0 {
		releaseArg = " " + release
	}
	debian := "bookworm"
	if Major(otp.Value) >= 28 {
		debian = "trixie"
	}
	buildPackages := []string{"build-essential", "ca-certificates", "git"}
	if web.npmInstall != "" {
		buildPackages = append(buildPackages, "nodejs", "npm")
	}
	return map[string]any{
		"debian":          debian,
		"buildPackages":   buildPackages,
		"runtimePackages": []string{"libstdc++6", "openssl", "libncurses6", "libsctp1", "locales", "ca-certificates"},
		"umbrella":        project.Umbrella,
		"lock":            src.File("mix.lock"),
		"configFiles":     configFiles,
		"runtimeConfig":   src.File("config/runtime.exs"),
		"priv":            !project.Umbrella && src.IsDir("priv"),
		"lib":             !project.Umbrella && src.IsDir("lib"),
		"rel":             src.IsDir("rel"),
		"release":         release,
		"releaseArg":      releaseArg,
		"webDir":          web.dir,
		"assets":          web.assets,
		"npmInstall":      web.npmInstall,
		"assetsSetup":     web.setup,
		"assetsDeploy":    web.deploy,
	}
}

func elixirVersions(src *Source) (elixir, otp Version) {
	elixir = FirstVersion(
		func() Version { return ToolVersion(src, "elixir") },
		func() Version { return FileVersion(src, ".elixir-version") },
	)
	erlang := FirstVersion(
		func() Version { return ToolVersion(src, "erlang", "erl") },
		func() Version { return FileVersion(src, ".erlang-version") },
	)
	if erlang.Found() {
		erlang.Value = strconv.Itoa(Major(erlang.Value))
	}

	if !elixir.Found() {
		elixir = Default(defaultElixir)
	}
	// "1.17.3-otp-27" pins both.
	version, otpSuffix, _ := strings.Cut(elixir.Value, "-otp-")
	elixir.Value = version

	switch {
	case erlang.Found():
		otp = erlang
	case otpSuffix != "":
		otp = Version{strconv.Itoa(Major(otpSuffix)), elixir.Source}
	default:
		otp = Default(maxOTP[Take(version, 2)])
		if otp.Value == "" {
			otp = Default(maxOTP[defaultElixir])
		}
	}
	return elixir, otp
}

func ectoNote(src *Source) string {
	if src.File("rel/overlays/bin/migrate") {
		return "Run migrations on deploy with `/app/bin/migrate`."
	}
	return "Ecto detected; run `mix phx.gen.release` for a migrate script, or call your Release.migrate/0 on deploy."
}

func elixirIgnore(webDir string) []string {
	return []string{
		"_build/", "deps/", "*.ez", "erl_crash.dump", "/cover", "/doc", "/test", "/tmp",
		".elixir_ls", ".lexical", ".expert",
		inDir(webDir, "assets") + "/node_modules",
		inDir(webDir, "priv") + "/static/assets",
		inDir(webDir, "priv") + "/static/cache_manifest.json",
	}
}

func inDir(dir, rel string) string {
	if dir == "." {
		return rel
	}
	return path.Join(dir, rel)
}
