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
// It also neutralises the other things a config inherited from Docker or Paketo
// gets wrong here:
//
//   - "user": always ignored at runtime, since the master process is uid 65532,
//     but fatal to nginx -t as root when the account is absent.
//   - a privileged listen port, which uid 65532 cannot bind at all.
//   - "daemon": the image command already passes -g "daemon off;", and nginx
//     rejects a second value for a directive the -g block has set, so a config
//     carrying its own "daemon off;" aborts with "directive is duplicate".
//   - "{{port}}": the Paketo nginx buildpack rendered its templates at container
//     start. apexpack bakes a static config, so the placeholder would survive
//     into the image; see assert_nginx_rendered for the rest of that story.
//
// Only an absolute path is rewritten, so "access_log off;" and syslog: targets
// are left alone. Arguments after the path (levels=, 1 2, keys_zone=) are
// preserved — only the path token itself changes.
//
// The listen rules are deliberately strict: each requires the directive to
// terminate with ";" on the same line, so any shape the rewriter does not fully
// understand (a continuation, a semicolon-less line, an already-unprivileged
// port) is left exactly as it was rather than half-rewritten into a config
// nginx cannot parse. The profile's own check step still fails the build if an
// untouched privileged port survives, so a skipped rewrite is loud, not silent.
//
// No backreference in a replacement is ever followed by a digit. "\18080" reads
// as \1 followed by 8080 only if the sed in question stops a backreference at
// one digit; the build environment's busybox sed is not the sed this is
// developed against, and a reference swallowing the port would drop the ";" and
// produce exactly 'directive "listen" is not terminated by ";"'. Capturing so
// that every \N abuts a letter, ":" or ";" removes the question entirely.
const hardenNginxConf = `
harden_nginx_conf() {
  sed -E \
    -e 's#^([[:space:]]*(client_body|proxy|fastcgi|uwsgi|scgi)_temp_path[[:space:]]+)/[^[:space:];]*#\1/tmp/nginx-\2#' \
    -e 's#^([[:space:]]*proxy_cache_path[[:space:]]+)/[^[:space:];]*#\1/tmp/nginx-cache#' \
    -e 's#^([[:space:]]*pid[[:space:]]+)/[^[:space:];]*#\1/tmp/nginx.pid#' \
    -e 's#^([[:space:]]*error_log[[:space:]]+)/[^[:space:];]*#\1/dev/stderr#' \
    -e 's#^([[:space:]]*access_log[[:space:]]+)/[^[:space:];]*#\1/dev/stdout#' \
    -e 's|^([[:space:]]*)(user[[:space:]]+[^;]*;)|\1# apexpack: \2 ignored - image runs as uid 65532|' \
    -e 's|^([[:space:]]*)(daemon[[:space:]]+[^;]*;)|\1# apexpack: \2 supplied by the image command as -g|' \
    -e 's#[{][{][[:space:]]*port[[:space:]]*[}][}]#8080#g' \
    -e 's#^([[:space:]]*)listen[[:space:]]+80([[:space:]]+[^;]*)?;(.*)$#\1listen 8080\2;\3#' \
    -e 's#^([[:space:]]*)listen[[:space:]]+([^;[:space:]]*):80([[:space:]]+[^;]*)?;(.*)$#\1listen \2:8080\3;\4#' \
    -e 's#^([[:space:]]*)listen[[:space:]]+443([[:space:]]+[^;]*)?;(.*)$#\1listen 8443\2;\3#' \
    -e 's#^([[:space:]]*)listen[[:space:]]+([^;[:space:]]*):443([[:space:]]+[^;]*)?;(.*)$#\1listen \2:8443\3;\4#' \
    "$1"
}

report_nginx_paths() {
  echo "  -> nginx: writable paths after hardening:"
  grep -nE '^[[:space:]]*(pid|error_log|access_log|proxy_cache_path|[a-z_]*_temp_path)[[:space:]]' "$1" \
    | sed 's/^/       /' \
    || echo "       (none declared - apexpack defaults apply)"
}

# Paketo served the site straight out of its own build directory (/workspace,
# /source/dist). apexpack installs it at /usr/share/nginx/html, so a root carried
# over from that era points at nothing in the image and every request 404s — from
# a config that parses perfectly, so no syntax check catches it.
#
# Only a root that is genuinely absent from the package is moved, so a project
# deliberately serving some other directory it ships keeps it.
retarget_nginx_root() {
  _conf="$1"
  if [ ! -d "$DESTDIR/usr/share/nginx/html" ]; then
    return 0
  fi
  sed -nE 's#^[[:space:]]*root[[:space:]]+([^;[:space:]]+)[[:space:]]*;.*#\1#p' "$_conf" \
    | sort -u \
    | while read -r _root; do
        if [ -z "$_root" ]; then continue; fi
        if [ -d "$DESTDIR$_root" ]; then continue; fi
        echo "  -> nginx: root $_root is absent from the image - serving /usr/share/nginx/html instead"
        _esc=$(printf '%s' "$_root" | sed 's#\.#\\.#g')
        sed -E "s#^([[:space:]]*root[[:space:]]+)$_esc[[:space:]]*;#\1/usr/share/nginx/html;#" \
          "$_conf" > "$_conf.apexpack"
        # Same trap as above: never move an empty result over the config.
        if [ ! -s "$_conf.apexpack" ]; then
          rm -f "$_conf.apexpack"
          echo "✗ nginx: rewriting root $_root in $_conf produced an empty file" >&2
          exit 1
        fi
        mv "$_conf.apexpack" "$_conf"
      done
}

# The Paketo nginx buildpack rendered {{...}} placeholders when the container
# started. apexpack bakes a static config at build time, so anything still
# unrendered reaches nginx verbatim — and nginx reads "{" as a block opener, so
# "listen {{port}};" fails with
#
#	directive "listen" is not terminated by ";"
#
# naming a line that looks perfectly well formed. Failing here, with the offending
# lines quoted, beats shipping an image that cannot start.
assert_nginx_rendered() {
  if grep -nE '[{][{]' "$1"; then
    echo "✗ nginx: the lines above still hold template placeholders in $1" >&2
    echo "  These are Paketo nginx buildpack templates, rendered at container start." >&2
    echo "  apexpack builds a static config, so it renders {{port}} and nothing else:" >&2
    echo "  {{env \"VAR\"}} and {{module \"name\"}} have no build-time value." >&2
    echo "  Replace them with literal values in the project's nginx.conf." >&2
    return 1
  fi
}

# Every path nginx writes to has a compiled-in default under the build prefix
# (/var/lib/nginx on Wolfi), and nginx creates all of them in ngx_create_paths
# at startup whether or not the feature is ever used — a plain static site still
# gets a uwsgi temp directory. harden_nginx_conf can only rewrite a directive
# that is actually written down, so a project config that simply omits them
# inherits the prefix and dies on a read-only root filesystem with
#
#	mkdir() "/var/lib/nginx/tmp/uwsgi" failed (30: Read-only file system)
#
# naming a directory nothing in the config mentions. Writing each one down is
# what stops nginx reaching for its default at all; the apko path entries for
# /var/lib/nginx cannot help, because those directories are read-only at runtime
# however permissive their mode.
ensure_nginx_writable_paths() {
  _conf="$1"
  # A conf.d fragment has no http block. The apexpack main config already
  # declares all of these, and pid is valid only in the main context, so a
  # fragment has to be left exactly as it is.
  grep -qE '^[[:space:]]*http[[:space:]]*\{' "$_conf" || return 0

  _http=""
  for _spec in \
    "client_body_temp_path /tmp/nginx-client-body" \
    "proxy_temp_path /tmp/nginx-proxy" \
    "fastcgi_temp_path /tmp/nginx-fastcgi" \
    "uwsgi_temp_path /tmp/nginx-uwsgi" \
    "scgi_temp_path /tmp/nginx-scgi"; do
    _name=${_spec%% *}
    if ! grep -qE "^[[:space:]]*$_name[[:space:]]" "$_conf"; then
      _http="$_http    $_spec;
"
    fi
  done
  if [ -n "$_http" ]; then
    echo "  -> nginx: these writable paths were left to the build prefix - pinning them under /tmp:"
    printf '%s' "$_http" | sed 's/^ */       /'
    # The directives reach awk through a file, not -v: an assignment cannot
    # carry a newline, and busybox and BSD awk both abort with "newline in
    # string" instead of failing cleanly.
    printf '%s' "$_http" > "$_conf.apexpack-ins"
    if ! awk -v insfile="$_conf.apexpack-ins" '
      !ins_done && $0 ~ /^[[:space:]]*http[[:space:]]*\{/ {
        print
        while ((getline _l < insfile) > 0) print _l
        close(insfile)
        ins_done = 1
        next
      }
      { print }
    ' "$_conf" > "$_conf.apexpack" || [ ! -s "$_conf.apexpack" ]; then
      # A filter that fails still leaves its redirect target behind, empty.
      # Moving that over the config would ship an image whose nginx.conf is
      # blank - which nginx accepts, then serves nothing.
      rm -f "$_conf.apexpack" "$_conf.apexpack-ins"
      echo "✗ nginx: could not declare writable paths in $_conf" >&2
      return 1
    fi
    rm -f "$_conf.apexpack-ins"
    mv "$_conf.apexpack" "$_conf"
  fi

  # pid is main-context only, so finding it anywhere settles the question.
  # error_log may also appear inside http or server; a copy there still leaves
  # the main-context default in place, but that only costs an alert at startup,
  # whereas inserting a second main-level directive could not be undone.
  _main=""
  grep -qE '^[[:space:]]*pid[[:space:]]' "$_conf" || _main="${_main}pid /tmp/nginx.pid;
"
  grep -qE '^[[:space:]]*error_log[[:space:]]' "$_conf" || _main="${_main}error_log /dev/stderr notice;
"
  if [ -n "$_main" ]; then
    echo "  -> nginx: adding main-context defaults the config omitted:"
    printf '%s' "$_main" | sed 's/^/       /'
    printf '%s' "$_main" | cat - "$_conf" > "$_conf.apexpack"
    if [ ! -s "$_conf.apexpack" ]; then
      rm -f "$_conf.apexpack"
      echo "✗ nginx: could not add main-context defaults to $_conf" >&2
      return 1
    fi
    mv "$_conf.apexpack" "$_conf"
  fi
}

finalise_nginx_conf() {
  ensure_nginx_writable_paths "$1"
  retarget_nginx_root "$1"
  assert_nginx_rendered "$1"
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
  finalise_nginx_conf "$DESTDIR` + nginxConfPath + `"
  report_nginx_paths "$DESTDIR` + nginxConfPath + `"
else
  cat > "$DESTDIR` + nginxConfPath + `" <<'APEXPACK_NGINX_MAIN'
` + mainConf + `APEXPACK_NGINX_MAIN
  if [ -f nginx.conf ]; then
    echo "  -> nginx: project nginx.conf is a server fragment - installing as /etc/nginx/conf.d/default.conf"
    harden_nginx_conf nginx.conf > "$DESTDIR/etc/nginx/conf.d/default.conf"
    finalise_nginx_conf "$DESTDIR/etc/nginx/conf.d/default.conf"
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
