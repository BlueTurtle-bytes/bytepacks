package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/apexpack/apexpack/internal/build"
	"github.com/apexpack/apexpack/internal/detect"
	"github.com/apexpack/apexpack/internal/patch"
	"github.com/apexpack/apexpack/internal/profile"
	"github.com/apexpack/apexpack/internal/types"
)

var version = "dev"

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "apexpack",
		Short: "Build secure OCI images from language profiles",
		Long: `apexpack builds minimal, secure OCI images using melange and apko.

Language profiles (profiles/*.yaml) define how each language is detected,
built, and assembled into an image. No Dockerfiles required.

Quick start:
  apexpack detect .              # detect the language in current directory
  apexpack build .               # build an OCI image
  apexpack profiles              # list available language profiles`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(
		detectCmd(),
		buildCmd(),
		scanCmd(),
		patchCmd(),
		normalizeSBOMCmd(),
		profilesCmd(),
		versionCmd(),
	)

	return root
}

// --- detect command ---

func detectCmd() *cobra.Command {
	var profilesDir string

	cmd := &cobra.Command{
		Use:   "detect [source-dir]",
		Short: "Detect the language of a project",
		Long: `Scans the source directory and matches it against all profiles in
the profiles/ directory. Prints every match sorted by confidence.

Examples:
  apexpack detect .
  apexpack detect /path/to/my-project
  apexpack detect . --profiles-dir /custom/profiles`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			srcDir := "."
			if len(args) > 0 {
				srcDir = args[0]
			}

			profiles, err := profile.LoadAll(profilesDir)
			if err != nil {
				return err
			}

			results := detect.Run(profiles, srcDir)

			if len(results) == 0 {
				fmt.Printf("No language detected in %s\n\n", srcDir)
				fmt.Println("Checked profiles:")
				for _, p := range profiles {
					fmt.Printf("  %-12s  looking for: %v\n", p.Runtime, p.Detect.Files)
				}
				return nil
			}

			fmt.Printf("Detected %d match(es) in %s:\n\n", len(results), srcDir)
			for i, r := range results {
				marker := "  "
				if i == 0 {
					marker = "→ " // Arrow marks the best match
				}
				fw := r.Framework
				if fw == "" {
					fw = "unknown framework"
				}
				fmt.Printf("%s%-12s  %.0f%%  framework: %-14s  (matched: %v)\n",
					marker,
					r.Profile.Runtime,
					r.Confidence*100,
					fw,
					r.MatchedFiles,
				)
			}

			fmt.Printf("\nTo build: apexpack build %s\n", srcDir)
			return nil
		},
	}

	cmd.Flags().StringVar(&profilesDir, "profiles-dir", profile.DefaultProfilesDir,
		"Directory containing language profile YAML files")

	return cmd
}

// --- build command ---

