package containerfile

import (
	"regexp"
	"strings"
)

var (
	rubyIgnore = []string{"/.bundle", "/vendor/bundle", "/log/*", "/tmp/*", "/storage/*", "/public/assets",
		"/node_modules", "/coverage", "/spec", "/test", "/config/master.key", "/config/credentials/*.key", ".rspec_status"}

	// Database gems: build-stage and runtime packages.
	rubyDBGems = []struct {
		gem            string
		build, runtime []string
	}{
		{"pg", []string{"libpq-dev"}, []string{"postgresql-client"}},
		{"mysql2", []string{"default-libmysqlclient-dev"}, []string{"default-mysql-client"}},
		{"trilogy", nil, []string{"default-mysql-client"}},
		{"sqlite3", nil, []string{"sqlite3"}},
	}

	lockRubyRe    = regexp.MustCompile(`RUBY VERSION\s+ruby (\d+\.\d+\.\d+)`)
	gemfileRubyRe = regexp.MustCompile(`(?m)^ruby\s+["']([^"']+)["']`)
)

// rubyScanner handles Gemfile/config.ru projects, with Rails support
// modeled on the Dockerfile Rails 7.1+ generates.
type rubyScanner struct{}

func (rubyScanner) Name() string { return "ruby" }

func (rubyScanner) Render(p *Plan) (string, error) { return renderTemplate("ruby", p) }

// rubyJS describes the JavaScript toolchain for jsbundling/cssbundling-rails.
type rubyJS struct{ Files, Setup, Install string }

func (rubyScanner) Detect(src *Source, forced bool) (*Plan, bool) {
	if !src.File("Gemfile", "config.ru") && !forced {
		return nil, false
	}
	lock := src.ReadString("Gemfile.lock")
	gem := func(name string) bool { return strings.Contains(lock, "    "+name+" (") }
	rails := src.File("bin/rails") || gem("rails")

	plan := newPlan("ruby")
	plan.PackageManager = "bundler"
	plan.Ignore = rubyIgnore
	plan.SetVersion("ruby", rubyVersion(src, lock))

	build := []string{"build-essential", "git", "libyaml-dev", "pkg-config"}
	runtime := []string{"curl"}
	for _, db := range rubyDBGems {
		if gem(db.gem) {
			build = append(build, db.build...)
			runtime = append(runtime, db.runtime...)
		}
	}
	assigns := map[string]any{
		"rails": rails, "workdir": "/app", "buildPackages": build, "runtimePackages": runtime,
		"bootsnap": false, "assets": false, "js": (*rubyJS)(nil), "nodeVersion": "", "entrypoint": []string(nil),
	}

	if rails {
		plan.Framework = "rails"
		plan.Port = 3000
		plan.Cmd = []string{"./bin/rails", "server", "-b", "0.0.0.0"}
		js := rubyJSBuild(src)
		assigns["workdir"] = "/rails"
		assigns["runtimePackages"] = append(runtime, "libjemalloc2", "libvips")
		assigns["bootsnap"] = gem("bootsnap")
		assigns["assets"] = gem("propshaft") || gem("sprockets")
		assigns["js"] = js
		if js != nil {
			assigns["nodeVersion"] = rubyNodeVersion(src)
		}
		if src.File("bin/docker-entrypoint") {
			assigns["entrypoint"] = []string{"/rails/bin/docker-entrypoint"}
		}
	} else if src.File("config.ru") {
		plan.Port = 8080
		plan.Cmd = []string{"bundle", "exec", "rackup", "--host", "0.0.0.0", "--port", "8080"}
	} else if file := src.FirstFile("app.rb", "main.rb", "server.rb"); file != "" {
		plan.Port = 8080
		plan.Cmd = []string{"bundle", "exec", "ruby", file}
	}
	if plan.Port != 0 && !rails {
		plan.Env["PORT"] = itoa(plan.Port)
	}
	plan.Assigns = assigns

	plan.Note(rails, "Set RAILS_MASTER_KEY (or SECRET_KEY_BASE) at runtime.")
	plan.Note(len(plan.Cmd) == 0, "Could not find an entrypoint; set CMD manually.")
	return plan, true
}

func rubyJSBuild(src *Source) *rubyJS {
	pkg := src.ReadJSON("package.json")
	if _, ok := jsonMap(pkg, "scripts")["build"]; !ok {
		return nil
	}
	switch {
	case src.File("yarn.lock"):
		return &rubyJS{"package.json yarn.lock", "npm install -g yarn", "yarn install --frozen-lockfile"}
	case src.File("pnpm-lock.yaml"):
		return &rubyJS{"package.json pnpm-lock.yaml", "npm install -g pnpm", "pnpm install --frozen-lockfile"}
	case src.File("bun.lock", "bun.lockb"):
		return &rubyJS{"package.json bun.lock*", "npm install -g bun", "bun install --frozen-lockfile"}
	default:
		return &rubyJS{"package*.json", "", "npm ci"}
	}
}

func rubyNodeVersion(src *Source) string {
	v := FirstVersion(
		func() Version { return FileVersion(src, ".node-version", ".nvmrc") },
		func() Version { return ToolVersion(src, "node", "nodejs") },
	)
	if major := Take(Extract(v.Value), 1); major != "" {
		return major
	}
	return "lts"
}

func rubyVersion(src *Source, lock string) Version {
	v := FirstVersion(
		func() Version { return FileVersion(src, ".ruby-version") },
		func() Version { return ToolVersion(src, "ruby") },
		func() Version { return MatchVersion(lock, lockRubyRe, "Gemfile.lock") },
		func() Version { return FileMatch(src, "Gemfile", gemfileRubyRe) },
	)
	if version := Extract(v.Value); version != "" {
		return Version{version, v.Source}
	}
	return Default("3.4")
}
