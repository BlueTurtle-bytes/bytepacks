// Package types defines every data structure in apexpack.
// All other packages import from here — nothing else imports from here.
// If you want to understand the shape of a profile YAML, read this file.
package types

// ============================================================================
// Profile — the language profile YAML schema
// ============================================================================

// Profile is the root structure of a profiles/*.yaml file.
// Each file describes one language: how to detect it, how to build it,
// what packages go in the image, and how to test it.
//
// Example file: profiles/golang.yaml
//
//	runtime: golang
//	detect:
//	  files:
//	    - go.mod
//	build:
//	  command: "go build -o /home/build/output/app ."
type Profile struct {
	// Runtime is the language identifier (e.g. "golang", "java", "python").
	// Must be unique across all profiles in the profiles/ directory.
	Runtime string `yaml:"runtime"`

	// Version is the profile schema version. Currently always "1".
	Version string `yaml:"version,omitempty"`

	// Description is a human-readable summary shown by `apexpack profiles`.
	Description string `yaml:"description,omitempty"`

	// Detect defines how apexpack recognises this language in a source directory.
	Detect DetectConfig `yaml:"detect"`

	// Build defines how to compile the project (feeds into melange.yaml).
	Build BuildConfig `yaml:"build"`

	// Image defines what goes into the final OCI image (feeds into apko.yaml).
	Image ImageConfig `yaml:"image"`

}

// ============================================================================
// DetectConfig — how to recognise a language in a source directory
// ============================================================================

// DetectConfig defines the rules apexpack uses to decide if a source directory
// matches this profile. Rules are evaluated in order; all file checks are ANDed.
type DetectConfig struct {
	// Files lists exact filenames that must exist in the source directory.
	// If ANY file in this list is found, detection considers it a match.
	// Example: ["go.mod"] means "go.mod must exist"
	Files []string `yaml:"files,omitempty"`

	// Patterns lists glob patterns matched against filenames in the source directory.
	// Useful for languages where the filename is variable (e.g. "*.csproj", "*.sln").
	// If ANY pattern matches at least one file, detection considers it a match.
	// Example: ["*.csproj", "*.sln"]
	Patterns []string `yaml:"patterns,omitempty"`

	// Content checks are optional extra rules applied after Files or Patterns match.
	// Each rule reads a file and checks if it contains a string.
	// A matching rule boosts confidence.
	Content []ContentRule `yaml:"content,omitempty"`

	// Confidence is the score returned when this profile matches (0.0–1.0).
	// Higher = more specific match. Default: 0.8
	Confidence float64 `yaml:"confidence,omitempty"`
}


// ContentRule checks whether a file contains a specific string pattern.
// Used to distinguish e.g. a Maven project (pom.xml + "spring-boot") from
// a plain Java project (pom.xml only), or to identify the specific framework
// in use (Express vs Fastify vs NestJS in a Node project).
type ContentRule struct {
	// File is the filename to read (relative to the source directory).
	File string `yaml:"file"`

	// Contains is a plain string the file must contain to match.
	Contains string `yaml:"contains"`

	// BoostConfidence is added to the base confidence when this rule matches.
	BoostConfidence float64 `yaml:"boost-confidence,omitempty"`

	// Framework sets DetectResult.Framework when this rule matches.
	// The first matching content rule with a non-empty Framework wins.
	// Example: framework: nextjs
	Framework string `yaml:"framework,omitempty"`
}

// ============================================================================
// BuildConfig — compile instructions (maps to melange.yaml)
// ============================================================================

// BuildConfig defines how to compile the project into an artifact.
// This maps directly to the melange.yaml pipeline section.
type BuildConfig struct {
	// Dependencies are APK packages installed in the build environment.
	// These are available during compilation but NOT in the final image.
	// Example: ["go", "build-base", "git"]
	Dependencies []string `yaml:"dependencies"`

	// Command is the shell command that compiles the project.
	// The working directory is the source root.
	// Output artifacts must be written to /home/build/output/
	// Example: "go build -o /home/build/output/app ."
	Command string `yaml:"command"`

	// Env sets environment variables during the build.
	// Example: {CGO_ENABLED: "0", GOFLAGS: "-trimpath"}
	Env map[string]string `yaml:"env,omitempty"`

	// Frameworks maps a detected framework name to build overrides.
	// When detection identifies a framework (e.g. "quarkus", "spring-boot"),
	// the matching entry's Command replaces the default, and its Env is merged on top.
	// Keys must match DetectResult.Framework values exactly.
	Frameworks map[string]FrameworkBuildOverride `yaml:"frameworks,omitempty"`
}

