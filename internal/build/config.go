package build

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/apexpack/apexpack/internal/types"
)

// marshalYAML encodes v to a 2-space indented YAML string.
// yaml.Marshal defaults to 4-space indentation; this matches the melange/apko
// convention of 2 spaces and makes generated configs easier to read.
func marshalYAML(v any) (string, error) {
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// buildMelangeConfig constructs a MelangeConfig from a profile and options.
// The struct is later marshalled to YAML by yaml.Marshal.
func buildMelangeConfig(p *types.Profile, opts Options) (types.MelangeConfig, error) {
	token := langVersionToken(p.Runtime)
	version := resolveVersion(p.Runtime, opts.LanguageVersion)
	if err := validateRuntimeVersion(p.Runtime, version); err != nil {
		return types.MelangeConfig{}, err
	}

	packages := vsubSlice(append([]string{"wolfi-baselayout"}, p.Build.Dependencies...), token, version)

	cfg := types.MelangeConfig{
		Package: types.MelangePackage{
			Name:        opts.ProjectName,
			Version:     opts.Version,
			Epoch:       0,
			Description: fmt.Sprintf("Built by apexpack (%s)", p.Runtime),
			Copyright:   []types.MelangeCopyright{{License: "Apache-2.0"}},
		},
		Environment: types.MelangeEnvironment{
			Contents: types.MelangeContents{
				Keyring:      []string{"https://packages.wolfi.dev/os/wolfi-signing.rsa.pub"},
				Repositories: []string{"https://packages.wolfi.dev/os"},
				Packages:     packages,
			},
			Env: vsubMap(p.Build.Env, token, version),
		},
		Pipeline: []types.MelangePipeline{{Runs: vsub(applyProjectTemplates(p.Build.Command, opts.ProjectName), token, version)}},
	}

	// Resolve the framework override using a three-level fallback:
	//   1. {framework}-{packageManager}  e.g. "nextjs-pnpm"
	//   2. {packageManager}              e.g. "pnpm"
	//   3. {framework}                   e.g. "nextjs"
	override, found := resolveOverride(p, opts.Framework, opts.PackageManager)
	if found {
		if len(override.Dependencies) > 0 {
			cfg.Environment.Contents.Packages = vsubSlice(append([]string{"wolfi-baselayout"}, override.Dependencies...), token, version)
		}
		if override.Command != "" {
			cfg.Pipeline = []types.MelangePipeline{{Runs: vsub(applyProjectTemplates(override.Command, opts.ProjectName), token, version)}}
		}
		for k, v := range override.Env {
			if cfg.Environment.Env == nil {
				cfg.Environment.Env = make(map[string]string)
			}
			cfg.Environment.Env[k] = vsub(v, token, version)
		}
	}

	// Dispatch to the language hook for runtime-specific melange patches.
	if hook, ok := hooks[p.Runtime]; ok {
		if err := hook.PatchMelange(&cfg, p, opts); err != nil {
			return types.MelangeConfig{}, err
		}
	}

	// Build the test section when the profile defines test steps.
	// The test sandbox always includes the local packages repo (where the built APK lives)
	// and the local signing key so the APK can be verified and installed.
	if len(p.Test.Pipeline) > 0 {
		testPkgs := vsubSlice(p.Test.Packages, token, version)
		steps := make([]types.MelangePipeline, len(p.Test.Pipeline))
		for i, step := range p.Test.Pipeline {
			steps[i] = types.MelangePipeline{
				Runs: vsub(applyProjectTemplates(step.Runs, opts.ProjectName), token, version),
			}
		}
		cfg.Test = &types.MelangeTest{
			Environment: types.MelangeTestEnvironment{
				Contents: types.MelangeContents{
					Keyring: []string{
						"https://packages.wolfi.dev/os/wolfi-signing.rsa.pub",
						"./melange.rsa.pub",
					},
					Repositories: []string{
						"https://packages.wolfi.dev/os",
						"./packages",
					},
					Packages: testPkgs,
				},
			},
			Pipeline: steps,
		}
	}

	// Propagate Go module env vars from the host into the melange.yaml environment
	// block. Melange explicitly passes these into the bubblewrap sandbox, so the
	// go build command running inside the sandbox uses the same proxy and TLS
	// settings as the host. Profile-defined values take precedence.
	for _, key := range []string{"GOPROXY", "GONOSUMDB", "GONOSUMCHECK", "GOINSECURE", "GOPRIVATE"} {
		if val := os.Getenv(key); val != "" {
			if cfg.Environment.Env == nil {
				cfg.Environment.Env = make(map[string]string)
			}
			if _, exists := cfg.Environment.Env[key]; !exists {
				cfg.Environment.Env[key] = val
			}
		}
	}

	// When a corporate CA is provided, inject the env vars declared by the profile
	// into the melange sandbox. The cert is copied to opts.SourceDir before melange
	// runs (see Run()), making it accessible inside the sandbox at /home/build/.
	// The keytool/update-ca-certificates pre-step (if any) is also injected in Run().
	if opts.TLSExtraCA != "" && len(p.Build.TLSCAEnv) > 0 {
		if cfg.Environment.Env == nil {
			cfg.Environment.Env = make(map[string]string)
		}
		caPath := "/home/build/.apexpack-ca-bundle.pem"
		for _, key := range p.Build.TLSCAEnv {
			if _, exists := cfg.Environment.Env[key]; !exists {
				cfg.Environment.Env[key] = caPath
			}
		}
	}

	return cfg, nil
}

// resolveOverride finds the most specific FrameworkBuildOverride for the detected
// framework and package manager, using the three-level fallback.
func resolveOverride(p *types.Profile, framework, pm string) (types.FrameworkBuildOverride, bool) {
	if len(p.Build.Frameworks) == 0 {
		return types.FrameworkBuildOverride{}, false
	}
	candidates := []string{}
	if framework != "" && pm != "" {
		candidates = append(candidates, framework+"-"+pm)
	}
	if pm != "" {
		candidates = append(candidates, pm)
	}
	if framework != "" {
		candidates = append(candidates, framework)
	}
	for _, key := range candidates {
		if override, ok := p.Build.Frameworks[key]; ok {
			return override, true
		}
	}
	return types.FrameworkBuildOverride{}, false
}

// buildApkoConfig constructs an ApkoConfig from a profile and options.
// The struct is later marshalled to YAML by yaml.Marshal.
func buildApkoConfig(p *types.Profile, opts Options) (types.ApkoConfig, error) {
	token := langVersionToken(p.Runtime)
	version := resolveVersion(p.Runtime, opts.LanguageVersion)

	packages := vsubSlice(append([]string{"wolfi-baselayout", opts.ProjectName}, p.Image.Packages...), token, version)

	runAs := p.Image.RunAs
	if runAs == 0 {
		runAs = 65532
	}

	// Entrypoint: profile wins (with {APP_NAME} and version token substituted);
	// fall back to Procfile "web:" command.
	entrypoint := vsub(applyProjectTemplates(p.Image.Entrypoint, opts.ProjectName), token, version)
	cmd := p.Image.Cmd
	if entrypoint == "" {
		if procCmd := readProcfileCmd(opts.SourceDir); procCmd != "" {
			parts := strings.Fields(procCmd)
			entrypoint = parts[0]
			if len(parts) > 1 {
				cmd = parts[1:]
			}
		}
	}

	cfg := types.ApkoConfig{
		Contents: types.ApkoContents{
			Keyring: []string{
				"https://packages.wolfi.dev/os/wolfi-signing.rsa.pub",
				"./melange.rsa.pub",
			},
			Repositories: []string{
				"https://packages.wolfi.dev/os",
				"./packages",
			},
			Packages: packages,
		},
		Entrypoint: types.ApkoEntrypoint{Command: entrypoint},
		Accounts: types.ApkoAccounts{
			RunAs:  fmt.Sprintf("%d", runAs),
			Users:  []types.ApkoUser{{Username: "nonroot", UID: runAs, GID: runAs}},
			Groups: []types.ApkoGroup{{Groupname: "nonroot", GID: runAs}},
		},
		Environment: vsubMap(p.Image.Env, token, version),
	}

	if len(cmd) > 0 {
		quoted := make([]string, len(cmd))
		for i, arg := range cmd {
			if strings.ContainsAny(arg, " \t") {
				quoted[i] = "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
			} else {
				quoted[i] = arg
			}
		}
		cfg.Cmd = strings.Join(quoted, " ")
	}

	// Dispatch to the language hook for runtime-specific apko patches.
	if hook, ok := hooks[p.Runtime]; ok {
		if err := hook.PatchApko(&cfg, p, opts); err != nil {
			return types.ApkoConfig{}, err
		}
	}

	return cfg, nil
}
