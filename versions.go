package containerfile

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Version is a detected version and the file it came from.
type Version struct {
	Value  string
	Source string
}

// Default returns a Version marked as a built-in default.
func Default(value string) Version { return Version{Value: value, Source: "default"} }

// Found reports whether the version was detected.
func (v Version) Found() bool { return v.Value != "" }

var miseFiles = []string{"mise.toml", ".mise.toml", ".config/mise.toml", "mise.local.toml"}

// ToolVersion looks up a tool in .tool-versions or mise config. names are
// accepted aliases, e.g. "node", "nodejs".
func ToolVersion(src *Source, names ...string) Version {
	if v := fromToolVersions(src, names); v.Found() {
		return v
	}
	return fromMise(src, names)
}

// FileVersion reads the first meaningful line of the first existing file,
// without a leading "v".
func FileVersion(src *Source, names ...string) Version {
	for _, name := range names {
		body, ok := src.Read(name)
		if !ok {
			continue
		}
		for _, line := range strings.Split(body, "\n") {
			if line = strings.TrimSpace(stripComment(line)); line != "" {
				return Version{strings.TrimPrefix(line, "v"), name}
			}
		}
	}
	return Version{}
}

// MatchVersion returns the first capture group of re in text, labelled
// with source.
func MatchVersion(text string, re *regexp.Regexp, source string) Version {
	if m := re.FindStringSubmatch(text); m != nil {
		return Version{m[1], source}
	}
	return Version{}
}

// FileMatch is MatchVersion on a file's contents, labelled with the file.
func FileMatch(src *Source, file string, re *regexp.Regexp) Version {
	return MatchVersion(src.ReadString(file), re, file)
}

// FirstVersion returns the first detected version from the lookups.
func FirstVersion(lookups ...func() Version) Version {
	for _, lookup := range lookups {
		if v := lookup(); v.Found() {
			return v
		}
	}
	return Version{}
}

var versionRe = regexp.MustCompile(`\d+(?:\.\d+){0,2}`)

// Extract returns the leading numeric version in s, e.g. "^8.2" -> "8.2".
func Extract(s string) string { return versionRe.FindString(s) }

// Take truncates a version to n components: Take("1.17.3", 2) == "1.17".
func Take(version string, n int) string {
	parts := strings.Split(version, ".")
	return strings.Join(parts[:min(n, len(parts))], ".")
}

// Major returns the leading integer of a version, or -1.
func Major(version string) int {
	end := strings.IndexFunc(version, func(r rune) bool { return r < '0' || r > '9' })
	if end < 0 {
		end = len(version)
	}
	n, err := strconv.Atoi(version[:end])
	if err != nil {
		return -1
	}
	return n
}

func fromToolVersions(src *Source, names []string) Version {
	for _, line := range strings.Split(src.ReadString(".tool-versions"), "\n") {
		fields := strings.Fields(stripComment(line))
		if len(fields) >= 2 && slices.Contains(names, fields[0]) {
			return Version{fields[1], ".tool-versions"}
		}
	}
	return Version{}
}

func fromMise(src *Source, names []string) Version {
	for _, file := range miseFiles {
		body, ok := src.Read(file)
		if !ok {
			continue
		}
		tools := tomlTable(body, "tools")
		for _, name := range names {
			if v := firstString(tools[name]); v != "" {
				return Version{v, file}
			}
		}
	}
	return Version{}
}

var (
	tomlHeaderRe = regexp.MustCompile(`^\[\[?([^\]]+)\]\]?\s*$`)
	tomlPairRe   = regexp.MustCompile(`^"?([\w@/:.-]+)"?\s*=\s*(.+)$`)
	quotedRe     = regexp.MustCompile(`"([^"]+)"|'([^']+)'`)
)

// tomlTable is a minimal TOML reader returning the raw `key = value` pairs
// of one table. For arrays of tables ([[bin]]) the first entry wins.
func tomlTable(body, table string) map[string]string {
	pairs := map[string]string{}
	current := ""
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(stripComment(line))
		if m := tomlHeaderRe.FindStringSubmatch(line); m != nil {
			current = m[1]
		} else if m := tomlPairRe.FindStringSubmatch(line); m != nil && current == table {
			if _, seen := pairs[m[1]]; !seen {
				pairs[m[1]] = m[2]
			}
		}
	}
	return pairs
}

// firstString returns the first quoted string in a TOML value, so
// "x", ["x", ...] and { version = "x" } all yield "x".
func firstString(value string) string {
	m := quotedRe.FindStringSubmatch(value)
	if m == nil {
		return ""
	}
	return m[1] + m[2]
}

func stripComment(line string) string {
	before, _, _ := strings.Cut(line, "#")
	return before
}
