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
	// Node: auto-detect entry point from package.json when cmd is still the profile default.
	// Priority: apexpacks.yaml image.cmd > package.json scripts.start / main > /app (directory fallback).
	if cfg.Cmd == "/app/server.js" {
		if entry := readNodeEntrypoint(opts.SourceDir); entry != "" {
			fmt.Printf("  → node entry point detected: %s\n", entry)
			cfg.Cmd = entry
		}
	}
	return nil
}
