package build

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed templates/maven/maven_settings.xml
var mavenSettingsTemplate string

//go:embed templates/nuget/nuget_config.xml
var nugetConfigTemplate string

//go:embed templates/gradle/gradle_init.gradle
var gradleInitTemplate string

// loadMavenTemplate returns the Maven settings.xml template.
// When name is empty the built-in maven_settings.xml is returned.
// When name is non-empty a user-supplied file is expected at
// <profilesDir>/templates/maven/<name>.xml — an error is returned if it is absent.
func loadMavenTemplate(name, profilesDir string) (string, error) {
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

// loadNuGetTemplate returns the NuGet.Config template.
// When name is empty the built-in nuget_config.xml is returned.
// When name is non-empty a user-supplied file is expected at
// <profilesDir>/templates/nuget/<name>.xml — an error is returned if it is absent.
func loadNuGetTemplate(name, profilesDir string) (string, error) {
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

// loadGradleTemplate returns the Gradle init script template.
// When name is empty the built-in gradle_init.gradle is returned.
// When name is non-empty a user-supplied file is expected at
// <profilesDir>/templates/gradle/<name>.gradle — an error is returned if it is absent.
func loadGradleTemplate(name, profilesDir string) (string, error) {
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
