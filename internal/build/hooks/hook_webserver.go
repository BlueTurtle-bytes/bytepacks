package hooks

import (
	"strings"

	"github.com/apexpack/apexpack/internal/build/templates"
	"github.com/apexpack/apexpack/internal/types"
)

// nginxConfPath is the config apexpack ships and nginx is told to load with -c.
//
// Writing to /etc/nginx/nginx.conf is not an option: that file belongs to the
// nginx-stable-config apk, and apk refuses to let the melange-built package
// overwrite a file another package owns.
const nginxConfPath = "/etc/nginx/apexpack.conf"

// hardenNginxConf is a shell function injected into the pipeline step.
//
// A config written for the official Docker nginx image points its writable
// paths at /var/cache/nginx and its logs at /var/log/nginx. Both are fatal
// here: the image runs with a read-only root filesystem, and nginx creates
// every temp path in ngx_create_paths at startup — so it dies before it serves
// anything, with mkdir() ... failed rather than anything about permissions.
//
// It also neutralises the two other things a Docker-era config gets wrong on a
// non-root image: "user" (always ignored at runtime, since the master process
// is uid 65532, but fatal to nginx -t as root when the account is absent) and a
// privileged listen port, which uid 65532 cannot bind at all.
//
// Only an absolute path is rewritten, so "access_log off;" and syslog: targets
// are left alone. Arguments after the path (levels=, 1 2, keys_zone=) are
// preserved — only the path token itself changes, and an already-unprivileged
// listen port is not touched.
const hardenNginxConf = `
harden_nginx_conf() {
  sed -E \
    -e 's#^([[:space:]]*(client_body|proxy|fastcgi|uwsgi|scgi)_temp_path[[:space:]]+)/[^[:space:];]*#\1/tmp/nginx-\2#' \
    -e 's#^([[:space:]]*proxy_cache_path[[:space:]]+)/[^[:space:];]*#\1/tmp/nginx-cache#' \
    -e 's#^([[:space:]]*pid[[:space:]]+)/[^[:space:];]*#\1/tmp/nginx.pid#' \
    -e 's#^([[:space:]]*error_log[[:space:]]+)/[^[:space:];]*#\1/dev/stderr#' \
    -e 's#^([[:space:]]*access_log[[:space:]]+)/[^[:space:];]*#\1/dev/stdout#' \
    -e 's|^([[:space:]]*)(user[[:space:]]+[^;]*;)|\1# apexpack: \2 ignored - image runs as uid 65532|' \
    -e 's#^([[:space:]]*listen[[:space:]]+)80([[:space:];])#\18080\2#' \
    -e 's#^([[:space:]]*listen[[:space:]]+[^;[:space:]]*:)80([[:space:];])#\18080\2#' \
    -e 's#^([[:space:]]*listen[[:space:]]+)443([[:space:];])#\18443\2#' \
    -e 's#^([[:space:]]*listen[[:space:]]+[^;[:space:]]*:)443([[:space:];])#\18443\2#' \
    "$1"
}

report_nginx_paths() {
  echo "  -> nginx: writable paths after hardening:"
  grep -nE '^[[:space:]]*(pid|error_log|access_log|proxy_cache_path|[a-z_]*_temp_path)[[:space:]]' "$1" \
    | sed 's/^/       /' \
    || echo "       (none declared - apexpack defaults apply)"
}
`

type webserverHook struct{}

func (webserverHook) PatchMelange(cfg *types.MelangeConfig, p *types.Profile, opts types.BuildOptions) error {
	if err := patchNpmRegistry(cfg, p, opts); err != nil {
		return err
	}
	step, err := nginxConfigStep(opts.ProjectSubpath, opts.ProfilesDir)
	if err != nil {
		return err
	}
	cfg.Pipeline = append(cfg.Pipeline, types.MelangePipeline{Runs: step})
	return nil
}

