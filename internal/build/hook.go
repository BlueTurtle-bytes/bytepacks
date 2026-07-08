package build

import "github.com/apexpack/apexpack/internal/types"

// LanguageHook applies runtime-specific patches to melange and apko configs.
// Each runtime registers exactly one hook. Adding a new language means:
// create hook_<lang>.go, implement the interface, add one line to hooks map.
type LanguageHook interface {
	PatchMelange(cfg *types.MelangeConfig, p *types.Profile, opts Options) error
	PatchApko(cfg *types.ApkoConfig, p *types.Profile, opts Options) error
}

var hooks = map[string]LanguageHook{
	"java":      &javaHook{},
	"node":      &nodeHook{},
	"dotnet":    &dotnetHook{},
	"python":    &pythonHook{},
	"golang":    &goHook{},
	"webserver": &webserverHook{},
}
