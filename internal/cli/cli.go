// Package cli implements the containerfile command line.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/bundar-dev/containerfile"
)

// Usage is printed by --help.
const Usage = `Usage: containerfile [options]

Scans a project directory and generates a Containerfile/Dockerfile
plus a matching ignore file.

Options:
  -d, --dir DIR        project directory to scan (default: current directory)
  -o, --out OUT        Dockerfile (default), Containerfile, stdout, or a path
                       relative to --dir
      --runtime NAME   skip detection and use this scanner (see --list)
      --port PORT      override the detected port
  -f, --force          overwrite existing files
      --no-ignore      don't write .dockerignore / .containerignore
      --plan           print what was detected, without writing anything
      --json           with --plan, print the plan as JSON
      --list           list available scanners in detection order
  -h, --help           show this help
  -v, --version        show version

The ignore file is written into --dir: .containerignore when --out is
named Containerfile, otherwise .dockerignore.
`

type options struct {
	dir, out, runtime                 string
	port                              int
	force, noIgnore, plan, json, list bool
	help, version                     bool
}

// Run executes the CLI and returns the process exit code.
func Run(args []string, version string, stdout, stderr io.Writer) int {
	if err := run(args, version, stdout, stderr); err != nil {
		fmt.Fprintln(stderr, "error: "+err.Error())
		return 1
	}
	return 0
}

func run(args []string, version string, stdout, stderr io.Writer) error {
	opts, err := parse(args)
	if err != nil {
		return err
	}
	switch {
	case opts.help:
		fmt.Fprint(stdout, Usage)
		return nil
	case opts.version:
		fmt.Fprintln(stdout, "containerfile "+version)
		return nil
	case opts.list:
		fmt.Fprintln(stdout, strings.Join(containerfile.ScannerNames(nil), "\n"))
		return nil
	}

	dir := opts.dir
	if dir == "" {
		if dir, err = os.Getwd(); err != nil {
			return err
		}
	}
	result, err := containerfile.Generate(dir, containerfile.Options{Runtime: opts.runtime, Port: opts.port})
	if err != nil {
		return describe(err, dir)
	}

	switch {
	case opts.plan:
		return printPlan(stdout, result.Plan, opts.json)
	case strings.EqualFold(opts.out, "stdout") || opts.out == "-":
		fmt.Fprint(stdout, result.Containerfile)
		report(stderr, result.Plan)
		return nil
	default:
		return writeFiles(stdout, result, containerfile.NewSource(dir).Dir, opts)
	}
}

func parse(args []string) (options, error) {
	var opts options
	fs := flag.NewFlagSet("containerfile", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	for _, name := range []string{"dir", "d"} {
		fs.StringVar(&opts.dir, name, "", "")
	}
	for _, name := range []string{"out", "o"} {
		fs.StringVar(&opts.out, name, "Dockerfile", "")
	}
	fs.StringVar(&opts.runtime, "runtime", "", "")
	fs.IntVar(&opts.port, "port", 0, "")
	for _, name := range []string{"force", "f"} {
		fs.BoolVar(&opts.force, name, false, "")
	}
	fs.BoolVar(&opts.noIgnore, "no-ignore", false, "")
	fs.BoolVar(&opts.plan, "plan", false, "")
	fs.BoolVar(&opts.json, "json", false, "")
	fs.BoolVar(&opts.list, "list", false, "")
	for _, name := range []string{"help", "h"} {
		fs.BoolVar(&opts.help, name, false, "")
	}
	for _, name := range []string{"version", "v"} {
		fs.BoolVar(&opts.version, name, false, "")
	}

	if err := fs.Parse(args); err != nil {
		return opts, fmt.Errorf("%w (see --help)", err)
	}
	if fs.NArg() > 0 {
		return opts, fmt.Errorf("unexpected argument(s): %s. Use --dir to pick a directory", strings.Join(fs.Args(), " "))
	}
	return opts, nil
}

func describe(err error, dir string) error {
	if errors.Is(err, containerfile.ErrNoMatch) {
		return fmt.Errorf("could not detect the project type in %s. Use --runtime to pick one (see --list)", dir)
	}
	return err
}

var containerfileNameRe = regexp.MustCompile(`(?i)^containerfile`)

func writeFiles(stdout io.Writer, result *containerfile.Result, dir string, opts options) error {
	out := opts.out
	if !filepath.IsAbs(out) {
		out = filepath.Join(dir, out)
	}
	if err := write(stdout, out, result.Containerfile, opts.force); err != nil {
		return err
	}
	if !opts.noIgnore {
		ignore := ".dockerignore"
		if containerfileNameRe.MatchString(filepath.Base(out)) {
			ignore = ".containerignore"
		}
		if err := write(stdout, filepath.Join(dir, ignore), result.Ignore, opts.force); err != nil {
			return err
		}
	}
	report(stdout, result.Plan)
	return nil
}

func write(stdout io.Writer, path, content string, force bool) error {
	existing, err := os.ReadFile(path)
	exists := err == nil
	switch {
	case exists && string(existing) == content:
		fmt.Fprintln(stdout, "* identical "+relative(path))
		return nil
	case exists && !force:
		fmt.Fprintln(stdout, "* skip "+relative(path)+" (exists, use --force to overwrite)")
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	verb := "create"
	if exists {
		verb = "overwrite"
	}
	fmt.Fprintln(stdout, "* "+verb+" "+relative(path))
	return nil
}

func relative(path string) string {
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, path); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return path
}

func report(w io.Writer, plan *containerfile.Plan) {
	fmt.Fprintln(w, "Detected "+plan.Summary())
	for _, note := range plan.Notes {
		fmt.Fprintln(w, "  note: "+note)
	}
}

func printPlan(w io.Writer, plan *containerfile.Plan, asJSON bool) error {
	if asJSON {
		return writeJSON(w, plan)
	}
	row := func(label, value string) { fmt.Fprintf(w, "%-17s%s\n", label+":", value) }
	orDash := func(s string) string {
		if s == "" {
			return "-"
		}
		return s
	}
	row("runtime", plan.Runtime)
	row("framework", orDash(plan.Framework))
	row("package manager", orDash(plan.PackageManager))
	for _, tool := range slices.Sorted(maps.Keys(plan.Versions)) {
		row(tool, plan.Versions[tool]+" ("+plan.Sources[tool]+")")
	}
	port := "-"
	if plan.Port != 0 {
		port = fmt.Sprint(plan.Port)
	}
	row("port", port)
	row("cmd", strings.Join(plan.Cmd, " "))
	for _, k := range slices.Sorted(maps.Keys(plan.Env)) {
		row("env", k+"="+plan.Env[k])
	}
	for _, note := range plan.Notes {
		row("note", note)
	}
	return nil
}
