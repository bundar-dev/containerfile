package containerfile

import (
	"regexp"
	"slices"
	"strings"
)

// MixFile is what the Elixir scanner needs from a mix.exs. The file is read
// with patterns, never evaluated.
type MixFile struct {
	App      string
	Deps     []string
	Umbrella bool
	Releases []string
	Aliases  []string
}

var (
	mixAppRe      = regexp.MustCompile(`\bapp:\s*:(\w+)`)
	mixDepRe      = regexp.MustCompile(`\{\s*:(\w+)\s*,`)
	mixReleasesRe = regexp.MustCompile(`\breleases:\s*\[\s*(\w+):`)
	mixAliasesRe  = regexp.MustCompile(`(?m)^([ \t]*)defp?\s+aliases\b[^\n]*\bdo\s*$`)
	mixAliasKeyRe = regexp.MustCompile(`(?m)(?:^|[\[,])\s*"?([\w.]+)"?:\s`)
)

// ParseMixFile extracts the app name, dependency names, umbrella flag,
// release names and alias names from mix.exs source.
func ParseMixFile(source string) MixFile {
	source = stripElixirComments(source)
	mix := MixFile{Umbrella: strings.Contains(source, "apps_path:")}

	if m := mixAppRe.FindStringSubmatch(source); m != nil {
		mix.App = m[1]
	}
	for _, m := range mixDepRe.FindAllStringSubmatch(source, -1) {
		if !slices.Contains(mix.Deps, m[1]) {
			mix.Deps = append(mix.Deps, m[1])
		}
	}
	if m := mixReleasesRe.FindStringSubmatch(source); m != nil {
		mix.Releases = []string{m[1]}
	}
	if body := aliasesBody(source); body != "" {
		for _, m := range mixAliasKeyRe.FindAllStringSubmatch(body, -1) {
			mix.Aliases = append(mix.Aliases, m[1])
		}
	}
	return mix
}

// aliasesBody returns the body of `defp aliases do ... end`, found by
// matching the indentation of the closing `end`.
func aliasesBody(source string) string {
	loc := mixAliasesRe.FindStringSubmatchIndex(source)
	if loc == nil {
		return ""
	}
	indent := source[loc[2]:loc[3]]
	rest := source[loc[1]:]
	endRe := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(indent) + `end\b`)
	if end := endRe.FindStringIndex(rest); end != nil {
		return rest[:end[0]]
	}
	return rest
}

var elixirCommentRe = regexp.MustCompile(`(?m)^\s*#.*$`)

func stripElixirComments(source string) string {
	return elixirCommentRe.ReplaceAllString(source, "")
}

// HasDep reports whether dep is a dependency.
func (m MixFile) HasDep(dep string) bool { return slices.Contains(m.Deps, dep) }

// HasAlias reports whether a mix alias is defined.
func (m MixFile) HasAlias(name string) bool { return slices.Contains(m.Aliases, name) }
