package build

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/apexpack/apexpack/internal/types"
)

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

	runner := opts.MelangeRunner
	if runner == "" {
		runner = "bubblewrap"
	}
	args := []string{
		"build", configFile,
		"--source-dir", opts.SourceDir,
		"--out-dir", packagesDir,
		"--signing-key", keyFile,
		"--arch", arch,
		"--runner", runner,
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

	runner := opts.MelangeRunner
	if runner == "" {
		runner = "bubblewrap"
	}
	arch := melangeArch(opts.Arch)
	args := []string{
		"test", configFile,
		"--arch", arch,
		"--runner", runner,
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

// ensureSigningKey generates a melange RSA key pair at keyFile if one does
// not already exist. Reused across builds in the same output directory.
func ensureSigningKey(keyFile string) error {
	if _, err := os.Stat(keyFile); err == nil {
		return nil
	}
	fmt.Println("  → Generating melange signing key...")
	return runTool("melange", []string{"keygen", keyFile})
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

// injectHealthCheckIntoTar loads the apko-built OCI tarball into Docker,
// builds a new image layer with a HEALTHCHECK instruction, and saves it back.
// apko's YAML schema has no healthcheck field, so this is the injection path.
func injectHealthCheckIntoTar(tarPath, imageTag string, hc *types.ApkoHealthCheck) error {
	if err := runTool("docker", []string{"load", "-i", tarPath}); err != nil {
		return fmt.Errorf("docker load: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "apexpack-hc-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	dfPath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dfPath, []byte(buildDockerfileWithHealthCheck(imageTag, hc)), 0o644); err != nil {
		return fmt.Errorf("writing Dockerfile: %w", err)
	}

	if err := runTool("docker", []string{"build", "--no-cache", "-t", imageTag, "-f", dfPath, tmpDir}); err != nil {
		return fmt.Errorf("docker build: %w", err)
	}

	if err := runTool("docker", []string{"save", "-o", tarPath, imageTag}); err != nil {
		return fmt.Errorf("docker save: %w", err)
	}

	return nil
}

// buildDockerfileWithHealthCheck generates a single-layer Dockerfile that adds
// an OCI HEALTHCHECK on top of the already-built image.
func buildDockerfileWithHealthCheck(imageTag string, hc *types.ApkoHealthCheck) string {
	var sb strings.Builder
	sb.WriteString("FROM " + imageTag + "\n")
	sb.WriteString("HEALTHCHECK")
	if hc.Interval != "" {
		sb.WriteString(" --interval=" + hc.Interval)
	}
	if hc.Timeout != "" {
		sb.WriteString(" --timeout=" + hc.Timeout)
	}
	if hc.StartPeriod != "" {
		sb.WriteString(" --start-period=" + hc.StartPeriod)
	}
	if hc.Retries > 0 {
		sb.WriteString(fmt.Sprintf(" --retries=%d", hc.Retries))
	}
	// hc.Command[0] is "CMD" or "CMD-SHELL"; remaining elements are the command.
	if len(hc.Command) > 1 {
		sb.WriteString(" " + hc.Command[0] + " " + strings.Join(hc.Command[1:], " "))
	}
	sb.WriteString("\n")
	return sb.String()
}
