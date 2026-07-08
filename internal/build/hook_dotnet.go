package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/apexpack/apexpack/internal/types"
)

type dotnetHook struct{}

func (dotnetHook) PatchMelange(cfg *types.MelangeConfig, p *types.Profile, opts Options) error {
	nugetTmplName := p.Build.NuGetSettingsTemplate
	if nugetTmplName == "" {
		nugetTmplName = "default"
	}
	nugetCustomTemplatePath := filepath.Join(opts.ProfilesDir, "templates", "nuget", nugetTmplName+".xml")
	_, nugetCustomTemplateExists := os.Stat(nugetCustomTemplatePath)
	if p.Build.NuGetMirrorURL != "" && (os.Getenv("ARTI_USER") != "" || nugetCustomTemplateExists == nil) {
		if cfg.Environment.Env == nil {
			cfg.Environment.Env = make(map[string]string)
		}
		for _, key := range []string{"ARTI_USER", "ARTI_PASSWORD"} {
			if val := os.Getenv(key); val != "" {
				if _, exists := cfg.Environment.Env[key]; !exists {
					cfg.Environment.Env[key] = val
				}
			}
		}
		nugetTmpl, err := loadNuGetTemplate(nugetTmplName, opts.ProfilesDir)
		if err != nil {
			return fmt.Errorf("nuget config template: %w", err)
		}
		nugetConfigXML := strings.ReplaceAll(nugetTmpl, "{{NUGET_MIRROR_URL}}", p.Build.NuGetMirrorURL)
		nugetConfigStep := fmt.Sprintf(
			"mkdir -p /home/build/.nuget/NuGet\n"+
				"cat > /home/build/.nuget/NuGet/NuGet.Config << APEXPACK_NUGET_EOF\n"+
				"%s"+
				"APEXPACK_NUGET_EOF\n"+
				"echo \"→ NuGet config: %s template, mirror: %s\"",
			nugetConfigXML, nugetTmplName, p.Build.NuGetMirrorURL,
		)
		cfg.Pipeline = append(
			[]types.MelangePipeline{{Runs: nugetConfigStep}},
			cfg.Pipeline...,
		)
	}
	return nil
}

func (dotnetHook) PatchApko(cfg *types.ApkoConfig, p *types.Profile, opts Options) error {
	return nil
}
