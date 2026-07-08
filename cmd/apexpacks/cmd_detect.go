package main

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/apexpack/apexpack/internal/apexctx"
	"github.com/apexpack/apexpack/internal/detect"
	"github.com/apexpack/apexpack/internal/profile"
)

func detectCmd() *cobra.Command {
	var (
		profilesDir          string
		projectName          string
		gitURL               string
		gitBranch            string
		gitCommit            string
		autoPatchOverride    string
		patchPersistOverride string
	)

	cmd := &cobra.Command{
		Use:   "detect [source-dir]",
		Short: "Detect the language of a project",
		Long: `Scans the source directory and matches it against all profiles in
the profiles/ directory. Prints every match sorted by confidence.
Writes detection results to .apexpack/context.json as a side effect.

Examples:
  apexpacks detect .
  apexpacks detect /path/to/my-project
  apexpacks detect . --profiles-dir /custom/profiles`,
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

			profiles, err := profile.LoadAll(resolveProfilesDir(profilesDir))
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
			fmt.Printf("\nTo build: apexpacks build %s\n", srcDir)

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
