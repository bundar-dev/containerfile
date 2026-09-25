package containerfile

import (
	"regexp"
	"strings"
)

var (
	javaVersionRes = []*regexp.Regexp{
		regexp.MustCompile(`<java\.version>\s*(\d+)`),
		regexp.MustCompile(`<maven\.compiler\.release>\s*(\d+)`),
		regexp.MustCompile(`<maven\.compiler\.source>\s*(?:1\.)?(\d+)`),
		regexp.MustCompile(`JavaLanguageVersion\.of\(\s*(\d+)`),
		regexp.MustCompile(`JavaVersion\.VERSION_(?:1_)?(\d+)`),
		regexp.MustCompile(`sourceCompatibility\s*=\s*['"]?(?:1\.)?(\d+)`),
	}
	javaMajorRe = regexp.MustCompile(`(?:^|\D)(?:1\.)?(\d{1,2})(?:\D|$)`)
)

// javaScanner handles Maven and Gradle projects, including Spring Boot. It
// uses the project wrapper when present and runs the jar on a Temurin JRE.
type javaScanner struct{}

func (javaScanner) Name() string { return "java" }

func (javaScanner) Render(p *Plan) (string, error) { return renderTemplate("java", p) }

func (javaScanner) Detect(src *Source, forced bool) (*Plan, bool) {
	if !src.File("pom.xml", "build.gradle", "build.gradle.kts") && !forced {
		return nil, false
	}
	tool, wrapperFile, jarDir := "gradle", "gradlew", "build/libs"
	build := src.ReadString("build.gradle") + "\n" + src.ReadString("build.gradle.kts")
	if src.File("pom.xml") {
		tool, wrapperFile, jarDir = "maven", "mvnw", "target"
		build = src.ReadString("pom.xml")
	}
	spring := strings.Contains(build, "spring-boot")
	wrapper := src.File(wrapperFile)

	plan := newPlan("java")
	plan.PackageManager = tool
	plan.Port = 8080
	plan.Env["PORT"] = "8080"
	plan.Cmd = []string{"java", "-jar", "/app/app.jar"}
	plan.Ignore = []string{"/target", "/build", "/.gradle", "/out", "*.class"}
	if spring {
		plan.Framework = "spring-boot"
	}
	plan.Assigns = map[string]any{
		"builderImage": javaBuilderImage(tool, wrapper),
		"buildCmd":     javaBuildCmd(tool, wrapper, spring),
		"jarDir":       jarDir,
	}
	plan.SetVersion("java", javaVersion(src, build))
	plan.Note(!spring, "Assumes the build produces a runnable (fat) jar.")
	return plan, true
}

func javaBuilderImage(tool string, wrapper bool) string {
	switch {
	case wrapper:
		return "docker.io/library/eclipse-temurin:${JAVA_VERSION}-jdk"
	case tool == "maven":
		return "docker.io/library/maven:3-eclipse-temurin-${JAVA_VERSION}"
	default:
		return "docker.io/library/gradle:jdk${JAVA_VERSION}"
	}
}

func javaBuildCmd(tool string, wrapper, spring bool) string {
	if tool == "maven" {
		if wrapper {
			return "chmod +x mvnw && ./mvnw -B package -DskipTests"
		}
		return "mvn -B package -DskipTests"
	}
	task := "build"
	if spring {
		task = "bootJar"
	}
	if wrapper {
		return "chmod +x gradlew && ./gradlew " + task + " -x test --no-daemon"
	}
	return "gradle " + task + " -x test --no-daemon"
}

func javaVersion(src *Source, build string) Version {
	v := FirstVersion(
		func() Version { return FileVersion(src, ".java-version") },
		func() Version { return ToolVersion(src, "java") },
		func() Version {
			for _, re := range javaVersionRes {
				if v := MatchVersion(build, re, "build file"); v.Found() {
					return v
				}
			}
			return Version{}
		},
	)
	if m := javaMajorRe.FindStringSubmatch(v.Value); m != nil {
		return Version{m[1], v.Source}
	}
	return Default("21")
}
