// Package detect matches language profiles against a source directory.
package detect

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/apexpack/apexpack/internal/types"
)

// Run checks every profile against srcDir and returns all matches,
// sorted by confidence (highest first).
//
// A profile matches when at least one file from detect.files or detect.patterns
// is found. Content rules boost the confidence score and set the framework field.
func Run(profiles []*types.Profile, srcDir string) []types.DetectResult {
	var results []types.DetectResult

	for _, p := range profiles {
		result, matched := matchProfile(p, srcDir)
		if matched {
			results = append(results, result)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Confidence > results[j].Confidence
	})

	return results
}

// Best returns the single highest-confidence match, or nil if nothing matched.
func Best(profiles []*types.Profile, srcDir string) *types.DetectResult {
	results := Run(profiles, srcDir)
	if len(results) == 0 {
		return nil
	}
	return &results[0]
}

// matchProfile checks whether a single profile matches srcDir.
func matchProfile(p *types.Profile, srcDir string) (types.DetectResult, bool) {
	var matchedFiles []string

	for _, filename := range p.Detect.Files {
		if fileExists(filepath.Join(srcDir, filename)) {
			matchedFiles = append(matchedFiles, filename)
		}
	}

	for _, pattern := range p.Detect.Patterns {
		matches, err := filepath.Glob(filepath.Join(srcDir, pattern))
		if err == nil && len(matches) > 0 {
			matchedFiles = append(matchedFiles, filepath.Base(matches[0]))
		}
	}

	if len(matchedFiles) == 0 {
		return types.DetectResult{}, false
	}

	confidence := p.Detect.Confidence
	if confidence == 0 {
		confidence = 0.8
	}

	var matchedContent []string
	var framework string
	for _, rule := range p.Detect.Content {
		if contentMatches(srcDir, rule.File, rule.Contains) {
			confidence += rule.BoostConfidence
			matchedContent = append(matchedContent, rule.File+":"+rule.Contains)
			if framework == "" && rule.Framework != "" {
				framework = rule.Framework
			}
		}
	}

	var packageManager string
	for _, rule := range p.Detect.PackageManagers {
		if fileExists(filepath.Join(srcDir, rule.File)) {
			packageManager = rule.Manager
			break
		}
	}

	if confidence > 1.0 {
		confidence = 1.0
	}

	return types.DetectResult{
		Profile:         p,
		Confidence:      confidence,
		MatchedFiles:    matchedFiles,
		MatchedContent:  matchedContent,
		Framework:       framework,
		PackageManager:  packageManager,
		LanguageVersion: detectLanguageVersion(p.Runtime, srcDir),
	}, true
}

// LanguageVersion detects the language version for the given runtime from source files.
// Exported so callers that skip auto-detection (e.g. --runtime flag) can still get the version.
func LanguageVersion(runtime, srcDir string) string {
	return detectLanguageVersion(runtime, srcDir)
}

// detectLanguageVersion dispatches to the per-runtime version detector.
func detectLanguageVersion(runtime, srcDir string) string {
	switch runtime {
	case "java":
		return detectJavaVersion(srcDir)
	case "node":
		return detectNodeVersion(srcDir)
	case "python":
		return detectPythonVersion(srcDir)
	case "dotnet":
		return detectDotnetVersion(srcDir)
	case "golang":
		return detectGoVersion(srcDir)
	}
	return ""
}

// ── Java ──────────────────────────────────────────────────────────────────────

