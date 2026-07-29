package runner

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/apexpack/apexpack/internal/build/helpers"
	"github.com/apexpack/apexpack/internal/types"
)

// InjectHealthCheckIntoTar loads the OCI tarball into Docker, injects a
// HEALTHCHECK instruction via a new image layer, and saves it back.
func InjectHealthCheckIntoTar(tarPath, imageTag, arch string, hc *types.ApkoHealthCheck) error {
	if err := runTool("docker", []string{"load", "-i", tarPath}); err != nil {
		return fmt.Errorf("docker load: %w", err)
	}

	archTag := imageTag + "-" + melangeArchToDockerSuffix(arch)
	if err := runTool("docker", []string{"tag", archTag, imageTag}); err != nil {
		fmt.Printf("  → (re-tag %s → %s skipped: %v)\n", archTag, imageTag, err)
	}

	tmpDir, err := os.MkdirTemp("", "apexpack-hc-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	dfPath := tmpDir + "/Dockerfile"
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

// RunHealthCheckTest starts the image in Docker and runs a two-stage check:
// 1. Boot check — confirms the container stays running after 5s.
// 2. Endpoint probe — HTTP or TCP check if configured or framework has a known default.
func RunHealthCheckTest(imageTag string, hc *types.HealthCheckConfig, framework string) {
	if hc != nil && hc.Disabled {
		return
	}

	fmt.Println("\n  → Health Check Test")
	fmt.Printf("     image: %s\n", imageTag)

	var port int
	var path string
	var isTCP bool
	var probeDesc string
	var probeSource string

	fwPath, fwPort := helpers.FrameworkProbeDefaults(framework)

	if hc != nil && !hc.Disabled {
		switch {
		case hc.HTTP != nil:
			port = hc.HTTP.Port
			if port == 0 {
				if fwPort > 0 {
					port = fwPort
				} else {
					port = 8080
				}
			}
			path = hc.HTTP.Path
			if (path == "" || path == "/") && fwPath != "" && fwPath != "/" {
				path = fwPath
				probeSource = "config + " + framework + " default path"
			} else {
				if path == "" {
					path = "/"
				}
				probeSource = "config"
			}
			probeDesc = fmt.Sprintf("HTTP GET http://localhost:%d%s", port, path)
		case hc.TCP != nil:
			port = hc.TCP.Port
			isTCP = true
			probeDesc = fmt.Sprintf("TCP connect localhost:%d", port)
			probeSource = "config"
		}
	}

	if probeDesc == "" && fwPort > 0 {
		path = fwPath
		port = fwPort
		probeDesc = fmt.Sprintf("HTTP GET http://localhost:%d%s", port, path)
		probeSource = framework + " default"
	}

	ctrName := fmt.Sprintf("apexpack-hctest-%d", time.Now().UnixNano())
	runArgs := []string{"run", "-d", "--name", ctrName}
	if port > 0 {
		runArgs = append(runArgs, "-p", fmt.Sprintf("%d:%d", port, port))
	}
	runArgs = append(runArgs, imageTag)

	if err := runTool("docker", runArgs); err != nil {
		fmt.Printf("     boot:   FAILED ✗  (could not start container: %v)\n", err)
		return
	}
	defer runTool("docker", []string{"rm", "-f", ctrName}) //nolint:errcheck

	time.Sleep(5 * time.Second)

	statusOut, _ := exec.Command("docker", "inspect", "--format={{.State.Status}}", ctrName).Output()
	containerStatus := strings.TrimSpace(string(statusOut))

	fmt.Println("     --- docker output ---")
	logsCmd := exec.Command("docker", "logs", "--tail", "20", ctrName)
	logsCmd.Stdout = os.Stdout
	logsCmd.Stderr = os.Stderr
	_ = logsCmd.Run()
	fmt.Println("     --- end output ---")

	if containerStatus == "exited" {
		fmt.Println("     boot:   FAILED ✗  (container exited on startup)")
		return
	}
	fmt.Println("     boot:   PASSED ✓  (container is running)")

	if probeDesc == "" {
		return
	}
	fmt.Printf("     probe:  %s  [%s]\n", probeDesc, probeSource)

	start := time.Now()
	const deadline = 30 * time.Second
	const tick = 500 * time.Millisecond
	var lastErr error

	for time.Since(start) < deadline {
		if isTCP {
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", port), time.Second)
			if err == nil {
				conn.Close()
				fmt.Printf("     probe:  PASSED ✓  (port live in %.1fs)\n", time.Since(start).Seconds())
				return
			}
			lastErr = err
		} else {
			resp, err := http.Get(fmt.Sprintf("http://localhost:%d%s", port, path)) //nolint:noctx
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					fmt.Printf("     probe:  PASSED ✓  (HTTP %d in %.1fs)\n", resp.StatusCode, time.Since(start).Seconds())
					return
				}
				lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			} else {
				lastErr = err
			}
		}
		time.Sleep(tick)
	}

	fmt.Printf("     probe:  FAILED ✗  (no response after 30s: %v)\n", lastErr)
}

func melangeArchToDockerSuffix(arch string) string {
	if arch == "aarch64" {
		return "arm64"
	}
	return "amd64"
}

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
	if len(hc.Command) > 1 {
		sb.WriteString(" " + hc.Command[0] + " " + strings.Join(hc.Command[1:], " "))
	}
	sb.WriteString("\n")
	return sb.String()
}
