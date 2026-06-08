// Package build generates melange and apko config structs from a profile,
// marshals them to YAML, writes them to disk, then runs the tools.
package build

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/apexpack/apexpack/internal/types"
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

// Options controls the build.
type Options struct {
	// SourceDir is the root of the project to build.
	SourceDir string

	// ProfilesDir is where profiles/*.yaml files live.
	ProfilesDir string

	// OutputDir is where melange writes the .apk and apko writes the image.
	// Defaults to <SourceDir>/.apexpack-output
	OutputDir string

	// ProjectName is used as the APK package name and image name.
	// Defaults to the base name of SourceDir.
	ProjectName string

	// Version is the APK package version. Defaults to "0.0.1".
	Version string

	// Tag is the full OCI image reference (e.g. ghcr.io/myorg/myapp:v1.0).
	// Defaults to <ProjectName>:latest
	Tag string

	// Framework is the detected framework (e.g. "spring-boot", "quarkus").
	// When set, build.Plan applies any matching FrameworkBuildOverride from the profile.
	Framework string

	// PackageManager is the detected build tool (e.g. "pnpm", "bun", "uv").
	// Used to resolve framework overrides with the fallback order:
	//   {framework}-{packageManager} → {packageManager} → {framework} → default
	PackageManager string

	// Profile is set internally by Run() to make cache paths available to tool runners.
	Profile *types.Profile

	// TLSExtraCA is a path to an extra CA certificate (PEM) to trust during builds.
	// Use this in corporate environments where a proxy replaces TLS certificates.
	// The cert is mounted into the melange container and added to SSL_CERT_DIR.
	// Falls back to the APEXPACK_EXTRA_CA environment variable if not set.
	TLSExtraCA string
}

// Plan builds a MelangeConfig and ApkoConfig from the profile and options.
// The profile has already been merged with per-project apexpack.yaml overrides
// by the caller (main.go). Does NOT write files or run tools.
func Plan(p *types.Profile, opts Options) (*types.BuildPlan, error) {
	opts = applyDefaults(opts)

	melangeCfg, err := buildMelangeConfig(p, opts)
	if err != nil {
		return nil, err
	}
	return &types.BuildPlan{
		ProjectName:    opts.ProjectName,
		Version:        opts.Version,
		Profile:        p,
		Framework:      opts.Framework,
		PackageManager: opts.PackageManager,
		ProcfileCmd:    readProcfileCmd(opts.SourceDir),
		Melange:        melangeCfg,
		Apko:           buildApkoConfig(p, opts),
	}, nil
}

