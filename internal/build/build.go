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

//go:embed templates/nuget/default.xml
var nugetTemplateDefault string

//go:embed templates/nuget/corporate.xml
var nugetTemplateCorporate string

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

	// Arch overrides the target build architecture passed to melange and apko.
	// Accepted values: "x86_64", "aarch64". Defaults to the host architecture.
	// Set to "x86_64" when building on Apple Silicon for a Linux x86_64 cluster.
	Arch string

	// SigningKey is the path to an existing melange RSA private key (PEM).
	// The matching .pub file must exist at SigningKey+".pub".
	// When empty (default), a key pair is generated in OutputDir on first build.
	SigningKey string

	// LocalBuild skips the registry push and writes a tarball to OutputDir instead.
	// When false (default), apko publish pushes directly to the registry in Tag.
	// When true, apko build produces a .tar file for local inspection or manual push.
	// On macOS this flag is a no-op — the darwin path always produces a tarball.
	LocalBuild bool

	// LanguageVersion is the detected language version (e.g. "17" for Java 17,
	// "20" for Node 20, "3.12" for Python 3.12, "8" for .NET 8).
	// Substituted for {JAVA_VERSION}, {NODE_VERSION}, {PYTHON_VERSION}, {DOTNET_VERSION}
	// tokens in profile package lists, env values, and build commands.
	// Empty string falls back to the built-in default for the runtime.
	LanguageVersion string
}

