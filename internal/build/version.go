package build

import (
	"fmt"
	"strings"
)

// defaultLangVersions maps runtimes to their built-in fallback version.
// Used when no version is detected from source files.
var defaultLangVersions = map[string]string{
	"java":   "21",
	"node":   "20",
	"python": "3.12",
	"dotnet": "8",
}

// supportedLangVersions lists the versions available in the Wolfi APK repo.
// Versions not in this set will cause an apk solve failure at build time.
var supportedLangVersions = map[string][]string{
	"dotnet": {"8", "9", "10"},
	"node":   {"18", "20", "22"},
}

// warnFallbackRuntimes are runtimes where an unsupported detected version produces
// a warning and falls back to the default rather than a hard error. This is appropriate
// for Node where EOL versions (.nvmrc says "14") are common but the app usually runs
// fine on the current LTS. dotnet is NOT in this set because version mismatches there
// are a hard compile-time failure.
var warnFallbackRuntimes = map[string]bool{
	"node": true,
}

// langVersionToken returns the substitution token for a given runtime.
func langVersionToken(runtime string) string {
	switch runtime {
	case "java":
		return "{JAVA_VERSION}"
	case "node":
		return "{NODE_VERSION}"
	case "python":
		return "{PYTHON_VERSION}"
	case "dotnet":
		return "{DOTNET_VERSION}"
	case "golang":
		return "{GO_VERSION}"
	}
	return ""
}

// resolveVersion returns the effective language version for the given runtime.
// Falls back to the built-in default when nothing was detected.
// For runtimes in warnFallbackRuntimes, if the detected version is not in
// supportedLangVersions it warns and returns the default instead of passing
// through an unsupported version that will fail at apk solve time.
func resolveVersion(runtime, detected string) string {
	def := defaultLangVersions[runtime]
	if detected == "" {
		return def
	}
	if supported, ok := supportedLangVersions[runtime]; ok && warnFallbackRuntimes[runtime] {
		for _, v := range supported {
			if v == detected {
				return detected
			}
		}
		fmt.Printf("  → WARN: %s version %q is not available in Wolfi (supported: %s) — using %s instead\n",
			runtime, detected, strings.Join(supported, ", "), def)
		return def
	}
	return detected
}

// validateRuntimeVersion returns an error if the resolved version is not available
// in the Wolfi APK repository for the given runtime.
// Runtimes in warnFallbackRuntimes are skipped — resolveVersion already converts
// unsupported detected versions to the default before this is called.
func validateRuntimeVersion(runtime, version string) error {
	if warnFallbackRuntimes[runtime] {
		return nil
	}
	supported, ok := supportedLangVersions[runtime]
	if !ok {
		return nil // no constraint defined for this runtime
	}
	for _, v := range supported {
		if v == version {
			return nil
		}
	}
	return fmt.Errorf(
		"unsupported %s version %q: Wolfi only provides versions %s — "+
			"upgrade TargetFramework in your .csproj (or sdk.version in global.json) to a supported release",
		runtime, version, strings.Join(supported, ", "),
	)
}

// vsub replaces the language version token in s.
func vsub(s, token, version string) string {
	if token == "" || version == "" {
		return s
	}
	return strings.ReplaceAll(s, token, version)
}

// vsubSlice applies vsub to every element of a string slice, returning a new slice.
func vsubSlice(ss []string, token, version string) []string {
	if token == "" || version == "" {
		return ss
	}
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = vsub(s, token, version)
	}
	return out
}

// vsubMap applies vsub to every value in a map, returning a new map.
func vsubMap(m map[string]string, token, version string) map[string]string {
	if token == "" || version == "" || len(m) == 0 {
		return m
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = vsub(v, token, version)
	}
	return out
}