// readProcfileCmd parses the "web:" process from a Procfile and returns its command.
// Returns empty string if no Procfile exists or no web process is defined.
func readProcfileCmd(srcDir string) string {
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

// Run writes melange.yaml and apko.yaml to disk, then runs the tools.
func Run(plan *types.BuildPlan, opts Options) error {
	opts = applyDefaults(opts)
	opts.Profile = plan.Profile
	opts.Framework = plan.Framework
	opts.PackageManager = plan.PackageManager
	if opts.TLSExtraCA == "" {
		opts.TLSExtraCA = os.Getenv("APEXPACK_EXTRA_CA")
	}

	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return fmt.Errorf("creating output dir: %w", err)
	}

	// Marshal each config struct → 2-space indented YAML string.
	melangeYAML, err := marshalYAML(&plan.Melange)
	if err != nil {
		return fmt.Errorf("marshalling melange config: %w", err)
	}
	melangeData := []byte(melangeYAML)

	apkoYAML, err := marshalYAML(&plan.Apko)
	if err != nil {
		return fmt.Errorf("marshalling apko config: %w", err)
	}
	apkoData := []byte(apkoYAML)

	// Write melange.yaml.
	melangeFile := filepath.Join(opts.OutputDir, "melange.yaml")
	if err := os.WriteFile(melangeFile, melangeData, 0o644); err != nil {
		return fmt.Errorf("writing melange.yaml: %w", err)
	}
	fmt.Printf("  → wrote %s\n", melangeFile)

	// Write apko.yaml.
	apkoFile := filepath.Join(opts.OutputDir, "apko.yaml")
	if err := os.WriteFile(apkoFile, apkoData, 0o644); err != nil {
		return fmt.Errorf("writing apko.yaml: %w", err)
	}
	fmt.Printf("  → wrote %s\n", apkoFile)

	// When a corporate CA is provided:
	// 1. Copy the raw cert into the source dir so the melange sandbox can read it
	//    at /home/build/.apexpack-ca.crt.
	// 2. Prepend a universal step that merges system CAs + corporate CA into a
	//    single PEM bundle at /home/build/.apexpack-ca-bundle.pem. SSL_CERT_FILE
	//    is set to this path (see buildMelangeConfig) so Go, .NET, Python, Rust,
	//    and curl all pick it up without any per-profile configuration.
	// 3. If the profile also declares a tls_ca_pre_step (e.g. Java keytool), inject
	//    it after the universal step so the bundle exists when keytool runs.
	if opts.TLSExtraCA != "" {
		absCA, _ := filepath.Abs(opts.TLSExtraCA)
		caCopyPath := filepath.Join(opts.SourceDir, ".apexpack-ca.crt")
		if caData, readErr := os.ReadFile(absCA); readErr == nil {
			if writeErr := os.WriteFile(caCopyPath, caData, 0o644); writeErr == nil {
				defer os.Remove(caCopyPath)

				universalStep := `CA_BUNDLE=/home/build/.apexpack-ca-bundle.pem
if [ -f /etc/ssl/certs/ca-certificates.crt ]; then
  cat /etc/ssl/certs/ca-certificates.crt > "$CA_BUNDLE"
  printf '\n' >> "$CA_BUNDLE"
fi
cat /home/build/.apexpack-ca.crt >> "$CA_BUNDLE"
echo "→ CA bundle ready: system CAs + corporate CA"`

				preSteps := []types.MelangePipeline{{Runs: universalStep}}
				if opts.Profile != nil && opts.Profile.Build.TLSCAPreStep != "" {
					preSteps = append(preSteps, types.MelangePipeline{Runs: opts.Profile.Build.TLSCAPreStep})
				}
				plan.Melange.Pipeline = append(preSteps, plan.Melange.Pipeline...)

				melangeYAML, err = marshalYAML(&plan.Melange)
				if err != nil {
					return fmt.Errorf("marshalling melange config (with TLS pre-step): %w", err)
				}
				melangeData = []byte(melangeYAML)
				if err := os.WriteFile(melangeFile, melangeData, 0o644); err != nil {
					return fmt.Errorf("writing melange.yaml (with TLS pre-step): %w", err)
				}
			}
		}
	}

	// Run melange.
	fmt.Println("\n[2/3] Running melange...")
	if err := runMelange(melangeFile, opts); err != nil {
		return fmt.Errorf("melange: %w", err)
	}

	// Run apko.
	fmt.Println("\n[3/3] Running apko...")
	if err := runApko(apkoFile, opts); err != nil {
		return fmt.Errorf("apko: %w", err)
	}

	return nil
}

// MarshalMelange returns the melange config as a YAML string.
// Used by --dry-run to print the config without writing to disk.
func MarshalMelange(plan *types.BuildPlan) (string, error) {
	return marshalYAML(&plan.Melange)
}

// MarshalApko returns the apko config as a YAML string.
// Used by --dry-run to print the config without writing to disk.
func MarshalApko(plan *types.BuildPlan) (string, error) {
	return marshalYAML(&plan.Apko)
}

