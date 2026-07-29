package helpers

import "github.com/apexpack/apexpack/internal/types"

// ResolveOverride finds the most specific FrameworkBuildOverride for the detected
// framework and package manager, using a three-level fallback:
//  1. {framework}-{packageManager}  e.g. "nextjs-pnpm"
//  2. {packageManager}              e.g. "pnpm"
//  3. {framework}                   e.g. "nextjs"
func ResolveOverride(p *types.Profile, framework, pm string) (types.FrameworkBuildOverride, bool) {
	if len(p.Build.Frameworks) == 0 {
		return types.FrameworkBuildOverride{}, false
	}
	var candidates []string
	if framework != "" && pm != "" {
		candidates = append(candidates, framework+"-"+pm)
	}
	if pm != "" {
		candidates = append(candidates, pm)
	}
	if framework != "" {
		candidates = append(candidates, framework)
	}
	for _, key := range candidates {
		if override, ok := p.Build.Frameworks[key]; ok {
			return override, true
		}
	}
	return types.FrameworkBuildOverride{}, false
}
