package containerfile

import (
	"path"
	"regexp"
	"strings"
)

var (
	pythonMarkers = []string{"requirements.txt", "pyproject.toml", "Pipfile", "poetry.lock", "uv.lock", "setup.py"}
	pythonIgnore  = []string{"__pycache__", "*.py[cod]", ".venv", "venv", ".pytest_cache", ".mypy_cache",
		".ruff_cache", ".tox", "*.egg-info", "htmlcov", ".coverage"}
	pythonCandidates = []string{"main.py", "app.py", "wsgi.py", "asgi.py", "server.py", "api.py",
		"streamlit_app.py", "app/main.py", "app/__init__.py", "app/app.py", "src/main.py", "src/app.py"}

	// Dependencies with native parts: build-stage and runtime packages.
	pythonNativeDeps = []struct {
		dep            string
		build, runtime []string
	}{
		{"psycopg2", []string{"libpq-dev"}, []string{"libpq5"}},
		{"mysqlclient", []string{"default-libmysqlclient-dev", "pkg-config"}, []string{"libmariadb3"}},
	}

	fastAPIRe       = regexp.MustCompile(`(\w+)\s*=\s*FastAPI\(`)
	flaskRe         = regexp.MustCompile(`(\w+)\s*=\s*Flask\(`)
	streamlitRe     = regexp.MustCompile(`(?m)^\s*import streamlit`)
	projectScriptRe = regexp.MustCompile(`(?ms)^\[project\.scripts\]\s*\n(.*?)(?:^\[|\z)`)
	scriptNameRe    = regexp.MustCompile(`(?m)^"?([\w.-]+)"?\s*=`)
)

// pythonScanner handles uv, Poetry, Pipenv, pip and plain pyproject
// projects, detecting Django, FastAPI, Flask and Streamlit. Dependencies go
// into /app/.venv in a build stage copied into a slim runtime image.
type pythonScanner struct{}

func (pythonScanner) Name() string { return "python" }

func (pythonScanner) Render(p *Plan) (string, error) { return renderTemplate("python", p) }

type pythonApp struct {
	framework string
	port      int
	cmd       []string
}

func (pythonScanner) Detect(src *Source, forced bool) (*Plan, bool) {
	if !src.File(pythonMarkers...) && !forced {
		return nil, false
	}
	var texts []string
	for _, f := range []string{"requirements.txt", "pyproject.toml", "Pipfile"} {
		texts = append(texts, src.ReadString(f))
	}
	depsText := strings.ToLower(strings.Join(texts, "\n"))
	has := func(name string) bool { return pythonHasDep(depsText, name) }
	app := pythonFramework(src, has)

	plan := newPlan("python")
	plan.Framework = app.framework
	plan.PackageManager = pythonPackageManager(src)
	plan.Port = app.port
	plan.Cmd = app.cmd
	plan.Ignore = pythonIgnore
	if app.port != 0 {
		plan.Env["PORT"] = itoa(app.port)
	}
	plan.SetVersion("python", pythonVersion(src))

	build, runtime := []string{"build-essential"}, []string{}
	for _, native := range pythonNativeDeps {
		if has(native.dep) {
			build = append(build, native.build...)
			runtime = append(runtime, native.runtime...)
		}
	}
	plan.Assigns = map[string]any{
		"buildPackages":   build,
		"runtimePackages": runtime,
		"pipfileLock":     src.File("Pipfile.lock"),
		"collectstatic":   app.framework == "django" && src.GlobContains("*/settings*.py", "STATIC_ROOT"),
	}

	plan.Note(len(app.cmd) == 0, "Could not find an entrypoint; set CMD manually.")
	plan.Note(app.framework == "django" && !has("gunicorn") && !has("uvicorn"),
		"Django without gunicorn/uvicorn uses `runserver`; add gunicorn for production.")
	return plan, true
}

func pythonHasDep(text, name string) bool {
	re := regexp.MustCompile(`(?m)(^|[\s"'\[,])` + regexp.QuoteMeta(name) + `($|[\s"'=<>~!\[;,])`)
	return re.MatchString(text)
}

func pythonPackageManager(src *Source) string {
	switch {
	case src.File("uv.lock"):
		return "uv"
	case src.File("poetry.lock") || src.Contains("[tool.poetry]", "pyproject.toml"):
		return "poetry"
	case src.File("Pipfile"):
		return "pipenv"
	case src.File("requirements.txt"):
		return "pip"
	default:
		return "pip-pyproject"
	}
}

