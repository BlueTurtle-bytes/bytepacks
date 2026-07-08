package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/apexpack/apexpack/internal/apexctx"
	"github.com/apexpack/apexpack/internal/patch"
)

// grypeScanResult is the portion of grype's JSON output we need for counts.
type grypeScanResult struct {
	Matches []struct {
		Vulnerability struct {
			Severity string `json:"severity"`
		} `json:"vulnerability"`
	} `json:"matches"`
}

func scanCmd() *cobra.Command {
	var (
		sbomPath  string
		outputDir string
		failOn    string
		format    string
		sourceDir string
		softFail  bool
		rescan    bool
	)

	cmd := &cobra.Command{
		Use:   "scan [output-dir]",
		Short: "Scan the built image SBOM for CVEs using grype",
		Long: `Scans the SBOM produced by 'apexpacks build' for known CVEs.
Normalises SBOM version strings automatically before scanning.
Writes severity counts and scan result to .apexpack/context.json.

Examples:
  apexpacks scan
  apexpacks scan /path/to/.apexpack-output
  apexpacks scan --sbom /path/to/sbom-x86_64.spdx.json
  apexpacks scan --fail-on high
  apexpacks scan --format sarif --output results.sarif
  apexpacks scan --soft-fail    # exit 0 even on failure (for auto-patch flows)
  apexpacks scan --rescan       # write results to rescan_* fields in context.json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Resolve SBOM path: flag → context.json → conventional default.
			if sbomPath == "" {
				if absSource, serr := filepath.Abs(sourceDir); serr == nil {
					if ctxData, cerr := apexctx.Load(absSource); cerr == nil && ctxData.SBOMPath != "" {
						sbomPath = ctxData.SBOMPath
					}
				}
			}
			if sbomPath == "" {
				dir := ".apexpack-output"
				if len(args) > 0 {
					dir = args[0]
				} else if outputDir != "" {
					dir = outputDir
				}
				sbomPath = filepath.Join(dir, "sbom-x86_64.spdx.json")
			}

			if _, err := os.Stat(sbomPath); err != nil {
				return fmt.Errorf("SBOM not found at %s\n\nRun 'apexpacks build' first to produce an SBOM", sbomPath)
			}

			grypePath, err := findTool("grype")
			if err != nil {
				return fmt.Errorf("grype not found in PATH\n\nInstall: brew install grype  or  go install github.com/anchore/grype@latest")
			}

			// Update grype's CVE database before scanning so results reflect
			// current vulnerability data. Best-effort: a network failure is
			// logged but does not abort the scan (cached DB is still used).
			fmt.Println("Updating grype CVE database...")
			dbUpdateCmd := exec.Command(grypePath, "db", "update")
			dbUpdateCmd.Stdout = cmd.OutOrStdout()
			dbUpdateCmd.Stderr = cmd.ErrOrStderr()
			if err := dbUpdateCmd.Run(); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "WARN: grype db update failed (%v) — using cached DB\n", err)
			}

			// Normalise SBOM version strings so grype can correlate packages.
			normalizedPath, err := patch.NormalizeSBOMFile(sbomPath)
			if err != nil {
				return fmt.Errorf("normalising SBOM: %w", err)
			}
			defer os.Remove(normalizedPath)
			normalizedSBOM := "sbom:" + normalizedPath

			fmt.Printf("Scanning %s\n\n", sbomPath)

			// Display human-readable table.
			tableCmd := exec.Command(grypePath, normalizedSBOM, "--output", "table")
			tableCmd.Stdout = cmd.OutOrStdout()
			tableCmd.Stderr = cmd.ErrOrStderr()
			tableCmd.Run() //nolint:errcheck — table output is best-effort display only

			fmt.Println()

			// Run JSON scan for accurate per-severity counts.
			jsonTmp, err := os.CreateTemp("", "grype-*.json")
			if err != nil {
				return fmt.Errorf("creating temp file: %w", err)
			}
			jsonTmpName := jsonTmp.Name()
			jsonTmp.Close()
			defer os.Remove(jsonTmpName)

			jsonCmd := exec.Command(grypePath, normalizedSBOM,
				"--output", "json", "--file", jsonTmpName, "--quiet")
			jsonCmd.Run() //nolint:errcheck — grype exits 1 when CVEs found; we use counts instead

			var counts grypeScanResult
			if data, rerr := os.ReadFile(jsonTmpName); rerr == nil {
				json.Unmarshal(data, &counts) //nolint:errcheck
			}

			critical, high, medium, low := 0, 0, 0, 0
			for _, m := range counts.Matches {
				switch m.Vulnerability.Severity {
				case "Critical":
					critical++
				case "High":
					high++
				case "Medium":
					medium++
				case "Low":
					low++
				}
			}
			fmt.Printf("CVE counts — critical: %d  high: %d  medium: %d  low: %d\n\n",
				critical, high, medium, low)

			// Write requested format report (reuse JSON or run a second pass).
			reportPath := ""
			if outputDir != "" {
				reportPath = filepath.Join(outputDir, "scan-results."+format)
				if format == "json" {
					os.Rename(jsonTmpName, reportPath) //nolint:errcheck
				} else {
					fmtCmd := exec.Command(grypePath, normalizedSBOM,
						"--output", format, "--file", reportPath, "--quiet")
					fmtCmd.Run() //nolint:errcheck
				}
				fmt.Printf("Report: %s\n", reportPath)
			}

			// Determine pass/fail from counts.
			failed := false
			switch failOn {
			case "critical":
				failed = critical > 0
			case "high":
				failed = critical > 0 || high > 0
			case "medium":
				failed = critical > 0 || high > 0 || medium > 0
			case "low":
				failed = critical > 0 || high > 0 || medium > 0 || low > 0
			}

			result := "pass"
			if failed {
				result = "fail"
			}

			// Write context.json.
			absSource, serr := filepath.Abs(sourceDir)
			if serr == nil {
				ctx, lerr := apexctx.Load(absSource)
				if lerr == nil {
					if rescan {
						ctx.RescanResult = result
						ctx.RescanCritical = strconv.Itoa(critical)
						ctx.RescanHigh = strconv.Itoa(high)
						ctx.RescanMedium = strconv.Itoa(medium)
						ctx.RescanLow = strconv.Itoa(low)
						ctx.AppendStage("rescan")
					} else {
						ctx.ScanResult = result
						ctx.ScanFailOn = failOn
						ctx.ScanCritical = strconv.Itoa(critical)
						ctx.ScanHigh = strconv.Itoa(high)
						ctx.ScanMedium = strconv.Itoa(medium)
						ctx.ScanLow = strconv.Itoa(low)
						if reportPath != "" {
							ctx.ScanReportPath = reportPath
						}
						ctx.AppendStage("scan")
					}
					apexctx.Save(absSource, ctx) //nolint:errcheck
				}
			}

			if failed {
				label := "scan"
				if rescan {
					label = "rescan"
				}
				msg := fmt.Sprintf("%s: CVEs found at or above %q severity", label, failOn)
				if softFail {
					fmt.Printf("%s (soft-fail: pipeline will attempt auto-patch)\n", msg)
					return nil
				}
				return fmt.Errorf("%s", msg)
			}

			fmt.Printf("No CVEs found at or above %q severity.\n", failOn)
			if failOn == "" {
				fmt.Println("Tip: use --fail-on high to gate on CVE severity.")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&sbomPath, "sbom", "",
		"Path to a specific SBOM file (default: <output-dir>/sbom-x86_64.spdx.json)")
	cmd.Flags().StringVarP(&outputDir, "output", "o", "",
		"Write scan report to this directory")
	cmd.Flags().StringVar(&failOn, "fail-on", "",
		"Exit 1 if CVEs found at this severity or above: critical, high, medium, low")
	cmd.Flags().StringVar(&format, "format", "table",
		"Output format: table, json, sarif, cyclonedx")
	cmd.Flags().StringVar(&sourceDir, "source", ".",
		"Source directory containing .apexpack/context.json")
	cmd.Flags().BoolVar(&softFail, "soft-fail", false,
		"Exit 0 even when CVEs are found (use in auto-patch flows so the pipeline continues)")
	cmd.Flags().BoolVar(&rescan, "rescan", false,
		"Write results to rescan_* fields in context.json (use for the post-patch rescan)")

	return cmd
}
