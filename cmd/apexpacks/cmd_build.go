package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/apexpack/apexpack/internal/apexctx"
	"github.com/apexpack/apexpack/internal/build"
	"github.com/apexpack/apexpack/internal/detect"
	"github.com/apexpack/apexpack/internal/profile"
	"github.com/apexpack/apexpack/internal/types"
)

func buildCmd() *cobra.Command {
	var (
		profilesDir string
		outputDir   string
		tag         string
		ver         string
		runtime_    string
		projectName string
		tlsExtraCA     string
		arch           string
		dryRun         bool
		localBuild     bool
		signingKey     string
		melangeRunner  string
		buildArgSlice  []string
	)

	cmd := &cobra.Command{
		Use:   "build [source-dir]",
		Short: "Build an OCI image from a detected or specified profile",
		Long: `Detects the project language, loads the matching profile, generates
melange.yaml and apko.yaml, then runs melange and apko to produce an OCI image.
Writes build artifact paths to .apexpack/context.json as a side effect.

Examples:
  apexpacks build .
  apexpacks build . --tag ghcr.io/myorg/myapp:v1.0
  apexpacks build . --runtime golang          # skip detection, use golang profile
  apexpacks build . --dry-run                 # print generated configs, don't build`,
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

			fmt.Println("⚡ apexpacks build")
			fmt.Println()

			fmt.Printf("[1/3] Loading profiles from %s...\n", profilesDir)
			profiles, err := profile.LoadAll(resolveProfilesDir(profilesDir))
			if err != nil {
				return err
			}
			fmt.Printf("  → %d profile(s) loaded\n", len(profiles))

			// If --runtime not given, check context.json set by a prior detect run.
			runtimeSource := "--runtime flag"
			var matchedProfile *types.Profile
			var detectedFramework string
			var detectedPM string
			var detectedLangVersion string

			if runtime_ == "" {
				if ctxData, cerr := apexctx.Load(absSrcDir); cerr == nil && ctxData.Runtime != "" {
					runtime_ = ctxData.Runtime
					runtimeSource = "context.json"
					// Carry forward framework saved by a prior detect run.
					detectedFramework = ctxData.Framework
				}
			}

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
					return fmt.Errorf("could not detect language in %s\n\nTry: apexpacks detect %s", absSrcDir, srcDir)
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
			runtime_ = matchedProfile.Runtime

			projCfg, err := profile.LoadProjectConfig(absSrcDir)
			if err != nil {
				return fmt.Errorf("loading apexpacks.yaml: %w", err)
			}
			if projCfg != nil {
				matchedProfile = profile.MergeProjectConfig(matchedProfile, projCfg)
				if projCfg.LanguageVersion != "" {
					detectedLangVersion = projCfg.LanguageVersion
					fmt.Printf("  → language_version overridden by apexpacks.yaml: %s\n", detectedLangVersion)
				}
				fmt.Println("  → Merged apexpacks.yaml project overrides")
			}

			ver = strings.TrimPrefix(ver, "v")

			buildArgs := make(map[string]string, len(buildArgSlice))
			for _, kv := range buildArgSlice {
				k, v, ok := strings.Cut(kv, "=")
				if !ok || k == "" {
					return fmt.Errorf("invalid --build-arg %q: must be KEY=VALUE", kv)
				}
				buildArgs[k] = v
			}

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
				MelangeRunner:   melangeRunner,
				Arch:            arch,
				LocalBuild:      localBuild,
				SigningKey:      signingKey,
				BuildArgs:       buildArgs,
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
			sbomFile := filepath.Join(outputDir, "sbom-"+actualArch+".spdx.json")
			apkPath := filepath.Join(outputDir, "packages", actualArch, "*.apk")

			ctx, err := apexctx.Load(absSrcDir)
			if err != nil {
				return fmt.Errorf("loading context: %w", err)
			}
			ctx.Version = ver
			ctx.Image = imageTag
			ctx.SBOMPath = sbomFile
			ctx.APKPath = apkPath
			ctx.Runtime = runtime_
			ctx.Arch = actualArch
			if localBuild || runtime.GOOS == "darwin" {
				// Local build: tarball written to OutputDir, no registry push.
				ctx.ImageTarball = filepath.Join(outputDir, build.SanitizeImageName(projectName)+".tar")
			} else {
				// Publish build: apko pushed directly; tarball is not produced.
				ctx.PushedImage = imageTag
			}
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
	cmd.Flags().StringVar(&melangeRunner, "melange-runner", "",
		`melange sandbox backend: bubblewrap (default), docker, qemu.
Use "docker" when bubblewrap user namespaces are unavailable and a Docker socket is accessible.`)
	cmd.Flags().BoolVar(&localBuild, "local", false,
		"Build tarball only, skip registry push (apko build). Default pushes directly via apko publish.")
	cmd.Flags().StringVar(&signingKey, "signing-key", "",
		"Path to an existing melange RSA private key (PEM). The .pub file must be at <path>.pub. "+
			"When empty, a key pair is generated in the output directory.")
	cmd.Flags().StringArrayVar(&buildArgSlice, "build-arg", nil,
		"Bake a KEY=VALUE pair into the image as an env var and OCI annotation.\n"+
			"Repeatable. Useful for CI metadata (build number, git SHA, release name).\n"+
			"Examples:\n"+
			"  --build-arg BUILD_VERSION=1.2.3\n"+
			"  --build-arg GIT_COMMIT=$(git rev-parse HEAD)\n"+
			"  --build-arg BUILD_ID=$(Build.BuildId)")

	return cmd
}
