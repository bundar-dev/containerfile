package containerfile

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var (
	// Extensions compiled into the official php images already.
	phpBuiltinExt = []string{"ctype", "curl", "date", "dom", "fileinfo", "filter", "ftp", "hash", "iconv", "json",
		"libxml", "mbstring", "mysqlnd", "openssl", "pcre", "pdo", "pdo_sqlite", "phar", "posix", "random",
		"readline", "reflection", "session", "simplexml", "sodium", "spl", "sqlite3", "standard", "tokenizer",
		"xml", "xmlreader", "xmlwriter", "zlib"}
	phpLaravelExt = []string{"bcmath", "intl", "opcache", "pdo_mysql", "pdo_pgsql", "zip"}
	phpIgnore     = []string{"/vendor", "/node_modules", "/storage/logs/*", "/storage/framework/cache/*",
		"/storage/framework/sessions/*", "/storage/framework/views/*", "/bootstrap/cache/*.php", "/public/build",
		"/public/hot", ".phpunit.result.cache"}
)

// phpScanner handles composer.json / index.php projects, with Laravel
// support. Composer runs in its own stage; the app runs on php:*-apache on
// port 8080 with extensions from `ext-*` requirements.
type phpScanner struct{}

func (phpScanner) Name() string { return "php" }

func (phpScanner) Render(p *Plan) (string, error) { return renderTemplate("php", p) }

func (phpScanner) Detect(src *Source, forced bool) (*Plan, bool) {
	if !src.File("composer.json", "index.php", "artisan") && !forced {
		return nil, false
	}
	composer := src.ReadJSON("composer.json")
	require := jsonMap(composer, "require")
	_, laravelDep := require["laravel/framework"]
	laravel := src.File("artisan") || laravelDep

	plan := newPlan("php")
	plan.Port = 8080
	plan.Cmd = []string{"apache2-foreground"}
	plan.Ignore = phpIgnore
	if composer != nil {
		plan.PackageManager = "composer"
	}
	if laravel {
		plan.Framework = "laravel"
		plan.Env["LOG_CHANNEL"] = "stderr"
	}
	plan.SetVersion("php", phpVersion(src, require))

	documentRoot := "/var/www/html"
	if laravel || src.File("public/index.php") {
		documentRoot = "/var/www/html/public"
	}
	_, assets := jsonMap(src.ReadJSON("package.json"), "scripts")["build"]
	assetsInstall := "npm install"
	if src.File("package-lock.json") {
		assetsInstall = "npm ci"
	}
	plan.Assigns = map[string]any{
		"composer":      composer != nil,
		"laravel":       laravel,
		"extensions":    phpExtensions(require, laravel),
		"documentRoot":  documentRoot,
		"assets":        assets,
		"assetsInstall": assetsInstall,
	}
	plan.Note(laravel, "Set APP_KEY (php artisan key:generate --show) and database settings at runtime.")
	return plan, true
}

func phpExtensions(require map[string]any, laravel bool) []string {
	var exts []string
	for key := range require {
		if ext, ok := strings.CutPrefix(key, "ext-"); ok {
			exts = append(exts, strings.ToLower(ext))
		}
	}
	if laravel {
		exts = append(exts, phpLaravelExt...)
	} else {
		exts = append(exts, "opcache")
	}
	exts = slices.DeleteFunc(exts, func(e string) bool { return slices.Contains(phpBuiltinExt, e) })
	slices.Sort(exts)
	return slices.Compact(exts)
}

func phpVersion(src *Source, require map[string]any) Version {
	v := FirstVersion(
		func() Version { return ToolVersion(src, "php") },
		func() Version { return Version{jsonString(require, "php"), "composer.json"} },
	)
	version := Extract(v.Value)
	if version == "" {
		v = Default("8.4")
		version = v.Value
	}
	selected := phpMinorVersion(version)
	packages, _ := src.ReadJSON("composer.lock")["packages"].([]any)
	for _, entry := range packages {
		pkg, _ := entry.(map[string]any)
		requirement := jsonString(jsonMap(pkg, "require"), "php")
		if needed, ok := phpRequiredMinor(requirement, selected); ok && needed > selected {
			selected = needed
			v.Source = "composer.lock"
		}
	}
	if v.Source == "composer.lock" {
		return Version{strconv.Itoa(selected/100) + "." + strconv.Itoa(selected%100), v.Source}
	}
	return Version{Take(version, 2), v.Source}
}

var phpConstraintRe = regexp.MustCompile(`^(>=|<=|>|<|\^|~|=)?(\d+)(?:\.(\d+|\*))?(?:\.(\d+|\*))?$`)

func phpMinorVersion(version string) int {
	parts := strings.Split(version, ".")
	major, _ := strconv.Atoi(parts[0])
	minor := 0
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	return major*100 + minor
}

// phpRequiredMinor finds the lowest PHP minor at or above selected that
// satisfies a lockfile constraint. The image tag selects the latest patch.
func phpRequiredMinor(requirement string, selected int) (int, bool) {
	best := 0
	for _, alternative := range strings.Split(requirement, "|") {
		candidate := selected
		parts := strings.Fields(strings.ReplaceAll(alternative, ",", " "))
		if len(parts) == 0 {
			continue
		}
		valid := true
		for _, part := range parts {
			match := phpConstraintRe.FindStringSubmatch(part)
			if match == nil {
				valid = false
				break
			}
			minimum := phpMinorVersion(match[2] + "." + match[3])
			if match[1] != "<" && match[1] != "<=" && minimum > candidate {
				candidate = minimum
			}
		}
		if !valid {
			continue
		}
		for _, part := range parts {
			match := phpConstraintRe.FindStringSubmatch(part)
			minimum := phpMinorVersion(match[2] + "." + match[3])
			switch match[1] {
			case "<":
				valid = candidate < minimum
			case "<=":
				valid = candidate <= minimum
			case "^":
				valid = candidate < (minimum/100+1)*100
			case "~":
				if match[4] != "" {
					valid = candidate < minimum+1
				} else {
					valid = candidate < (minimum/100+1)*100
				}
			case "", "=":
				valid = match[3] == "" && candidate/100 == minimum/100 ||
					match[3] == "*" && candidate/100 == minimum/100 ||
					match[4] == "*" && candidate == minimum ||
					match[3] != "" && match[3] != "*" && match[4] != "*" && candidate == minimum
			}
			if !valid {
				break
			}
		}
		if valid && (best == 0 || candidate < best) {
			best = candidate
		}
	}
	return best, best != 0
}
