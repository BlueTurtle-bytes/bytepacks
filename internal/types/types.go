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

	// Scan configures CVE scanning and auto-patch defaults for this language profile.
	// Projects can override these via apexpack.yaml. In the Tekton pipeline the
	// AUTO_PATCH and PATCH_PERSIST params take precedence.
	Scan ScanConfig `yaml:"scan,omitempty"`
}

// ============================================================================
// ScanConfig — CVE scan and auto-patch settings
// ============================================================================

// ScanConfig controls CVE scanning behaviour. It is embedded in both Profile
// (as a language-level default) and ProjectConfig (as a per-project override).
type ScanConfig struct {
	// AutoPatch triggers 'apexpack patch --apply' when CVEs are found.
	// In the Tekton pipeline the AUTO_PATCH param takes precedence.
	AutoPatch bool `yaml:"auto-patch,omitempty"`

	// PatchPersist commits the patched profiles back to git after auto-patching.
	// Requires git credentials to be available. In Tekton, use PATCH_PERSIST param.
	PatchPersist bool `yaml:"patch-persist,omitempty"`
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

	// PackageManagers lists rules that identify the build tool by file existence.
	// The first matching rule sets DetectResult.PackageManager.
	// Example: pnpm-lock.yaml → "pnpm", bun.lockb → "bun"
	PackageManagers []PackageManagerRule `yaml:"package-managers,omitempty"`

	// Confidence is the score returned when this profile matches (0.0–1.0).
	// Higher = more specific match. Default: 0.8
	Confidence float64 `yaml:"confidence,omitempty"`
}

// PackageManagerRule identifies a build tool by the presence of a specific file.
type PackageManagerRule struct {
	// File is the filename whose existence signals this package manager.
	File string `yaml:"file"`

	// Manager is the identifier set on DetectResult.PackageManager when the file exists.
	// Example: "pnpm", "bun", "yarn", "yarn-berry", "uv", "poetry"
	Manager string `yaml:"manager"`
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

	// Caches lists absolute paths inside the build container to persist between runs.
	// On macOS (Docker), each path is mounted as a named Docker volume.
	// Example: ["/home/build/.npm", "/home/build/go/pkg/mod"]
	Caches []string `yaml:"caches,omitempty"`

	// Frameworks maps a detected framework name to build overrides.
	// When detection identifies a framework (e.g. "quarkus", "spring-boot"),
	// the matching entry's Command replaces the default, and its Env is merged on top.
	// Keys must match DetectResult.Framework values exactly.
	Frameworks map[string]FrameworkBuildOverride `yaml:"frameworks,omitempty"`

	// TLSCAEnv lists environment variable names that should be set to the path of
	// the corporate CA certificate inside the melange sandbox when --tls-extra-ca
	// is provided. Each runtime uses different variables (SSL_CERT_FILE for Go/Python,
	// NODE_EXTRA_CA_CERTS for Node.js, PIP_CERT for pip, etc.).
	TLSCAEnv []string `yaml:"tls_ca_env,omitempty"`

	// TLSCAPreStep is an optional shell script prepended to the melange pipeline
	// when --tls-extra-ca is provided. Use this for runtimes that require importing
	// the CA into their own certificate store (e.g. keytool for JVM, update-ca-certificates for .NET).
	// The CA cert is available inside the sandbox at /home/build/.apexpack-ca.crt.
	TLSCAPreStep string `yaml:"tls_ca_pre_step,omitempty"`

	// MavenMirrorURL is the URL of a corporate Artifactory (or Nexus) Maven proxy.
	// When set, apexpack injects a ~/.m2/settings.xml into every build that mirrors
	// all Maven repository requests through this URL instead of hitting Maven Central
	// directly. This is the standard fix for corporate networks that block or
	// SSL-inspect outbound connections to repo1.maven.org.
	// Example: "https://artifactory.corp.example.com/artifactory/libs-release"
	MavenMirrorURL string `yaml:"maven_mirror_url,omitempty"`

	// MavenSettingsTemplate selects which settings.xml template to inject.
	// Built-in templates: "default" (mirrors + servers only) and "corporate"
	// (adds pluginRepositories + profiles + activeProfiles for Artifactory setups
	// that serve plugins from a separate repo group).
	// Custom templates can be placed at <profiles-dir>/templates/maven/<name>.xml
	// and will take precedence over the built-in ones.
	// Defaults to "default" when empty.
	MavenSettingsTemplate string `yaml:"maven_settings_template,omitempty"`
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

	// Caches replaces Build.Caches when set.
	Caches []string `yaml:"caches,omitempty"`
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
//	scan:
//	  auto-patch: true        # override the profile's scan.auto-patch default
//	  patch-persist: false    # commit patched profiles back to git
type ProjectConfig struct {
	// Runtime overrides auto-detection (e.g. "golang", "java").
	Runtime string `yaml:"runtime,omitempty"`

	// Image overrides are merged on top of the profile's image config.
	// Packages are appended, not replaced. Env vars are merged (project wins).
	Image *ProjectImageOverride `yaml:"image,omitempty"`

	// Build overrides are merged on top of the profile's build config.
	Build *ProjectBuildOverride `yaml:"build,omitempty"`

	// Scan overrides the language profile's scan defaults for this project.
	Scan *ScanConfig `yaml:"scan,omitempty"`
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

	// PackageManager is the build tool identified by a PackageManagerRule.
	// Examples: "pnpm", "bun", "yarn", "yarn-berry", "uv", "poetry"
	// Empty string means the default package manager for the runtime is used.
	PackageManager string
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

	// PackageManager is the detected build tool (e.g. "pnpm", "bun", "uv").
	// Empty if the default package manager for the runtime is used.
	PackageManager string

	// ProcfileCmd is the "web:" command parsed from a Procfile, if one exists.
	// Used as the image entrypoint/cmd when the profile has no explicit entrypoint.
	ProcfileCmd string

	// Melange is the structured melange.yaml config — marshalled to YAML on write.
	Melange MelangeConfig

	// Apko is the structured apko.yaml config — marshalled to YAML on write.
	Apko ApkoConfig
}
