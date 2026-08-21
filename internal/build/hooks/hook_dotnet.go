package hooks

import (
	"fmt"
	"os"

	"github.com/apexpack/apexpack/internal/types"
)

const minimalNuGetConfig = `<?xml version="1.0" encoding="utf-8"?>
<configuration>
  <packageSources>
  </packageSources>
</configuration>
`

type dotnetHook struct{}

func (dotnetHook) PatchMelange(cfg *types.MelangeConfig, p *types.Profile, opts types.BuildOptions) error {
	if p.Build.NuGetMirrorURL != "" && os.Getenv("ARTI_USER") != "" {
		if cfg.Environment.Env == nil {
			cfg.Environment.Env = make(map[string]string)
		}
		for _, key := range []string{"ARTI_USER", "ARTI_PASSWORD", "ARTI_REPO"} {
			if val := os.Getenv(key); val != "" {
				if _, exists := cfg.Environment.Env[key]; !exists {
					cfg.Environment.Env[key] = val
				}
			}
		}
		nugetConfigStep := "mkdir -p /home/build/.nuget/NuGet\n" +
			"cat > /home/build/.nuget/NuGet/NuGet.Config << APEXPACK_NUGET_EOF\n" +
			minimalNuGetConfig +
			"APEXPACK_NUGET_EOF"
		artifactoryRepo := os.Getenv("ARTIFACTORY_REPO")
		if artifactoryRepo == "" {
			artifactoryRepo = "substonic-nuget"
		}
		pipelineSource := fmt.Sprintf(
			"dotnet nuget add source %s/%s -n Artifactory -u %s -p %s --store-password-in-clear-text --configfile /home/build/.nuget/NuGet/NuGet.Config",
			p.Build.NuGetMirrorURL, artifactoryRepo, os.Getenv("ARTI_USER"), os.Getenv("ARTI_PASSWORD"),
		)
		cfg.Pipeline = append(
			[]types.MelangePipeline{
				{Runs: nugetConfigStep},
				{Runs: pipelineSource},
			},
			cfg.Pipeline...,
		)
	}
	return nil
}

func (dotnetHook) PatchApko(cfg *types.ApkoConfig, p *types.Profile, opts types.BuildOptions) error {
	return nil
}