// marshalYAML encodes v to a 2-space indented YAML string.
// yaml.Marshal defaults to 4-space indentation; this matches the melange/apko
// convention of 2 spaces and makes generated configs easier to read.
func marshalYAML(v any) (string, error) {
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ============================================================================
// Config builders — profile → typed structs
// ============================================================================

// buildMelangeConfig constructs a MelangeConfig from a profile and options.
// The struct is later marshalled to YAML by yaml.Marshal.
func buildMelangeConfig(p *types.Profile, opts Options) (types.MelangeConfig, error) {
	packages := append([]string{"wolfi-baselayout"}, p.Build.Dependencies...)

	cfg := types.MelangeConfig{
		Package: types.MelangePackage{
			Name:        opts.ProjectName,
			Version:     opts.Version,
			Epoch:       0,
			Description: fmt.Sprintf("Built by apexpack (%s)", p.Runtime),
			Copyright:   []types.MelangeCopyright{{License: "Apache-2.0"}},
		},
		Environment: types.MelangeEnvironment{
			Contents: types.MelangeContents{
				Keyring:      []string{"https://packages.wolfi.dev/os/wolfi-signing.rsa.pub"},
				Repositories: []string{"https://packages.wolfi.dev/os"},
				Packages:     packages,
			},
			Env: p.Build.Env,
		},
		Pipeline: []types.MelangePipeline{{Runs: applyProjectTemplates(p.Build.Command, opts.ProjectName)}},
	}

	// Resolve the framework override using a three-level fallback:
	//   1. {framework}-{packageManager}  e.g. "nextjs-pnpm"
	//   2. {packageManager}              e.g. "pnpm"
	//   3. {framework}                   e.g. "nextjs"
	override, found := resolveOverride(p, opts.Framework, opts.PackageManager)
	if found {
		if len(override.Dependencies) > 0 {
			cfg.Environment.Contents.Packages = append([]string{"wolfi-baselayout"}, override.Dependencies...)
		}
		if override.Command != "" {
			cfg.Pipeline = []types.MelangePipeline{{Runs: applyProjectTemplates(override.Command, opts.ProjectName)}}
		}
		for k, v := range override.Env {
			if cfg.Environment.Env == nil {
				cfg.Environment.Env = make(map[string]string)
			}
			cfg.Environment.Env[k] = v
		}
	}

	// Inject a Maven settings.xml step for corporate Artifactory mirrors.
	// Fires when maven_mirror_url is set AND either:
	//   a) MAVEN_MIRROR_USER is present (credentials from Kubernetes secret), or
	//   b) a custom template exists in the profiles dir (template supplies its own auth).
	// Without either, the mirror step is skipped so Maven resolves from Maven Central
	// directly — keeps local/CI-without-Artifactory builds working.
	tmplName := p.Build.MavenSettingsTemplate
	if tmplName == "" {
		tmplName = "default"
	}
	customTemplatePath := filepath.Join(opts.ProfilesDir, "templates", "maven", tmplName+".xml")
	_, customTemplateExists := os.Stat(customTemplatePath)
	if p.Build.MavenMirrorURL != "" && (os.Getenv("MAVEN_MIRROR_USER") != "" || customTemplateExists == nil) {
		if cfg.Environment.Env == nil {
			cfg.Environment.Env = make(map[string]string)
		}
		for _, key := range []string{"MAVEN_MIRROR_USER", "MAVEN_MIRROR_PASSWORD"} {
			if val := os.Getenv(key); val != "" {
				if _, exists := cfg.Environment.Env[key]; !exists {
					cfg.Environment.Env[key] = val
				}
			}
		}
		tmpl, err := loadMavenTemplate(tmplName, opts.ProfilesDir)
		if err != nil {
			return types.MelangeConfig{}, fmt.Errorf("maven settings template: %w", err)
		}
		settingsXML := strings.ReplaceAll(tmpl, "{{MAVEN_MIRROR_URL}}", p.Build.MavenMirrorURL)
		mirrorStep := fmt.Sprintf(
			"mkdir -p /home/build/.m2\n"+
				"cat > /home/build/.m2/settings.xml << APEXPACK_SETTINGS_EOF\n"+
				"%s"+
				"APEXPACK_SETTINGS_EOF\n"+
				"echo \"→ Maven settings: %s template, mirror: %s\"",
			settingsXML, tmplName, p.Build.MavenMirrorURL,
		)
		cfg.Pipeline = append(
			[]types.MelangePipeline{{Runs: mirrorStep}},
			cfg.Pipeline...,
		)
	}

	// Propagate Go module env vars from the host into the melange.yaml environment
	// block. Melange explicitly passes these into the bubblewrap sandbox, so the
	// go build command running inside the sandbox uses the same proxy and TLS
	// settings as the host. Profile-defined values take precedence.
	for _, key := range []string{"GOPROXY", "GONOSUMDB", "GONOSUMCHECK", "GOINSECURE", "GOPRIVATE"} {
		if val := os.Getenv(key); val != "" {
			if cfg.Environment.Env == nil {
				cfg.Environment.Env = make(map[string]string)
			}
			if _, exists := cfg.Environment.Env[key]; !exists {
				cfg.Environment.Env[key] = val
			}
		}
	}

	// When a corporate CA is provided, set standard env vars so all runtimes that
	// respect SSL_CERT_FILE (Go, .NET, Python, Rust, curl) trust the merged bundle.
	// The bundle (system CAs + corporate CA) is created by the universal pre-step
	// injected in Run() before the build executes.
	// Java is the exception — it does not respect SSL_CERT_FILE and needs the
	// keytool pre-step declared in java.yaml instead.
	if opts.TLSExtraCA != "" {
		if cfg.Environment.Env == nil {
			cfg.Environment.Env = make(map[string]string)
		}
		bundle := "/home/build/.apexpack-ca-bundle.pem"
		for key, val := range map[string]string{
			"SSL_CERT_FILE":       bundle,
			"NODE_EXTRA_CA_CERTS": bundle,
			"REQUESTS_CA_BUNDLE":  bundle,
			"CURL_CA_BUNDLE":      bundle,
		} {
			if _, exists := cfg.Environment.Env[key]; !exists {
				cfg.Environment.Env[key] = val
			}
		}
	}

	return cfg, nil
}

// resolveOverride finds the most specific FrameworkBuildOverride for the detected
// framework and package manager, using the three-level fallback.
func resolveOverride(p *types.Profile, framework, pm string) (types.FrameworkBuildOverride, bool) {
	if len(p.Build.Frameworks) == 0 {
		return types.FrameworkBuildOverride{}, false
	}
	candidates := []string{}
	if framework != "" && pm != "" {
		candidates = append(candidates, framework+"-"+pm)
	}
	if pm != "" {
		candidates = append(candidates, pm)
	}
	if framework != "" {
		candidates = append(candidates, framework)
	}
	for _, key := range candidates {
		if override, ok := p.Build.Frameworks[key]; ok {
			return override, true
		}
	}
	return types.FrameworkBuildOverride{}, false
}

// buildApkoConfig constructs an ApkoConfig from a profile and options.
// The struct is later marshalled to YAML by yaml.Marshal.
func buildApkoConfig(p *types.Profile, opts Options) types.ApkoConfig {
	packages := append([]string{"wolfi-baselayout", opts.ProjectName}, p.Image.Packages...)

	runAs := p.Image.RunAs
	if runAs == 0 {
		runAs = 65532
	}

	// Entrypoint: profile wins (with {APP_NAME} substituted); fall back to Procfile "web:" command.
	entrypoint := applyProjectTemplates(p.Image.Entrypoint, opts.ProjectName)
	cmd := p.Image.Cmd
	if entrypoint == "" {
		if procCmd := readProcfileCmd(opts.SourceDir); procCmd != "" {
			parts := strings.Fields(procCmd)
			entrypoint = parts[0]
			if len(parts) > 1 {
				cmd = parts[1:]
			}
		}
	}

	cfg := types.ApkoConfig{
		Contents: types.ApkoContents{
			Keyring: []string{
				"https://packages.wolfi.dev/os/wolfi-signing.rsa.pub",
				"./melange.rsa.pub",
			},
			Repositories: []string{
				"https://packages.wolfi.dev/os",
				"./packages",
			},
			Packages: packages,
		},
		Entrypoint:  types.ApkoEntrypoint{Command: entrypoint},
		Accounts: types.ApkoAccounts{
			RunAs: fmt.Sprintf("%d", runAs),
			Users: []types.ApkoUser{{Username: "nonroot", UID: runAs, GID: runAs}},
			Groups: []types.ApkoGroup{{Groupname: "nonroot", GID: runAs}},
		},
		Environment: p.Image.Env,
	}

	if len(cmd) > 0 {
		cfg.Cmd = strings.Join(cmd, " ")
	}

	return cfg
}

// applyProjectTemplates replaces {APP_NAME} with the project name.
// Use {APP_NAME} in profile build commands and image entrypoints to produce
// binaries and entrypoints named after the project rather than a fixed "app".
func applyProjectTemplates(s, projectName string) string {
	return strings.ReplaceAll(s, "{APP_NAME}", projectName)
}

// cacheVolumeName returns a stable Docker volume name for a build cache path.
func cacheVolumeName(cachePath string) string {
	safe := strings.NewReplacer("/", "-", ".", "-", " ", "-").Replace(strings.TrimPrefix(cachePath, "/"))
	return "apexpack-cache-" + safe
}

// ============================================================================
// Tool runners
// ============================================================================

func runMelange(configFile string, opts Options) error {
	packagesDir := filepath.Join(opts.OutputDir, "packages")
	if err := os.MkdirAll(packagesDir, 0o755); err != nil {
		return fmt.Errorf("creating packages dir: %w", err)
	}

	// melange signs every APK it builds. Generate a key pair locally —
	// keygen is pure Go and works on any OS. The key pair is stored in
	// the output directory and reused on subsequent builds.
	keyFile := filepath.Join(opts.OutputDir, "melange.rsa")
	if err := ensureSigningKey(keyFile); err != nil {
		return fmt.Errorf("generating signing key: %w", err)
	}

	if runtime.GOOS == "darwin" {
		return runMelangeInDocker(configFile, keyFile, opts)
	}

	arch := melangeArch()
	fmt.Printf("  → melange arch: %s (GOARCH=%s)\n", arch, runtime.GOARCH)
	env := os.Environ()
	if opts.TLSExtraCA != "" {
		absCA, _ := filepath.Abs(opts.TLSExtraCA)
		env = append(env, "SSL_CERT_DIR=/etc/ssl/certs:"+filepath.Dir(absCA))
	}
	return runToolEnv("melange", []string{
		"build", configFile,
		"--source-dir", opts.SourceDir,
		"--out-dir", packagesDir,
		"--signing-key", keyFile,
		"--arch", arch,
		"--runner", "bubblewrap",
	}, env)
}

// runMelangeInDocker runs the melange build entirely inside a Linux container.
//
// On macOS, running melange locally fails because apko (which melange uses to
// set up the build environment) installs packages in an order where glibc
// creates lib/ as a directory before wolfi-baselayout can create the required
// lib → usr/lib symlink. This is a macOS filesystem behaviour issue.
//
// The fix is to run melange inside cgr.dev/chainguard/melange — a Linux
// container — where the package installation order works correctly.
// The source directory and output directory are bind-mounted into the container.
func runMelangeInDocker(configFile, keyFile string, opts Options) error {
	fmt.Println("  → macOS detected: running melange inside Linux container")

	// Resolve absolute paths — Docker volume mounts require absolute paths.
	absSrc, err := filepath.Abs(opts.SourceDir)
	if err != nil {
		return fmt.Errorf("resolving source dir: %w", err)
	}
	absOut, err := filepath.Abs(opts.OutputDir)
	if err != nil {
		return fmt.Errorf("resolving output dir: %w", err)
	}

	// Inside the container, source is at /work/src and output at /work/output.
	// The melange.yaml and signing key live in the output dir, so their
	// container paths are derived from /work/output/.
	containerConfig := "/work/output/" + filepath.Base(configFile)
	containerKey := "/work/output/" + filepath.Base(keyFile)

	args := []string{
		"run", "--rm",
		"--privileged",
		"-v", absSrc + ":/work/src:ro",
		"-v", absOut + ":/work/output",
	}

	// Mount named Docker volumes for each declared cache path.
	// Volumes persist across builds so package managers reuse their caches.
	for _, cachePath := range opts.Profile.Build.Caches {
		args = append(args, "-v", cacheVolumeName(cachePath)+":"+cachePath)
	}
	// Also apply cache overrides from the resolved framework/package-manager entry.
	if override, found := resolveOverride(opts.Profile, opts.Framework, opts.PackageManager); found {
		for _, cachePath := range override.Caches {
			args = append(args, "-v", cacheVolumeName(cachePath)+":"+cachePath)
		}
	}

	// Inject corporate CA cert so melange can reach Wolfi repositories through
	// a TLS-intercepting proxy. SSL_CERT_DIR adds the cert directory alongside
	// the container's existing system certs — both are trusted.
	if opts.TLSExtraCA != "" {
		absCA, err := filepath.Abs(opts.TLSExtraCA)
		if err != nil {
			return fmt.Errorf("resolving TLS CA path: %w", err)
		}
		args = append(args,
			"-v", absCA+":/extra-certs/"+filepath.Base(absCA)+":ro",
			"-e", "SSL_CERT_DIR=/etc/ssl/certs:/extra-certs",
		)
	}

	// Forward Go toolchain env vars from the host into the build container.
	// This ensures the container's go build uses the same proxy, CA, and
	// checksum settings as the host — essential in corporate proxy environments.
	for _, key := range []string{
		"GOPROXY", "GONOSUMDB", "GONOSUMCHECK", "GOINSECURE", "GOPRIVATE",
		"GOFLAGS", "SSL_CERT_FILE", "SSL_CERT_DIR",
	} {
		if val := os.Getenv(key); val != "" {
			args = append(args, "-e", key+"="+val)
		}
	}

	// APEXPACK_DOCKER_EXTRA_HOSTS: comma-separated host:ip pairs injected as
	// --add-host flags. Use this to make corporate hostnames resolvable inside
	// the melange container on macOS where --network=host is not supported.
	// Example: export APEXPACK_DOCKER_EXTRA_HOSTS="artifactory.corp:10.0.0.5"
	if extraHosts := os.Getenv("APEXPACK_DOCKER_EXTRA_HOSTS"); extraHosts != "" {
		for _, entry := range strings.Split(extraHosts, ",") {
			entry = strings.TrimSpace(entry)
			if entry != "" {
				args = append(args, "--add-host", entry)
			}
		}
	}

	args = append(args,
		"cgr.dev/chainguard/melange",
		"build", containerConfig,
		"--source-dir", "/work/src",
		"--out-dir", "/work/output/packages",
		"--signing-key", containerKey,
		"--arch", melangeArch(),
	)

	return runTool("docker", args)
}

// ensureSigningKey generates a melange RSA key pair at keyFile if one does
// not already exist. Reused across builds in the same output directory.
func ensureSigningKey(keyFile string) error {
	if _, err := os.Stat(keyFile); err == nil {
		return nil
	}
	fmt.Println("  → Generating melange signing key...")
	return runTool("melange", []string{"keygen", keyFile})
}

func runApko(configFile string, opts Options) error {
	imageTag := opts.Tag
	if imageTag == "" {
		imageTag = opts.ProjectName + ":latest"
	}
	outputTar := filepath.Join(opts.OutputDir, opts.ProjectName+".tar")

	if runtime.GOOS == "darwin" {
		return runApkoInDocker(configFile, imageTag, outputTar, opts)
	}

	args := []string{
		"build", configFile,
		imageTag,
		outputTar,
		"--arch", melangeArch(),
	}

	if opts.TLSExtraCA == "" {
		return runToolInDir(opts.OutputDir, "apko", args)
	}

	// Corporate proxy: merge the system CA bundle with the extra CA into a temp
	// file and expose it via SSL_CERT_FILE so apko's Go TLS stack trusts it.
	absCA, _ := filepath.Abs(opts.TLSExtraCA)
	merged, err := mergeCABundles(absCA)
	if err != nil {
		fmt.Printf("  → WARN: could not prepare merged CA bundle (%v); apko may fail on TLS\n", err)
		return runToolInDir(opts.OutputDir, "apko", args)
	}
	defer os.Remove(merged)

	return runToolInDirEnv(opts.OutputDir, "apko", args, append(os.Environ(), "SSL_CERT_FILE="+merged))
}

// runApkoInDocker runs apko inside a Linux container on macOS.
// The local apko binary (Homebrew) uses an older signature format than the
// melange container (RSA256), so we run the same cgr.dev/chainguard/apko
// image to guarantee signature format compatibility.
func runApkoInDocker(configFile, imageTag, outputTar string, opts Options) error {
	fmt.Println("  → macOS detected: running apko inside Linux container")

	absOut, err := filepath.Abs(opts.OutputDir)
	if err != nil {
		return fmt.Errorf("resolving output dir: %w", err)
	}

	containerConfig := "/work/output/" + filepath.Base(configFile)
	containerTar := "/work/output/" + filepath.Base(outputTar)

	args := []string{
		"run", "--rm",
		"-w", "/work/output",
		"-v", absOut + ":/work/output",
	}

	if opts.TLSExtraCA != "" {
		absCA, err := filepath.Abs(opts.TLSExtraCA)
		if err != nil {
			return fmt.Errorf("resolving TLS CA path: %w", err)
		}
		args = append(args,
			"-v", absCA+":/extra-certs/"+filepath.Base(absCA)+":ro",
			"-e", "SSL_CERT_DIR=/etc/ssl/certs:/extra-certs",
		)
	}

	args = append(args,
		"cgr.dev/chainguard/apko",
		"build", containerConfig,
		imageTag,
		containerTar,
		"--arch", melangeArch(),
	)

	return runTool("docker", args)
}

// melangeArch maps GOARCH values to the architecture names melange and apko expect.
func melangeArch() string {
	switch runtime.GOARCH {
	case "arm64":
		return "aarch64"
	default:
		return "x86_64"
	}
}

// runTool runs an external binary, streaming output to stdout/stderr.
func runTool(name string, args []string) error {
	path, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("%s not found in PATH", name)
	}

	cmd := exec.Command(path, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Printf("  → %s %s\n", name, strings.Join(args, " "))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s exited with error: %w", name, err)
	}
	return nil
}

