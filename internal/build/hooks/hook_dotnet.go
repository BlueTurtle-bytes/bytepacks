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

// global.json pins the SDK version for developer machines. In the container the SDK
// is controlled by build.dependencies, so the pin is irrelevant and can cause failures
// when Wolfi ships a different feature band than what the project requests.
const globalJSONPatch = `find /home/build -maxdepth 4 -name "global.json" -delete 2>/dev/null; true`

func (dotnetHook) PatchMelange(cfg *types.MelangeConfig, p *types.Profile, opts types.BuildOptions) error {
	// Suppress auto-detected SO deps: dotnet publish bundles native libs (e.g. librdkafka)
	// compiled against old libsasl2.so.2 which doesn't exist in Wolfi. Runtime deps are
	// satisfied by image packages (aspnet-runtime, cyrus-sasl-heimdal-libs) rather than
	// APK metadata.
	cfg.Package.Options = &types.MelangePackageOptions{NoDepends: true}

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
		artifactoryRepo := os.Getenv("ARTI_REPO")
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

	// Prepend last so it lands at position 0, before any dotnet invocation.
	// global.json pins SDK to a specific feature band (e.g. 10.0.400) but Wolfi ships
	// a different band (e.g. 10.0.111). Remove it — the SDK is controlled by build.dependencies.
	cfg.Pipeline = append(
		[]types.MelangePipeline{{Runs: globalJSONPatch}},
		cfg.Pipeline...,
	)
	return nil
}

func (dotnetHook) PatchApko(cfg *types.ApkoConfig, p *types.Profile, opts types.BuildOptions) error {
	return nil
}
