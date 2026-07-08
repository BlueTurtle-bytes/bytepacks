package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SanitizeImageName lowercases s and replaces any character that is not
// a-z, 0-9, '.', '-', or '_' with '-', then trims leading/trailing '-' and '.'.
// This is applied to ProjectName before using it as the APK package name and
// tarball filename, so callers that need the actual filename should use it too.
func SanitizeImageName(s string) string {
	s = strings.ToLower(s)
	b := strings.Builder{}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-.")
}

// applyProjectTemplates replaces {APP_NAME} with the project name.
// Use {APP_NAME} in profile build commands and image entrypoints to produce
// binaries and entrypoints named after the project rather than a fixed "app".
func applyProjectTemplates(s, projectName string) string {
	return strings.ReplaceAll(s, "{APP_NAME}", projectName)
}

// readProcfileCmd parses the "web:" process from a Procfile and returns its command.
// Returns empty string if no Procfile exists or no web process is defined.
func readProcfileCmd(srcDir string) string {
	data, err := os.ReadFile(filepath.Join(srcDir, "Procfile"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "web:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "web:"))
		}
	}
	return ""
}

// cacheVolumeName returns a stable Docker volume name for a build cache path.
func cacheVolumeName(cachePath string) string {
	safe := strings.NewReplacer("/", "-", ".", "-", " ", "-").Replace(strings.TrimPrefix(cachePath, "/"))
	return "apexpack-cache-" + safe
}

// readNodeEntrypoint detects the Node.js entry point from package.json.
// Checks scripts.start (for "node <file>") then the main field.
// Returns an absolute /app/... path, or "" if nothing useful is found.
func readNodeEntrypoint(srcDir string) string {
	data, err := os.ReadFile(filepath.Join(srcDir, "package.json"))
	if err != nil {
		return ""
	}
	// scripts.start: "node server.js" or "node dist/index.js"
	if m := regexp.MustCompile(`"start"\s*:\s*"node\s+(\S+\.m?js)"`).FindSubmatch(data); len(m) == 2 {
		entry := string(m[1])
		if !strings.HasPrefix(entry, "/") {
			entry = "/app/" + entry
		}
		return entry
	}
	// main: "index.js" or "dist/server.js"
	if m := regexp.MustCompile(`"main"\s*:\s*"([^"]+\.m?js)"`).FindSubmatch(data); len(m) == 2 {
		entry := string(m[1])
		if !strings.HasPrefix(entry, "/") {
			entry = "/app/" + entry
		}
		return entry
	}
	return ""
}
