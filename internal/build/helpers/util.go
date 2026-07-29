package helpers

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SanitizeImageName lowercases s and replaces invalid chars with '-'.
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

// ApplyProjectTemplates replaces {APP_NAME} with the project name.
func ApplyProjectTemplates(s, projectName string) string {
	return strings.ReplaceAll(s, "{APP_NAME}", projectName)
}

// ReadProcfileCmd parses the "web:" process from a Procfile.
func ReadProcfileCmd(srcDir string) string {
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

// CacheVolumeName returns a stable Docker volume name for a build cache path.
func CacheVolumeName(cachePath string) string {
	safe := strings.NewReplacer("/", "-", ".", "-", " ", "-").Replace(strings.TrimPrefix(cachePath, "/"))
	return "apexpack-cache-" + safe
}

// ReadNodeEntrypoint detects the Node.js entry point from package.json.
func ReadNodeEntrypoint(srcDir string) string {
	data, err := os.ReadFile(filepath.Join(srcDir, "package.json"))
	if err != nil {
		return ""
	}
	if m := regexp.MustCompile(`"start"\s*:\s*"node\s+(\S+\.m?js)"`).FindSubmatch(data); len(m) == 2 {
		entry := string(m[1])
		if !strings.HasPrefix(entry, "/") {
			entry = "/app/" + entry
		}
		return entry
	}
	if m := regexp.MustCompile(`"main"\s*:\s*"([^"]+\.m?js)"`).FindSubmatch(data); len(m) == 2 {
		entry := string(m[1])
		if !strings.HasPrefix(entry, "/") {
			entry = "/app/" + entry
		}
		return entry
	}
	return ""
}
