package hooks

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/apexpack/apexpack/internal/build/helpers"
	"github.com/apexpack/apexpack/internal/build/templates"
	"github.com/apexpack/apexpack/internal/types"
)

type nodeHook struct{}

func (nodeHook) PatchMelange(cfg *types.MelangeConfig, p *types.Profile, opts types.BuildOptions) error {
	if p.Build.NpmRegistryURL == "" || os.Getenv("ARTI_USER") == "" {
		fmt.Printf("  → npm registry: skipped (npm_registry_url or ARTI_USER not set)\n")
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

	// Build full registry URL: base from profile + ARTI_REPO from env (same pattern as dotnet/java hooks).
	registryURL := strings.TrimRight(p.Build.NpmRegistryURL, "/") + "/" + os.Getenv("ARTI_REPO") + "/"
	scope := "//" + strings.TrimPrefix(strings.TrimPrefix(registryURL, "https://"), "http://")
	scopeKey := strings.TrimRight(scope, "/")

	yarnAuthIdent := base64.StdEncoding.EncodeToString([]byte(os.Getenv("ARTI_USER") + ":" + os.Getenv("ARTI_PASSWORD")))

	r := strings.NewReplacer(
		"{{REGISTRY_URL}}", registryURL,
		"{{SCOPE_KEY}}", scopeKey,
		"{{ARTI_USER}}", os.Getenv("ARTI_USER"),
		"{{ARTI_PASSWORD}}", os.Getenv("ARTI_PASSWORD"),
		"{{YARN_AUTH_IDENT}}", yarnAuthIdent,
	)

	npmrcTmpl, err := templates.LoadNpmrcTemplate(opts.ProfilesDir)
	if err != nil {
		return err
	}
	npmrcContent := r.Replace(npmrcTmpl)
	fmt.Printf("  → npm registry: %s\n", registryURL)
	fmt.Printf("  → .npmrc:\n%s\n", npmrcContent)
	step := "mkdir -p /home/build\n" +
		"cat >> /home/build/.npmrc << APEXPACK_NPMRC_EOF\n" +
		npmrcContent +
		"APEXPACK_NPMRC_EOF"

	if opts.Framework == "yarn-berry" {
		// Append to the project's existing .yarnrc.yml (always present in Yarn Berry projects).
		// Yarn Berry merges the file top-to-bottom; later root keys override earlier ones.
		yarnrcTmpl, err := templates.LoadYarnrcTemplate(opts.ProfilesDir)
		if err != nil {
			return err
		}
		yarnrcContent := r.Replace(yarnrcTmpl)
		step += "\ncat >> /home/build/.yarnrc.yml << APEXPACK_YARNRC_EOF\n" +
			yarnrcContent +
			"APEXPACK_YARNRC_EOF"
	}

	cfg.Pipeline = append([]types.MelangePipeline{{Runs: step}}, cfg.Pipeline...)
	return nil
}

func (nodeHook) PatchApko(cfg *types.ApkoConfig, p *types.Profile, opts types.BuildOptions) error {
	if cfg.Cmd == "/app/server.js" {
		if entry := helpers.ReadNodeEntrypoint(opts.SourceDir); entry != "" {
			fmt.Printf("  → node entry point detected: %s\n", entry)
			cfg.Cmd = entry
		}
	}
	return nil
}
