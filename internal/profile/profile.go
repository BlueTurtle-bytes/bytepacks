// Package profile loads and validates language profile YAML files
// from the profiles/ directory.
package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/apexpack/apexpack/internal/types"
)

// DefaultProfilesDir is the profiles directory relative to the working directory.
const DefaultProfilesDir = "profiles"

// Load reads a single profile YAML file from disk and returns a validated Profile.
//
// Usage:
//
//	p, err := profile.Load("profiles/golang.yaml")
func Load(path string) (*types.Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading profile %s: %w", path, err)
	}

	var p types.Profile
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parsing profile %s: %w", path, err)
	}

	if err := validate(&p, path); err != nil {
		return nil, err
	}

	return &p, nil
}

// LoadAll reads every .yaml file from dir and returns all valid profiles.
// Invalid profiles are skipped with a warning printed to stderr.
//
// Usage:
//
//	profiles, err := profile.LoadAll("profiles")
func LoadAll(dir string) ([]*types.Profile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// If the profiles directory doesn't exist, return a helpful error.
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("profiles directory %q not found — create it and add language profiles", dir)
		}
		return nil, fmt.Errorf("reading profiles directory %s: %w", dir, err)
	}

	var profiles []*types.Profile

	for _, entry := range entries {
		// Skip subdirectories and non-YAML files.
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		fullPath := filepath.Join(dir, name)
		p, err := Load(fullPath)
		if err != nil {
			// Print warning but continue — one bad profile shouldn't break all others.
			fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", fullPath, err)
			continue
		}

		profiles = append(profiles, p)
	}

	if len(profiles) == 0 {
		return nil, fmt.Errorf("no valid profiles found in %s", dir)
	}

	return profiles, nil
}

// GetByRuntime returns the first profile whose Runtime matches the given name.
// Returns nil if no match is found.
//
// Usage:
//
//	p := profile.GetByRuntime(profiles, "golang")
func GetByRuntime(profiles []*types.Profile, runtime string) *types.Profile {
	for _, p := range profiles {
		if p.Runtime == runtime {
			return p
		}
	}
	return nil
}

// LoadProjectConfig reads an optional apexpack.yaml from the project root.
// Returns nil (not an error) if no file exists — per-project config is optional.
func LoadProjectConfig(srcDir string) (*types.ProjectConfig, error) {
	path := filepath.Join(srcDir, "apexpack.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no project config is fine
		}
		return nil, fmt.Errorf("reading apexpack.yaml: %w", err)
	}
	var cfg types.ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing apexpack.yaml: %w", err)
	}
	return &cfg, nil
}

// MergeProjectConfig applies per-project overrides on top of a detected profile.
// The profile is not modified — a new Profile with merged values is returned.
func MergeProjectConfig(p *types.Profile, proj *types.ProjectConfig) *types.Profile {
	if proj == nil {
		return p
	}

	// Deep copy the profile so we don't mutate the original.
	merged := *p

	if proj.Build != nil {
		merged.Build.Dependencies = dedupe(append(merged.Build.Dependencies, proj.Build.Dependencies...))
		if proj.Build.Command != "" {
			merged.Build.Command = proj.Build.Command
		}
		if merged.Build.Env == nil {
			merged.Build.Env = make(map[string]string)
		}
		for k, v := range proj.Build.Env {
			merged.Build.Env[k] = v
		}
	}

	if proj.Image != nil {
		merged.Image.Packages = dedupe(append(merged.Image.Packages, proj.Image.Packages...))
		if proj.Image.Entrypoint != "" {
			merged.Image.Entrypoint = proj.Image.Entrypoint
		}
		if merged.Image.Env == nil {
			merged.Image.Env = make(map[string]string)
		}
		for k, v := range proj.Image.Env {
			merged.Image.Env[k] = v
		}
	}

	return &merged
}

func dedupe(items []string) []string {
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}

// validate checks that required fields are present in a loaded profile.
// Returns a descriptive error so the profile author knows exactly what's missing.
func validate(p *types.Profile, path string) error {
	if p.Runtime == "" {
		return fmt.Errorf("profile %s: missing required field 'runtime'", path)
	}
	if len(p.Detect.Files) == 0 && len(p.Detect.Patterns) == 0 {
		return fmt.Errorf("profile %s (%s): 'detect.files' or 'detect.patterns' must list at least one entry", path, p.Runtime)
	}
	if p.Build.Command == "" {
		return fmt.Errorf("profile %s (%s): missing required field 'build.command'", path, p.Runtime)
	}
	if p.Image.Entrypoint == "" {
		return fmt.Errorf("profile %s (%s): missing required field 'image.entrypoint'", path, p.Runtime)
	}
	return nil
}
