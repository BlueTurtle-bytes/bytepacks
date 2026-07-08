package build

import (
	"github.com/apexpack/apexpack/internal/types"
)

type webserverHook struct{}

func (webserverHook) PatchMelange(cfg *types.MelangeConfig, p *types.Profile, opts Options) error {
	return nil
}

func (webserverHook) PatchApko(cfg *types.ApkoConfig, p *types.Profile, opts Options) error {
	// Webserver: set permissions on nginx runtime dirs after all packages are
	// installed. This overrides any restrictive permissions nginx-mainline-config
	// sets on these directories, allowing nginx to run as a non-root user.
	cfg.Paths = []types.ApkoPath{
		{Path: "/var/lib/nginx", Type: "directory", Permissions: 0o777},
		{Path: "/var/lib/nginx/logs", Type: "directory", Permissions: 0o777},
		{Path: "/var/lib/nginx/tmp", Type: "directory", Permissions: 0o777},
		{Path: "/var/log/nginx", Type: "directory", Permissions: 0o777},
		{Path: "/run", Type: "directory", Permissions: 0o777},
	}
	return nil
}