func detectJavaVersion(srcDir string) string {
	// .java-version (jenv / SDKMAN-managed repos)
	if v := readFirstLine(filepath.Join(srcDir, ".java-version")); v != "" {
		if m := javaMajor(v); m != "" {
			return m
		}
	}
	// .sdkmanrc: java=17.0.1-tem
	if data, err := os.ReadFile(filepath.Join(srcDir, ".sdkmanrc")); err == nil {
		re := regexp.MustCompile(`(?m)^java=(\S+)`)
		if m := re.FindSubmatch(data); len(m) == 2 {
			if v := javaMajor(string(m[1])); v != "" {
				return v
			}
		}
	}
	// .tool-versions (asdf/mise): "java adoptopenjdk-17.0.1+12"
	if v := toolVersionsEntry(srcDir, "java"); v != "" {
		if m := regexp.MustCompile(`(\d+)`).FindStringSubmatch(v); len(m) == 2 {
			return m[1]
		}
	}
	// pom.xml property tags
	if v := javaMajor(extractXMLTag(srcDir, "pom.xml",
		"java.version", "maven.compiler.source", "maven.compiler.release", "maven.compiler.target",
	)); v != "" {
		return v
	}
	// build.gradle / build.gradle.kts
	for _, gf := range []string{"build.gradle", "build.gradle.kts"} {
		if v := gradleJavaVersion(filepath.Join(srcDir, gf)); v != "" {
			return v
		}
	}
	return ""
}

// javaMajor normalises "17.0.1", "17", "1.8", "adoptopenjdk-17.0.1+12" → "17", "17", "8", "17"
func javaMajor(v string) string {
	v = strings.TrimSpace(v)
	// Strip distribution prefix: "adoptopenjdk-17.0.1+12" → "17.0.1+12"
	if idx := strings.LastIndex(v, "-"); idx >= 0 {
		if tail := v[idx+1:]; len(tail) > 0 && tail[0] >= '0' && tail[0] <= '9' {
			v = tail
		}
	}
	// Old-style "1.8" → "8"
	if strings.HasPrefix(v, "1.") {
		if parts := strings.SplitN(v, ".", 3); len(parts) >= 2 {
			return parts[1]
		}
	}
	if m := regexp.MustCompile(`^(\d+)`).FindStringSubmatch(v); len(m) == 2 {
		return m[1]
	}
	return ""
}

// gradleJavaVersion extracts the Java version from a Gradle build file.
func gradleJavaVersion(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := string(data)
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`sourceCompatibility\s*=\s*['"]?(\d+)['"]?`),
		regexp.MustCompile(`sourceCompatibility\s*=\s*JavaVersion\.VERSION_(\d+)`),
		regexp.MustCompile(`JavaLanguageVersion\.of\((\d+)\)`),
		regexp.MustCompile(`targetCompatibility\s*=\s*['"]?(\d+)['"]?`),
	}
	for _, re := range patterns {
		if m := re.FindStringSubmatch(content); len(m) == 2 {
			return m[1]
		}
	}
	return ""
}

// ── Node.js ───────────────────────────────────────────────────────────────────

func detectNodeVersion(srcDir string) string {
	// .nvmrc or .node-version: "20", "v20.1.0"
	for _, f := range []string{".nvmrc", ".node-version"} {
		if v := readFirstLine(filepath.Join(srcDir, f)); v != "" {
			v = strings.TrimPrefix(v, "v")
			if m := regexp.MustCompile(`^(\d+)`).FindStringSubmatch(v); len(m) == 2 {
				return m[1]
			}
		}
	}
	// .tool-versions: "nodejs 20.1.0"
	if v := toolVersionsEntry(srcDir, "nodejs"); v != "" {
		if m := regexp.MustCompile(`^(\d+)`).FindStringSubmatch(v); len(m) == 2 {
			return m[1]
		}
	}
	// package.json engines.node: ">=20", "20.x", "^20.0.0"
	if data, err := os.ReadFile(filepath.Join(srcDir, "package.json")); err == nil {
		re := regexp.MustCompile(`"node"\s*:\s*"[><=^~v]*(\d+)`)
		if m := re.FindSubmatch(data); len(m) == 2 {
			return string(m[1])
		}
	}
	return ""
}

// ── Python ────────────────────────────────────────────────────────────────────