var (
	requiresPythonRe = regexp.MustCompile(`requires-python\s*=\s*"([^"]+)"`)
	poetryPythonRe   = regexp.MustCompile(`(?m)^python\s*=\s*"([^"]+)"`)
	pipfilePythonRe  = regexp.MustCompile(`python_version\s*=\s*"([^"]+)"`)
)

func pythonVersion(src *Source) Version {
	v := FirstVersion(
		func() Version { return FileVersion(src, ".python-version") },
		func() Version { return ToolVersion(src, "python") },
		func() Version { return FileVersion(src, "runtime.txt") },
		func() Version { return FileMatch(src, "pyproject.toml", requiresPythonRe) },
		func() Version { return FileMatch(src, "pyproject.toml", poetryPythonRe) },
		func() Version { return FileMatch(src, "Pipfile", pipfilePythonRe) },
	)
	if version := Extract(v.Value); version != "" {
		return Version{Take(version, 2), v.Source}
	}
	return Default("3.13")
}

func pythonFramework(src *Source, has func(string) bool) pythonApp {
	switch {
	case src.File("manage.py") && has("django"):
		return pythonDjango(src, has)
	case has("fastapi"):
		module, variable, file := pythonFindApp(src, fastAPIRe, "main")
		cmd := []string{"fastapi", "run", file, "--port", "8000"}
		if has("uvicorn") || has("fastapi[standard]") {
			cmd = []string{"uvicorn", module + ":" + variable, "--host", "0.0.0.0", "--port", "8000"}
		}
		return pythonApp{"fastapi", 8000, cmd}
	case has("flask"):
		module, variable, _ := pythonFindApp(src, flaskRe, "app")
		cmd := []string{"flask", "--app", module + ":" + variable, "run", "--host", "0.0.0.0", "--port", "8000"}
		if has("gunicorn") {
			cmd = []string{"gunicorn", "--bind", "0.0.0.0:8000", module + ":" + variable}
		}
		return pythonApp{"flask", 8000, cmd}
	case has("streamlit"):
		file := "app.py"
		for _, f := range pythonCandidateFiles(src) {
			if src.Matches(streamlitRe, f) {
				file = f
				break
			}
		}
		return pythonApp{"streamlit", 8501, []string{"streamlit", "run", file, "--server.port", "8501", "--server.address", "0.0.0.0"}}
	default:
		if file := src.FirstFile("main.py", "app.py", "server.py", "run.py", "__main__.py"); file != "" {
			return pythonApp{cmd: []string{"python", file}}
		}
		return pythonApp{cmd: pythonProjectScript(src)}
	}
}

func pythonDjango(src *Source, has func(string) bool) pythonApp {
	project := "config"
	if wsgi := src.Glob("*/wsgi.py"); len(wsgi) > 0 {
		project = path.Dir(wsgi[0])
	}
	cmd := []string{"python", "manage.py", "runserver", "0.0.0.0:8000"}
	switch {
	case has("uvicorn") && src.File(project+"/asgi.py"):
		cmd = []string{"uvicorn", project + ".asgi:application", "--host", "0.0.0.0", "--port", "8000"}
	case has("gunicorn"):
		cmd = []string{"gunicorn", "--bind", "0.0.0.0:8000", "--workers", "2", project + ".wsgi"}
	}
	return pythonApp{"django", 8000, cmd}
}

// pythonProjectScript returns the first [project.scripts] entry.
func pythonProjectScript(src *Source) []string {
	if m := projectScriptRe.FindStringSubmatch(src.ReadString("pyproject.toml")); m != nil {
		if s := scriptNameRe.FindStringSubmatch(m[1]); s != nil {
			return []string{s[1]}
		}
	}
	return []string{}
}

func pythonCandidateFiles(src *Source) []string {
	files := existing(src, pythonCandidates)
	for _, f := range src.Glob("*.py") {
		if !containsString(files, f) {
			files = append(files, f)
		}
	}
	return files
}

// pythonFindApp finds `var = Framework(` and returns the module path, the
// variable and the file.
func pythonFindApp(src *Source, re *regexp.Regexp, fallback string) (module, variable, file string) {
	for _, f := range pythonCandidateFiles(src) {
		if m := re.FindStringSubmatch(src.ReadString(f)); m != nil {
			module = strings.TrimSuffix(strings.TrimSuffix(f, "/__init__.py"), ".py")
			return strings.ReplaceAll(module, "/", "."), m[1], f
		}
	}
	return fallback, "app", fallback + ".py"
}
