package hooks

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/apexpack/apexpack/internal/build/helpers"
	"github.com/apexpack/apexpack/internal/types"
)

type nodeHook struct{}

func (nodeHook) PatchMelange(cfg *types.MelangeConfig, p *types.Profile, opts types.BuildOptions) error {
	if p.Build.NpmRegistryURL == "" || os.Getenv("ARTI_USER") == "" {
		return nil
	}

	for _, key := range []string{"ARTI_USER", "ARTI_PASSWORD", "ARTI_TOKEN", "ARTI_REPO"} {
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

	// Prefer _authToken (raw Artifactory token) over _auth (base64 user:pass).
	// Both are computed in Go so the shell script contains no dynamic computation.
	var authLine string
	if token := os.Getenv("ARTI_TOKEN"); token != "" {
		authLine = scopeKey + "/:_authToken=" + token
	} else {
		encoded := base64.StdEncoding.EncodeToString([]byte(os.Getenv("ARTI_USER") + ":" + os.Getenv("ARTI_PASSWORD")))
		authLine = scopeKey + "/:_auth=" + encoded
	}

	yarnAuthIdent := base64.StdEncoding.EncodeToString([]byte(os.Getenv("ARTI_USER") + ":" + os.Getenv("ARTI_PASSWORD")))

	r := strings.NewReplacer(
		"__REGISTRY_URL__", registryURL,
		"__SCOPE_KEY__", scopeKey,
		"__ARTI_USER__", os.Getenv("ARTI_USER"),
		"__AUTH_LINE__", authLine,
		"__YARN_AUTH_IDENT__", yarnAuthIdent,
	)

	step := r.Replace(`mkdir -p /home/build

cat >> /home/build/.npmrc << APEXPACK_NPMRC_EOF
always-auth=true
strict-ssl=true
registry=__REGISTRY_URL__
__SCOPE_KEY__/:email=__ARTI_USER__
__AUTH_LINE__
APEXPACK_NPMRC_EOF

echo "→ npm registry: __REGISTRY_URL__"`)

	if opts.Framework == "yarn-berry" {
		// Append to the project's existing .yarnrc.yml (always present in Yarn Berry projects).
		// Yarn Berry merges the file top-to-bottom; later root keys override earlier ones.
		step += r.Replace(`

cat >> /home/build/.yarnrc.yml << APEXPACK_YARNRC_EOF
npmRegistryServer: "__REGISTRY_URL__"
npmRegistries:
  "__SCOPE_KEY__/":
    npmAuthIdent: "__YARN_AUTH_IDENT__"
    npmAlwaysAuth: true
APEXPACK_YARNRC_EOF`)
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
