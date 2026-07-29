// Package templates loads the built-in melange config templates.
package templates

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed maven/maven_settings.xml
var mavenSettingsTemplate string

//go:embed nuget/nuget_config.xml
var nugetConfigTemplate string

//go:embed gradle/gradle_init.gradle
var gradleInitTemplate string

// LoadMavenTemplate returns the Maven settings.xml template.
func LoadMavenTemplate(name, profilesDir string) (string, error) {
	if name == "" {
		return mavenSettingsTemplate, nil
	}
	custom := filepath.Join(profilesDir, "templates", "maven", name+".xml")
	data, err := os.ReadFile(custom)
	if err != nil {
		return "", fmt.Errorf("maven settings template %q not found: place it at %s", name, custom)
	}
	return string(data), nil
}

// LoadNuGetTemplate returns the NuGet.Config template.
func LoadNuGetTemplate(name, profilesDir string) (string, error) {
	if name == "" {
		return nugetConfigTemplate, nil
	}
	custom := filepath.Join(profilesDir, "templates", "nuget", name+".xml")
	data, err := os.ReadFile(custom)
	if err != nil {
		return "", fmt.Errorf("nuget config template %q not found: place it at %s", name, custom)
	}
	return string(data), nil
}

// LoadGradleTemplate returns the Gradle init script template.
func LoadGradleTemplate(name, profilesDir string) (string, error) {
	if name == "" {
		return gradleInitTemplate, nil
	}
	custom := filepath.Join(profilesDir, "templates", "gradle", name+".gradle")
	data, err := os.ReadFile(custom)
	if err != nil {
		return "", fmt.Errorf("gradle init script template %q not found: place it at %s", name, custom)
	}
	return string(data), nil
}
