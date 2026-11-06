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

	const pipConfPath = "/home/build/.config/pip/pip.conf"
	// Configure Poetry http-basic credentials for every [[tool.poetry.source]] entry.
	// awk extracts the name field from each source section; poetry config stores it
	// in ~/.config/pypoetry/auth.toml which Poetry reads before resolving packages.
	poetryCredStep := `if [ -f pyproject.toml ] && command -v poetry >/dev/null 2>&1; then
  awk '/^\[\[tool\.poetry\.source\]\]/{f=1} f && /^name\s*=/{gsub(/.*=\s*"|"\s*$/,""); print; f=0}' pyproject.toml 2>/dev/null \
    | while IFS= read -r src; do poetry config "http-basic.$src" "$ARTI_USER" "$ARTI_PASSWORD" 2>/dev/null || true; done
fi`

	step := "mkdir -p /home/build/.config/pip\n" +
		"cat >> " + pipConfPath + " << APEXPACK_PIPCONF_EOF\n" +
		pipConfContent +
		"APEXPACK_PIPCONF_EOF\n" +
		"cat >> /home/build/.pypirc << APEXPACK_PYPIRC_EOF\n" +
		pypircContent +
		"APEXPACK_PYPIRC_EOF\n" +
		poetryCredStep

	// Set explicit env vars so pip finds the config regardless of HOME.
	if cfg.Environment.Env == nil {
		cfg.Environment.Env = make(map[string]string)
	}
	cfg.Environment.Env["PIP_CONFIG_FILE"] = pipConfPath
	cfg.Environment.Env["PIP_INDEX_URL"] = registryURLWithAuth + "/simple"

	cfg.Pipeline = append([]types.MelangePipeline{{Runs: step}}, cfg.Pipeline...)
	return nil
}

func (pythonHook) PatchApko(cfg *types.ApkoConfig, p *types.Profile, opts types.BuildOptions) error {
	return nil
}
