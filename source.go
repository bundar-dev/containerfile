package containerfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Source is a read-only view of the project directory being scanned.
// All paths passed to its methods are relative to Dir.
type Source struct {
	Dir string
}

// NewSource returns a Source for dir, expanding a leading "~".
func NewSource(dir string) *Source {
	return &Source{Dir: expandHome(dir)}
}

func expandHome(dir string) string {
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, strings.TrimPrefix(dir, "~"))
		}
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

// Path returns the absolute path of rel inside the source dir.
func (s *Source) Path(rel string) string { return filepath.Join(s.Dir, rel) }

// File reports whether any of names is a regular file.
func (s *Source) File(names ...string) bool {
	for _, name := range names {
		if info, err := os.Stat(s.Path(name)); err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

// IsDir reports whether name is a directory.
func (s *Source) IsDir(name string) bool {
	info, err := os.Stat(s.Path(name))
	return err == nil && info.IsDir()
}

// FirstFile returns the first of names that exists as a file, or "".
func (s *Source) FirstFile(names ...string) string {
	for _, name := range names {
		if s.File(name) {
			return name
		}
	}
	return ""
}

var skipDirs = []string{"node_modules", "deps", "_build", ".git", ".venv", "venv",
	"site-packages", "vendor", "target", "dist", "build", "bin", "obj"}

// Glob returns sorted relative paths matching pattern, skipping dependency
// and build directories. Brace alternatives like "*.{a,b}" are supported.
func (s *Source) Glob(pattern string) []string {
	var out []string
	for _, p := range expandBraces(pattern) {
		matches, _ := filepath.Glob(s.Path(p))
		for _, m := range matches {
			rel, err := filepath.Rel(s.Dir, m)
			if err == nil && !inSkippedDir(rel) {
				out = append(out, filepath.ToSlash(rel))
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func expandBraces(pattern string) []string {
	open := strings.Index(pattern, "{")
	closing := strings.Index(pattern, "}")
	if open < 0 || closing < open {
		return []string{pattern}
	}
	var out []string
	for _, alt := range strings.Split(pattern[open+1:closing], ",") {
		out = append(out, expandBraces(pattern[:open]+alt+pattern[closing+1:])...)
	}
	return out
}

func inSkippedDir(rel string) bool {
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if slices.Contains(skipDirs, part) {
			return true
		}
	}
	return false
}

// Read returns the file contents with NUL bytes stripped, and whether the
// file could be read.
func (s *Source) Read(name string) (string, bool) {
	body, err := os.ReadFile(s.Path(name))
	if err != nil {
		return "", false
	}
	return strings.ReplaceAll(string(body), "\x00", ""), true
}

// ReadString returns the file contents, or "" when missing.
func (s *Source) ReadString(name string) string {
	body, _ := s.Read(name)
	return body
}

// Contains reports whether any of files contains substr.
func (s *Source) Contains(substr string, files ...string) bool {
	for _, f := range files {
		if body, ok := s.Read(f); ok && strings.Contains(body, substr) {
			return true
		}
	}
	return false
}

// Matches reports whether any of files matches re.
func (s *Source) Matches(re *regexp.Regexp, files ...string) bool {
	for _, f := range files {
		if body, ok := s.Read(f); ok && re.MatchString(body) {
			return true
		}
	}
	return false
}

// GlobContains reports whether any file matching pattern contains substr.
func (s *Source) GlobContains(pattern, substr string) bool {
	return s.Contains(substr, s.Glob(pattern)...)
}

// ReadJSON decodes a JSON object, returning nil when missing or invalid.
func (s *Source) ReadJSON(name string) map[string]any {
	body, ok := s.Read(name)
	if !ok {
		return nil
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		return nil
	}
	return data
}

// jsonMap returns data[key] as a map, or an empty map.
func jsonMap(data map[string]any, key string) map[string]any {
	if m, ok := data[key].(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// jsonString returns data[key] as a string, or "".
func jsonString(data map[string]any, key string) string {
	s, _ := data[key].(string)
	return s
}
