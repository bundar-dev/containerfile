package containerfile

import (
	"embed"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"text/template"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

// controlLineRe matches lines holding only a control action. Such lines
// are joined with the next one so they leave no blank line behind, while
// output actions on their own line (like {{env .env}}) keep their newline.
var controlLineRe = regexp.MustCompile(`(?m)^[ \t]*(\{\{-?\s*(?:if|else|end|range|with)\b[^}]*\}\})[ \t]*\n`)

// Preprocess removes the line breaks after control-only template lines.
func Preprocess(source string) string {
	return controlLineRe.ReplaceAllString(source, "$1")
}

var templates = loadTemplates()

func loadTemplates() *template.Template {
	root := template.New("").Funcs(template.FuncMap{
		"exec":       Exec,
		"env":        Env,
		"apt":        AptInstall,
		"nginxStage": NginxStage,
		"join":       strings.Join,
		"q":          Quote,
		"list":       func(items ...string) []string { return items },
	})
	entries, err := templateFS.ReadDir("templates")
	if err != nil {
		panic(err)
	}
	for _, entry := range entries {
		body, err := templateFS.ReadFile("templates/" + entry.Name())
		if err != nil {
			panic(err)
		}
		name := strings.TrimSuffix(entry.Name(), ".tmpl")
		template.Must(root.New(name).Option("missingkey=error").Parse(Preprocess(string(body))))
	}
	return root
}

// renderTemplate executes the embedded template `name` with the plan.
func renderTemplate(name string, plan *Plan) (string, error) {
	var b strings.Builder
	if err := templates.ExecuteTemplate(&b, name, plan.templateData()); err != nil {
		return "", fmt.Errorf("render %s: %w", name, err)
	}
	return b.String(), nil
}

// Quote returns s as a double-quoted, escaped string.
func Quote(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

// Exec renders an exec-form JSON array for CMD/ENTRYPOINT.
func Exec(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = Quote(a)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// Env renders an ENV instruction, one sorted variable per line. An empty
// map renders nothing.
func Env(vars map[string]string) string {
	if len(vars) == 0 {
		return ""
	}
	var lines []string
	for _, k := range slices.Sorted(maps.Keys(vars)) {
		lines = append(lines, k+"="+Quote(vars[k]))
	}
	return "ENV " + strings.Join(lines, " \\\n    ")
}

// AptInstall renders a Debian package install; no packages renders nothing.
func AptInstall(packages []string) string {
	if len(packages) == 0 {
		return ""
	}
	pkgs := slices.Clone(packages)
	slices.Sort(pkgs)
	pkgs = slices.Compact(pkgs)
	return "RUN apt-get update -qq && \\\n" +
		"    apt-get install -y --no-install-recommends " + strings.Join(pkgs, " ") + " && \\\n" +
		"    rm -rf /var/lib/apt/lists/*"
}

// NginxStage renders a final stage serving static files from `from` with
// unprivileged nginx on port 8080, falling back to /index.html for
// client-side routing. stage is the build stage to copy from, or "" to copy
// from the build context.
func NginxStage(stage, from string) string {
	cp := "COPY " + from + " /usr/share/nginx/html"
	if stage != "" {
		cp = "COPY --from=" + stage + " " + from + " /usr/share/nginx/html"
	}
	return `FROM docker.io/nginxinc/nginx-unprivileged:stable-alpine

# serve files, falling back to index.html for client-side routing
USER root
RUN printf 'server {\n    listen 8080;\n    root /usr/share/nginx/html;\n    location / {\n        try_files $uri $uri/ /index.html;\n    }\n}\n' \
    > /etc/nginx/conf.d/default.conf
USER nginx

` + cp + `

EXPOSE 8080`
}
