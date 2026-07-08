package build

import (
	"fmt"

	"github.com/apexpack/apexpack/internal/types"
)

type nodeHook struct{}

func (nodeHook) PatchMelange(cfg *types.MelangeConfig, p *types.Profile, opts Options) error {
	return nil
}

func (nodeHook) PatchApko(cfg *types.ApkoConfig, p *types.Profile, opts Options) error {
	// Auto-detect entry point from package.json, but only when cmd is still the
	// node profile default. A user-supplied apexpacks.yaml image.cmd override will
	// already have changed cfg.Cmd away from this sentinel value, so we skip.
	if cfg.Cmd == "/app/server.js" {
		if entry := readNodeEntrypoint(opts.SourceDir); entry != "" {
			fmt.Printf("  → node entry point detected: %s\n", entry)
			cfg.Cmd = entry
		}
	}
	return nil
}