// FrameworkBuildOverride lets a specific framework replace or extend the default build.
// Only set the fields you need to override — unset fields fall back to Build defaults.
type FrameworkBuildOverride struct {
	// Dependencies replaces Build.Dependencies when set.
	Dependencies []string `yaml:"dependencies,omitempty"`

	// Command replaces Build.Command when set.
	Command string `yaml:"command,omitempty"`

	// Env is merged on top of Build.Env. Framework values win on key conflicts.
	Env map[string]string `yaml:"env,omitempty"`
}

// ============================================================================
// ImageConfig — final image assembly (maps to apko.yaml)
// ============================================================================

// ImageConfig defines what goes into the final OCI image.
// This maps directly to the apko.yaml contents section.
type ImageConfig struct {
	// Packages are APK packages installed into the final image.
	// Keep this minimal — every package is a potential CVE surface.
	// Example: ["ca-certificates-bundle", "wolfi-baselayout"]
	Packages []string `yaml:"packages"`

	// Entrypoint is the ENTRYPOINT of the final image.
	// Example: "/usr/bin/app"
	Entrypoint string `yaml:"entrypoint"`

	// Cmd is the default CMD arguments passed to the entrypoint.
	// Example: ["--port", "8080"]
	Cmd []string `yaml:"cmd,omitempty"`

	// Env are environment variables baked into the final image.
	// Example: {APP_ENV: "production"}
	Env map[string]string `yaml:"env,omitempty"`

	// RunAs is the UID the container process runs as.
	// 65532 is the standard nonroot UID used by Wolfi/Chainguard images.
	// Never use 0 (root) in production images.
	RunAs uint32 `yaml:"run-as,omitempty"`

	// Ports are informational — they are added as OCI annotations.
	Ports []string `yaml:"ports,omitempty"`
}

// ============================================================================
// ProjectConfig — per-project apexpack.yaml override file
// ============================================================================

// ProjectConfig is an optional file placed in the project root (apexpack.yaml).
// It overrides or extends the detected language profile for this specific project.
//
// Example apexpack.yaml in a Go project that uses SQLite:
//
//	runtime: golang          # optional — overrides auto-detection
//	image:
//	  packages:
//	    - ca-certificates-bundle
//	    - sqlite-libs         # project-specific runtime dependency
//	build:
//	  env:
//	    CGO_ENABLED: "1"      # this project requires CGO
type ProjectConfig struct {
	// Runtime overrides auto-detection (e.g. "golang", "java").
	Runtime string `yaml:"runtime,omitempty"`

	// Image overrides are merged on top of the profile's image config.
	// Packages are appended, not replaced. Env vars are merged (project wins).
	Image *ProjectImageOverride `yaml:"image,omitempty"`

	// Build overrides are merged on top of the profile's build config.
	Build *ProjectBuildOverride `yaml:"build,omitempty"`
}

// ProjectImageOverride lets a project add extra runtime packages or env vars.
type ProjectImageOverride struct {
	// Packages are appended to the profile's image.packages list.
	Packages []string `yaml:"packages,omitempty"`

	// Env vars are merged with the profile's image.env (project values win on conflict).
	Env map[string]string `yaml:"env,omitempty"`

	// Entrypoint overrides the profile's entrypoint.
	Entrypoint string `yaml:"entrypoint,omitempty"`
}

// ProjectBuildOverride lets a project add extra build deps or env vars.
type ProjectBuildOverride struct {
	// Dependencies are appended to the profile's build.dependencies list.
	Dependencies []string `yaml:"dependencies,omitempty"`

	// Env vars are merged with the profile's build.env (project values win on conflict).
	Env map[string]string `yaml:"env,omitempty"`

	// Command fully replaces the profile's build.command if set.
	Command string `yaml:"command,omitempty"`
}

// ============================================================================
// DetectResult — output of the detection step
// ============================================================================

// DetectResult is returned by detect.Run() for each profile that matched.
// Multiple profiles can match the same directory (e.g. a Node+TypeScript project).
// Results are sorted by Confidence descending — highest confidence first.
type DetectResult struct {
	// Profile is the matched profile.
	Profile *Profile

	// Confidence is the match score (0.0–1.0).
	Confidence float64

	// MatchedFiles lists which detection files were found.
	MatchedFiles []string

	// MatchedContent lists which content rules triggered a boost.
	MatchedContent []string

	// Framework is the specific framework identified by a content rule.
	// Set by the first ContentRule whose Framework field is non-empty and matches.
	// Examples: "express", "nextjs", "fastify", "nestjs", "spring-boot",
	//           "django", "fastapi", "flask", "gin", "grpc"
	// Empty string means no specific framework was identified.
	Framework string
}

