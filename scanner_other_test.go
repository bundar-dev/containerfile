package containerfile

import "testing"

func TestRails(t *testing.T) {
	r := generate(t, map[string]string{
		"Gemfile":      "source 'https://rubygems.org'\nruby \"3.3.4\"\n",
		"Gemfile.lock": "GEM\n  specs:\n    bootsnap (1.18.3)\n    pg (1.5.6)\n    propshaft (0.9.0)\n    rails (7.2.0)\n",
		"bin/rails":    "", "bin/docker-entrypoint": "",
		"package.json": `{"scripts": {"build": "esbuild app/javascript/*.*"}}`, "yarn.lock": "",
	})
	assertEqual(t, r.Plan.Framework, "rails")
	assertEqual(t, r.Plan.Port, 3000)
	assertEqual(t, r.Plan.Versions["ruby"], "3.3.4")
	assertContains(t, r.Containerfile, "FROM docker.io/library/node:${NODE_VERSION}-slim AS node",
		"COPY --from=node /usr/local/bin/node /usr/local/bin/node", "RUN npm install -g yarn",
		"libpq-dev", "postgresql-client", "bundle exec bootsnap precompile --gemfile",
		"SECRET_KEY_BASE_DUMMY=1 ./bin/rails assets:precompile", `ENTRYPOINT ["/rails/bin/docker-entrypoint"]`)

	for lock, install := range map[string]string{
		"pnpm-lock.yaml": "pnpm install --frozen-lockfile", "bun.lock": "bun install --frozen-lockfile", "package-lock.json": "npm ci",
	} {
		r := generate(t, map[string]string{
			"Gemfile": "", "Gemfile.lock": "GEM\n  specs:\n    rails (8.0.0)\n\nRUBY VERSION\n   ruby 3.3.1\n",
			"package.json": `{"scripts": {"build": "esbuild"}}`, lock: "", ".node-version": "22.1.0",
		})
		assertContains(t, r.Containerfile, "RUN "+install, "ARG NODE_VERSION=22")
		assertEqual(t, r.Plan.Versions["ruby"], "3.3.1")
	}
}

func TestRubyRackAndScripts(t *testing.T) {
	rack := generate(t, map[string]string{"Gemfile": "", "config.ru": "run App"})
	assertEqual(t, rack.Plan.Port, 8080)
	assertContains(t, rack.Containerfile, `CMD ["bundle", "exec", "rackup"`)
	refuteContains(t, rack.Containerfile, "RAILS_ENV", "ENTRYPOINT")

	script := generate(t, map[string]string{"Gemfile": "", "app.rb": "", "bin/docker-entrypoint": ""})
	assertSlice(t, script.Plan.Cmd, "bundle", "exec", "ruby", "app.rb")
	assertEqual(t, script.Plan.Versions["ruby"], "3.4")
	refuteContains(t, script.Containerfile, "ENTRYPOINT")

	none := generate(t, map[string]string{"Gemfile": ""})
	assertEqual(t, len(none.Plan.Cmd), 0)
}

func TestLaravel(t *testing.T) {
	r := generate(t, map[string]string{
		"artisan":       "",
		"composer.json": `{"require": {"php": "^8.2", "ext-redis": "*", "ext-json": "*"}}`,
		"composer.lock": "{}", "package.json": `{"scripts": {"build": "vite build"}}`, "public/index.php": "",
	})
	assertEqual(t, r.Plan.Framework, "laravel")
	assertEqual(t, r.Plan.Versions["php"], "8.2")
	assertContains(t, r.Containerfile, "FROM docker.io/library/composer:2 AS vendor",
		"RUN install-php-extensions bcmath intl opcache pdo_mysql pdo_pgsql redis zip",
		"APACHE_DOCUMENT_ROOT=/var/www/html/public", "COPY --from=assets --chown=www-data:www-data /app/public/build")
}

func TestPlainPHP(t *testing.T) {
	r := generate(t, map[string]string{"index.php": "<?php echo 1;"})
	assertEqual(t, r.Plan.PackageManager, "")
	assertEqual(t, r.Plan.Versions["php"], "8.4")
	assertContains(t, r.Containerfile, "APACHE_DOCUMENT_ROOT=/var/www/html\n", "COPY --chown=www-data:www-data . /var/www/html",
		"RUN install-php-extensions opcache")
	refuteContains(t, r.Containerfile, "composer")
}