func buildCmd() *cobra.Command {
	var (
		profilesDir string
		outputDir   string
		tag         string
		version     string
		runtime     string
		projectName string
		tlsExtraCA  string
		dryRun      bool
	)

	cmd := &cobra.Command{
		Use:   "build [source-dir]",
		Short: "Build an OCI image from a detected or specified profile",
		Long: `Detects the project language, loads the matching profile, generates
melange.yaml and apko.yaml, then runs melange and apko to produce an OCI image.

Examples:
  apexpack build .
  apexpack build . --tag ghcr.io/myorg/myapp:v1.0
  apexpack build . --runtime golang          # skip detection, use golang profile
  apexpack build . --dry-run                 # print generated configs, don't build`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			srcDir := "."
			if len(args) > 0 {
				srcDir = args[0]
			}

			absSrcDir, err := filepath.Abs(srcDir)
			if err != nil {
				return fmt.Errorf("resolving source dir: %w", err)
			}

			// Default project name = directory name (overridable via --project-name).
			if projectName == "" {
				projectName = filepath.Base(absSrcDir)
			}

			// Default output dir = <source>/.apexpack-output
			if outputDir == "" {
				outputDir = filepath.Join(absSrcDir, ".apexpack-output")
			}

			fmt.Println("⚡ apexpack build")
			fmt.Println()

			// Load all profiles.
			fmt.Printf("[1/3] Loading profiles from %s...\n", profilesDir)
			profiles, err := profile.LoadAll(profilesDir)
			if err != nil {
				return err
			}
			fmt.Printf("  → %d profile(s) loaded\n", len(profiles))

			// Detect or use the explicitly specified runtime.
			var matchedProfile *types.Profile
			var detectedFramework string
			var detectedPM string
			if runtime != "" {
				matchedProfile = profile.GetByRuntime(profiles, runtime)
				if matchedProfile == nil {
					return fmt.Errorf("profile for runtime %q not found in %s", runtime, profilesDir)
				}
				fmt.Printf("  → Using profile: %s (specified via --runtime)\n", runtime)
			} else {
				fmt.Printf("[2/3] Detecting language in %s...\n", absSrcDir)
				result := detect.Best(profiles, absSrcDir)
				if result == nil {
					return fmt.Errorf("could not detect language in %s\n\nTry: apexpack detect %s", absSrcDir, srcDir)
				}
				matchedProfile = result.Profile
				detectedFramework = result.Framework
				detectedPM = result.PackageManager
				fw := detectedFramework
				if fw == "" {
					fw = "no framework identified"
				}
				fmt.Printf("  → Detected: %s (%.0f%% confidence) — %s\n",
					result.Profile.Runtime, result.Confidence*100, fw)
			}

			// Load optional per-project apexpack.yaml from the source directory.
			projCfg, err := profile.LoadProjectConfig(absSrcDir)
			if err != nil {
				return fmt.Errorf("loading apexpack.yaml: %w", err)
			}
			if projCfg != nil {
				matchedProfile = profile.MergeProjectConfig(matchedProfile, projCfg)
				fmt.Println("  → Merged apexpack.yaml project overrides")
			}

			opts := build.Options{
				SourceDir:      absSrcDir,
				ProfilesDir:    profilesDir,
				OutputDir:      outputDir,
				ProjectName:    projectName,
				Version:        version,
				Tag:            tag,
				Framework:      detectedFramework,
				PackageManager: detectedPM,
				TLSExtraCA:     tlsExtraCA,
			}

			plan, err := build.Plan(matchedProfile, opts)
			if err != nil {
				return fmt.Errorf("planning build: %w", err)
			}

			// Dry-run: marshal the structs to YAML strings and print them.
			if dryRun {
				melangeYAML, err := build.MarshalMelange(plan)
				if err != nil {
					return err
				}
				apkoYAML, err := build.MarshalApko(plan)
				if err != nil {
					return err
				}
				fmt.Println("\n── melange.yaml ──────────────────────────────")
				fmt.Print(melangeYAML)
				fmt.Println("── apko.yaml ─────────────────────────────────")
				fmt.Print(apkoYAML)
				fmt.Println("── (dry-run: no files written, no tools run) ──")
				return nil
			}

			// Run the actual build.
			fmt.Printf("[3/3] Building %s:%s...\n", plan.ProjectName, plan.Version)
			if err := build.Run(plan, opts); err != nil {
				return err
			}

			imageTag := opts.Tag
			if imageTag == "" {
				imageTag = plan.ProjectName + ":latest"
			}
			fmt.Printf("\n✓ Image built: %s\n", imageTag)
			fmt.Printf("✓ Output:      %s\n", outputDir)
			return nil
		},
	}

	cmd.Flags().StringVar(&profilesDir, "profiles-dir", profile.DefaultProfilesDir,
		"Directory containing language profile YAML files")
	cmd.Flags().StringVarP(&outputDir, "output", "o", "",
		"Output directory for generated configs and image tarball")
	cmd.Flags().StringVarP(&tag, "tag", "t", "",
		"OCI image tag (e.g. ghcr.io/myorg/myapp:v1.0)")
	cmd.Flags().StringVar(&version, "version", "0.0.1",
		"Version to embed in the APK package")
	cmd.Flags().StringVar(&runtime, "runtime", "",
		"Skip detection and use this runtime profile directly (e.g. golang)")
	cmd.Flags().StringVar(&projectName, "project-name", "",
		"Override the project name (defaults to the source directory name)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"Print generated melange.yaml and apko.yaml without building")
	cmd.Flags().StringVar(&tlsExtraCA, "tls-extra-ca", "",
		"Path to an extra CA certificate (PEM) to trust — use in corporate proxy environments (env: APEXPACK_EXTRA_CA)")

	return cmd
}

// --- scan command ---