// ============================================================================
// MelangeConfig — Go struct representation of melange.yaml
// ============================================================================
//
// melange.yaml tells melange how to compile source code into an APK package.
// We define it as a struct so yaml.Marshal always produces valid, correctly-
// indented YAML — no manual string formatting required.
//
// Full melange schema reference:
// https://github.com/chainguard-dev/melange/blob/main/docs/md/melange_build.md

type MelangeConfig struct {
	Package     MelangePackage     `yaml:"package"`
	Environment MelangeEnvironment `yaml:"environment"`
	Pipeline    []MelangePipeline  `yaml:"pipeline"`
}

type MelangePackage struct {
	Name        string             `yaml:"name"`
	Version     string             `yaml:"version"`
	Epoch       int                `yaml:"epoch"`
	Description string             `yaml:"description,omitempty"`
	Copyright   []MelangeCopyright `yaml:"copyright,omitempty"`
}

type MelangeCopyright struct {
	License string `yaml:"license"`
}

type MelangeEnvironment struct {
	Contents MelangeContents   `yaml:"contents"`
	Env      map[string]string `yaml:"environment,omitempty"`
}

type MelangeContents struct {
	Keyring      []string `yaml:"keyring,omitempty"`
	Repositories []string `yaml:"repositories"`
	Packages     []string `yaml:"packages"`
}

// MelangePipeline is one step in the melange build pipeline.
// Use Runs for a shell script, Uses for a named melange action (e.g. go/build).
type MelangePipeline struct {
	// Runs is a shell script executed inside the build sandbox.
	// Multi-line scripts are written as a YAML block scalar (|).
	Runs string `yaml:"runs,omitempty"`

	// Uses is a named melange action (e.g. "go/build", "python/pip-install").
	// When set, With provides the action's parameters.
	Uses string `yaml:"uses,omitempty"`

	// With are the named parameters for a Uses action.
	With map[string]string `yaml:"with,omitempty"`
}

// ============================================================================
// ApkoConfig — Go struct representation of apko.yaml
// ============================================================================
//
// apko.yaml tells apko which APK packages to install into the final OCI image
// and how to configure the container (entrypoint, user, env vars).
//
// Full apko schema reference:
// https://github.com/chainguard-dev/apko/blob/main/docs/apko_file.md

type ApkoConfig struct {
	Contents    ApkoContents      `yaml:"contents"`
	Entrypoint  ApkoEntrypoint    `yaml:"entrypoint"`
	Cmd         string            `yaml:"cmd,omitempty"`
	Accounts    ApkoAccounts      `yaml:"accounts"`
	Environment map[string]string `yaml:"environment,omitempty"`
}

type ApkoContents struct {
	Keyring      []string `yaml:"keyring,omitempty"`
	Repositories []string `yaml:"repositories"`
	Packages     []string `yaml:"packages"`
}

type ApkoEntrypoint struct {
	// Command is the ENTRYPOINT string (e.g. "/usr/bin/app").
	Command string `yaml:"command"`
}

type ApkoAccounts struct {
	RunAs  string      `yaml:"run-as"`
	Users  []ApkoUser  `yaml:"users"`
	Groups []ApkoGroup `yaml:"groups"`
}

type ApkoUser struct {
	Username string `yaml:"username"`
	UID      uint32 `yaml:"uid"`
	GID      uint32 `yaml:"gid"`
}

type ApkoGroup struct {
	Groupname string `yaml:"groupname"`
	GID       uint32 `yaml:"gid"`
}

// ============================================================================
// BuildPlan — intermediate representation passed to the build step
// ============================================================================

// BuildPlan holds the generated melange and apko configs before they are
// written to disk. The build step receives this and writes the YAML files.
type BuildPlan struct {
	// ProjectName is derived from the source directory name or go.mod module path.
	ProjectName string

	// Version is set from a flag or defaults to "0.0.1".
	Version string

	// Profile is the matched language profile.
	Profile *Profile

	// Framework is the detected framework (e.g. "spring-boot", "quarkus").
	// Empty if detection found no specific framework.
	Framework string

	// Melange is the structured melange.yaml config — marshalled to YAML on write.
	Melange MelangeConfig

	// Apko is the structured apko.yaml config — marshalled to YAML on write.
	Apko ApkoConfig
}