// nginxConfigStep installs the nginx configuration into the package.
//
// Wolfi ships upstream's vanilla nginx.conf, which includes nothing but
// mime.types, listens on 80 and roots at the build prefix (/var/lib/nginx/html)
// — so a project config dropped into conf.d or http.d is never read, and every
// writable path lands outside /tmp. Both templates replace it wholesale; either
// can be overridden per-deployment under <profilesDir>/templates/nginx/.
//
// It runs as its own pipeline step, after the profile's build command has
// produced /usr/share/nginx/html, so it has to re-enter the subpath itself —
// only the first step gets the cd that BuildMelangeConfig prepends.
func nginxConfigStep(projectSubpath, profilesDir string) (string, error) {
	mainConf, err := templates.LoadNginxConfTemplate(profilesDir)
	if err != nil {
		return "", err
	}
	defaultServer, err := templates.LoadNginxDefaultServerTemplate(profilesDir)
	if err != nil {
		return "", err
	}
	mainConf = strings.ReplaceAll(mainConf, "{{NGINX_CONF_PATH}}", nginxConfPath)

	// A template edited by hand may lose its trailing newline; without one the
	// heredoc terminator would be glued to the last config line.
	mainConf = ensureTrailingNewline(mainConf)
	defaultServer = ensureTrailingNewline(defaultServer)

	var b strings.Builder
	if projectSubpath != "" {
		b.WriteString("cd " + projectSubpath + "\n")
	}
	b.WriteString("set -e\nDESTDIR=\"${{targets.destdir}}\"\nmkdir -p \"$DESTDIR/etc/nginx/conf.d\"\n")
	b.WriteString(hardenNginxConf)
	b.WriteString(`
# A project nginx.conf declaring events{} or http{} is a complete config and
# replaces ours; anything else is a server-level fragment for conf.d. Guessing
# wrong either way means nginx silently runs a config nobody wrote.
if [ -f nginx.conf ] && grep -qE '^[[:space:]]*(events|http)[[:space:]]*\{' nginx.conf; then
  echo "  -> nginx: project nginx.conf is a full config - installing as ` + nginxConfPath + `"
  harden_nginx_conf nginx.conf > "$DESTDIR` + nginxConfPath + `"
  report_nginx_paths "$DESTDIR` + nginxConfPath + `"
else
  cat > "$DESTDIR` + nginxConfPath + `" <<'APEXPACK_NGINX_MAIN'
` + mainConf + `APEXPACK_NGINX_MAIN
  if [ -f nginx.conf ]; then
    echo "  -> nginx: project nginx.conf is a server fragment - installing as /etc/nginx/conf.d/default.conf"
    harden_nginx_conf nginx.conf > "$DESTDIR/etc/nginx/conf.d/default.conf"
    report_nginx_paths "$DESTDIR/etc/nginx/conf.d/default.conf"
  else
    echo "  -> nginx: no project nginx.conf - using the apexpack default server block"
    cat > "$DESTDIR/etc/nginx/conf.d/default.conf" <<'APEXPACK_NGINX_SERVER'
` + defaultServer + `APEXPACK_NGINX_SERVER
  fi
fi
`)
	return b.String(), nil
}

func ensureTrailingNewline(s string) string {
	if s == "" || strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

func (webserverHook) PatchApko(cfg *types.ApkoConfig, p *types.Profile, opts types.BuildOptions) error {
	cfg.Paths = []types.ApkoPath{
		// nginx writes its pid and every temp path here. On a cluster that sets
		// readOnlyRootFilesystem the pod still needs an emptyDir mounted at
		// /tmp — this only guarantees the directory exists in the image.
		{Path: "/tmp", Type: "directory", Permissions: 0o777},
		// Retained for projects whose own full nginx.conf still points at the
		// prefix; unused by the apexpack default config.
		{Path: "/var/lib/nginx", Type: "directory", Permissions: 0o777},
		{Path: "/var/lib/nginx/logs", Type: "directory", Permissions: 0o777},
		{Path: "/var/lib/nginx/tmp", Type: "directory", Permissions: 0o777},
		{Path: "/var/log/nginx", Type: "directory", Permissions: 0o777},
		{Path: "/run", Type: "directory", Permissions: 0o777},
	}
	return nil
}
