package helpers

import (
	"testing"

	"github.com/apexpack/apexpack/internal/types"
)

func TestResolveOverride(t *testing.T) {
	p := &types.Profile{
		Build: types.BuildConfig{
			Frameworks: map[string]types.FrameworkBuildOverride{
				"nextjs":      {Command: "next build"},
				"pnpm":        {Command: "pnpm build"},
				"nextjs-pnpm": {Command: "pnpm next build"},
			},
		},
	}

	got, ok := ResolveOverride(p, "nextjs", "pnpm")
	if !ok || got.Command != "pnpm next build" {
		t.Errorf("framework+pm: got %q ok=%v, want pnpm next build", got.Command, ok)
	}

	got, ok = ResolveOverride(p, "remix", "pnpm")
	if !ok || got.Command != "pnpm build" {
		t.Errorf("pm fallback: got %q ok=%v, want pnpm build", got.Command, ok)
	}

	got, ok = ResolveOverride(p, "nextjs", "")
	if !ok || got.Command != "next build" {
		t.Errorf("framework fallback: got %q ok=%v, want next build", got.Command, ok)
	}

	_, ok = ResolveOverride(p, "unknown", "yarn")
	if ok {
		t.Error("expected no match for unknown framework+pm")
	}

	_, ok = ResolveOverride(&types.Profile{}, "nextjs", "pnpm")
	if ok {
		t.Error("expected no match when no frameworks defined")
	}
}