// Plan builds a MelangeConfig and ApkoConfig from the profile and options.
// The profile has already been merged with per-project apexpacks.yaml overrides
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

	// When a corporate CA is provided, copy it into the source dir so the melange
	// sandbox (source mounted at /home/build) can access it at /home/build/.apexpack-ca.crt.
	// Two kinds of TLS pipeline steps may be injected, in this order (first = outermost):
	//   1. Bundle step — for profiles using tls_ca_env (Go, Node, Python): merges system CAs
	//      + corp CA into .apexpack-ca-bundle.pem so SSL_CERT_FILE doesn't drop system CAs.
	//   2. Profile pre-step — for runtimes with their own cert stores (JVM keytool, .NET).
	if opts.TLSExtraCA != "" {
		absCA, _ := filepath.Abs(opts.TLSExtraCA)
		caCopyPath := filepath.Join(opts.SourceDir, ".apexpack-ca.crt")
		if caData, readErr := readCACerts(absCA); readErr == nil {
			if writeErr := os.WriteFile(caCopyPath, caData, 0o644); writeErr == nil {
				defer os.Remove(caCopyPath)
				pipelineModified := false

				// Prepend the profile's runtime-specific pre-step (e.g. keytool for JVM).
				if opts.Profile != nil && opts.Profile.Build.TLSCAPreStep != "" {
					plan.Melange.Pipeline = append(
						[]types.MelangePipeline{{Runs: opts.Profile.Build.TLSCAPreStep}},
						plan.Melange.Pipeline...,
					)
					pipelineModified = true
				}

				// Prepend a bundle-creation step for profiles that use tls_ca_env.
				// Setting SSL_CERT_FILE to only the corp CA drops all system CAs, which
				// breaks connections to public endpoints (e.g. proxy.golang.org, pypi.org).
				// This step merges system CAs + corp CA into a single bundle file that
				// tls_ca_env vars are pointed at (see buildMelangeConfig).
				if opts.Profile != nil && len(opts.Profile.Build.TLSCAEnv) > 0 {
					const bundleStep = `if [ -f "/home/build/.apexpack-ca.crt" ]; then
  if [ -f "/etc/ssl/certs/ca-certificates.crt" ]; then
    cat /etc/ssl/certs/ca-certificates.crt /home/build/.apexpack-ca.crt > /home/build/.apexpack-ca-bundle.pem
  else
    cp /home/build/.apexpack-ca.crt /home/build/.apexpack-ca-bundle.pem
  fi
fi`
					plan.Melange.Pipeline = append(
						[]types.MelangePipeline{{Runs: bundleStep}},
						plan.Melange.Pipeline...,
					)
					pipelineModified = true
				}

				// Append a post-build step to bake the corporate CA into the runtime image.
				// Copies an updated system bundle (Wolfi CAs + corp CA) and the individual
				// cert into ${{targets.destdir}} so they're packaged into the app APK that
				// apko installs. This replaces ca-certificates-bundle in the runtime image,
				// which is removed from plan.Apko below to avoid an apk file conflict.
				const imageCAStep = `if [ -f /home/build/.apexpack-ca.crt ] && [ -f /etc/ssl/certs/ca-certificates.crt ]; then
  mkdir -p "${{targets.destdir}}/etc/ssl/certs"
  cat /etc/ssl/certs/ca-certificates.crt /home/build/.apexpack-ca.crt \
    > "${{targets.destdir}}/etc/ssl/certs/ca-certificates.crt"
  cp /home/build/.apexpack-ca.crt \
     "${{targets.destdir}}/etc/ssl/certs/apexpack-corp-ca.crt"
  echo "  → Corporate CA baked into runtime image"
fi`
				plan.Melange.Pipeline = append(plan.Melange.Pipeline, types.MelangePipeline{Runs: imageCAStep})
				pipelineModified = true

				// Java: copy the JVM cacerts (already has corp CA imported by
				// tls_ca_pre_step's keytool call) to /etc/ssl/certs/cacerts — a path
				// our APK owns. We cannot put it at the JRE's own cacerts path
				// (/usr/lib/jvm/.../security/cacerts) because openjdk-*-jre owns that
				// file and apko would report a file conflict.
				// JAVA_TOOL_OPTIONS (added to the apko environment below) points the
				// JVM at our path at runtime — no startup script changes needed.
				if opts.Profile != nil && opts.Profile.Runtime == "java" {
					const jvmCACertsStep = `JAVA_CACERTS=$(find /usr/lib/jvm -name "cacerts" 2>/dev/null | head -1)
if [ -n "$JAVA_CACERTS" ]; then
  mkdir -p "${{targets.destdir}}/etc/ssl/certs"
  cp "$JAVA_CACERTS" "${{targets.destdir}}/etc/ssl/certs/cacerts"
  echo "  → JVM cacerts (with corp CA) baked into runtime image at /etc/ssl/certs/cacerts"
fi`
					plan.Melange.Pipeline = append(plan.Melange.Pipeline, types.MelangePipeline{Runs: jvmCACertsStep})

					if plan.Apko.Environment == nil {
						plan.Apko.Environment = make(map[string]string)
					}
					plan.Apko.Environment["JAVA_TOOL_OPTIONS"] = "-Djavax.net.ssl.trustStore=/etc/ssl/certs/cacerts -Djavax.net.ssl.trustStorePassword=changeit"
				}

				// Set replaces + provides on the melange package so APK treats our
				// APK as the drop-in replacement for ca-certificates-bundle. Without
				// this, ca-certificates-bundle is still pulled in as a transitive
				// dependency (e.g. openjdk-*-jre → ca-certificates virtual package),
				// causing a file conflict on /etc/ssl/certs/ca-certificates.crt.
				var caBundleVersion string
				for _, pkg := range plan.Apko.Contents.Packages {
					if strings.HasPrefix(pkg, "ca-certificates-bundle=") {
						caBundleVersion = strings.TrimPrefix(pkg, "ca-certificates-bundle=")
						break
					}
				}
				plan.Melange.Package.Dependencies.Replaces = []string{"ca-certificates-bundle"}
				if caBundleVersion != "" {
					plan.Melange.Package.Dependencies.Provides = []string{
						"ca-certificates-bundle=" + caBundleVersion,
						"ca-certificates=" + caBundleVersion,
					}
				} else {
					plan.Melange.Package.Dependencies.Provides = []string{"ca-certificates-bundle", "ca-certificates"}
				}

				// Remove ca-certificates-bundle from the runtime image package list.
				// Our APK now owns /etc/ssl/certs/ca-certificates.crt (the updated bundle),
				// so keeping ca-certificates-bundle would cause an apk file conflict.
				filtered := plan.Apko.Contents.Packages[:0]
				for _, pkg := range plan.Apko.Contents.Packages {
					if !strings.HasPrefix(pkg, "ca-certificates-bundle") {
						filtered = append(filtered, pkg)
					}
				}
				plan.Apko.Contents.Packages = filtered
				apkoYAML, err = marshalYAML(&plan.Apko)
				if err != nil {
					return fmt.Errorf("marshalling apko config (with CA): %w", err)
				}
				apkoData = []byte(apkoYAML)
				if err := os.WriteFile(apkoFile, apkoData, 0o644); err != nil {
					return fmt.Errorf("writing apko.yaml (with CA): %w", err)
				}

				if pipelineModified {
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
	}

	// Run melange.
	fmt.Println("\n  → Running melange...")
	if err := runMelange(melangeFile, opts); err != nil {
		return fmt.Errorf("melange: %w", err)
	}

	// Run melange test when the profile defines a test pipeline.
	if plan.Melange.Test != nil {
		fmt.Println("\n  → Running melange test...")
		if err := runMelangeTest(melangeFile, opts); err != nil {
			return fmt.Errorf("melange test: %w", err)
		}
	}

	// Run apko.
	fmt.Println("\n  → Running apko...")
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
	token := langVersionToken(p.Runtime)
	version := resolveVersion(p.Runtime, opts.LanguageVersion)
	if err := validateRuntimeVersion(p.Runtime, version); err != nil {
		return types.MelangeConfig{}, err
	}

	packages := vsubSlice(append([]string{"wolfi-baselayout"}, p.Build.Dependencies...), token, version)

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
			Env: fixJavaHome(vsubMap(p.Build.Env, token, version), p.Runtime, version),
		},
		Pipeline: []types.MelangePipeline{{Runs: vsub(applyProjectTemplates(p.Build.Command, opts.ProjectName), token, version)}},
	}

	// Resolve the framework override using a three-level fallback:
	//   1. {framework}-{packageManager}  e.g. "nextjs-pnpm"
	//   2. {packageManager}              e.g. "pnpm"
	//   3. {framework}                   e.g. "nextjs"
	override, found := resolveOverride(p, opts.Framework, opts.PackageManager)
	if found {
		if len(override.Dependencies) > 0 {
			cfg.Environment.Contents.Packages = vsubSlice(append([]string{"wolfi-baselayout"}, override.Dependencies...), token, version)
		}
		if override.Command != "" {
			cfg.Pipeline = []types.MelangePipeline{{Runs: vsub(applyProjectTemplates(override.Command, opts.ProjectName), token, version)}}
		}
		for k, v := range override.Env {
			if cfg.Environment.Env == nil {
				cfg.Environment.Env = make(map[string]string)
			}
			cfg.Environment.Env[k] = vsub(v, token, version)
		}
	}

	// Inject a Maven settings.xml step for corporate Artifactory mirrors.
	// Fires when maven_mirror_url is set AND either:
	//   a) ARTI_USER is present (credentials from regcred docker secret), or
	//   b) a custom template exists in the profiles dir (template supplies its own auth).
	// Without either, the mirror step is skipped so Maven resolves from Maven Central
	// directly — keeps local/CI-without-Artifactory builds working.
	tmplName := p.Build.MavenSettingsTemplate
	if tmplName == "" {
		tmplName = "default"
	}
	customTemplatePath := filepath.Join(opts.ProfilesDir, "templates", "maven", tmplName+".xml")
	_, customTemplateExists := os.Stat(customTemplatePath)
	if p.Build.MavenMirrorURL != "" && (os.Getenv("ARTI_USER") != "" || customTemplateExists == nil) {
		if cfg.Environment.Env == nil {
			cfg.Environment.Env = make(map[string]string)
		}
		for _, key := range []string{"ARTI_USER", "ARTI_PASSWORD"} {
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

	// Inject a NuGet.Config for corporate Artifactory NuGet feeds.
	// Fires when nuget_mirror_url is set AND either:
	//   a) ARTI_USER is present (credentials from regcred docker secret), or
	//   b) a custom template exists in the profiles dir (template supplies its own auth).
	// Without either, skipped so builds work locally and in OSS CI without Artifactory.
	nugetTmplName := p.Build.NuGetSettingsTemplate
	if nugetTmplName == "" {
		nugetTmplName = "default"
	}
	nugetCustomTemplatePath := filepath.Join(opts.ProfilesDir, "templates", "nuget", nugetTmplName+".xml")
	_, nugetCustomTemplateExists := os.Stat(nugetCustomTemplatePath)
	if p.Build.NuGetMirrorURL != "" && (os.Getenv("ARTI_USER") != "" || nugetCustomTemplateExists == nil) {
		if cfg.Environment.Env == nil {
			cfg.Environment.Env = make(map[string]string)
		}
		for _, key := range []string{"ARTI_USER", "ARTI_PASSWORD"} {
			if val := os.Getenv(key); val != "" {
				if _, exists := cfg.Environment.Env[key]; !exists {
					cfg.Environment.Env[key] = val
				}
			}
		}
		nugetTmpl, err := loadNuGetTemplate(nugetTmplName, opts.ProfilesDir)
		if err != nil {
			return types.MelangeConfig{}, fmt.Errorf("nuget config template: %w", err)
		}
		nugetConfigXML := strings.ReplaceAll(nugetTmpl, "{{NUGET_MIRROR_URL}}", p.Build.NuGetMirrorURL)
		nugetConfigStep := fmt.Sprintf(
			"mkdir -p /home/build/.nuget/NuGet\n"+
				"cat > /home/build/.nuget/NuGet/NuGet.Config << APEXPACK_NUGET_EOF\n"+
				"%s"+
				"APEXPACK_NUGET_EOF\n"+
				"echo \"→ NuGet config: %s template, mirror: %s\"",
			nugetConfigXML, nugetTmplName, p.Build.NuGetMirrorURL,
		)
		cfg.Pipeline = append(
			[]types.MelangePipeline{{Runs: nugetConfigStep}},
			cfg.Pipeline...,
		)
	}

	// Build the test section when the profile defines test steps.
	// The test sandbox always includes the local packages repo (where the built APK lives)
	// and the local signing key so the APK can be verified and installed.
	if len(p.Test.Pipeline) > 0 {
		testPkgs := vsubSlice(p.Test.Packages, token, version)
		steps := make([]types.MelangePipeline, len(p.Test.Pipeline))
		for i, step := range p.Test.Pipeline {
			steps[i] = types.MelangePipeline{
				Runs: vsub(applyProjectTemplates(step.Runs, opts.ProjectName), token, version),
			}
		}
		cfg.Test = &types.MelangeTest{
			Environment: types.MelangeTestEnvironment{
				Contents: types.MelangeContents{
					Keyring: []string{
						"https://packages.wolfi.dev/os/wolfi-signing.rsa.pub",
						"./melange.rsa.pub",
					},
					Repositories: []string{
						"https://packages.wolfi.dev/os",
						"./packages",
					},
					Packages: testPkgs,
				},
			},
			Pipeline: steps,
		}
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

	// When a corporate CA is provided, inject the env vars declared by the profile
	// into the melange sandbox. The cert is copied to opts.SourceDir before melange
	// runs (see Run()), making it accessible inside the sandbox at /home/build/.
	// The keytool/update-ca-certificates pre-step (if any) is also injected in Run().
	if opts.TLSExtraCA != "" && len(p.Build.TLSCAEnv) > 0 {
		if cfg.Environment.Env == nil {
			cfg.Environment.Env = make(map[string]string)
		}
		caPath := "/home/build/.apexpack-ca-bundle.pem"
		for _, key := range p.Build.TLSCAEnv {
			if _, exists := cfg.Environment.Env[key]; !exists {
				cfg.Environment.Env[key] = caPath
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
	token := langVersionToken(p.Runtime)
	version := resolveVersion(p.Runtime, opts.LanguageVersion)

	packages := vsubSlice(append([]string{"wolfi-baselayout", opts.ProjectName}, p.Image.Packages...), token, version)

	runAs := p.Image.RunAs
	if runAs == 0 {
		runAs = 65532
	}

	// Entrypoint: profile wins (with {APP_NAME} and version token substituted);
	// fall back to Procfile "web:" command.
	entrypoint := vsub(applyProjectTemplates(p.Image.Entrypoint, opts.ProjectName), token, version)
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
		Environment: fixJavaHome(vsubMap(p.Image.Env, token, version), p.Runtime, version),
	}

	if len(cmd) > 0 {
		cfg.Cmd = strings.Join(cmd, " ")
	}

	// If JAVA_HOME is set in the image env, derive PATH and resolve the bare
	// "java" entrypoint to a full path. This keeps JAVA_HOME as the single
	// version-pinned value — changing openjdk-21 → openjdk-17 only requires
	// updating JAVA_HOME; PATH and entrypoint follow automatically.
	if javaHome, ok := cfg.Environment["JAVA_HOME"]; ok && javaHome != "" {
		if _, hasPath := cfg.Environment["PATH"]; !hasPath {
			cfg.Environment["PATH"] = javaHome + "/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
		}
		if cfg.Entrypoint.Command == "java" {
			cfg.Entrypoint.Command = javaHome + "/bin/java"
		}
	}

	return cfg
}

// applyProjectTemplates replaces {APP_NAME} with the project name.
// Use {APP_NAME} in profile build commands and image entrypoints to produce
// binaries and entrypoints named after the project rather than a fixed "app".
func applyProjectTemplates(s, projectName string) string {
	return strings.ReplaceAll(s, "{APP_NAME}", projectName)
}

// defaultLangVersions maps runtimes to their built-in fallback version.
// Used when no version is detected from source files.
var defaultLangVersions = map[string]string{
	"java":   "21",
	"node":   "20",
	"python": "3.12",
	"dotnet": "8",
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

// supportedLangVersions lists the versions available in the Wolfi APK repo.
// Versions not in this set will cause an apk solve failure at build time.
var supportedLangVersions = map[string][]string{
	"dotnet": {"8", "9", "10"},
}

// javaHomeDirVersion returns the JVM directory version for JAVA_HOME paths.
// Java 8 uses the pre-9 "1.N" naming convention in its directory name:
//   openjdk-8-jre  →  /usr/lib/jvm/java-1.8-openjdk  (Wolfi convention)
//   openjdk-17-jre →  /usr/lib/jvm/java-17-openjdk
func javaHomeDirVersion(major string) string {
	if major == "8" {
		return "1.8"
	}
	return major
}

// fixJavaHome corrects JAVA_HOME paths after {JAVA_VERSION} token substitution.
// For Java 8, Wolfi installs the JRE at java-1.8-openjdk, not java-8-openjdk.
// This only touches the JAVA_HOME key; all other env vars are passed through unchanged.
func fixJavaHome(env map[string]string, runtime, version string) map[string]string {
	if runtime != "java" || env == nil {
		return env
	}
	dirVer := javaHomeDirVersion(version)
	if dirVer == version {
		return env // no correction needed
	}
	wrong := "/java-" + version + "-openjdk"
	right := "/java-" + dirVer + "-openjdk"
	out := make(map[string]string, len(env))
	for k, v := range env {
		out[k] = strings.ReplaceAll(v, wrong, right)
	}
	return out
}

// resolveVersion returns the detected version, falling back to the built-in default.
func resolveVersion(runtime, detected string) string {
	if detected != "" {
		return detected
	}
	return defaultLangVersions[runtime]
}

// validateRuntimeVersion returns an error if the resolved version is not available
// in the Wolfi APK repository for the given runtime.
func validateRuntimeVersion(runtime, version string) error {
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

	keyFile := filepath.Join(opts.OutputDir, "melange.rsa")
	if opts.SigningKey != "" {
		if err := copySigningKey(opts.SigningKey, keyFile); err != nil {
			return fmt.Errorf("copying signing key: %w", err)
		}
	} else if err := ensureSigningKey(keyFile); err != nil {
		return fmt.Errorf("generating signing key: %w", err)
	}

	if runtime.GOOS == "darwin" {
		return runMelangeInDocker(configFile, keyFile, opts)
	}

	arch := melangeArch(opts.Arch)
	fmt.Printf("  → melange arch: %s (GOARCH=%s)\n", arch, runtime.GOARCH)

	args := []string{
		"build", configFile,
		"--source-dir", opts.SourceDir,
		"--out-dir", packagesDir,
		"--signing-key", keyFile,
		"--arch", arch,
		"--runner", "bubblewrap",
	}

	if opts.TLSExtraCA == "" {
		return runTool("melange", args)
	}

	// Corporate proxy: merge the system CA bundle with the extra CA into a single
	// temp PEM file and point SSL_CERT_FILE at it so melange's Go TLS stack
	// (which fetches wolfi packages via go-apk) trusts both public root CAs and
	// the corporate CA. SSL_CERT_DIR covers any OpenSSL-linked subprocess.
	absCA, _ := filepath.Abs(opts.TLSExtraCA)
	merged, err := mergeCABundles(absCA)
	if err != nil {
		fmt.Printf("  → WARN: could not prepare merged CA bundle (%v); melange may fail on TLS\n", err)
		return runTool("melange", args)
	}
	defer os.Remove(merged)

	return runToolEnv("melange", args, append(os.Environ(),
		"SSL_CERT_FILE="+merged,
		"SSL_CERT_DIR=/etc/ssl/certs",
	))
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

	arch := melangeArch(opts.Arch)
	args := []string{
		"run", "--rm",
		"--privileged",
		"--platform", archToDockerPlatform(arch),
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
		"--arch", arch,
	)

	return runTool("docker", args)
}

// runMelangeTest runs `melange test` against the already-built APK.
// melange installs the built package into a fresh sandbox and runs the test pipeline
// defined in the test: block of the melange.yaml config.
func runMelangeTest(configFile string, opts Options) error {
	if runtime.GOOS == "darwin" {
		return runMelangeTestInDocker(configFile, opts)
	}

	arch := melangeArch(opts.Arch)
	args := []string{
		"test", configFile,
		"--arch", arch,
		"--runner", "bubblewrap",
	}

	if opts.TLSExtraCA == "" {
		return runToolInDir(opts.OutputDir, "melange", args)
	}

	absCA, _ := filepath.Abs(opts.TLSExtraCA)
	merged, err := mergeCABundles(absCA)
	if err != nil {
		fmt.Printf("  → WARN: could not prepare merged CA bundle for melange test (%v)\n", err)
		return runToolInDir(opts.OutputDir, "melange", args)
	}
	defer os.Remove(merged)

	return runToolInDirEnv(opts.OutputDir, "melange", args, append(os.Environ(),
		"SSL_CERT_FILE="+merged,
		"SSL_CERT_DIR=/etc/ssl/certs",
	))
}

// runMelangeTestInDocker runs `melange test` inside the cgr.dev/chainguard/melange
// container on macOS. The output directory (containing the built APK and signing key)
// is mounted at /work/output so the test environment can install the package.
func runMelangeTestInDocker(configFile string, opts Options) error {
	fmt.Println("  → macOS detected: running melange test inside Linux container")

	absOut, err := filepath.Abs(opts.OutputDir)
	if err != nil {
		return fmt.Errorf("resolving output dir: %w", err)
	}

	containerConfig := "/work/output/" + filepath.Base(configFile)
	arch := melangeArch(opts.Arch)

	args := []string{
		"run", "--rm",
		"--privileged",
		"--platform", archToDockerPlatform(arch),
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
		"cgr.dev/chainguard/melange",
		"test", containerConfig,
		"--arch", arch,
	)

	return runTool("docker", args)
}

// copySigningKey copies src (private key) and src+".pub" (public key) to dst
// and dst+".pub" so the rest of the build can reference them by convention.
func copySigningKey(src, dst string) error {
	for _, pair := range [][2]string{{src, dst}, {src + ".pub", dst + ".pub"}} {
		data, err := os.ReadFile(pair[0])
		if err != nil {
			return fmt.Errorf("reading %s: %w", pair[0], err)
		}
		if err := os.WriteFile(pair[1], data, 0o600); err != nil {
			return fmt.Errorf("writing %s: %w", pair[1], err)
		}
	}
	fmt.Println("  → Using cluster signing key")
	return nil
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
	arch := melangeArch(opts.Arch)

	// macOS always builds a local tarball via a Linux container — apko publish
	// requires direct network access to the registry from inside the container
	// which conflicts with the docker-in-docker setup used for macOS builds.
	if runtime.GOOS == "darwin" {
		if !opts.LocalBuild {
			fmt.Println("  → macOS: apko publish not supported; building tarball only (use crane to push)")
		}
		return runApkoInDocker(configFile, imageTag, outputTar, opts)
	}

	var args []string
	if opts.LocalBuild {
		args = []string{"build", configFile, imageTag, outputTar, "--arch", arch}
	} else {
		// --sbom-path is a directory; apko publish writes sbom-<arch>.spdx.json inside it.
		args = []string{"publish", configFile, imageTag, "--arch", arch, "--sbom-path", opts.OutputDir}
	}

	if opts.TLSExtraCA == "" {
		return runToolInDir(opts.OutputDir, "apko", args)
	}

	// Corporate proxy: merge the system CA bundle with the extra CA (file or
	// directory) into a temp PEM so apko's Go TLS stack trusts it.
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

	apkoArch := melangeArch(opts.Arch)
	args := []string{
		"run", "--rm",
		"--platform", archToDockerPlatform(apkoArch),
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
		"--arch", apkoArch,
	)

	return runTool("docker", args)
}

// melangeArch returns the architecture name melange and apko expect.
// archOverride (e.g. "x86_64", "aarch64") takes precedence over the host GOARCH.
func melangeArch(archOverride string) string {
	if archOverride != "" {
		return archOverride
	}
	switch runtime.GOARCH {
	case "arm64":
		return "aarch64"
	default:
		return "x86_64"
	}
}

// archToDockerPlatform maps a melange arch name to a Docker --platform value.
func archToDockerPlatform(arch string) string {
	if arch == "aarch64" {
		return "linux/arm64"
	}
	return "linux/amd64"
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

// readCACerts reads PEM certificate data from path, which may be either a
// single PEM/CRT file or a directory. For directories every *.pem and *.crt
// file found directly inside is concatenated in sorted order.
func readCACerts(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		fmt.Printf("  → CA cert: %s (%d bytes)\n", path, len(data))
		return data, nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var buf []byte
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".pem") && !strings.HasSuffix(name, ".crt") {
			continue
		}
		full := filepath.Join(path, name)
		data, err := os.ReadFile(full)
		if err != nil {
			fmt.Printf("  → CA cert: %s (skipped: %v)\n", full, err)
			continue
		}
		fmt.Printf("  → CA cert: %s (%d bytes)\n", full, len(data))
		buf = append(buf, data...)
		buf = append(buf, '\n')
	}
	if len(buf) == 0 {
		return nil, fmt.Errorf("no .pem or .crt files found in %s", path)
	}
	return buf, nil
}

// mergeCABundles concatenates the system CA bundle with an extra CA certificate
// (file or directory) into a temp PEM file so a single SSL_CERT_FILE covers both.
func mergeCABundles(extraCAPath string) (string, error) {
	systemBundles := []string{
		"/etc/ssl/certs/ca-certificates.crt", // Debian/Ubuntu
		"/etc/pki/tls/certs/ca-bundle.crt",   // RHEL/CentOS
		"/etc/ssl/ca-bundle.pem",              // SUSE
		"/etc/ssl/cert.pem",                   // Alpine/Wolfi
	}

	var systemBundle string
	var systemCerts []byte
	for _, p := range systemBundles {
		if data, err := os.ReadFile(p); err == nil {
			systemBundle = p
			systemCerts = data
			break
		}
	}
	if systemBundle != "" {
		fmt.Printf("  → system CA bundle: %s (%d bytes)\n", systemBundle, len(systemCerts))
	} else {
		fmt.Println("  → system CA bundle: not found — merged bundle will contain extra CA only")
	}

	extraCerts, err := readCACerts(extraCAPath)
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

	fmt.Printf("  → merged CA bundle: %s (%d bytes)\n", f.Name(), len(systemCerts)+len(extraCerts))
	return f.Name(), nil
}

// sanitizeImageName lowercases s and replaces any character that is not
// [a-z0-9._-] with a hyphen, making the result safe to use as an OCI
// repository name component.
// SanitizeImageName lowercases s and replaces any character that is not
// a-z, 0-9, '.', '-', or '_' with '-', then trims leading/trailing '-' and '.'.
// This is applied to ProjectName before using it as the APK package name and
// tarball filename, so callers that need the actual filename should use it too.
func SanitizeImageName(s string) string {
	s = strings.ToLower(s)
	b := strings.Builder{}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-.")
}

// applyDefaults fills in zero-value options.
func applyDefaults(opts Options) Options {
	if opts.ProjectName == "" {
		opts.ProjectName = SanitizeImageName(filepath.Base(opts.SourceDir))
	} else {
		opts.ProjectName = SanitizeImageName(opts.ProjectName)
	}
	if opts.Version == "" {
		opts.Version = "0.0.1"
	}
	if opts.OutputDir == "" {
		opts.OutputDir = filepath.Join(opts.SourceDir, ".apexpack-output")
	}
	return opts
}
