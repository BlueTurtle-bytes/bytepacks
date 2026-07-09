// Package build generates melange and apko config structs from a profile,
// marshals them to YAML, writes them to disk, then runs the tools.
package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/apexpack/apexpack/internal/types"
)

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

	// MelangeRunner selects the sandbox backend melange uses for builds.
	// Accepted values: "bubblewrap" (default on Linux), "docker", "qemu".
	// Use "docker" when bubblewrap user namespaces are unavailable (e.g. restricted
	// Kubernetes pods) and a Docker socket is accessible in the build environment.
	MelangeRunner string

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
	apkoCfg, err := buildApkoConfig(p, opts)
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
		Apko:           apkoCfg,
	}, nil
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

	// Strip world-write from all staged files before melange's worldwrite linter
	// runs. Build tools (Maven, Gradle, dotnet publish) produce 0666 files when
	// running inside Docker where the default umask is 0000.
	plan.Melange.Pipeline = append(plan.Melange.Pipeline, types.MelangePipeline{
		Runs: `find "${{targets.destdir}}" -type f -perm /0002 -exec chmod o-w {} \;`,
	})
	melangeYAML, err = marshalYAML(&plan.Melange)
	if err != nil {
		return fmt.Errorf("marshalling melange config (perm fix): %w", err)
	}
	if err := os.WriteFile(melangeFile, []byte(melangeYAML), 0o644); err != nil {
		return fmt.Errorf("writing melange.yaml (perm fix): %w", err)
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