func TestGo(t *testing.T) {
	r := generate(t, map[string]string{
		"go.mod": "module github.com/acme/api\n\ngo 1.23.2\n", "go.sum": "",
		"cmd/api/main.go": "", "cmd/migrate/main.go": "",
	})
	assertEqual(t, r.Plan.Versions["go"], "1.23")
	assertContains(t, r.Containerfile, "COPY go.mod go.sum ./",
		`CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/api`, "distroless/static-debian12:nonroot")

	cgo := generate(t, map[string]string{"go.mod": "module x\n\ngo 1.22\nrequire github.com/mattn/go-sqlite3 v1.14.0\n", "main.go": ""})
	assertContains(t, cgo.Containerfile, "CGO_ENABLED=1", "distroless/base-debian12", "COPY go.mod ./")
	if !hasNote(cgo.Plan, "go.sum") {
		t.Error("expected go.sum note")
	}

	single := generate(t, map[string]string{"go.mod": "module x\n", ".tool-versions": "golang 1.24.1\n", "cmd/web/main.go": ""})
	assertEqual(t, single.Plan.Versions["go"], "1.24")
	assertContains(t, single.Containerfile, "-o /out/server ./cmd/web")

	many := generate(t, map[string]string{"go.mod": "go 1.22\n", "cmd/a/main.go": "", "cmd/b/main.go": ""})
	assertContains(t, many.Containerfile, "./cmd/a")
	if !hasNote(many.Plan, "Several commands") {
		t.Error("expected several commands note")
	}
	assertEqual(t, generate(t, map[string]string{"go.mod": ""}).Plan.Versions["go"], "1")
}

func TestRust(t *testing.T) {
	r := generate(t, map[string]string{
		"Cargo.toml":          "[package]\nname = \"hello-api\"\nversion = \"0.1.0\"\n\n[dependencies]\naxum = \"0.7\"\n",
		"Cargo.lock":          "[[package]]\nname = \"openssl-sys\"\n",
		"rust-toolchain.toml": "[toolchain]\nchannel = \"1.80\"\n",
	})
	assertEqual(t, r.Plan.Framework, "axum")
	assertEqual(t, r.Plan.Versions["rust"], "1.80")
	assertContains(t, r.Containerfile, "cargo build --release --bin hello-api", "libssl-dev", "libssl3",
		`CMD ["/usr/local/bin/hello-api"]`)

	rocket := generate(t, map[string]string{
		"Cargo.toml": "[package]\nname = \"site\"\nrust-version = \"1.79\"\n\n[[bin]]\nname = \"server\"\n\n[[bin]]\nname = \"other\"\n\n[dependencies]\nrocket = \"0.5\"\n",
	})
	assertEqual(t, rocket.Plan.Framework, "rocket")
	assertSlice(t, rocket.Plan.Cmd, "/usr/local/bin/server")
	assertEqual(t, rocket.Plan.Env["ROCKET_ADDRESS"], "0.0.0.0")
	assertEqual(t, rocket.Plan.Versions["rust"], "1.79")
	refuteContains(t, rocket.Containerfile, "libssl")

	ws := generate(t, map[string]string{"Cargo.toml": "[workspace]\nmembers = [\"a\"]\n", "rust-toolchain": "1.81.0\n"})
	assertEqual(t, ws.Plan.Versions["rust"], "1.81.0")
	if !hasNote(ws.Plan, "workspace") {
		t.Error("expected workspace note")
	}
	assertEqual(t, generate(t, map[string]string{"Cargo.toml": ""}).Plan.Versions["rust"], "1")
}

