// Package build generates melange and apko config structs from a profile,
// marshals them to YAML, writes them to disk, then runs the tools.
package build

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"

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
}

// Plan builds a MelangeConfig and ApkoConfig from the profile and options.
// The profile has already been merged with per-project apexpack.yaml overrides
// by the caller (main.go). Does NOT write files or run tools.
func Plan(p *types.Profile, opts Options) (*types.BuildPlan, error) {
	opts = applyDefaults(opts)

	return &types.BuildPlan{
		ProjectName: opts.ProjectName,
		Version:     opts.Version,
		Profile:     p,
		Framework:   opts.Framework,
		Melange:     buildMelangeConfig(p, opts),
		Apko:        buildApkoConfig(p, opts),
	}, nil
}

// Run writes melange.yaml and apko.yaml to disk, then runs the tools.
func Run(plan *types.BuildPlan, opts Options) error {
	opts = applyDefaults(opts)

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
func buildMelangeConfig(p *types.Profile, opts Options) types.MelangeConfig {
	// Build-time packages: always start with wolfi-baselayout, add profile deps.
	packages := append(
		[]string{"wolfi-baselayout"},
		p.Build.Dependencies...,
	)

	cfg := types.MelangeConfig{
		Package: types.MelangePackage{
			Name:        opts.ProjectName,
			Version:     opts.Version,
			Epoch:       0,
			Description: fmt.Sprintf("Built by apexpack (%s)", p.Runtime),
			Copyright: []types.MelangeCopyright{
				{License: "Apache-2.0"},
			},
		},
		Environment: types.MelangeEnvironment{
			Contents: types.MelangeContents{
				Keyring:      []string{"https://packages.wolfi.dev/os/wolfi-signing.rsa.pub"},
				Repositories: []string{"https://packages.wolfi.dev/os"},
				Packages:     packages,
			},
			// Build-time environment variables from the profile.
			Env: p.Build.Env,
		},
		Pipeline: []types.MelangePipeline{
			{Runs: p.Build.Command},
		},
	}

	// Apply framework-specific overrides. Only set fields replace their defaults.
	if opts.Framework != "" {
		if override, ok := p.Build.Frameworks[opts.Framework]; ok {
			if len(override.Dependencies) > 0 {
				cfg.Environment.Contents.Packages = append(
					[]string{"wolfi-baselayout"},
					override.Dependencies...,
				)
			}
			if override.Command != "" {
				cfg.Pipeline = []types.MelangePipeline{{Runs: override.Command}}
			}
			for k, v := range override.Env {
				if cfg.Environment.Env == nil {
					cfg.Environment.Env = make(map[string]string)
				}
				cfg.Environment.Env[k] = v
			}
		}
	}

	return cfg
}

// buildApkoConfig constructs an ApkoConfig from a profile and options.
// The struct is later marshalled to YAML by yaml.Marshal.
func buildApkoConfig(p *types.Profile, opts Options) types.ApkoConfig {
	// Runtime packages: wolfi-baselayout + the built APK + profile image packages.
	packages := append(
		[]string{"wolfi-baselayout", opts.ProjectName},
		p.Image.Packages...,
	)

	// Default run-as UID is 65532 (Wolfi nonroot convention).
	runAs := p.Image.RunAs
	if runAs == 0 {
		runAs = 65532
	}

	cfg := types.ApkoConfig{
		Contents: types.ApkoContents{
			// Relative paths are resolved from the apko.yaml file location
			// (the output directory). This works both when apko runs on the
			// host and when it runs inside a Docker container with the output
			// directory mounted at a different absolute path.
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
		Entrypoint: types.ApkoEntrypoint{
			Command: p.Image.Entrypoint,
		},
		Accounts: types.ApkoAccounts{
			RunAs: fmt.Sprintf("%d", runAs),
			Users: []types.ApkoUser{
				{Username: "nonroot", UID: runAs, GID: runAs},
			},
			Groups: []types.ApkoGroup{
				{Groupname: "nonroot", GID: runAs},
			},
		},
		// Runtime environment variables from the profile.
		Environment: p.Image.Env,
	}

	// Optional CMD — space-joined string because apko takes cmd as a string.
	if len(p.Image.Cmd) > 0 {
		cfg.Cmd = strings.Join(p.Image.Cmd, " ")
	}

	return cfg
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

	return runTool("melange", []string{
		"build", configFile,
		"--source-dir", opts.SourceDir,
		"--out-dir", packagesDir,
		"--signing-key", keyFile,
		"--arch", "x86_64",
	})
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
		// --privileged lets melange use bubblewrap inside the Linux container.
		// This is safe for local development on Docker Desktop.
		"--privileged",
		"-v", absSrc + ":/work/src:ro",
		"-v", absOut + ":/work/output",
		"cgr.dev/chainguard/melange",
		"build", containerConfig,
		"--source-dir", "/work/src",
		"--out-dir", "/work/output/packages",
		"--signing-key", containerKey,
		"--arch", "x86_64",
	}

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

	return runTool("apko", []string{
		"build", configFile,
		imageTag,
		outputTar,
		"--arch", "x86_64",
	})
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
		"-w", "/work/output", // set CWD so ./melange.rsa.pub and ./packages resolve correctly
		"-v", absOut + ":/work/output",
		"cgr.dev/chainguard/apko",
		"build", containerConfig,
		imageTag,
		containerTar,
		"--arch", "x86_64",
	}

	return runTool("docker", args)
}

// runTool runs an external binary, streaming output to stdout/stderr.
func runTool(name string, args []string) error {
	path, err := exec.LookPath(name)
	if err != nil {
		home, _ := os.UserHomeDir()
		fallback := filepath.Join(home, ".nimbopacks", "toolchain", "bin", name)
		if _, statErr := os.Stat(fallback); statErr == nil {
			path = fallback
		} else {
			return fmt.Errorf("%s not found in PATH — run: nimbopacks toolchain install", name)
		}
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
