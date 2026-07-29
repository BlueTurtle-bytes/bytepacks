// Package hooks defines the LanguageHook interface and the registry of
// built-in language hook implementations.
package hooks

import (
	"github.com/apexpack/apexpack/internal/types"
)

// LanguageHook applies runtime-specific patches to melange and apko configs.
type LanguageHook interface {
	PatchMelange(cfg *types.MelangeConfig, p *types.Profile, opts types.BuildOptions) error
	PatchApko(cfg *types.ApkoConfig, p *types.Profile, opts types.BuildOptions) error
}

var registry = map[string]LanguageHook{
	"java":      &javaHook{},
	"node":      &nodeHook{},
	"dotnet":    &dotnetHook{},
	"python":    &pythonHook{},
	"golang":    &goHook{},
	"webserver": &webserverHook{},
}

// Get returns the hook for the given runtime, or false if none is registered.
func Get(runtime string) (LanguageHook, bool) {
	h, ok := registry[runtime]
	return h, ok
}
