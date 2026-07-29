// Package helpers provides pure utility functions shared across build subpackages.
package helpers

import (
	"fmt"
	"strings"
)

var defaultLangVersions = map[string]string{
	"java":   "21",
	"node":   "20",
	"python": "3.12",
	"dotnet": "8",
}

var supportedLangVersions = map[string][]string{
	"dotnet": {"8", "9", "10"},
	"node":   {"18", "20", "22"},
}

var warnFallbackRuntimes = map[string]bool{
	"node": true,
}

// LangVersionToken returns the substitution token for a given runtime.
func LangVersionToken(runtime string) string {
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

// ResolveVersion returns the effective language version for the given runtime.
func ResolveVersion(runtime, detected string) string {
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

// ValidateRuntimeVersion returns an error if the resolved version is not available in Wolfi.
func ValidateRuntimeVersion(runtime, version string) error {
	if warnFallbackRuntimes[runtime] {
		return nil
	}
	supported, ok := supportedLangVersions[runtime]
	if !ok {
		return nil
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

// Vsub replaces the language version token in s.
func Vsub(s, token, version string) string {
	if token == "" || version == "" {
		return s
	}
	return strings.ReplaceAll(s, token, version)
}

// VsubSlice applies Vsub to every element of a string slice.
func VsubSlice(ss []string, token, version string) []string {
	if token == "" || version == "" {
		return ss
	}
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = Vsub(s, token, version)
	}
	return out
}

// VsubMap applies Vsub to every value in a map.
func VsubMap(m map[string]string, token, version string) map[string]string {
	if token == "" || version == "" || len(m) == 0 {
		return m
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = Vsub(v, token, version)
	}
	return out
}
