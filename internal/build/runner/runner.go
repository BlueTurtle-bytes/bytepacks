package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/apexpack/apexpack/internal/build/helpers"
	"github.com/apexpack/apexpack/internal/types"
)

// RunMelange runs melange build, choosing native or Docker path based on OS.
func RunMelange(configFile string, opts types.BuildOptions) error {
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

	absCA, _ := filepath.Abs(opts.TLSExtraCA)
	merged, err := mergeCABundles(absCA)
	if err != nil {
		fmt.Printf("  → WARN: could not prepare merged CA bundle (%v); melange may fail on TLS\n", err)
		return runTool("melange", args)
	}
	defer os.Remove(merged)

	return runToolEnv("melange", args, envWithOverrides(os.Environ(),
		"SSL_CERT_FILE="+merged,
		"SSL_CERT_DIR=/etc/ssl/certs",
	))
}

func runMelangeInDocker(configFile, keyFile string, opts types.BuildOptions) error {
	fmt.Println("  → macOS detected: running melange inside Linux container")

	absSrc, err := filepath.Abs(opts.SourceDir)
	if err != nil {
		return fmt.Errorf("resolving source dir: %w", err)
	}
	absOut, err := filepath.Abs(opts.OutputDir)
	if err != nil {
		return fmt.Errorf("resolving output dir: %w", err)
	}

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

	for _, cachePath := range opts.Profile.Build.Caches {
		args = append(args, "-v", helpers.CacheVolumeName(cachePath)+":"+cachePath)
	}
	if override, found := helpers.ResolveOverride(opts.Profile, opts.Framework, opts.PackageManager); found {
		for _, cachePath := range override.Caches {
			args = append(args, "-v", helpers.CacheVolumeName(cachePath)+":"+cachePath)
		}
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

	for _, key := range []string{
		"GOPROXY", "GONOSUMDB", "GONOSUMCHECK", "GOINSECURE", "GOPRIVATE",
		"GOFLAGS", "SSL_CERT_FILE", "SSL_CERT_DIR",
	} {
		if val := os.Getenv(key); val != "" {
			args = append(args, "-e", key+"="+val)
		}
	}

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

// RunMelangeTest runs melange test against the already-built APK.
func RunMelangeTest(configFile string, opts types.BuildOptions) error {
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

	return runToolInDirEnv(opts.OutputDir, "melange", args, envWithOverrides(os.Environ(),
		"SSL_CERT_FILE="+merged,
		"SSL_CERT_DIR=/etc/ssl/certs",
	))
}

func runMelangeTestInDocker(configFile string, opts types.BuildOptions) error {
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

// RunApko runs apko build or publish depending on opts.LocalBuild.
func RunApko(configFile string, opts types.BuildOptions) error {
	imageTag := opts.Tag
	if imageTag == "" {
		imageTag = opts.ProjectName + ":latest"
	}
	outputTar := filepath.Join(opts.OutputDir, opts.ProjectName+".tar")
	arch := melangeArch(opts.Arch)

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
		args = []string{"publish", configFile, imageTag, "--arch", arch, "--sbom-path", opts.OutputDir}
	}
	for k, v := range opts.BuildArgs {
		args = append(args, "--image-annotation", "dev.apexpack.build."+k+"="+v)
	}

	if opts.TLSExtraCA == "" {
		return runToolInDir(opts.OutputDir, "apko", args)
	}

	absCA, _ := filepath.Abs(opts.TLSExtraCA)
	merged, err := mergeCABundles(absCA)
	if err != nil {
		fmt.Printf("  → WARN: could not prepare merged CA bundle (%v); apko may fail on TLS\n", err)
		return runToolInDir(opts.OutputDir, "apko", args)
	}
	defer os.Remove(merged)

	return runToolInDirEnv(opts.OutputDir, "apko", args, envWithOverrides(os.Environ(),
		"SSL_CERT_FILE="+merged,
		"SSL_CERT_DIR=/etc/ssl/certs",
	))
}

func runApkoInDocker(configFile, imageTag, outputTar string, opts types.BuildOptions) error {
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

	apkoArgs := []string{
		"build", containerConfig,
		imageTag,
		containerTar,
		"--arch", apkoArch,
	}
	for k, v := range opts.BuildArgs {
		apkoArgs = append(apkoArgs, "--image-annotation", "dev.apexpack.build."+k+"="+v)
	}
	args = append(args, "cgr.dev/chainguard/apko")
	args = append(args, apkoArgs...)

	return runTool("docker", args)
}

// MelangeArch returns the melange arch string — exported for use by build.go.
func MelangeArch(archOverride string) string {
	return melangeArch(archOverride)
}

// ReadCACerts reads PEM certificate data from path — exported for use by build.go.
func ReadCACerts(path string) ([]byte, error) {
	return readCACerts(path)
}

func ensureSigningKey(keyFile string) error {
	if _, err := os.Stat(keyFile); err == nil {
		return nil
	}
	fmt.Println("  → Generating melange signing key...")
	return runTool("melange", []string{"keygen", keyFile})
}

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
