package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/apexpack/apexpack/internal/patch"
	"github.com/apexpack/apexpack/internal/profile"
)

func profilesCmd() *cobra.Command {
	var profilesDir string

	cmd := &cobra.Command{
		Use:   "profiles",
		Short: "List available language profiles",
		RunE: func(_ *cobra.Command, _ []string) error {
			profiles, err := profile.LoadAll(resolveProfilesDir(profilesDir))
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

func normalizeSBOMCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "normalize-sbom <sbom-path>",
		Short: "Normalize SBOM version strings for accurate grype scanning",
		Long: `Rewrites an SPDX SBOM to a temp file with normalized versionInfo fields.
Strips non-APK prefixes (e.g. "openssl-3.6.2" → "3.6.2", "v1.2.0" → "1.2.0")
so grype can match packages against its CVE database correctly.

Note: 'apexpacks scan' now normalises automatically. This command remains
available for scripting and debugging.

Prints the temp file path (no newline) — designed for shell substitution:
  NORMALIZED=$(apexpacks normalize-sbom sbom.json)
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

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("apexpacks %s\n", version)
		},
	}
}
