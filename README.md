# containerfile

Generates a `Containerfile` (or `Dockerfile`) and a matching ignore file
by scanning a project directory. It ships as a single static binary, a
container image, and a Go library.

## Container image

You don't need anything installed except podman or docker. The tool runs
fully offline, and with `--userns=keep-id` the files it writes are owned
by you:

```sh
podman run --rm \
  --network=none \
  --userns=keep-id \
  -v "$SOURCE:/workspace:Z" \
  -w /workspace \
  ghcr.io/bundar-dev/containerfile:1.2.3
```

Arguments after the image name go to the CLI, e.g. `--out Containerfile` or
`--plan`. With Docker, use `--user "$(id -u):$(id -g)"` instead of
`--userns=keep-id`. The image is a single static binary on
`distroless/static`.

## Binary

```sh
go install github.com/bundar-dev/containerfile/cmd/containerfile@latest
# or download a release archive (linux/darwin/windows, amd64/arm64)
```

```sh
containerfile                                  # ./Dockerfile + ./.dockerignore
containerfile --dir ~/app --out Containerfile  # ~/app/Containerfile + .containerignore
containerfile --dir ~/app --out stdout         # print only
containerfile --plan                           # show what was detected
containerfile --plan --json                    # ... as JSON
containerfile --runtime node --port 8080       # skip detection, override port
```

| Option | Description |
| --- | --- |
| `-d`, `--dir` | Project directory to scan. Default: current directory. |
| `-o`, `--out` | `Dockerfile` (default), `Containerfile`, `stdout`, or a path relative to `--dir`. |
| `--runtime` | Force a scanner instead of auto-detecting. Run `--list` to see the names. |
| `--port` | Override the detected port. |
| `-f`, `--force` | Overwrite existing files. Without it, existing files are left alone. |
| `--no-ignore` | Don't write `.dockerignore` / `.containerignore`. |
| `--plan` | Print what was detected and write nothing. Add `--json` for JSON output. |
| `--list` | List scanners in detection order. |
| `-h`, `--help`, `-v`, `--version` | Show usage or the version. |

## Library

```go
import "github.com/bundar-dev/containerfile"

result, err := containerfile.Generate("~/app", containerfile.Options{})
fmt.Println(result.Plan.Framework) // "phoenix"
fmt.Println(result.Plan.Notes)     // ["Set SECRET_KEY_BASE ..."]
os.WriteFile("Containerfile", []byte(result.Containerfile), 0o644)

// or step by step
plan, err := containerfile.Scan(dir, containerfile.Options{Port: 8080})
plan.Cmd = []string{"/app/bin/server"}
text, err := containerfile.Render(plan)
```

To add support for another stack, implement `containerfile.Scanner`
(`Name`, `Detect`, `Render`) and pass
`Options{Scanners: append([]containerfile.Scanner{mine}, containerfile.Scanners()...)}`.

## Supported projects

Scanners are tried in this order:

| Scanner | Detected by | Frameworks and tools |
| --- | --- | --- |
| `elixir` | `mix.exs` | Phoenix (assets, `rel/`, runtime config), umbrella apps, plain `mix release` |
| `ruby` | `Gemfile`, `config.ru` | Rails (bootsnap, assets, jsbundling, pg/mysql/sqlite), Rack |
| `php` | `composer.json`, `index.php`, `artisan` | Laravel (Vite assets), `ext-*` extensions, Apache on port 8080 |
| `python` | `requirements.txt`, `pyproject.toml`, `Pipfile`, lockfiles | Django, FastAPI, Flask, Streamlit; uv, Poetry, Pipenv, pip |
| `deno` | `deno.json(c)`, `deno.lock` | Fresh; `start` task or `main.ts` |
| `node` | `package.json` | Next.js (standalone), Nuxt, Remix, React Router, SvelteKit, Astro, NestJS, Vite/CRA SPA on nginx; npm, pnpm, yarn 1/berry, bun |
| `go` | `go.mod` | `cmd/*` layouts, cgo detection; distroless runtime |
| `rust` | `Cargo.toml` | cargo-chef caching; axum, actix, rocket, warp, poem |
| `java` | `pom.xml`, `build.gradle[.kts]` | Spring Boot; mvnw/gradlew |
| `dotnet` | `*.csproj`, `*.fsproj` (up to two levels deep) | ASP.NET Core, console apps |
| `static` | `index.html`, `public/index.html` | nginx on port 8080 |

Versions are read from the first source that has one:

1. `.tool-versions`, `mise.toml`
2. Per-tool files: `.nvmrc`, `.python-version`, `.ruby-version`, and so on
3. Manifests: `engines.node`, `requires-python`, `go.mod`, `composer.json`, `Gemfile.lock`, and so on
4. A pinned default

Each version is written into the Containerfile as an `ARG`, so you can
change it at build time with `--build-arg`. The final stage of each image
runs as a non-root user.

## Development

```sh
go test -race -cover ./...
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@latest ./...
go build -o bin/containerfile ./cmd/containerfile
```

Templates live in `templates/*.tmpl` and are embedded into the binary. A
line holding only a control action (`{{if}}`, `{{end}}`, ...) leaves no
blank line behind.