func scanCmd() *cobra.Command {
	var (
		sbomPath  string
		outputDir string
		failOn    string
		format    string
	)

	cmd := &cobra.Command{
		Use:   "scan [output-dir]",
		Short: "Scan the built image SBOM for CVEs using grype",
		Long: `Scans the SBOM produced by 'apexpack build' for known CVEs.

Examples:
  # Scan the last build in the default output directory
  apexpack scan

  # Scan a specific output directory
  apexpack scan /path/to/.apexpack-output

  # Scan a specific SBOM file
  apexpack scan --sbom /path/to/sbom-x86_64.spdx.json

  # Fail the command if any HIGH or above CVE is found (for CI)
  apexpack scan --fail-on high

  # Output SARIF for GitHub Code Scanning
  apexpack scan --format sarif --output results.sarif

CVE patching:
  Wolfi patches CVEs within hours. Once a patch is available, simply rebuild:
    apexpack build .
  The rebuilt image picks up the patched package version automatically.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Resolve the SBOM path.
			if sbomPath == "" {
				dir := ".apexpack-output"
				if len(args) > 0 {
					dir = args[0]
				} else if outputDir != "" {
					dir = outputDir
				}
				// Look for the most recent SBOM in the output dir.
				sbomPath = filepath.Join(dir, "sbom-x86_64.spdx.json")
			}

			if _, err := os.Stat(sbomPath); err != nil {
				return fmt.Errorf("SBOM not found at %s\n\nRun 'apexpack build' first to produce an SBOM", sbomPath)
			}

			fmt.Printf("Scanning %s\n\n", sbomPath)

			// Find grype.
			grypePath, err := findTool("grype")
			if err != nil {
				return fmt.Errorf("grype not found in PATH\n\nInstall: brew install grype  (Mac)  or  go install github.com/anchore/grype@latest")
			}

			args2 := []string{
				fmt.Sprintf("sbom:%s", sbomPath),
				"--output", format,
			}
			if failOn != "" {
				args2 = append(args2, "--fail-on", failOn)
			}
			if outputDir != "" {
				outFile := filepath.Join(outputDir, "scan-results."+format)
				args2 = append(args2, "--file", outFile)
				fmt.Printf("Report: %s\n\n", outFile)
			}

			c := exec.Command(grypePath, args2...)
			c.Stdout = cmd.OutOrStdout()
			c.Stderr = cmd.ErrOrStderr()
			if err := c.Run(); err != nil {
				if failOn != "" {
					return fmt.Errorf("CVEs found at or above %q severity — rebuild to apply patches", failOn)
				}
			}

			if failOn == "" {
				fmt.Println("\nTo patch: run 'apexpack build' — rebuilt images pick up the latest Wolfi package versions.")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&sbomPath, "sbom", "", "Path to a specific SBOM file (default: <output-dir>/sbom-x86_64.spdx.json)")
	cmd.Flags().StringVarP(&outputDir, "output", "o", "", "Write scan report to this directory")
	cmd.Flags().StringVar(&failOn, "fail-on", "", "Exit 1 if CVEs found at this severity or above: critical, high, medium, low")
	cmd.Flags().StringVar(&format, "format", "table", "Output format: table, json, sarif, cyclonedx")
	return cmd
}

// --- patch command ---

func patchCmd() *cobra.Command {
	var (
		sbomPath    string
		profilesDir string
		apply       bool
		arch        string
		runtime     string
	)

	cmd := &cobra.Command{
		Use:   "patch [output-dir]",
		Short: "Check for package updates and patch language profiles",
		Long: `Compares installed package versions (from the last build SBOM) against
the latest versions in the Wolfi package index. Cross-references with
grype to identify which outdated packages have known CVEs.

With --apply, updates the language profile YAML files to pin the
patched package versions. Pinned versions give you an audit trail —
the profile records exactly which version fixed which CVE.

Examples:
  # Show what packages can be updated
  apexpack patch

  # Show updates for a specific output directory
  apexpack patch /path/to/.apexpack-output

  # Apply patches — update profile YAML files with pinned versions
  apexpack patch --apply --profiles-dir ./profiles

  # After patching profiles, rebuild to apply
  apexpack build .`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			// Resolve the SBOM path.
			if sbomPath == "" {
				dir := ".apexpack-output"
				if len(args) > 0 {
					dir = args[0]
				}
				sbomPath = filepath.Join(dir, "sbom-x86_64.spdx.json")
			}

			if _, err := os.Stat(sbomPath); err != nil {
				return fmt.Errorf("SBOM not found at %s\n\nRun 'apexpack build' first", sbomPath)
			}

			fmt.Println("⚡ apexpack patch")
			fmt.Printf("\n[1/2] Checking packages against Wolfi index...\n")

			result, err := patch.Check(sbomPath, arch)
			if err != nil {
				return err
			}

			if len(result.Updates) == 0 {
				fmt.Println("\n✓ All packages are up to date. No patches needed.")
				return nil
			}

			// Print the update table.
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
				fmt.Println("\nTo update profiles and rebuild:")
				fmt.Printf("  apexpack patch --apply --profiles-dir %s\n", profilesDir)
				fmt.Printf("  apexpack build .\n")
				return nil
			}

			// --apply: update profile YAML files.
			fmt.Printf("\n[2/2] Applying patches to profiles in %s...\n", profilesDir)

			profiles, err := profile.LoadAll(profilesDir)
			if err != nil {
				return err
			}

			totalApplied := 0
			for _, p := range profiles {
				if runtime != "" && p.Runtime != runtime {
					continue // scope patch to the detected runtime only
				}
				profilePath := filepath.Join(profilesDir, p.Runtime+".yaml")
				applied, applyErr := patch.ApplyToProfile(profilePath, result.Updates)
				if applyErr != nil {
					fmt.Printf("  warning: %s: %v\n", p.Runtime, applyErr)
					continue
				}
				if len(applied) > 0 {
					fmt.Printf("\n  %s.yaml:\n", p.Runtime)
					for _, change := range applied {
						fmt.Printf("    ↑ %s\n", change)
					}
					totalApplied += len(applied)
				}
			}

			if totalApplied == 0 {
				fmt.Println("\nNo profiles contained the affected packages.")
				fmt.Println("Packages are floating — rebuilding will pick up the latest versions automatically.")
			} else {
				fmt.Printf("\n✓ %d package(s) pinned to patched versions across profiles.\n", totalApplied)
				fmt.Println("\nNext step — rebuild to apply the patches:")
				fmt.Println("  apexpack build .")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&sbomPath, "sbom", "", "Path to SBOM file (default: <output-dir>/sbom-x86_64.spdx.json)")
	cmd.Flags().StringVar(&profilesDir, "profiles-dir", profile.DefaultProfilesDir, "Directory containing language profile YAML files")
	cmd.Flags().BoolVar(&apply, "apply", false, "Update profile YAML files with pinned patched versions")
	cmd.Flags().StringVar(&arch, "arch", "x86_64", "Architecture to check against the Wolfi index")
	cmd.Flags().StringVar(&runtime, "runtime", "", "Only patch the profile for this runtime (e.g. java). Empty = patch all profiles.")
	return cmd
}

// --- normalize-sbom command ---

func normalizeSBOMCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "normalize-sbom <sbom-path>",
		Short: "Normalize SBOM version strings for accurate grype scanning",
		Long: `Rewrites an SPDX SBOM to a temp file with normalized versionInfo fields.
Strips non-APK prefixes (e.g. "openssl-3.6.2" → "3.6.2", "v1.2.0" → "1.2.0")
so grype can match packages against its CVE database correctly.

Prints the temp file path (no newline) — designed for shell substitution:
  NORMALIZED=$(apexpack normalize-sbom sbom.json)
  grype sbom:$NORMALIZED ...
  rm -f "$NORMALIZED"`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			tmpPath, err := patch.NormalizeSBOMFile(args[0])
			if err != nil {
				return err
			}
			fmt.Print(tmpPath) // no trailing newline — safe for $(...) capture
			return nil
		},
	}
}

// findTool looks for a binary in PATH.
func findTool(name string) (string, error) {
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("%s not found in PATH", name)
}

// --- profiles command ---

func profilesCmd() *cobra.Command {
	var profilesDir string

	cmd := &cobra.Command{
		Use:   "profiles",
		Short: "List available language profiles",
		RunE: func(_ *cobra.Command, _ []string) error {
			profiles, err := profile.LoadAll(profilesDir)
			if err != nil {
				return err
			}

			fmt.Printf("Language profiles in %s:\n\n", profilesDir)
			for _, p := range profiles {
				desc := p.Description
				if desc == "" {
					desc = "(no description)"
				}
				fmt.Printf("  %-12s  %s\n", p.Runtime, desc)
				allDetect := append(p.Detect.Files, p.Detect.Patterns...)
			fmt.Printf("  %-12s  detects: %v\n", "", allDetect)
				fmt.Printf("  %-12s  build deps: %v\n\n", "", p.Build.Dependencies)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&profilesDir, "profiles-dir", profile.DefaultProfilesDir,
		"Directory containing language profile YAML files")

	return cmd
}

// --- version command ---

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("apexpack %s\n", version)
		},
	}
}