func TestJava(t *testing.T) {
	spring := generate(t, map[string]string{
		"pom.xml": "<project><parent><artifactId>spring-boot-starter-parent</artifactId></parent><properties><java.version>17</java.version></properties></project>",
		"mvnw":    "",
	})
	assertEqual(t, spring.Plan.Framework, "spring-boot")
	assertEqual(t, spring.Plan.Versions["java"], "17")
	assertContains(t, spring.Containerfile, "eclipse-temurin:${JAVA_VERSION}-jdk AS build", "./mvnw -B package -DskipTests",
		"eclipse-temurin:${JAVA_VERSION}-jre")

	maven := generate(t, map[string]string{"pom.xml": "<maven.compiler.source>1.8</maven.compiler.source>"})
	assertEqual(t, maven.Plan.Versions["java"], "8")
	assertContains(t, maven.Containerfile, "FROM docker.io/library/maven:3-eclipse-temurin-${JAVA_VERSION}", "RUN mvn -B package -DskipTests")

	gradle := generate(t, map[string]string{"build.gradle.kts": "java { toolchain { languageVersion = JavaLanguageVersion.of(21) } }"})
	assertEqual(t, gradle.Plan.Versions["java"], "21")
	assertContains(t, gradle.Containerfile, "FROM docker.io/library/gradle:jdk${JAVA_VERSION} AS build", "RUN gradle build -x test")

	wrapper := generate(t, map[string]string{"build.gradle": "// spring-boot", "gradlew": "", ".java-version": "17.0.2"})
	assertEqual(t, wrapper.Plan.Versions["java"], "17")
	assertContains(t, wrapper.Containerfile, "./gradlew bootJar -x test --no-daemon")

	def := generate(t, map[string]string{"pom.xml": "<project/>"})
	assertEqual(t, def.Plan.Versions["java"], "21")
}

func TestDotnet(t *testing.T) {
	web := generate(t, map[string]string{
		"src/Web/Web.csproj": `<Project Sdk="Microsoft.NET.Sdk.Web"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>`,
	})
	assertEqual(t, web.Plan.Framework, "aspnetcore")
	assertEqual(t, web.Plan.Port, 8080)
	assertContains(t, web.Containerfile, "ARG DOTNET_VERSION=8.0", `dotnet publish "src/Web/Web.csproj"`,
		"USER $APP_UID", `CMD ["dotnet", "Web.dll"]`)

	console := generate(t, map[string]string{
		"Tool.fsproj": `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><AssemblyName>tool</AssemblyName></PropertyGroup></Project>`,
	})
	assertSlice(t, console.Plan.Cmd, "dotnet", "tool.dll")
	assertEqual(t, console.Plan.Versions["dotnet"], "10.0")
	assertContains(t, console.Containerfile, "mcr.microsoft.com/dotnet/runtime:${DOTNET_VERSION}")
	refuteContains(t, console.Containerfile, "EXPOSE")

	old := generate(t, map[string]string{"App.csproj": "<TargetFramework>net6.0</TargetFramework>"})
	refuteContains(t, old.Containerfile, "APP_UID")

	forced, _ := Scan(project(t, nil), Options{Runtime: "dotnet"})
	assertSlice(t, forced.Cmd, "dotnet", "app.dll")

	if _, err := Scan(project(t, map[string]string{"a/b/c/Deep.csproj": ""}), Options{}); err != ErrNoMatch {
		t.Errorf("deep project should not match: %v", err)
	}
}

func TestDeno(t *testing.T) {
	task := generate(t, map[string]string{"deno.json": `{"tasks": {"start": "deno run -A main.ts"}}`, "main.ts": ""})
	assertSlice(t, task.Plan.Cmd, "deno", "task", "start")
	assertContains(t, task.Containerfile, "RUN deno install --entrypoint main.ts")

	fresh := generate(t, map[string]string{
		"deno.json": `{"imports": {"$fresh/": "https://deno.land/x/fresh/"}, "tasks": {"build": "x"}}`, "main.ts": "",
	})
	assertEqual(t, fresh.Plan.Framework, "fresh")
	assertSlice(t, fresh.Plan.Cmd, "deno", "run", "-A", "main.ts")
	assertContains(t, fresh.Containerfile, "RUN deno task build")

	empty := generate(t, map[string]string{"deno.lock": "{}", ".tool-versions": "deno 2.1.4\n"})
	assertEqual(t, len(empty.Plan.Cmd), 0)
	assertEqual(t, empty.Plan.Versions["deno"], "2.1.4")
	assertContains(t, empty.Containerfile, "RUN deno install\n")
}

func TestStatic(t *testing.T) {
	r := generate(t, map[string]string{"public/index.html": ""})
	assertEqual(t, r.Plan.Runtime, "static")
	assertContains(t, r.Containerfile, "COPY public /usr/share/nginx/html")

	forced, _ := Scan(project(t, nil), Options{Runtime: "static"})
	assertEqual(t, forced.Assigns["dir"], ".")
}
