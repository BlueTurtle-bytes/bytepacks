package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/apexpack/apexpack/internal/apexctx"
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
	var (
		profilesDir       string
		projectName       string
		gitURL            string
		gitBranch         string
		gitCommit         string
		autoPatchOverride string
		patchPersistOverride string
	)

	cmd := &cobra.Command{
		Use:   "detect [source-dir]",
		Short: "Detect the language of a project",
		Long: `Scans the source directory and matches it against all profiles in
the profiles/ directory. Prints every match sorted by confidence.
Writes detection results to .apexpack/context.json as a side effect.

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
			absSrcDir, err := filepath.Abs(srcDir)
			if err != nil {
				return fmt.Errorf("resolving source dir: %w", err)
			}

			profiles, err := profile.LoadAll(profilesDir)
			if err != nil {
				return err
			}

			results := detect.Run(profiles, absSrcDir)

			if len(results) == 0 {
				fmt.Printf("No language detected in %s\n\n", absSrcDir)
				fmt.Println("Checked profiles:")
				for _, p := range profiles {
					fmt.Printf("  %-12s  looking for: %v\n", p.Runtime, p.Detect.Files)
				}
				return nil
			}

			fmt.Printf("Detected %d match(es) in %s:\n\n", len(results), absSrcDir)
			for i, r := range results {
				marker := "  "
				if i == 0 {
					marker = "→ "
				}
				fw := r.Framework
				if fw == "" {
					fw = "unknown framework"
				}
				ver := r.LanguageVersion
				if ver == "" {
					ver = "-"
				}
				fmt.Printf("%s%-12s  %.0f%%  framework: %-14s  version: %-8s  (matched: %v)\n",
					marker,
					r.Profile.Runtime,
					r.Confidence*100,
					fw,
					ver,
					r.MatchedFiles,
				)
			}
			fmt.Printf("\nTo build: apexpack build %s\n", srcDir)

			// Write context.json
			best := results[0]

			// Resolve project name
			name := projectName
			if name == "" {
				name = filepath.Base(absSrcDir)
			}

			// Resolve git info — use flags first, fall back to git commands
			if gitURL == "" {
				gitURL = gitRemoteURL(absSrcDir)
			}
			if gitBranch == "" {
				gitBranch = gitCurrentBranch(absSrcDir)
			}
			if gitCommit == "" {
				gitCommit = gitHeadCommit(absSrcDir)
			}

			// Resolve auto-patch / patch-persist from profile, then apply overrides
			autoPatch := strconv.FormatBool(best.Profile.Scan.AutoPatch)
			if autoPatchOverride == "true" {
				autoPatch = "true"
			}
			patchPersist := strconv.FormatBool(best.Profile.Scan.PatchPersist)
			if patchPersistOverride == "true" {
				patchPersist = "true"
			}

			// Compute arch
			arch, archRID := hostArch()

			ctx, err := apexctx.Load(absSrcDir)
			if err != nil {
				return fmt.Errorf("loading context: %w", err)
			}
			ctx.ProjectName = name
			ctx.Runtime = best.Profile.Runtime
			ctx.Framework = best.Framework
			ctx.Confidence = fmt.Sprintf("%.0f", best.Confidence*100)
			ctx.LanguageVersion = best.LanguageVersion
			ctx.Arch = arch
			ctx.ArchRID = archRID
			ctx.GitURL = gitURL
			ctx.GitBranch = gitBranch
			ctx.GitCommit = gitCommit
			ctx.AutoPatch = autoPatch
			ctx.PatchPersist = patchPersist
			ctx.AppendStage("detect")

			if err := apexctx.Save(absSrcDir, ctx); err != nil {
				return fmt.Errorf("saving context: %w", err)
			}
			fmt.Printf("\nContext: %s\n", filepath.Join(absSrcDir, ".apexpack/context.json"))
			return nil
		},
	}

	cmd.Flags().StringVar(&profilesDir, "profiles-dir", profile.DefaultProfilesDir,
		"Directory containing language profile YAML files")
	cmd.Flags().StringVar(&projectName, "project-name", "",
		"Override the project name (defaults to source directory name)")
	cmd.Flags().StringVar(&gitURL, "git-url", "",
		"Repository URL written to context.json (auto-detected from git if empty)")
	cmd.Flags().StringVar(&gitBranch, "git-branch", "",
		"Branch or revision written to context.json (auto-detected from git if empty)")
	cmd.Flags().StringVar(&gitCommit, "git-commit", "",
		"Commit SHA written to context.json (auto-detected from git if empty)")
	cmd.Flags().StringVar(&autoPatchOverride, "auto-patch", "",
		"Force auto-patch on ('true') regardless of profile setting")
	cmd.Flags().StringVar(&patchPersistOverride, "patch-persist", "",
		"Force patch-persist on ('true') regardless of profile setting")

	return cmd
}

// --- build command ---

func buildCmd() *cobra.Command {
	var (
		profilesDir string
		outputDir   string
		tag         string
		ver         string
		runtime_    string
		projectName string
		tlsExtraCA  string
		arch        string
		dryRun      bool
	)

	cmd := &cobra.Command{
		Use:   "build [source-dir]",
		Short: "Build an OCI image from a detected or specified profile",
		Long: `Detects the project language, loads the matching profile, generates
melange.yaml and apko.yaml, then runs melange and apko to produce an OCI image.
Writes build artifact paths to .apexpack/context.json as a side effect.

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

			if projectName == "" {
				projectName = filepath.Base(absSrcDir)
			}
			if outputDir == "" {
				outputDir = filepath.Join(absSrcDir, ".apexpack-output")
			}

			fmt.Println("⚡ apexpack build")
			fmt.Println()

			fmt.Printf("[1/3] Loading profiles from %s...\n", profilesDir)
			profiles, err := profile.LoadAll(profilesDir)
			if err != nil {
				return err
			}
			fmt.Printf("  → %d profile(s) loaded\n", len(profiles))

			// If --runtime not given, check context.json set by a prior detect run.
			runtimeSource := "--runtime flag"
			if runtime_ == "" {
				if ctxData, cerr := apexctx.Load(absSrcDir); cerr == nil && ctxData.Runtime != "" {
					runtime_ = ctxData.Runtime
					runtimeSource = "context.json"
				}
			}

			var matchedProfile *types.Profile
			var detectedFramework string
			var detectedPM string
			var detectedLangVersion string
			if runtime_ != "" {
				matchedProfile = profile.GetByRuntime(profiles, runtime_)
				if matchedProfile == nil {
					return fmt.Errorf("profile for runtime %q not found in %s", runtime_, profilesDir)
				}
				detectedLangVersion = detect.LanguageVersion(matchedProfile.Runtime, absSrcDir)
				versionSuffix := ""
				if detectedLangVersion != "" {
					versionSuffix = " — version " + detectedLangVersion
				}
				fmt.Printf("  → Using profile: %s (from %s)%s\n", runtime_, runtimeSource, versionSuffix)
			} else {
				fmt.Printf("[2/3] Detecting language in %s...\n", absSrcDir)
				result := detect.Best(profiles, absSrcDir)
				if result == nil {
					return fmt.Errorf("could not detect language in %s\n\nTry: apexpack detect %s", absSrcDir, srcDir)
				}
				matchedProfile = result.Profile
				detectedFramework = result.Framework
				detectedPM = result.PackageManager
				detectedLangVersion = result.LanguageVersion
				fw := detectedFramework
				if fw == "" {
					fw = "no framework identified"
				}
				versionSuffix := ""
				if detectedLangVersion != "" {
					versionSuffix = " — version " + detectedLangVersion
				}
				fmt.Printf("  → Detected: %s (%.0f%% confidence) — %s%s\n",
					result.Profile.Runtime, result.Confidence*100, fw, versionSuffix)
			}

			projCfg, err := profile.LoadProjectConfig(absSrcDir)
			if err != nil {
				return fmt.Errorf("loading apexpack.yaml: %w", err)
			}
			if projCfg != nil {
				matchedProfile = profile.MergeProjectConfig(matchedProfile, projCfg)
				fmt.Println("  → Merged apexpack.yaml project overrides")
			}

			ver = strings.TrimPrefix(ver, "v")

			opts := build.Options{
				SourceDir:       absSrcDir,
				ProfilesDir:     profilesDir,
				OutputDir:       outputDir,
				ProjectName:     projectName,
				Version:         ver,
				Tag:             tag,
				Framework:       detectedFramework,
				PackageManager:  detectedPM,
				LanguageVersion: detectedLangVersion,
				TLSExtraCA:      tlsExtraCA,
				Arch:            arch,
			}

			plan, err := build.Plan(matchedProfile, opts)
			if err != nil {
				return fmt.Errorf("planning build: %w", err)
			}

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

			// Write build artifact paths to context.json.
			actualArch := buildArch(arch)
			tarball := filepath.Join(outputDir, projectName+".tar")
			sbomFile := filepath.Join(outputDir, "sbom-"+actualArch+".spdx.json")
			apkPath := filepath.Join(outputDir, "packages", actualArch, "*.apk")

			ctx, err := apexctx.Load(absSrcDir)
			if err != nil {
				return fmt.Errorf("loading context: %w", err)
			}
			ctx.Version = ver
			ctx.Image = imageTag
			ctx.ImageTarball = tarball
			ctx.SBOMPath = sbomFile
			ctx.APKPath = apkPath
			ctx.AppendStage("build")
			if err := apexctx.Save(absSrcDir, ctx); err != nil {
				return fmt.Errorf("saving context: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&profilesDir, "profiles-dir", profile.DefaultProfilesDir,
		"Directory containing language profile YAML files")
	cmd.Flags().StringVarP(&outputDir, "output", "o", "",
		"Output directory for generated configs and image tarball")
	cmd.Flags().StringVarP(&tag, "tag", "t", "",
		"OCI image tag (e.g. ghcr.io/myorg/myapp:v1.0)")
	cmd.Flags().StringVar(&ver, "version", "0.0.1",
		"Version to embed in the APK package")
	cmd.Flags().StringVar(&runtime_, "runtime", "",
		"Skip detection and use this runtime profile directly (e.g. golang)")
	cmd.Flags().StringVar(&projectName, "project-name", "",
		"Override the project name (defaults to the source directory name)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"Print generated melange.yaml and apko.yaml without building")
	cmd.Flags().StringVar(&tlsExtraCA, "tls-extra-ca", "",
		"Path to an extra CA certificate (PEM) to trust — use in corporate proxy environments")
	cmd.Flags().StringVar(&arch, "arch", "",
		"Target build architecture: x86_64 or aarch64 (default: host arch)")

	return cmd
}

// --- scan command ---

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
		Long: `Scans the SBOM produced by 'apexpack build' for known CVEs.
Normalises SBOM version strings automatically before scanning.
Writes severity counts and scan result to .apexpack/context.json.

Examples:
  apexpack scan
  apexpack scan /path/to/.apexpack-output
  apexpack scan --sbom /path/to/sbom-x86_64.spdx.json
  apexpack scan --fail-on high
  apexpack scan --format sarif --output results.sarif
  apexpack scan --soft-fail    # exit 0 even on failure (for auto-patch flows)
  apexpack scan --rescan       # write results to rescan_* fields in context.json`,
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
				return fmt.Errorf("SBOM not found at %s\n\nRun 'apexpack build' first to produce an SBOM", sbomPath)
			}

			grypePath, err := findTool("grype")
			if err != nil {
				return fmt.Errorf("grype not found in PATH\n\nInstall: brew install grype  or  go install github.com/anchore/grype@latest")
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

// --- patch command ---

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
		Short: "Check for package updates and patch language profiles",
		Long: `Compares installed package versions (from the last build SBOM) against
the latest versions in the Wolfi package index. Cross-references with
grype to identify which outdated packages have known CVEs.

With --apply, updates the language profile YAML files to pin the
patched package versions. Writes patch results to .apexpack/context.json.

Examples:
  apexpack patch
  apexpack patch /path/to/.apexpack-output
  apexpack patch --apply --profiles-dir ./profiles
  apexpack build .`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			absSource, err := filepath.Abs(sourceDir)
			if err != nil {
				return fmt.Errorf("resolving source dir: %w", err)
			}

			// Read sbom_path and runtime from context.json when not given as flags.
			if ctxData, cerr := apexctx.Load(absSource); cerr == nil {
				if sbomPath == "" && ctxData.SBOMPath != "" {
					sbomPath = ctxData.SBOMPath
				}
				if runtime_ == "" && ctxData.Runtime != "" {
					runtime_ = ctxData.Runtime
				}
			}

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

			fmt.Printf("\n[2/2] Applying patches to profiles in %s...\n", profilesDir)

			profiles, err := profile.LoadAll(profilesDir)
			if err != nil {
				return err
			}

			var allApplied []string
			for _, p := range profiles {
				if runtime_ != "" && p.Runtime != runtime_ {
					continue
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
					allApplied = append(allApplied, applied...)
				}
			}

			if len(allApplied) == 0 {
				fmt.Println("\nNo profiles contained the affected packages.")
				fmt.Println("Packages are floating — rebuilding will pick up the latest versions automatically.")
			} else {
				fmt.Printf("\n✓ %d package(s) pinned to patched versions across profiles.\n", len(allApplied))
				fmt.Println("\nNext step — rebuild to apply the patches:")
				fmt.Println("  apexpack build .")
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
	cmd.Flags().StringVar(&arch, "arch", "x86_64",
		"Architecture to check against the Wolfi index")
	cmd.Flags().StringVar(&runtime_, "runtime", "",
		"Only patch the profile for this runtime (e.g. java). Empty = patch all profiles.")
	cmd.Flags().StringVar(&sourceDir, "source", ".",
		"Source directory containing .apexpack/context.json")

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

Note: 'apexpack scan' now normalises automatically. This command remains
available for scripting and debugging.

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
			fmt.Print(tmpPath)
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

// --- helpers ---

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
