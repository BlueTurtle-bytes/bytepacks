package build

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed templates/maven/default.xml
var mavenTemplateDefault string

//go:embed templates/maven/corporate.xml
var mavenTemplateCorporate string

// loadMavenTemplate returns the settings.xml template for the given name.
// Lookup order:
//  1. <profilesDir>/templates/maven/<name>.xml  (user-supplied, wins over built-ins)
//  2. Built-in embedded template (default / corporate)
func loadMavenTemplate(name, profilesDir string) (string, error) {
	if name == "" {
		name = "default"
	}
	if profilesDir != "" {
		custom := filepath.Join(profilesDir, "templates", "maven", name+".xml")
		if data, err := os.ReadFile(custom); err == nil {
			return string(data), nil
		}
	}
	switch name {
	case "default":
		return mavenTemplateDefault, nil
	case "corporate":
		return mavenTemplateCorporate, nil
	default:
		return "", fmt.Errorf("maven settings template %q not found (built-ins: default, corporate; custom: place at <profiles-dir>/templates/maven/%s.xml)", name, name)
	}
}

//go:embed templates/nuget/default.xml
var nugetTemplateDefault string

//go:embed templates/nuget/corporate.xml
var nugetTemplateCorporate string

//go:embed templates/gradle/default.gradle
var gradleTemplateDefault string

//go:embed templates/gradle/corporate.gradle
var gradleTemplateCorporate string

// loadNuGetTemplate returns the NuGet.Config template for the given name.
// Lookup order:
//  1. <profilesDir>/templates/nuget/<name>.xml  (user-supplied, wins over built-ins)
//  2. Built-in embedded template (default / corporate)
func loadNuGetTemplate(name, profilesDir string) (string, error) {
	if name == "" {
		name = "default"
	}
	if profilesDir != "" {
		custom := filepath.Join(profilesDir, "templates", "nuget", name+".xml")
		if data, err := os.ReadFile(custom); err == nil {
			return string(data), nil
		}
	}
	switch name {
	case "default":
		return nugetTemplateDefault, nil
	case "corporate":
		return nugetTemplateCorporate, nil
	default:
		return "", fmt.Errorf("nuget config template %q not found (built-ins: default, corporate; custom: place at <profiles-dir>/templates/nuget/%s.xml)", name, name)
	}
}

// loadGradleTemplate returns the Gradle init script template for the given name.
// Lookup order:
//  1. <profilesDir>/templates/gradle/<name>.gradle  (user-supplied, wins over built-ins)
//  2. Built-in embedded template (default / corporate)
func loadGradleTemplate(name, profilesDir string) (string, error) {
	if name == "" {
		name = "corporate"
	}
	if profilesDir != "" {
		custom := filepath.Join(profilesDir, "templates", "gradle", name+".gradle")
		if data, err := os.ReadFile(custom); err == nil {
			return string(data), nil
		}
	}
	switch name {
	case "default":
		return gradleTemplateDefault, nil
	case "corporate":
		return gradleTemplateCorporate, nil
	default:
		return "", fmt.Errorf("gradle init script template %q not found (built-ins: default, corporate; custom: place at <profiles-dir>/templates/gradle/%s.gradle)", name, name)
	}
}
