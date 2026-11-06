package hooks

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/apexpack/apexpack/internal/build/templates"
	"github.com/apexpack/apexpack/internal/types"
)

type pythonHook struct{}

func (pythonHook) PatchMelange(cfg *types.MelangeConfig, p *types.Profile, opts types.BuildOptions) error {
	if p.Build.PypiRegistryURL == "" || os.Getenv("ARTI_USER") == "" {
		fmt.Printf("  → pypi registry: skipped (pypi_registry_url or ARTI_USER not set)\n")
		return nil
	}

	for _, key := range []string{"ARTI_USER", "ARTI_PASSWORD", "ARTI_REPO"} {
		if val := os.Getenv(key); val != "" {
			if cfg.Environment.Env == nil {
				cfg.Environment.Env = make(map[string]string)
			}
			if _, exists := cfg.Environment.Env[key]; !exists {
				cfg.Environment.Env[key] = val
			}
		}
	}

	registryURL := strings.TrimRight(p.Build.PypiRegistryURL, "/") + "/" + os.Getenv("ARTI_REPO")

	// pip.conf index-url embeds credentials: https://user:pass@host/path/repo/simple
	parsed, err := url.Parse(registryURL)
	if err != nil {
		return fmt.Errorf("invalid pypi_registry_url: %w", err)
	}
	registryHost := parsed.Hostname()
    parsed.User = url.UserPassword(os.Getenv("ARTI_USER"), os.Getenv("ARTI_PASSWORD"))
	registryURLWithAuth := parsed.String()

	r := strings.NewReplacer(
		"{{REGISTRY_URL}}", registryURL,
		"{{REGISTRY_URL_WITH_AUTH}}", registryURLWithAuth,
		"{{REGISTRY_HOST}}", registryHost,
		"{{ARTI_USER}}", os.Getenv("ARTI_USER"),
		"{{ARTI_PASSWORD}}", os.Getenv("ARTI_PASSWORD"),
	)

	pipConfTmpl, err := templates.LoadPipConfTemplate(opts.ProfilesDir)
	if err != nil {
		return err
	}
	pypircTmpl, err := templates.LoadPypircTemplate(opts.ProfilesDir)
	if err != nil {
		return err
	}

	pipConfContent := r.Replace(pipConfTmpl)
	pypircContent := r.Replace(pypircTmpl)

	fmt.Printf("  → pypi registry: %s\n", registryURL)
	fmt.Printf("  → pip.conf:\n%s\n", pipConfContent)

	step := "mkdir -p /home/build/.config/pip /home/build\n" +
		"cat >> /home/build/.config/pip/pip.conf << APEXPACK_PIPCONF_EOF\n" +
		pipConfContent +
		"APEXPACK_PIPCONF_EOF\n" +
		"cat >> /home/build/.pypirc << APEXPACK_PYPIRC_EOF\n" +
		pypircContent +
		"APEXPACK_PYPIRC_EOF"

	cfg.Pipeline = append([]types.MelangePipeline{{Runs: step}}, cfg.Pipeline...)
	return nil
}

func (pythonHook) PatchApko(cfg *types.ApkoConfig, p *types.Profile, opts types.BuildOptions) error {
	return nil
}
