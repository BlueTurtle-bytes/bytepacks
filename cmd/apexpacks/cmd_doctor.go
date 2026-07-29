package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

type toolCheck struct {
	name             string
	description      string
	required         bool
	macInstall       string // display hint
	linuxInstall     string // display hint
	macInstallCmd    string // shell command run by --install
	linuxInstallCmd  string // shell command run by --install
}

func doctorCmd() *cobra.Command {
	var install bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check that required tools are installed and reachable",
		Long: `Checks that all tools required by apexpacks are installed and available in PATH.

On macOS, melange and apko are installed via Homebrew. The actual build and
image assembly run inside Docker containers automatically — Docker is required.

On Linux, melange and apko must be installed natively (no Docker wrapper).

Use --install to automatically install any missing tools.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runDoctor(install)
		},
	}

	cmd.Flags().BoolVar(&install, "install", false, "Install any missing tools automatically")
	return cmd
}

func runDoctor(install bool) error {
	isMac := runtime.GOOS == "darwin"

	// Arch string for Linux release downloads (amd64 / arm64)
	linuxArch := "amd64"
	if runtime.GOARCH == "arm64" {
		linuxArch = "arm64"
	}

	tools := []toolCheck{
		{
			name:            "docker",
			description:     "Container runtime — Colima or Docker Desktop (macOS); Docker Engine (Linux)",
			required:        true,
			macInstall:      "brew install colima docker && colima start",
			linuxInstall:    "sudo apt install -y docker.io && sudo systemctl enable --now docker",
			macInstallCmd:   "brew install colima docker",
			linuxInstallCmd: "sudo apt install -y docker.io && sudo systemctl enable --now docker",
		},
		{
			name:            "melange",
			description:     "APK package builder — needed natively for key generation; build runs in Docker on macOS",
			required:        true,
			macInstall:      "brew install melange",
			linuxInstall:    fmt.Sprintf("curl -sL https://github.com/chainguard-dev/melange/releases/latest/download/melange_linux_%s.tar.gz | tar xz && sudo mv melange /usr/local/bin/", linuxArch),
			macInstallCmd:   "brew install melange",
			linuxInstallCmd: fmt.Sprintf("curl -sL https://github.com/chainguard-dev/melange/releases/latest/download/melange_linux_%s.tar.gz | tar xz && sudo mv melange /usr/local/bin/", linuxArch),
		},
		{
			name:            "apko",
			description:     "OCI image assembler — build runs in Docker on macOS, native on Linux",
			required:        true,
			macInstall:      "brew install apko",
			linuxInstall:    fmt.Sprintf("curl -sL https://github.com/chainguard-dev/apko/releases/latest/download/apko_linux_%s.tar.gz | tar xz && sudo mv apko /usr/local/bin/", linuxArch),
			macInstallCmd:   "brew install apko",
			linuxInstallCmd: fmt.Sprintf("curl -sL https://github.com/chainguard-dev/apko/releases/latest/download/apko_linux_%s.tar.gz | tar xz && sudo mv apko /usr/local/bin/", linuxArch),
		},
		{
			name:            "grype",
			description:     "CVE scanner — required for 'apexpacks scan'",
			required:        false,
			macInstall:      "brew install grype",
			linuxInstall:    "curl -sSfL https://raw.githubusercontent.com/anchore/grype/main/install.sh | sh -s -- -b /usr/local/bin",
			macInstallCmd:   "brew install grype",
			linuxInstallCmd: "curl -sSfL https://raw.githubusercontent.com/anchore/grype/main/install.sh | sh -s -- -b /usr/local/bin",
		},
	}

	fmt.Println("apexpacks doctor")
	fmt.Println()

	allOK := true
	missingRequired := false

	for _, t := range tools {
		_, err := exec.LookPath(t.name)
		missing := err != nil

		if missing && install {
			fmt.Printf("  %-10s  installing...\n", t.name)
			var installCmd string
			if isMac {
				installCmd = t.macInstallCmd
			} else {
				installCmd = t.linuxInstallCmd
			}
			if installCmd != "" {
				if runErr := runShell(installCmd); runErr != nil {
					fmt.Printf("  %-10s  ✗ install failed: %v\n\n", t.name, runErr)
					allOK = false
					if t.required {
						missingRequired = true
					}
					continue
				}
				// Re-check after install
				_, err = exec.LookPath(t.name)
				missing = err != nil
			}
		}

		var status, detail string
		ok := !missing

		switch {
		case ok:
			path, _ := exec.LookPath(t.name)
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
			hint := t.linuxInstall
			if isMac {
				hint = t.macInstall
			}
			fmt.Printf("  %-10s  Install: %s\n", "", hint)
		}
		fmt.Println()
	}

	// Docker daemon check — after tool checks so installs run first
	if _, err := exec.LookPath("docker"); err == nil {
		daemonRunning := exec.Command("docker", "info", "--format", "{{.ServerVersion}}").Run() == nil
		if !daemonRunning {
			if install {
				fmt.Println("  docker      starting daemon...")
				var startCmd string
				if isMac {
					startCmd = "colima start"
				} else {
					startCmd = "sudo systemctl start docker"
				}
				if runErr := runShell(startCmd); runErr != nil {
					fmt.Printf("  docker      ✗ could not start daemon: %v\n", runErr)
					fmt.Println("              Try starting it manually and re-run 'apexpacks doctor'")
					allOK = false
					missingRequired = true
				} else {
					fmt.Println("  docker      ✓ daemon started")
				}
			} else {
				fmt.Println("  docker      ✗ daemon not running")
				if isMac {
					fmt.Println("              colima start  # or: start Docker Desktop")
				} else {
					fmt.Println("              sudo systemctl start docker")
				}
				allOK = false
				missingRequired = true
			}
			fmt.Println()
		}
	}

	if allOK {
		fmt.Println("All checks passed. apexpacks is ready to use.")
	} else if missingRequired {
		if install {
			fmt.Println("Some tools could not be installed. See errors above.")
		} else {
			fmt.Println("Required tools missing — run 'apexpacks doctor --install' to install them.")
		}
		return fmt.Errorf("required tools missing")
	} else {
		if install {
			fmt.Println("Optional tools installed. apexpacks is ready to use.")
		} else {
			fmt.Println("Optional tools missing — 'apexpacks build' will work, but 'apexpacks scan' requires grype.")
		}
	}

	return nil
}

// runShell runs a shell command string, streaming output to stdout/stderr.
func runShell(cmd string) error {
	c := exec.Command("sh", "-c", cmd)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
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