func detectPythonVersion(srcDir string) string {
	// .python-version: "3.11.0" → "3.11"
	if v := readFirstLine(filepath.Join(srcDir, ".python-version")); v != "" {
		if m := pythonMinorVersion(v); m != "" {
			return m
		}
	}
	// .tool-versions: "python 3.11.0"
	if v := toolVersionsEntry(srcDir, "python"); v != "" {
		if m := pythonMinorVersion(v); m != "" {
			return m
		}
	}
	// runtime.txt: "python-3.11.0" (Heroku / render.com)
	if v := readFirstLine(filepath.Join(srcDir, "runtime.txt")); v != "" {
		v = strings.TrimPrefix(v, "python-")
		if m := pythonMinorVersion(v); m != "" {
			return m
		}
	}
	// pyproject.toml: requires-python = ">=3.11"
	if data, err := os.ReadFile(filepath.Join(srcDir, "pyproject.toml")); err == nil {
		re := regexp.MustCompile(`requires-python\s*=\s*["'][><=!~]*3\.(\d+)`)
		if m := re.FindSubmatch(data); len(m) == 2 {
			return "3." + string(m[1])
		}
	}
	// setup.cfg: python_requires = >=3.11
	if data, err := os.ReadFile(filepath.Join(srcDir, "setup.cfg")); err == nil {
		re := regexp.MustCompile(`python_requires\s*=\s*[><=!~]*3\.(\d+)`)
		if m := re.FindSubmatch(data); len(m) == 2 {
			return "3." + string(m[1])
		}
	}
	return ""
}

// pythonMinorVersion extracts "3.11" from "3.11.0", "3.11", "python3.11".
func pythonMinorVersion(v string) string {
	if m := regexp.MustCompile(`3\.(\d+)`).FindStringSubmatch(v); len(m) == 2 {
		return "3." + m[1]
	}
	return ""
}

// ── .NET ──────────────────────────────────────────────────────────────────────

func detectDotnetVersion(srcDir string) string {
	// global.json: {"sdk":{"version":"8.0.100"}}
	if data, err := os.ReadFile(filepath.Join(srcDir, "global.json")); err == nil {
		re := regexp.MustCompile(`"version"\s*:\s*"(\d+)\.`)
		if m := re.FindSubmatch(data); len(m) == 2 {
			return string(m[1])
		}
	}
	// *.csproj in the project root or one level down
	for _, pattern := range []string{
		filepath.Join(srcDir, "*.csproj"),
		filepath.Join(srcDir, "*", "*.csproj"),
	} {
		matches, _ := filepath.Glob(pattern)
		for _, f := range matches {
			if data, err := os.ReadFile(f); err == nil {
				re := regexp.MustCompile(`<TargetFramework>net(\d+)`)
				if m := re.FindSubmatch(data); len(m) == 2 {
					return string(m[1])
				}
			}
		}
	}
	return ""
}

// ── Go ────────────────────────────────────────────────────────────────────────

func detectGoVersion(srcDir string) string {
	data, err := os.ReadFile(filepath.Join(srcDir, "go.mod"))
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`(?m)^go\s+(\d+\.\d+)`)
	if m := re.FindSubmatch(data); len(m) == 2 {
		return string(m[1])
	}
	return ""
}

// ── Shared helpers ────────────────────────────────────────────────────────────

// extractXMLTag searches a file for the first value of any of the given XML tag names.
// Uses simple string matching — no full XML parser required for these flat property tags.
func extractXMLTag(srcDir, filename string, tagNames ...string) string {
	data, err := os.ReadFile(filepath.Join(srcDir, filename))
	if err != nil {
		return ""
	}
	content := string(data)
	for _, tag := range tagNames {
		re := regexp.MustCompile(`<` + regexp.QuoteMeta(tag) + `>\s*([^<\s]+)\s*</` + regexp.QuoteMeta(tag) + `>`)
		if m := re.FindStringSubmatch(content); len(m) == 2 {
			return m[1]
		}
	}
	return ""
}

// toolVersionsEntry reads a .tool-versions file (asdf/mise) and returns the version
// string for the given tool name. Returns empty string if not found.
func toolVersionsEntry(srcDir, tool string) string {
	data, err := os.ReadFile(filepath.Join(srcDir, ".tool-versions"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, tool+" ") || strings.HasPrefix(line, tool+"\t") {
			return strings.TrimSpace(line[len(tool):])
		}
	}
	return ""
}

// readFirstLine reads the first non-empty line of a file, trimmed.
func readFirstLine(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func contentMatches(srcDir, filename, contains string) bool {
	data, err := os.ReadFile(filepath.Join(srcDir, filename))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), contains)
}