// runToolInDir is like runTool but sets the working directory before exec.
func runToolInDir(dir, name string, args []string) error {
	path, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("%s not found in PATH", name)
	}

	cmd := exec.Command(path, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Printf("  → %s %s\n", name, strings.Join(args, " "))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s exited with error: %w", name, err)
	}
	return nil
}

// runToolEnv is like runTool but uses a custom environment instead of inheriting the process env.
func runToolEnv(name string, args []string, env []string) error {
	path, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("%s not found in PATH", name)
	}

	cmd := exec.Command(path, args...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Printf("  → %s %s\n", name, strings.Join(args, " "))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s exited with error: %w", name, err)
	}
	return nil
}

// runToolInDirEnv is like runToolInDir but uses the provided environment
// instead of inheriting the process environment.
func runToolInDirEnv(dir, name string, args []string, env []string) error {
	path, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("%s not found in PATH", name)
	}

	cmd := exec.Command(path, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Printf("  → %s %s\n", name, strings.Join(args, " "))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s exited with error: %w", name, err)
	}
	return nil
}

// mergeCABundles concatenates the system CA bundle with an extra CA certificate
// into a temp PEM file so a single SSL_CERT_FILE covers both.
func mergeCABundles(extraCAPath string) (string, error) {
	systemBundles := []string{
		"/etc/ssl/certs/ca-certificates.crt", // Debian/Ubuntu
		"/etc/pki/tls/certs/ca-bundle.crt",   // RHEL/CentOS
		"/etc/ssl/ca-bundle.pem",              // SUSE
		"/etc/ssl/cert.pem",                   // Alpine/Wolfi
	}

	var systemCerts []byte
	for _, p := range systemBundles {
		if data, err := os.ReadFile(p); err == nil {
			systemCerts = data
			break
		}
	}

	extraCerts, err := os.ReadFile(extraCAPath)
	if err != nil {
		return "", fmt.Errorf("reading extra CA: %w", err)
	}

	f, err := os.CreateTemp("", "apexpack-ca-*.pem")
	if err != nil {
		return "", fmt.Errorf("creating temp CA bundle: %w", err)
	}
	defer f.Close()

	if len(systemCerts) > 0 {
		f.Write(systemCerts)
		f.Write([]byte("\n"))
	}
	f.Write(extraCerts)

	return f.Name(), nil
}

// applyDefaults fills in zero-value options.
func applyDefaults(opts Options) Options {
	if opts.ProjectName == "" {
		opts.ProjectName = filepath.Base(opts.SourceDir)
	}
	if opts.Version == "" {
		opts.Version = "0.0.1"
	}
	if opts.OutputDir == "" {
		opts.OutputDir = filepath.Join(opts.SourceDir, ".apexpack-output")
	}
	return opts
}
