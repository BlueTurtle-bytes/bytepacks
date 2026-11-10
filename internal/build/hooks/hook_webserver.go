package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/apexpack/apexpack/internal/types"
)

type webserverHook struct{}

func (webserverHook) PatchMelange(cfg *types.MelangeConfig, p *types.Profile, opts types.BuildOptions) error {
	fixPackageJSONOverrides(opts.SourceDir)
	return patchNpmRegistry(cfg, p, opts)
}

func (webserverHook) PatchApko(cfg *types.ApkoConfig, p *types.Profile, opts types.BuildOptions) error {
	cfg.Paths = []types.ApkoPath{
		{Path: "/var/lib/nginx", Type: "directory", Permissions: 0o777},
		{Path: "/var/lib/nginx/logs", Type: "directory", Permissions: 0o777},
		{Path: "/var/lib/nginx/tmp", Type: "directory", Permissions: 0o777},
		{Path: "/var/log/nginx", Type: "directory", Permissions: 0o777},
		{Path: "/run", Type: "directory", Permissions: 0o777},
	}
	return nil
}

// fixPackageJSONOverrides removes override entries that also appear as direct or dev
// dependencies. npm 7+ throws EOVERRIDE in that case even when the values are identical.
// The override is redundant — the direct dependency already pins the version — so
// removing it is safe. Errors are silently ignored; a broken package.json will surface
// as a clearer error when npm runs.
func fixPackageJSONOverrides(srcDir string) {
	path := filepath.Join(srcDir, "package.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	var pkg map[string]json.RawMessage
	if err := json.Unmarshal(data, &pkg); err != nil {
		return
	}

	rawOverrides, ok := pkg["overrides"]
	if !ok {
		return
	}
	var overrides map[string]json.RawMessage
	if err := json.Unmarshal(rawOverrides, &overrides); err != nil {
		return
	}

	direct := make(map[string]bool)
	for _, field := range []string{"dependencies", "devDependencies"} {
		var deps map[string]json.RawMessage
		if err := json.Unmarshal(pkg[field], &deps); err == nil {
			for name := range deps {
				direct[name] = true
			}
		}
	}

	changed := false
	for name := range overrides {
		if direct[name] {
			delete(overrides, name)
			changed = true
		}
	}
	if !changed {
		return
	}

	if len(overrides) == 0 {
		delete(pkg, "overrides")
	} else {
		raw, err := json.Marshal(overrides)
		if err != nil {
			return
		}
		pkg["overrides"] = raw
	}

	out, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, append(out, '\n'), 0644)
}
