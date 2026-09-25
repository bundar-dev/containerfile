package containerfile

import (
	"path"
	"regexp"
	"strings"
)

var (
	assemblyNameRe    = regexp.MustCompile(`<AssemblyName>([^<]+)<`)
	targetFrameworkRe = regexp.MustCompile(`<TargetFrameworks?>net(\d+\.\d+)`)
)

// dotnetScanner handles *.csproj/*.fsproj projects. It publishes with the
// SDK image and runs on the ASP.NET (web) or .NET runtime image.
type dotnetScanner struct{}

func (dotnetScanner) Name() string { return "dotnet" }

func (dotnetScanner) Render(p *Plan) (string, error) { return renderTemplate("dotnet", p) }

func (dotnetScanner) Detect(src *Source, forced bool) (*Plan, bool) {
	project := dotnetProject(src)
	if project == "" && !forced {
		return nil, false
	}
	body := ""
	if project != "" {
		body = src.ReadString(project)
	}

	assembly := "app"
	if m := assemblyNameRe.FindStringSubmatch(body); m != nil {
		assembly = m[1]
	} else if project != "" {
		assembly = strings.TrimSuffix(path.Base(project), path.Ext(project))
	}
	version := MatchVersion(body, targetFrameworkRe, project)
	if !version.Found() {
		version = Default("10.0")
	}
	if project == "" {
		project = "app.csproj"
	}

	plan := newPlan("dotnet")
	plan.PackageManager = "nuget"
	plan.Cmd = []string{"dotnet", assembly + ".dll"}
	plan.Ignore = []string{"**/bin", "**/obj"}
	plan.SetVersion("dotnet", version)
	runtimeImage := "runtime"
	if strings.Contains(body, "Microsoft.NET.Sdk.Web") {
		plan.Framework = "aspnetcore"
		plan.Port = 8080
		plan.Env["ASPNETCORE_HTTP_PORTS"] = "8080"
		runtimeImage = "aspnet"
	}
	plan.Assigns = map[string]any{
		"project":      project,
		"runtimeImage": runtimeImage,
		"appUser":      Major(version.Value) >= 8,
	}
	return plan, true
}

// dotnetProject finds a project file at most two directories deep (never
// the whole tree), preferring shallow paths and web projects.
func dotnetProject(src *Source) string {
	var files []string
	for _, depth := range []string{"*", "*/*", "*/*/*"} {
		files = append(files, src.Glob(depth+".{csproj,fsproj}")...)
	}
	for _, f := range files {
		if src.Contains("Microsoft.NET.Sdk.Web", f) {
			return f
		}
	}
	if len(files) > 0 {
		return files[0]
	}
	return ""
}
