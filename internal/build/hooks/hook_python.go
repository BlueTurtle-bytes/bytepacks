package hooks

import "github.com/apexpack/apexpack/internal/types"

type pythonHook struct{}

func (pythonHook) PatchMelange(cfg *types.MelangeConfig, p *types.Profile, opts types.BuildOptions) error {
	return nil
}

func (pythonHook) PatchApko(cfg *types.ApkoConfig, p *types.Profile, opts types.BuildOptions) error {
	return nil
}
