package build

import "github.com/apexpack/apexpack/internal/types"

type goHook struct{}

func (goHook) PatchMelange(cfg *types.MelangeConfig, p *types.Profile, opts Options) error {
	return nil
}

func (goHook) PatchApko(cfg *types.ApkoConfig, p *types.Profile, opts Options) error {
	return nil
}
