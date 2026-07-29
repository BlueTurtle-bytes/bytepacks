package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

type toolCheck struct {
	name         string
	description  string
	required     bool // false = optional
	macInstall   string
	linuxInstall string
}

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check that required tools are installed and reachable",
		Long: `Checks that all tools required by apexpacks are installed and available in PATH.

On macOS, melange and apko are installed via Homebrew. The actual build and
image assembly run inside Docker containers automatically — Docker is required.

On Linux, melange and apko must be installed natively (no Docker wrapper).`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runDoctor()
		},
	}
}

func runDoctor() error {
	isMac := runtime.GOOS == "darwin"

	tools := []toolCheck{
		{
			name:         "docker",
			description:  "Container runtime — Docker Desktop or Colima (macOS); Docker Engine (Linux)",
			required:     true,
			macInstall:   "brew install colima docker && colima start  # or: Docker Desktop https://www.docker.com/products/docker-desktop/",
			linuxInstall: "sudo apt install docker.io  # or: https://docs.docker.com/engine/install/",
		},
		{
			name:         "melange",
			description:  "APK package builder — needed natively for key generation; build runs in Docker on macOS",
			required:     true,
			macInstall:   "brew install melange",
			linuxInstall: "curl -sL https://github.com/chainguard-dev/melange/releases/latest/download/melange_linux_amd64.tar.gz | tar xz && sudo mv melange /usr/local/bin/",
		},
		{
			name:         "apko",
			description:  "OCI image assembler — build runs in Docker on macOS, native on Linux",
			required:     true,
			macInstall:   "brew install apko",
			linuxInstall: "curl -sL https://github.com/chainguard-dev/apko/releases/latest/download/apko_linux_amd64.tar.gz | tar xz && sudo mv apko /usr/local/bin/",
		},
		{
			name:         "grype",
			description:  "CVE scanner — required for 'apexpacks scan'",
			required:     false,
			macInstall:   "brew install grype",
			linuxInstall: "curl -sSfL https://raw.githubusercontent.com/anchore/grype/main/install.sh | sh -s -- -b /usr/local/bin",
		},
	}

	fmt.Println("apexpacks doctor")
	fmt.Println()

	allOK := true
	missingRequired := false

	for _, t := range tools {
		path, err := exec.LookPath(t.name)

		var status, detail string
		ok := err == nil

		switch {
		case ok:
			ver := toolVersion(t.name)
			status = "✓ OK"
			if ver != "" {
				detail = fmt.Sprintf("%s  (%s)", path, ver)
			} else {
				detail = path
			}
		case t.required:
			status = "✗ MISSING (required)"
			allOK = false
			missingRequired = true
		default:
			status = "– missing (optional)"
			allOK = false
		}

		fmt.Printf("  %-10s  %s\n", t.name, status)
		if ok {
			fmt.Printf("  %-10s  %s\n", "", detail)
		}
		fmt.Printf("  %-10s  %s\n", "", t.description)
		if !ok {
			if isMac {
				fmt.Printf("  %-10s  Install: %s\n", "", t.macInstall)
			} else {
				fmt.Printf("  %-10s  Install: %s\n", "", t.linuxInstall)
			}
		}
		fmt.Println()
	}

	// Docker daemon check (separate from binary presence check)
	if _, err := exec.LookPath("docker"); err == nil {
		if err := exec.Command("docker", "info", "--format", "{{.ServerVersion}}").Run(); err != nil {
			fmt.Println("  docker      ✗ daemon not running")
			if isMac {
				fmt.Println("              colima start  # or: start Docker Desktop from your Applications folder")
			} else {
				fmt.Println("              sudo systemctl start docker")
			}
			fmt.Println()
			allOK = false
			missingRequired = true
		}
	}

	if allOK {
		fmt.Println("All checks passed. apexpacks is ready to use.")
	} else if missingRequired {
		fmt.Println("One or more required tools are missing. Install them and re-run 'apexpacks doctor'.")
		return fmt.Errorf("required tools missing")
	} else {
		fmt.Println("Optional tools missing — 'apexpacks build' will work, but 'apexpacks scan' requires grype.")
	}

	return nil
}

// toolVersion returns a short version string for known tools, or empty string on failure.
func toolVersion(name string) string {
	switch name {
	case "docker":
		out, err := exec.Command("docker", "--version").Output()
		if err != nil {
			return ""
		}
		return firstLine(string(out))

	case "melange", "apko":
		// These print an ASCII banner then "GitVersion: v0.x.y"
		out, err := exec.Command(name, "version").Output()
		if err != nil {
			return ""
		}
		for _, line := range splitLines(string(out)) {
			if len(line) > 11 && line[:11] == "GitVersion:" {
				return strings.TrimSpace(line[11:])
			}
		}
		return ""

	case "grype":
		// "Version:             0.112.0"
		out, err := exec.Command("grype", "version").Output()
		if err != nil {
			return ""
		}
		for _, line := range splitLines(string(out)) {
			if len(line) > 8 && line[:8] == "Version:" {
				return strings.TrimSpace(line[8:])
			}
		}
		return ""
	}
	return ""
}

func firstLine(s string) string {
	for i, c := range s {
		if c == '\n' {
			return strings.TrimSpace(s[:i])
		}
	}
	return strings.TrimSpace(s)
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
