package hooks

import "github.com/apexpack/apexpack/internal/types"

type webserverHook struct{}

func (webserverHook) PatchMelange(cfg *types.MelangeConfig, p *types.Profile, opts types.BuildOptions) error {
	return nil
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
