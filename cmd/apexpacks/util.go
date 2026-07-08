package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/apexpack/apexpack/internal/profile"
)

// resolveProfilesDir returns dir unchanged when it exists on disk.
// When dir is the default relative "profiles" and it doesn't exist, it falls
// back to the path where the apexpack image installs bundled profiles.
func resolveProfilesDir(dir string) string {
	if _, err := os.Stat(dir); err == nil {
		return dir
	}
	if dir == profile.DefaultProfilesDir {
		const bundled = "/etc/apexpack/profiles"
		if _, err := os.Stat(bundled); err == nil {
			return bundled
		}
	}
	return dir
}

// findTool looks for a binary in PATH.
func findTool(name string) (string, error) {
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("%s not found in PATH", name)
}

// buildArch returns the melange/apko architecture name for the given override
// (empty string means use the host architecture).
func buildArch(archOverride string) string {
	if archOverride != "" {
		return archOverride
	}
	if runtime.GOARCH == "arm64" {
		return "aarch64"
	}
	return "x86_64"
}

// hostArch returns the host arch string and its .NET RID equivalent.
func hostArch() (arch, rid string) {
	switch runtime.GOARCH {
	case "arm64":
		return "aarch64", "linux-arm64"
	default:
		return "x86_64", "linux-x64"
	}
}

// gitRemoteURL returns the origin remote URL or empty string on any error.
func gitRemoteURL(dir string) string {
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitCurrentBranch returns the current branch name or empty string on any error.
func gitCurrentBranch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitHeadCommit returns the full HEAD commit SHA or empty string on any error.
func gitHeadCommit(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
