// Package config generates melange and apko config structs from a language profile.
// It has no knowledge of hooks or external tool execution — pure data transformation.
package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/apexpack/apexpack/internal/build/helpers"
	"github.com/apexpack/apexpack/internal/types"
)

// MarshalYAML encodes v to a 2-space indented YAML string.
func MarshalYAML(v any) (string, error) {
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// BuildMelangeConfig constructs a MelangeConfig from a profile and options.
// Hook patches are NOT applied here — the caller (build.Plan) applies them after.
func BuildMelangeConfig(p *types.Profile, opts types.BuildOptions) (types.MelangeConfig, error) {
	token := helpers.LangVersionToken(p.Runtime)
	version := helpers.ResolveVersion(p.Runtime, opts.LanguageVersion)
	if err := helpers.ValidateRuntimeVersion(p.Runtime, version); err != nil {
		return types.MelangeConfig{}, err
	}

	packages := helpers.VsubSlice(append([]string{"wolfi-baselayout"}, p.Build.Dependencies...), token, version)

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
			Env: helpers.VsubMap(p.Build.Env, token, version),
		},
		Pipeline: []types.MelangePipeline{{Runs: helpers.Vsub(helpers.ApplyProjectTemplates(p.Build.Command, opts.ProjectName), token, version)}},
	}

	override, found := helpers.ResolveOverride(p, opts.Framework, opts.PackageManager)
	if found {
		if len(override.Dependencies) > 0 {
			cfg.Environment.Contents.Packages = helpers.VsubSlice(append([]string{"wolfi-baselayout"}, override.Dependencies...), token, version)
		}
		if override.Command != "" {
			cfg.Pipeline = []types.MelangePipeline{{Runs: helpers.Vsub(helpers.ApplyProjectTemplates(override.Command, opts.ProjectName), token, version)}}
		}
		for k, v := range override.Env {
			if cfg.Environment.Env == nil {
				cfg.Environment.Env = make(map[string]string)
			}
			cfg.Environment.Env[k] = helpers.Vsub(v, token, version)
		}
	}

	if len(p.Test.Pipeline) > 0 {
		testPkgs := helpers.VsubSlice(p.Test.Packages, token, version)
		steps := make([]types.MelangePipeline, len(p.Test.Pipeline))
		for i, step := range p.Test.Pipeline {
			steps[i] = types.MelangePipeline{
				Runs: helpers.Vsub(helpers.ApplyProjectTemplates(step.Runs, opts.ProjectName), token, version),
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

// BuildApkoConfig constructs an ApkoConfig from a profile and options.
// Hook patches are NOT applied here — the caller applies them after.
func BuildApkoConfig(p *types.Profile, opts types.BuildOptions) (types.ApkoConfig, *types.ApkoHealthCheck, error) {
	token := helpers.LangVersionToken(p.Runtime)
	version := helpers.ResolveVersion(p.Runtime, opts.LanguageVersion)

	packages := helpers.VsubSlice(append([]string{"wolfi-baselayout", opts.ProjectName}, p.Image.Packages...), token, version)

	runAs := p.Image.RunAs
	if runAs == 0 {
		runAs = 65532
	}

	entrypoint := helpers.Vsub(helpers.ApplyProjectTemplates(p.Image.Entrypoint, opts.ProjectName), token, version)
	cmd := p.Image.Cmd
	if entrypoint == "" {
		if procCmd := helpers.ReadProcfileCmd(opts.SourceDir); procCmd != "" {
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
		Environment: helpers.VsubMap(p.Image.Env, token, version),
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

	var healthCheck *types.ApkoHealthCheck
	if hc := p.Image.HealthCheck; hc != nil && !hc.Disabled {
		if apkoHC := BuildHealthCheck(hc); apkoHC != nil {
			healthCheck = apkoHC
			if hc.HTTP != nil {
				cfg.Contents.Packages = EnsurePackage(cfg.Contents.Packages, "wget")
			}
			if hc.TCP != nil {
				cfg.Contents.Packages = EnsurePackage(cfg.Contents.Packages, "busybox")
			}
		}
	}

	return cfg, healthCheck, nil
}

// BuildHealthCheck converts a HealthCheckConfig into an ApkoHealthCheck struct.
func BuildHealthCheck(hc *types.HealthCheckConfig) *types.ApkoHealthCheck {
	interval := "30s"
	if hc.Interval != "" {
		interval = hc.Interval
	}
	timeout := "5s"
	if hc.Timeout != "" {
		timeout = hc.Timeout
	}
	startPeriod := "10s"
	if hc.StartPeriod != "" {
		startPeriod = hc.StartPeriod
	}
	retries := 3
	if hc.Retries > 0 {
		retries = hc.Retries
	}

	var cmd []string
	switch {
	case hc.HTTP != nil:
		port := 8080
		if hc.HTTP.Port > 0 {
			port = hc.HTTP.Port
		}
		path := "/"
		if hc.HTTP.Path != "" {
			path = hc.HTTP.Path
		}
		cmd = []string{"CMD", "/usr/bin/wget", "--spider", "-q",
			fmt.Sprintf("http://localhost:%d%s", port, path)}
	case hc.TCP != nil:
		cmd = []string{"CMD-SHELL",
			fmt.Sprintf("nc -z -w1 localhost %d", hc.TCP.Port)}
	default:
		return nil
	}

	return &types.ApkoHealthCheck{
		Command:     cmd,
		Interval:    interval,
		Timeout:     timeout,
		StartPeriod: startPeriod,
		Retries:     retries,
	}
}

// EnsurePackage appends pkg to pkgs if not already present.
func EnsurePackage(pkgs []string, pkg string) []string {
	for _, p := range pkgs {
		if p == pkg || strings.HasPrefix(p, pkg+"=") {
			return pkgs
		}
	}
	return append(pkgs, pkg)
}

