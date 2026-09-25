package containerfile

import "regexp"

var (
	rustFrameworks = []struct{ crate, name string }{
		{"axum", "axum"}, {"actix-web", "actix"}, {"rocket", "rocket"}, {"warp", "warp"}, {"poem", "poem"},
	}
	rustToolchainRe = regexp.MustCompile(`channel\s*=\s*"(\d+\.\d+(?:\.\d+)?)"`)
	rustPlainRe     = regexp.MustCompile(`(?m)^\s*(\d+\.\d+(?:\.\d+)?)\s*$`)
	rustVersionRe   = regexp.MustCompile(`(?m)^rust-version\s*=\s*"([^"]+)"`)
	opensslDepRe    = regexp.MustCompile(`(?m)^openssl\s*=`)
)

// rustScanner handles Cargo crates. It caches dependency builds with
// cargo-chef and runs the release binary on Debian slim.
type rustScanner struct{}

func (rustScanner) Name() string { return "rust" }

func (rustScanner) Render(p *Plan) (string, error) { return renderTemplate("rust", p) }

func (rustScanner) Detect(src *Source, forced bool) (*Plan, bool) {
	if !src.File("Cargo.toml") && !forced {
		return nil, false
	}
	cargo := src.ReadString("Cargo.toml")
	bin := firstString(tomlTable(cargo, "bin")["name"])
	if bin == "" {
		bin = firstString(tomlTable(cargo, "package")["name"])
	}
	openssl := src.Contains(`name = "openssl-sys"`, "Cargo.lock") || opensslDepRe.MatchString(cargo)

	plan := newPlan("rust")
	plan.PackageManager = "cargo"
	plan.Port = 8080
	plan.Env["PORT"] = "8080"
	plan.Ignore = []string{"/target"}
	for _, f := range rustFrameworks {
		if regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(f.crate) + `\s*=`).MatchString(cargo) {
			plan.Framework = f.name
			break
		}
	}
	if plan.Framework == "rocket" {
		plan.Env["ROCKET_ADDRESS"] = "0.0.0.0"
		plan.Env["ROCKET_PORT"] = "8080"
	}

	name := bin
	if name == "" {
		name = "app"
	}
	plan.Cmd = []string{"/usr/local/bin/" + name}
	build, runtime := []string{}, []string{"ca-certificates"}
	if openssl {
		build = []string{"libssl-dev", "pkg-config"}
		runtime = append(runtime, "libssl3")
	}
	plan.Assigns = map[string]any{"bin": name, "buildPackages": build, "runtimePackages": runtime}

	v := FirstVersion(
		func() Version { return FileMatch(src, "rust-toolchain.toml", rustToolchainRe) },
		func() Version { return FileMatch(src, "rust-toolchain", rustPlainRe) },
		func() Version { return ToolVersion(src, "rust") },
		func() Version { return MatchVersion(cargo, rustVersionRe, "Cargo.toml rust-version") },
	)
	if version := Extract(v.Value); version != "" {
		plan.SetVersion("rust", Version{version, v.Source})
	} else {
		plan.SetVersion("rust", Default("1"))
	}

	plan.Note(bin == "", "Could not find a [package] or [[bin]] name (workspace?); set --bin in the Containerfile.")
	return plan, true
}
