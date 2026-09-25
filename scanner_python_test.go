package containerfile

import "testing"

func TestPythonFastAPI(t *testing.T) {
	r := generate(t, map[string]string{
		"requirements.txt": "fastapi[standard]>=0.110\npsycopg2==2.9\n",
		"app/main.py":      "from fastapi import FastAPI\napi = FastAPI()\n", ".python-version": "3.12.4\n",
	})
	assertEqual(t, r.Plan.Framework, "fastapi")
	assertEqual(t, r.Plan.PackageManager, "pip")
	assertEqual(t, r.Plan.Versions["python"], "3.12")
	assertContains(t, r.Containerfile, "ARG PYTHON_VERSION=3.12", "libpq-dev", "libpq5",
		".venv/bin/pip install --no-cache-dir -r requirements.txt",
		`CMD ["uvicorn", "app.main:api", "--host", "0.0.0.0", "--port", "8000"]`)

	cli := generate(t, map[string]string{"requirements.txt": "fastapi\n", "api.py": "app = FastAPI()"})
	assertSlice(t, cli.Plan.Cmd, "fastapi", "run", "api.py", "--port", "8000")
}

func TestPythonDjango(t *testing.T) {
	r := generate(t, map[string]string{
		"pyproject.toml": "[project]\nname = \"d\"\nrequires-python = \">=3.11\"\ndependencies = [\"django>=5\", \"gunicorn\"]\n",
		"uv.lock":        "", "manage.py": "", "mysite/wsgi.py": "", "mysite/settings.py": "STATIC_ROOT = BASE_DIR / 'static'",
	})
	assertEqual(t, r.Plan.PackageManager, "uv")
	assertContains(t, r.Containerfile, "ARG PYTHON_VERSION=3.11", "RUN uv sync --frozen --no-dev --no-install-project",
		"manage.py collectstatic --noinput",
		`CMD ["gunicorn", "--bind", "0.0.0.0:8000", "--workers", "2", "mysite.wsgi"]`)

	asgi := generate(t, map[string]string{"requirements.txt": "django\nuvicorn\n", "manage.py": "", "proj/wsgi.py": "", "proj/asgi.py": ""})
	assertSlice(t, asgi.Plan.Cmd, "uvicorn", "proj.asgi:application", "--host", "0.0.0.0", "--port", "8000")

	dev := generate(t, map[string]string{"requirements.txt": "django\n", "manage.py": ""})
	assertSlice(t, dev.Plan.Cmd, "python", "manage.py", "runserver", "0.0.0.0:8000")
	if !hasNote(dev.Plan, "gunicorn") {
		t.Error("expected gunicorn note")
	}
}

func TestPythonFlask(t *testing.T) {
	r := generate(t, map[string]string{
		"pyproject.toml": "[tool.poetry]\nname = \"f\"\n\n[tool.poetry.dependencies]\npython = \"^3.10\"\nflask = \"^3\"\n",
		"poetry.lock":    "", "app.py": "from flask import Flask\napp = Flask(__name__)\n",
	})
	assertEqual(t, r.Plan.PackageManager, "poetry")
	assertEqual(t, r.Plan.Versions["python"], "3.10")
	assertContains(t, r.Containerfile, "RUN poetry install --only main --no-root", `CMD ["flask", "--app", "app:app", "run"`)

	gunicorn := generate(t, map[string]string{
		"requirements.txt": "flask\ngunicorn\nmysqlclient\n", "app/__init__.py": "server = Flask(__name__)",
	})
	assertSlice(t, gunicorn.Plan.Cmd, "gunicorn", "--bind", "0.0.0.0:8000", "app:server")
	assertContains(t, gunicorn.Containerfile, "default-libmysqlclient-dev", "libmariadb3")
}

func TestPythonStreamlitPipenv(t *testing.T) {
	r := generate(t, map[string]string{
		"Pipfile":      "[packages]\nstreamlit = \"*\"\n\n[requires]\npython_version = \"3.11\"\n",
		"Pipfile.lock": "{}", "dashboard.py": "import streamlit as st\n",
	})
	assertEqual(t, r.Plan.Framework, "streamlit")
	assertEqual(t, r.Plan.PackageManager, "pipenv")
	assertEqual(t, r.Plan.Port, 8501)
	assertEqual(t, r.Plan.Versions["python"], "3.11")
	assertEqual(t, r.Plan.Cmd[2], "dashboard.py")
	assertContains(t, r.Containerfile, "RUN pipenv install --deploy")
}

func TestPythonGeneric(t *testing.T) {
	r := generate(t, map[string]string{"requirements.txt": "requests\n", "main.py": "print(1)"})
	assertSlice(t, r.Plan.Cmd, "python", "main.py")
	assertEqual(t, r.Plan.Versions["python"], "3.13")
	refuteContains(t, r.Containerfile, "EXPOSE")

	script := generate(t, map[string]string{
		"pyproject.toml": "[project]\nname = \"tool\"\n\n[project.scripts]\nmytool = \"tool.cli:main\"\n\n[build-system]\n",
	})
	assertEqual(t, script.Plan.PackageManager, "pip-pyproject")
	assertSlice(t, script.Plan.Cmd, "mytool")
	assertContains(t, script.Containerfile, "RUN .venv/bin/pip install --no-cache-dir .")

	none := generate(t, map[string]string{"setup.py": ""})
	assertEqual(t, len(none.Plan.Cmd), 0)
	refuteContains(t, none.Containerfile, "CMD")
}
