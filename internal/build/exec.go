package build

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

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
