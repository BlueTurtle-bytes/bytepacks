package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/spf13/cobra"

	"github.com/apexpack/apexpack/internal/patch"
	"github.com/apexpack/apexpack/internal/profile"
)

func profilesCmd() *cobra.Command {
	var profilesDir string

	cmd := &cobra.Command{
		Use:   "profiles",
		Short: "List, export, or create language profiles",
		Long: `List available language profiles, export them for customisation,
or scaffold a new custom profile.

  apexpacks profiles                       # list built-in profiles
  apexpacks profiles export ./my-profiles  # export to a directory you can edit
  apexpacks profiles new rust              # scaffold a new profile`,
		RunE: func(_ *cobra.Command, _ []string) error {
			profiles, err := profile.LoadAll(resolveProfilesDir(profilesDir))
			if err != nil {
				return err
			}

			resolved := resolveProfilesDir(profilesDir)
			src := "built-in"
			if _, err := os.Stat(resolved); err == nil {
				if abs, err := filepath.Abs(resolved); err == nil {
					src = abs
				} else {
					src = resolved
				}
			}
			fmt.Printf("Language profiles (%s):\n\n", src)
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

	cmd.AddCommand(profilesExportCmd(), profilesNewCmd())
	return cmd
}

func profilesExportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "export <dir>",
		Short: "Export built-in profiles to a directory for customisation",
		Long: `Writes all built-in profiles to <dir> so you can edit them.
Point --profiles-dir at that directory to use your customised versions:

  apexpacks profiles export ./my-profiles
  apexpacks build . --profiles-dir ./my-profiles`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			dir := args[0]
			if err := profile.ExportEmbedded(dir); err != nil {
				return err
			}
			abs, _ := filepath.Abs(dir)
			fmt.Printf("Profiles exported to %s\n\n", abs)
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".yaml") {
					fmt.Printf("  %s\n", e.Name())
				}
			}
			fmt.Printf("\nEdit these files, then build with:\n")
			fmt.Printf("  apexpacks build . --profiles-dir %s\n", dir)
			return nil
		},
	}
}

var profileTemplate = template.Must(template.New("profile").Parse(`# Custom profile for {{.Name}}
# Place this file in your profiles directory and run:
#   apexpacks build . --profiles-dir <profiles-dir>

runtime: {{.Name}}
version: "1"
description: {{.Title}} application

detect:
  files:
    # List files that identify a {{.Title}} project (e.g. go.mod, requirements.txt)
    - ""
  confidence: 0.85

build:
  dependencies:
    - busybox
    # Add build-time packages here (e.g. go, nodejs, python-3.12)
  command: |
    # Build your application and copy artifacts to ${{"{{"}}targets.destdir{{"}}"}}
    mkdir -p ${{"{{"}}targets.destdir{{"}}"}}/app
    # Example: cp -r ./dist/. ${{"{{"}}targets.destdir{{"}}"}}/app/
  env:
    # CI: "true"

image:
  packages:
    - ca-certificates-bundle=20260413-r0
    # Add runtime packages here (e.g. nodejs-22, python-3.12)
  entrypoint: /app/{{.Name}}
  run-as: 65532
  ports:
    - "8080"
  health-check:
    http:
      port: 8080
    interval: 30s
    timeout: 5s
    start-period: 10s
    retries: 3

scan:
  auto-patch: false
  patch-persist: false

test:
  packages:
    - busybox
  pipeline:
    - runs: |
        test -f /app/{{.Name}} || { echo "ERROR: /app/{{.Name}} not found"; exit 1; }
        echo "✓ {{.Title}} app is present"
`))

func profilesNewCmd() *cobra.Command {
	var outputDir string

	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Scaffold a new custom language profile",
		Long: `Creates a starter profile YAML for a new language or framework.
Edit the generated file, then use it with --profiles-dir:

  apexpacks profiles new rust --output-dir ./my-profiles
  apexpacks build . --profiles-dir ./my-profiles`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := strings.ToLower(args[0])
			title := strings.ToUpper(name[:1]) + name[1:]

			if err := os.MkdirAll(outputDir, 0o755); err != nil {
				return fmt.Errorf("creating directory %s: %w", outputDir, err)
			}

			outPath := filepath.Join(outputDir, name+".yaml")
			f, err := os.Create(outPath)
			if err != nil {
				return fmt.Errorf("creating %s: %w", outPath, err)
			}
			defer f.Close()

			if err := profileTemplate.Execute(f, map[string]string{
				"Name":  name,
				"Title": title,
			}); err != nil {
				return fmt.Errorf("writing profile: %w", err)
			}

			abs, _ := filepath.Abs(outPath)
			fmt.Printf("Profile scaffolded: %s\n\n", abs)
			fmt.Printf("Next steps:\n")
			fmt.Printf("  1. Edit %s to define detect rules, build command, and image packages\n", outPath)
			fmt.Printf("  2. apexpacks build . --profiles-dir %s\n", outputDir)
			return nil
		},
	}

	cmd.Flags().StringVar(&outputDir, "output-dir", ".", "Directory to write the new profile YAML")
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
