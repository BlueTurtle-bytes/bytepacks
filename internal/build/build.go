// Package build generates melange and apko configs from a language profile,
// applies language hooks, writes the configs to disk, and runs the tools.
package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/apexpack/apexpack/internal/build/config"
	"github.com/apexpack/apexpack/internal/build/hooks"
	"github.com/apexpack/apexpack/internal/build/probes"
	"github.com/apexpack/apexpack/internal/build/runner"
	"github.com/apexpack/apexpack/internal/build/helpers"
	"github.com/apexpack/apexpack/internal/types"
)

// Options is an alias for types.BuildOptions for callers that import this package.
type Options = types.BuildOptions

// Plan builds a MelangeConfig and ApkoConfig from the profile and options.
// Does NOT write files or run tools.
func Plan(p *types.Profile, opts Options) (*types.BuildPlan, error) {
	opts = applyDefaults(opts)

	melangeCfg, err := config.BuildMelangeConfig(p, opts)
	if err != nil {
		return nil, err
	}

	// Apply language-specific melange patches.
	if hook, ok := hooks.Get(p.Runtime); ok {
		if err := hook.PatchMelange(&melangeCfg, p, opts); err != nil {
			return nil, err
		}
	}

	apkoCfg, apkoHC, err := config.BuildApkoConfig(p, opts)
	if err != nil {
		return nil, err
	}

	// Apply language-specific apko patches.
	if hook, ok := hooks.Get(p.Runtime); ok {
		if err := hook.PatchApko(&apkoCfg, p, opts); err != nil {
			return nil, err
		}
	}

	// Wrap the real entrypoint with the runtime CA script after all hooks have run,
	// so hook-patched entrypoints (e.g. Java's java-1.8 path) are captured correctly.
	// The wrapper is transparent when APEXPACK_RUNTIME_CA is not set (exec "$@").
	if apkoCfg.Entrypoint.Command != "" {
		realCmd := apkoCfg.Entrypoint.Command
		if apkoCfg.Cmd != "" {
			realCmd = realCmd + " " + apkoCfg.Cmd
		}
		apkoCfg.Entrypoint.Command = "/usr/bin/apexpack-entrypoint"
		apkoCfg.Cmd = realCmd

		wrapperScript := buildRuntimeCAWrapper(p)
		melangeCfg.Pipeline = append(melangeCfg.Pipeline, types.MelangePipeline{
			Runs: `mkdir -p "${{targets.destdir}}/usr/bin"
cat > "${{targets.destdir}}/usr/bin/apexpack-entrypoint" << 'APEXPACK_ENTRYPOINT_EOF'
` + wrapperScript + `APEXPACK_ENTRYPOINT_EOF
chmod +x "${{targets.destdir}}/usr/bin/apexpack-entrypoint"`,
		})
	}

	return &types.BuildPlan{
		ProjectName:    opts.ProjectName,
		Version:        opts.Version,
		Profile:        p,
		Framework:      opts.Framework,
		PackageManager: opts.PackageManager,
		ProcfileCmd:    helpers.ReadProcfileCmd(opts.SourceDir),
		Melange:        melangeCfg,
		Apko:           apkoCfg,
		HealthCheck:    apkoHC,
	}, nil
}

// buildRuntimeCAWrapper returns a sh script baked into the image as
// /usr/bin/apexpack-entrypoint. When APEXPACK_RUNTIME_CA (base64 PEM) is set
// at container startup it decodes the cert, merges it with the system bundle,
// exports SSL_CERT_FILE (and any profile-specific vars), runs the optional
// pre-exec snippet, then execs the real command passed as "$@".
func buildRuntimeCAWrapper(p *types.Profile) string {
	var extraEnv string
	for _, key := range p.Build.TLSRuntimeCAEnv {
		extraEnv += "  export " + key + "=/tmp/apexpack-runtime-ca.pem\n"
	}
	preExec := ""
	if p.Build.TLSRuntimeCAPreExec != "" {
		preExec = p.Build.TLSRuntimeCAPreExec
	}
	return `#!/bin/sh
set -e
if [ -n "$APEXPACK_RUNTIME_CA" ]; then
  printf '%s' "$APEXPACK_RUNTIME_CA" | base64 -d > /tmp/apexpack-runtime-ca.pem
  if [ -f /etc/ssl/certs/ca-certificates.crt ]; then
    cat /etc/ssl/certs/ca-certificates.crt /tmp/apexpack-runtime-ca.pem > /tmp/apexpack-ca-bundle.pem
  else
    cp /tmp/apexpack-runtime-ca.pem /tmp/apexpack-ca-bundle.pem
  fi
  export SSL_CERT_FILE=/tmp/apexpack-ca-bundle.pem
` + extraEnv + preExec + `fi
exec "$@"
`
}

