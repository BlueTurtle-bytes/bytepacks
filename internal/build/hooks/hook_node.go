package hooks

import (
	"fmt"

	"github.com/apexpack/apexpack/internal/build/helpers"
	"github.com/apexpack/apexpack/internal/types"
)

type nodeHook struct{}

func (nodeHook) PatchMelange(cfg *types.MelangeConfig, p *types.Profile, opts types.BuildOptions) error {
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
