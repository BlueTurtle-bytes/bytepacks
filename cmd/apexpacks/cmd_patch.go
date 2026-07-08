package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/apexpack/apexpack/internal/apexctx"
	"github.com/apexpack/apexpack/internal/patch"
	"github.com/apexpack/apexpack/internal/profile"
)

func patchCmd() *cobra.Command {
	var (
		sbomPath    string
		profilesDir string
		apply       bool
		arch        string
		runtime_    string
		sourceDir   string
	)

	cmd := &cobra.Command{
		Use:   "patch [output-dir]",
		Short: "Check for package updates and CVEs in installed packages",
		Long: `Compares installed package versions (from the last build SBOM) against
the latest versions in the Wolfi package index. Cross-references with
grype to identify which outdated packages have known CVEs.

With --apply, updates apexpacks.yaml in the project root to pin the
patched package versions. Writes patch results to .apexpack/context.json.

Examples:
  apexpacks patch
  apexpacks patch /path/to/.apexpack-output
  apexpacks patch --apply
  apexpacks build .`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			absSource, err := filepath.Abs(sourceDir)
			if err != nil {
				return fmt.Errorf("resolving source dir: %w", err)
			}

			// Read sbom_path, runtime, and arch from context.json when not given as flags.
			if ctxData, cerr := apexctx.Load(absSource); cerr == nil {
				if sbomPath == "" && ctxData.SBOMPath != "" {
					sbomPath = ctxData.SBOMPath
				}
				if runtime_ == "" && ctxData.Runtime != "" {
					runtime_ = ctxData.Runtime
				}
				if arch == "" && ctxData.Arch != "" {
					arch = ctxData.Arch
				}
			}
			if arch == "" {
				arch, _ = hostArch()
			}

			if sbomPath == "" {
				dir := ".apexpack-output"
				if len(args) > 0 {
					dir = args[0]
				}
				sbomPath = filepath.Join(dir, "sbom-x86_64.spdx.json")
			}

			if _, err := os.Stat(sbomPath); err != nil {
				return fmt.Errorf("SBOM not found at %s\n\nRun 'apexpacks build' first", sbomPath)
			}

			fmt.Println("⚡ apexpacks patch")
			fmt.Printf("\n[1/2] Checking packages against Wolfi index...\n")

			result, err := patch.Check(sbomPath, arch)
			if err != nil {
				return err
			}

			if len(result.Updates) == 0 {
				fmt.Println("\n✓ All packages are up to date. No patches needed.")
				return nil
			}

			fmt.Printf("\n  %-35s %-20s %-20s %s\n", "PACKAGE", "INSTALLED", "LATEST", "CVEs")
			fmt.Printf("  %s\n", strings.Repeat("─", 90))
			for _, u := range result.Updates {
				cveStr := strings.Join(u.CVEs, ", ")
				if cveStr == "" {
					cveStr = "-"
				}
				marker := "  "
				if len(u.CVEs) > 0 {
					switch u.Severity {
					case "Critical":
						marker = "🔴"
					case "High":
						marker = "🟠"
					case "Medium":
						marker = "🟡"
					default:
						marker = "🔵"
					}
				} else if u.NeedsUpdate {
					marker = "↑ "
				}
				fmt.Printf("%s %-33s %-20s %-20s %s\n",
					marker, u.Name, u.CurrentVersion, u.LatestVersion, cveStr)
			}

			if !apply {
				fmt.Printf("\n%d package update(s) available.\n", len(result.Updates))
				fmt.Println("\nTo update apexpacks.yaml and rebuild:")
				fmt.Println("  apexpacks patch --apply")
				fmt.Println("  apexpacks build .")
				return nil
			}

			fmt.Printf("\n[2/2] Applying patches...\n")

			var allApplied []string

			projectConfigPath := filepath.Join(absSource, "apexpacks.yaml")
			if _, statErr := os.Stat(projectConfigPath); statErr == nil {
				applied, applyErr := patch.ApplyToProfile(projectConfigPath, result.Updates)
				if applyErr != nil {
					fmt.Printf("  warning: apexpacks.yaml: %v\n", applyErr)
				} else if len(applied) > 0 {
					fmt.Printf("\n  apexpacks.yaml:\n")
					for _, change := range applied {
						fmt.Printf("    ↑ %s\n", change)
					}
					allApplied = append(allApplied, applied...)
				}
			}

			if len(allApplied) == 0 {
				fmt.Println("\nNo profiles contained the affected packages.")
				fmt.Println("Packages are floating — rebuilding will pick up the latest versions automatically.")
			} else {
				fmt.Printf("\n✓ %d package(s) pinned to patched versions across profiles.\n", len(allApplied))
				fmt.Println("\nNext step — rebuild to apply the patches:")
				fmt.Println("  apexpacks build .")
			}

			// Write patch results to context.json.
			if ctx, lerr := apexctx.Load(absSource); lerr == nil {
				ctx.PatchesApplied = strconv.Itoa(len(allApplied))
				ctx.PatchedPackages = allApplied
				ctx.AppendStage("patch")
				apexctx.Save(absSource, ctx) //nolint:errcheck
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&sbomPath, "sbom", "",
		"Path to SBOM file (default: <output-dir>/sbom-x86_64.spdx.json)")
	cmd.Flags().StringVar(&profilesDir, "profiles-dir", profile.DefaultProfilesDir,
		"Directory containing language profile YAML files")
	cmd.Flags().BoolVar(&apply, "apply", false,
		"Update profile YAML files with pinned patched versions")
	cmd.Flags().StringVar(&arch, "arch", "",
		"Architecture to check against the Wolfi index (default: from context.json or host arch)")
	cmd.Flags().StringVar(&runtime_, "runtime", "",
		"Only patch the profile for this runtime (e.g. java). Empty = patch all profiles.")
	cmd.Flags().StringVar(&sourceDir, "source", ".",
		"Source directory containing .apexpack/context.json")

	return cmd
}