// Run writes melange.yaml and apko.yaml to disk then runs the tools.
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

	melangeYAML, err := config.MarshalYAML(&plan.Melange)
	if err != nil {
		return fmt.Errorf("marshalling melange config: %w", err)
	}
	melangeData := []byte(melangeYAML)

	apkoYAML, err := config.MarshalYAML(&plan.Apko)
	if err != nil {
		return fmt.Errorf("marshalling apko config: %w", err)
	}
	apkoData := []byte(apkoYAML)

	melangeFile := filepath.Join(opts.OutputDir, "melange.yaml")
	if err := os.WriteFile(melangeFile, melangeData, 0o644); err != nil {
		return fmt.Errorf("writing melange.yaml: %w", err)
	}
	fmt.Printf("  → wrote %s\n", melangeFile)

	apkoFile := filepath.Join(opts.OutputDir, "apko.yaml")
	if err := os.WriteFile(apkoFile, apkoData, 0o644); err != nil {
		return fmt.Errorf("writing apko.yaml: %w", err)
	}
	fmt.Printf("  → wrote %s\n", apkoFile)

	if opts.TLSExtraCA != "" {
		absCA, _ := filepath.Abs(opts.TLSExtraCA)
		caCopyPath := filepath.Join(opts.SourceDir, ".apexpack-ca.crt")
		if caData, readErr := runner.ReadCACerts(absCA); readErr == nil {
			if writeErr := os.WriteFile(caCopyPath, caData, 0o644); writeErr == nil {
				defer os.Remove(caCopyPath)
				pipelineModified := false

				if opts.Profile != nil && opts.Profile.Build.TLSCAPreStep != "" {
					plan.Melange.Pipeline = append(
						[]types.MelangePipeline{{Runs: opts.Profile.Build.TLSCAPreStep}},
						plan.Melange.Pipeline...,
					)
					pipelineModified = true
				}

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

				filtered := plan.Apko.Contents.Packages[:0]
				for _, pkg := range plan.Apko.Contents.Packages {
					if !strings.HasPrefix(pkg, "ca-certificates-bundle") {
						filtered = append(filtered, pkg)
					}
				}
				plan.Apko.Contents.Packages = filtered
				apkoYAML, err = config.MarshalYAML(&plan.Apko)
				if err != nil {
					return fmt.Errorf("marshalling apko config (with CA): %w", err)
				}
				apkoData = []byte(apkoYAML)
				if err := os.WriteFile(apkoFile, apkoData, 0o644); err != nil {
					return fmt.Errorf("writing apko.yaml (with CA): %w", err)
				}

				if pipelineModified {
					melangeYAML, err = config.MarshalYAML(&plan.Melange)
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

	plan.Melange.Pipeline = append(plan.Melange.Pipeline, types.MelangePipeline{
		Runs: `find "${{targets.destdir}}" -type f -perm /0002 -exec chmod o-w {} \;`,
	})
	melangeYAML, err = config.MarshalYAML(&plan.Melange)
	if err != nil {
		return fmt.Errorf("marshalling melange config (perm fix): %w", err)
	}
	if err := os.WriteFile(melangeFile, []byte(melangeYAML), 0o644); err != nil {
		return fmt.Errorf("writing melange.yaml (perm fix): %w", err)
	}

	fmt.Println("\n  → Running melange...")
	if err := runner.RunMelange(melangeFile, opts); err != nil {
		return fmt.Errorf("melange: %w", err)
	}

	if plan.Melange.Test != nil {
		fmt.Println("\n  → Running melange test...")
		if err := runner.RunMelangeTest(melangeFile, opts); err != nil {
			return fmt.Errorf("melange test: %w", err)
		}
	}

	imageTag := opts.Tag
	if imageTag == "" {
		imageTag = opts.ProjectName + ":latest"
	}

	fmt.Println("\n  → Running apko...")
	if err := runner.RunApko(apkoFile, opts); err != nil {
		return fmt.Errorf("apko: %w", err)
	}

	if opts.LocalBuild {
		if plan.HealthCheck != nil {
			arch := runner.MelangeArch(opts.Arch)
			outputTar := filepath.Join(opts.OutputDir, opts.ProjectName+".tar")
			fmt.Println("\n  → Injecting OCI HEALTHCHECK...")
			if err := runner.InjectHealthCheckIntoTar(outputTar, imageTag, arch, plan.HealthCheck); err != nil {
				fmt.Printf("  → WARN: healthcheck injection failed: %v\n", err)
			}
		}
		runner.RunHealthCheckTest(imageTag, plan.Profile.Image.HealthCheck, opts.Framework)
	}

	probes.EmitProbesYAML(opts, plan.Profile.Image.HealthCheck)

	return nil
}

// MarshalMelange returns the melange config as a YAML string (for --dry-run).
func MarshalMelange(plan *types.BuildPlan) (string, error) {
	return config.MarshalYAML(&plan.Melange)
}

// MarshalApko returns the apko config as a YAML string (for --dry-run).
func MarshalApko(plan *types.BuildPlan) (string, error) {
	return config.MarshalYAML(&plan.Apko)
}

// SanitizeImageName is re-exported for cmd/ callers.
func SanitizeImageName(s string) string {
	return helpers.SanitizeImageName(s)
}

func applyDefaults(opts Options) Options {
	if opts.ProjectName == "" {
		opts.ProjectName = helpers.SanitizeImageName(filepath.Base(opts.SourceDir))
	} else {
		opts.ProjectName = helpers.SanitizeImageName(opts.ProjectName)
	}
	if opts.Version == "" {
		opts.Version = "0.0.1"
	}
	if opts.OutputDir == "" {
		opts.OutputDir = filepath.Join(opts.SourceDir, ".apexpack-output")
	}
	return opts
}
