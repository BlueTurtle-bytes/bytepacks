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

//go:embed npm/npmrc.tmpl
var npmrcTemplate string

//go:embed npm/yarnrc.tmpl
var yarnrcTemplate string

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

// LoadNpmrcTemplate returns the .npmrc template.
// Falls back to the embedded default if no custom template is found at <profilesDir>/templates/npm/npmrc.tmpl.
func LoadNpmrcTemplate(profilesDir string) (string, error) {
	custom := filepath.Join(profilesDir, "templates", "npm", "npmrc.tmpl")
	data, err := os.ReadFile(custom)
	if err == nil {
		return string(data), nil
	}
	return npmrcTemplate, nil
}

// LoadYarnrcTemplate returns the .yarnrc.yml template.
// Falls back to the embedded default if no custom template is found at <profilesDir>/templates/npm/yarnrc.tmpl.
func LoadYarnrcTemplate(profilesDir string) (string, error) {
	custom := filepath.Join(profilesDir, "templates", "npm", "yarnrc.tmpl")
	data, err := os.ReadFile(custom)
	if err == nil {
		return string(data), nil
	}
	return yarnrcTemplate, nil
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
