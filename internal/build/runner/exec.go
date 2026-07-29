// Package runner executes external build tools (melange, apko, docker).
package runner

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// melangeArch returns the architecture name melange and apko expect.
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
