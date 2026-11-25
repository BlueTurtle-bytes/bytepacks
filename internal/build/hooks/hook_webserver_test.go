package hooks

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/apexpack/apexpack/internal/build/config"
	"github.com/apexpack/apexpack/internal/build/templates"
	"github.com/apexpack/apexpack/internal/types"
	"gopkg.in/yaml.v3"
)

// mainConf returns the embedded default main config (no profilesDir override).
func mainConf(t *testing.T) string {
	t.Helper()
	c, err := templates.LoadNginxConfTemplate("")
	if err != nil {
		t.Fatalf("LoadNginxConfTemplate: %v", err)
	}
	return c
}

func defaultServer(t *testing.T) string {
	t.Helper()
	c, err := templates.LoadNginxDefaultServerTemplate("")
	if err != nil {
		t.Fatalf("LoadNginxDefaultServerTemplate: %v", err)
	}
	return c
}

func step(t *testing.T, subpath, profilesDir string) string {
	t.Helper()
	s, err := nginxConfigStep(subpath, profilesDir)
	if err != nil {
		t.Fatalf("nginxConfigStep: %v", err)
	}
	return s
}

// The config nginx actually loads must keep every writable path under /tmp —
// the images run with a read-only root filesystem, and the prefix-relative
// defaults (/var/lib/nginx/logs, /var/lib/nginx/tmp) are what broke before.
func TestNginxMainConfWritablePathsAreUnderTmp(t *testing.T) {
	conf := mainConf(t)
	for _, directive := range []string{
		"pid",
		"client_body_temp_path",
		"proxy_temp_path",
		"fastcgi_temp_path",
		"uwsgi_temp_path",
		"scgi_temp_path",
	} {
		line := directiveLine(conf, directive)
		if line == "" {
			t.Errorf("%s is not set; nginx would fall back to its prefix default", directive)
			continue
		}
		if !strings.Contains(line, "/tmp/") {
			t.Errorf("%s points outside /tmp: %q", directive, line)
		}
	}
	if !strings.Contains(conf, "error_log  /dev/stderr") {
		t.Error("error_log must go to stderr, not a file on a read-only filesystem")
	}
}

// Wolfi's stock nginx.conf includes nothing, which is why project configs
// dropped into http.d were silently ignored. Ours has to pull in conf.d.
func TestNginxMainConfIncludesConfD(t *testing.T) {
	conf := mainConf(t)
	if !strings.Contains(conf, "include /etc/nginx/conf.d/*.conf;") {
		t.Error("main config must include conf.d, or project fragments are dead files")
	}
	// mime.types ships in nginx-stable-config; the profile has to install it.
	if !strings.Contains(conf, "include       /etc/nginx/mime.types;") {
		t.Error("main config must include mime.types")
	}
}

// The default server block only applies when the project has none. It must
// listen on a port uid 65532 can bind and serve where the build puts files.
func TestNginxDefaultServerPortAndRoot(t *testing.T) {
	conf := defaultServer(t)
	if !strings.Contains(conf, "listen      8080;") {
		t.Error("default server must listen on 8080; 80 is unbindable as uid 65532")
	}
	if !strings.Contains(conf, "root        /usr/share/nginx/html;") {
		t.Error("default server root must match where the build command writes files")
	}
	// $uri must survive into the step verbatim — the heredoc is quoted, so the
	// build shell must not have expanded it to the empty string.
	if !strings.Contains(step(t, "", ""), "try_files $uri $uri/ /index.html;") {
		t.Error("$uri was mangled on its way into the pipeline step")
	}
}

// Every {{TOKEN}} has to be substituted before the config reaches the image;
// a leftover token is a config nginx will reject at best.
func TestNginxStepLeavesNoUnsubstitutedTokens(t *testing.T) {
	s := step(t, "", "")
	if strings.Contains(s, "{{NGINX_CONF_PATH}}") {
		t.Error("{{NGINX_CONF_PATH}} was not substituted")
	}
	// ${{targets.destdir}} is melange's own token and must survive.
	if !strings.Contains(s, "${{targets.destdir}}") {
		t.Error("melange's ${{targets.destdir}} must be left for melange to expand")
	}
	// apexpack's own tokens are {{UPPER_SNAKE}}. The step also quotes Paketo's
	// lowercase placeholders ({{port}}, {{env ...}}) in its diagnostics, which
	// are literal text and must survive, so match only apexpack's shape.
	if m := regexp.MustCompile(`\{\{[A-Z][A-Z0-9_]*\}\}`).FindString(s); m != "" {
		t.Errorf("unsubstituted template token remains: %q", m)
	}
}

// A deployment can replace either config without rebuilding the binary, which
// is the whole point of putting them in templates/ rather than in consts.
func TestNginxTemplatesHonourProfilesDirOverride(t *testing.T) {
	dir := t.TempDir()
	nginxDir := filepath.Join(dir, "templates", "nginx")
	if err := os.MkdirAll(nginxDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// No trailing newline: a hand-edited file often loses it, and the heredoc
	// terminator must still land on its own line.
	custom := "# custom\npid /tmp/custom.pid;\nevents { worker_connections 1; }"
	if err := os.WriteFile(filepath.Join(nginxDir, "nginx.conf.tmpl"), []byte(custom), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	s := step(t, "", dir)
	if !strings.Contains(s, "pid /tmp/custom.pid;") {
		t.Error("the override under <profilesDir>/templates/nginx was not used")
	}
	if !strings.Contains(s, "\n# custom\n") {
		t.Error("override content did not land in the heredoc")
	}
	if !strings.Contains(s, "worker_connections 1; }\nAPEXPACK_NGINX_MAIN\n") {
		t.Error("heredoc terminator must start its own line even without a trailing newline")
	}
	// The default server template was not overridden, so it stays embedded.
	if !strings.Contains(s, "listen      8080;") {
		t.Error("a partial override must not drop the other template")
	}
}

// A full config replaces ours outright; a fragment is included. Both branches
// have to exist in the generated step, keyed off events{}/http{}.
func TestNginxConfigStepHandlesBothConfigShapes(t *testing.T) {
	s := step(t, "", "")
	for _, want := range []string{
		`grep -qE '^[[:space:]]*(events|http)([[:space:]]*\{|[[:space:]]*$)' nginx.conf`,
		`harden_nginx_conf nginx.conf > "$DESTDIR/etc/nginx/apexpack.conf"`,
		`harden_nginx_conf nginx.conf > "$DESTDIR/etc/nginx/conf.d/default.conf"`,
		"APEXPACK_NGINX_MAIN",
		"APEXPACK_NGINX_SERVER",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("generated step is missing %q", want)
		}
	}
	if strings.HasPrefix(s, "cd ") {
		t.Error("no subpath was set, so the step must not cd anywhere")
	}
}

// Only the first pipeline step gets the cd that BuildMelangeConfig prepends,
// so this one — appended after it — has to re-enter the subpath itself or it
// looks for nginx.conf at the mounted root of a multi-project tree.
func TestNginxConfigStepEntersSubpath(t *testing.T) {
	s := step(t, "apps/portal", "")
	if !strings.HasPrefix(s, "cd apps/portal\n") {
		t.Errorf("step must cd into the subpath first, got:\n%s", firstLine(s))
	}
}

// The step is appended, not inserted, so it runs after the build command has
// created /usr/share/nginx/html.
func TestWebserverHookAppendsConfigStepLast(t *testing.T) {
	cfg := &types.MelangeConfig{
		Pipeline: []types.MelangePipeline{{Runs: "echo build"}},
	}
	p := &types.Profile{Runtime: "webserver"}
	if err := (webserverHook{}).PatchMelange(cfg, p, types.BuildOptions{}); err != nil {
		t.Fatalf("PatchMelange: %v", err)
	}
	if len(cfg.Pipeline) != 2 {
		t.Fatalf("expected the build step plus an nginx step, got %d", len(cfg.Pipeline))
	}
	if cfg.Pipeline[0].Runs != "echo build" {
		t.Error("the profile's build command must stay first")
	}
	if !strings.Contains(cfg.Pipeline[1].Runs, nginxConfPath) {
		t.Error("the appended step must install the nginx config")
	}
}

func TestWebserverHookCreatesTmp(t *testing.T) {
	cfg := &types.ApkoConfig{}
	if err := (webserverHook{}).PatchApko(cfg, &types.Profile{}, types.BuildOptions{}); err != nil {
		t.Fatalf("PatchApko: %v", err)
	}
	for _, path := range cfg.Paths {
		if path.Path == "/tmp" {
			return
		}
	}
	t.Error("/tmp must exist in the image; nginx puts its pid and temp paths there")
}

// The nginx config is emitted as a heredoc inside a YAML block scalar, so the
// closing delimiters have to land in column 0 once YAML strips the block's
// indentation. Round-trip the real melange.yaml encoding and ask the shell.
func TestNginxConfigStepSurvivesYAMLRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}

	cfg := types.MelangeConfig{
		Pipeline: []types.MelangePipeline{{Runs: step(t, "", "")}},
	}
	// config.MarshalYAML is the encoder that writes the real melange.yaml, so
	// the block-scalar indentation here is exactly what melange is handed.
	encoded, err := config.MarshalYAML(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded types.MelangeConfig
	if err := yaml.Unmarshal([]byte(encoded), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.Pipeline) != 1 {
		t.Fatalf("expected 1 step after round-trip, got %d", len(decoded.Pipeline))
	}

	// melange expands ${{targets.destdir}} before the shell ever sees it.
	script := strings.ReplaceAll(decoded.Pipeline[0].Runs, "${{targets.destdir}}", "/out")

	path := filepath.Join(t.TempDir(), "step.sh")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if out, err := exec.Command("sh", "-n", path).CombinedOutput(); err != nil {
		t.Fatalf("generated script is not valid shell: %v\n%s\n--- script ---\n%s", err, out, script)
	}

	// A heredoc delimiter that kept its indentation would swallow the rest of
	// the script instead of ending the document.
	for _, delim := range []string{"APEXPACK_NGINX_MAIN", "APEXPACK_NGINX_SERVER"} {
		if !strings.Contains(script, "\n"+delim+"\n") {
			t.Errorf("%s is not at column 0 after the YAML round-trip", delim)
		}
	}
}

func directiveLine(conf, directive string) string {
	for _, line := range strings.Split(conf, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), directive+" ") {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// A Docker-era nginx.conf is the common shape across this estate: temp paths
// under /var/cache/nginx, logs under /var/log/nginx, user nginx, listen 80.
// Every one of those is fatal on a read-only, non-root image, so run the real
// shell function and check what it produces.
func TestHardenNginxConfRewritesDockerEraConfig(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}

	const input = `user  nginx;
userid on;
worker_processes auto;
error_log  /var/log/nginx/error.log warn;
pid        /var/run/nginx.pid;

http {
    access_log  /var/log/nginx/access.log  main;
    client_body_temp_path /var/cache/nginx/client_temp;
    proxy_temp_path       /var/cache/nginx/proxy_temp 1 2;
    fastcgi_temp_path     /var/cache/nginx/fastcgi_temp 1 2;
    uwsgi_temp_path       /var/cache/nginx/uwsgi_temp;
    scgi_temp_path        /var/cache/nginx/scgi_temp;
    proxy_cache_path      /var/cache/nginx/cache levels=1:2 keys_zone=z:10m;

    server {
        listen 80;
        listen [::]:80;
        listen 443 ssl http2;
        listen 3000 default_server;
        access_log off;
        error_log syslog:server=1.2.3.4 warn;
        proxy_set_header X-User $remote_user;
        location / { try_files $uri /index.html; }
    }
}
`

	dir := t.TempDir()
	in := filepath.Join(dir, "nginx.conf")
	if err := os.WriteFile(in, []byte(input), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	script := filepath.Join(dir, "harden.sh")
	if err := os.WriteFile(script, []byte(hardenNginxConf+"\nharden_nginx_conf \"$1\"\n"), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	out, err := exec.Command("sh", script, in).Output()
	if err != nil {
		t.Fatalf("harden_nginx_conf: %v", err)
	}
	got := string(out)

	for _, want := range []string{
		// Writable paths all land under /tmp, trailing arguments intact.
		"pid        /tmp/nginx.pid;",
		"client_body_temp_path /tmp/nginx-client_body;",
		"proxy_temp_path       /tmp/nginx-proxy 1 2;",
		"proxy_cache_path      /tmp/nginx-cache levels=1:2 keys_zone=z:10m;",
		"error_log  /dev/stderr warn;",
		"access_log  /dev/stdout  main;",
		// Privileged ports become bindable; the address form survives.
		"listen 8080;",
		"listen [::]:8080;",
		"listen 8443 ssl http2;",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in hardened config, got:\n%s", want, got)
		}
	}

	for _, unwanted := range []string{"/var/cache/nginx", "/var/log/nginx", "/var/run/nginx.pid"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("%s survived hardening; it is unwritable on a read-only rootfs", unwanted)
		}
	}

	// "user" is ignored at runtime anyway, but as root nginx -t aborts on it
	// when the account is missing, so it must be commented out rather than left.
	if !strings.Contains(got, "# apexpack: user  nginx;") {
		t.Error("the user directive must be commented out")
	}
	if directiveLine(got, "user") != "" {
		t.Errorf("an active user directive remains: %q", directiveLine(got, "user"))
	}

	// Things that must survive untouched.
	for _, keep := range []string{
		"userid on;",                            // not the user directive
		"listen 3000 default_server;",           // already unprivileged
		"access_log off;",                       // not a path
		"error_log syslog:server=1.2.3.4 warn;", // not a path
		"proxy_set_header X-User $remote_user;",
		"try_files $uri /index.html;",
	} {
		if !strings.Contains(got, keep) {
			t.Errorf("hardening altered %q, which it must leave alone:\n%s", keep, got)
		}
	}
}

// Both branches that install a project config must harden it — a server-level
// fragment can carry client_body_temp_path and access_log just as a full
// config can.
func TestNginxConfigStepHardensBothBranches(t *testing.T) {
	s := step(t, "", "")
	if n := strings.Count(s, "harden_nginx_conf nginx.conf"); n != 2 {
		t.Errorf("expected both the full-config and fragment branches to harden, found %d call(s)", n)
	}
}

// TestHardenNginxConfPreservesListenTerminators guards the regression that broke
// mymi-gate: a listen rewrite that dropped the directive's ";" and produced
//
//	nginx: [emerg] directive "listen" is not terminated by ";"
//
// The invariant is narrow and absolute — hardening may change a port, never a
// line's terminator. Shapes the rewriter does not fully understand (a
// continuation line, an already-unprivileged port, a unix socket) must come out
// byte-for-byte unchanged rather than half-rewritten.
func TestHardenNginxConfPreservesListenTerminators(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}

	// Each case is one input line and the line it must become.
	cases := []struct{ in, want string }{
		{"    listen 80;", "    listen 8080;"},
		{"    listen       80;", "    listen 8080;"},
		{"    listen 80 default_server;", "    listen 8080 default_server;"},
		{"    listen 80;  # serve http", "    listen 8080;  # serve http"},
		{"    listen [::]:80;", "    listen [::]:8080;"},
		{"    listen [::]:80 ipv6only=on;", "    listen [::]:8080 ipv6only=on;"},
		{"    listen *:80;", "    listen *:8080;"},
		{"    listen 0.0.0.0:80;", "    listen 0.0.0.0:8080;"},
		{"    listen 443 ssl http2;", "    listen 8443 ssl http2;"},
		{"    listen [::]:443 ssl;", "    listen [::]:8443 ssl;"},
		// Already fine, or not understood: must be returned verbatim.
		{"    listen 8080;", "    listen 8080;"},
		{"    listen 3000 default_server;", "    listen 3000 default_server;"},
		{"    listen 80", "    listen 80"},
		{"    listen unix:/var/run/nginx.sock;", "    listen unix:/var/run/nginx.sock;"},
	}

	var in strings.Builder
	for _, c := range cases {
		in.WriteString(c.in + "\n")
	}

	dir := t.TempDir()
	conf := filepath.Join(dir, "nginx.conf")
	if err := os.WriteFile(conf, []byte(in.String()), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	script := filepath.Join(dir, "harden.sh")
	if err := os.WriteFile(script, []byte(hardenNginxConf+"\nharden_nginx_conf \"$1\"\n"), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	out, err := exec.Command("sh", script, conf).Output()
	if err != nil {
		t.Fatalf("harden_nginx_conf: %v", err)
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(got) != len(cases) {
		t.Fatalf("hardening changed the line count: got %d lines, want %d:\n%s",
			len(got), len(cases), out)
	}

	for i, c := range cases {
		if got[i] != c.want {
			t.Errorf("line %d:\n  in   %q\n  got  %q\n  want %q", i+1, c.in, got[i], c.want)
		}
		// The terminator invariant, checked independently of the expected text:
		// whatever else changed, a line that ended the directive still does.
		if strings.Contains(c.in, ";") && !strings.Contains(got[i], ";") {
			t.Errorf("line %d lost its semicolon — nginx would report "+
				"'directive \"listen\" is not terminated by \";\"':\n  in  %q\n  got %q",
				i+1, c.in, got[i])
		}
	}
}

// runHardenScript executes the injected shell helpers against conf, with DESTDIR
// pointing at destdir, and returns stdout+stderr and the exit status.
func runHardenScript(t *testing.T, body, conf, destdir string) (string, int) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	script := filepath.Join(t.TempDir(), "run.sh")
	src := "DESTDIR=\"" + destdir + "\"\n" + hardenNginxConf + "\n" + body
	if err := os.WriteFile(script, []byte(src), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}
	out, err := exec.Command("sh", script, conf).CombinedOutput()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run: %v", err)
	}
	return string(out), code
}

// TestHardenNginxConfRendersPaketoConfig covers the mymi-gate failure. Its config
// came from the Paketo nginx buildpack, which rendered {{...}} when the container
// started and served the app out of its own build directory. apexpack bakes a
// static config instead, so all three of those assumptions have to be unwound at
// build time or the image cannot start.
func TestHardenNginxConfRendersPaketoConfig(t *testing.T) {
	const input = `worker_processes auto;
daemon off;

http {
    server {
        listen {{port}};
        server_name _;
        root /source/dist;
        index index.html;
    }
}
`
	dir := t.TempDir()
	conf := filepath.Join(dir, "nginx.conf")
	if err := os.WriteFile(conf, []byte(input), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	destdir := filepath.Join(dir, "destdir")
	if err := os.MkdirAll(filepath.Join(destdir, "usr/share/nginx/html"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	out := filepath.Join(dir, "out.conf")

	log, code := runHardenScript(t,
		"harden_nginx_conf \"$1\" > "+out+"\nfinalise_nginx_conf "+out+"\n", conf, destdir)
	if code != 0 {
		t.Fatalf("finalise failed (%d):\n%s", code, log)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(b)

	// {{port}} is nginx-fatal, not merely wrong: nginx reads "{" as a block
	// opener and reports 'directive "listen" is not terminated by ";"'.
	if !strings.Contains(got, "listen 8080;") {
		t.Errorf("{{port}} was not rendered:\n%s", got)
	}
	// The image command already passes -g "daemon off;", and nginx rejects a
	// second value for a directive the -g block set.
	if directiveLine(got, "daemon") != "" {
		t.Errorf("an active daemon directive remains: %q", directiveLine(got, "daemon"))
	}
	// Paketo's build directory does not exist in the image; left alone, every
	// request 404s from a config that parses cleanly.
	if !strings.Contains(got, "root /usr/share/nginx/html;") {
		t.Errorf("stale root was not retargeted:\n%s", got)
	}
	if strings.Contains(got, "/source/dist") {
		t.Errorf("/source/dist survived; it is absent from the image:\n%s", got)
	}
	if strings.Contains(got, "{{") {
		t.Errorf("template placeholders survived:\n%s", got)
	}
}

// TestFinaliseNginxConfKeepsRootThatExists guards the retarget from overreach: a
// project serving a directory it genuinely ships must keep it.
func TestFinaliseNginxConfKeepsRootThatExists(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "nginx.conf")
	if err := os.WriteFile(conf, []byte("server {\n    root /opt/site;\n}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	destdir := filepath.Join(dir, "destdir")
	for _, d := range []string{"usr/share/nginx/html", "opt/site"} {
		if err := os.MkdirAll(filepath.Join(destdir, d), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	log, code := runHardenScript(t, "finalise_nginx_conf \"$1\"\n", conf, destdir)
	if code != 0 {
		t.Fatalf("finalise failed (%d):\n%s", code, log)
	}
	b, _ := os.ReadFile(conf)
	if !strings.Contains(string(b), "root /opt/site;") {
		t.Errorf("a root the package ships was retargeted anyway:\n%s", b)
	}
}

// TestFinaliseNginxConfRejectsUnrenderableTemplates: {{env}} and {{module}} had
// launch-time values under Paketo and have none at build time. Failing the build
// with the line quoted beats shipping an image that cannot start.
func TestFinaliseNginxConfRejectsUnrenderableTemplates(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "nginx.conf")
	body := "server {\n    add_header X-Api \"{{env \"API_URL\"}}\" always;\n}\n"
	if err := os.WriteFile(conf, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	destdir := filepath.Join(dir, "destdir")
	if err := os.MkdirAll(filepath.Join(destdir, "usr/share/nginx/html"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	log, code := runHardenScript(t, "finalise_nginx_conf \"$1\"\n", conf, destdir)
	if code == 0 {
		t.Fatalf("an unrenderable placeholder must fail the build, got exit 0:\n%s", log)
	}
	if !strings.Contains(log, "API_URL") {
		t.Errorf("the failure must quote the offending line, got:\n%s", log)
	}
}

// TestEnsureWritablePathsCoversPrefixDefaults reproduces the read-only crash a
// migrated project hit on boot:
//
//	nginx: [emerg] mkdir() "/var/lib/nginx/tmp/uwsgi" failed (30: Read-only file system)
//
// The config was a complete one, so it replaced the apexpack template that
// declares these paths, and it named none of them itself. nginx creates every
// temp directory at startup regardless of whether the feature is used, so a
// static site died on a uwsgi directory its config never mentions.
func TestEnsureWritablePathsCoversPrefixDefaults(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "nginx.conf")
	body := "worker_processes auto;\n\nevents {\n    worker_connections 1024;\n}\n\n" +
		"http {\n    include /etc/nginx/mime.types;\n    sendfile on;\n\n" +
		"    server {\n        listen 8080;\n        root /usr/share/nginx/html;\n    }\n}\n"
	if err := os.WriteFile(conf, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	destdir := filepath.Join(dir, "destdir")
	if err := os.MkdirAll(filepath.Join(destdir, "usr/share/nginx/html"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	log, code := runHardenScript(t, "finalise_nginx_conf \"$1\"\n", conf, destdir)
	if code != 0 {
		t.Fatalf("finalise failed (%d):\n%s", code, log)
	}
	got, _ := os.ReadFile(conf)
	out := string(got)

	for _, d := range []string{
		"client_body_temp_path /tmp/nginx-client-body;",
		"proxy_temp_path /tmp/nginx-proxy;",
		"fastcgi_temp_path /tmp/nginx-fastcgi;",
		"uwsgi_temp_path /tmp/nginx-uwsgi;",
		"scgi_temp_path /tmp/nginx-scgi;",
		"pid /tmp/nginx.pid;",
	} {
		if !strings.Contains(out, d) {
			t.Errorf("%q was left to the build prefix; nginx would mkdir under /var/lib/nginx:\n%s", d, out)
		}
	}
	// Every temp path is an http-context directive: outside that block nginx
	// rejects the config outright.
	httpAt := strings.Index(out, "http {")
	if httpAt < 0 {
		t.Fatalf("http block lost:\n%s", out)
	}
	if i := strings.Index(out, "uwsgi_temp_path"); i < httpAt {
		t.Errorf("uwsgi_temp_path was inserted outside the http block:\n%s", out)
	}
}

// TestEnsureWritablePathsKeepsWhatTheConfigDeclares: a project that has already
// chosen a writable location keeps it. Only the directives it never wrote down
// are supplied, so this never silently overrides a deliberate choice.
func TestEnsureWritablePathsKeepsWhatTheConfigDeclares(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "nginx.conf")
	body := "pid /tmp/mine.pid;\nerror_log /dev/stderr;\n\nhttp {\n" +
		"    uwsgi_temp_path /tmp/mine-uwsgi;\n}\n"
	if err := os.WriteFile(conf, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	destdir := filepath.Join(dir, "destdir")
	if err := os.MkdirAll(filepath.Join(destdir, "usr/share/nginx/html"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	log, code := runHardenScript(t, "finalise_nginx_conf \"$1\"\n", conf, destdir)
	if code != 0 {
		t.Fatalf("finalise failed (%d):\n%s", code, log)
	}
	out := string(mustRead(t, conf))
	for _, keep := range []string{"pid /tmp/mine.pid;", "uwsgi_temp_path /tmp/mine-uwsgi;"} {
		if !strings.Contains(out, keep) {
			t.Errorf("a path the project chose was overridden, want %q:\n%s", keep, out)
		}
	}
	if strings.Count(out, "uwsgi_temp_path") != 1 {
		t.Errorf("uwsgi_temp_path was declared twice; nginx rejects a duplicate:\n%s", out)
	}
	if strings.Count(out, "pid ") != 1 {
		t.Errorf("pid was declared twice:\n%s", out)
	}
	if !strings.Contains(out, "scgi_temp_path /tmp/nginx-scgi;") {
		t.Errorf("a directive the config omitted was not supplied:\n%s", out)
	}
}

// TestEnsureWritablePathsLeavesFragmentsAlone: a conf.d fragment is included
// inside the apexpack main config's http block, which already declares these.
// Adding them again would duplicate them, and pid is invalid outside the main
// context, so a fragment has to come through untouched.
func TestEnsureWritablePathsLeavesFragmentsAlone(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "default.conf")
	body := "server {\n    listen 8080;\n    root /usr/share/nginx/html;\n}\n"
	if err := os.WriteFile(conf, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	destdir := filepath.Join(dir, "destdir")
	if err := os.MkdirAll(filepath.Join(destdir, "usr/share/nginx/html"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	log, code := runHardenScript(t, "finalise_nginx_conf \"$1\"\n", conf, destdir)
	if code != 0 {
		t.Fatalf("finalise failed (%d):\n%s", code, log)
	}
	out := string(mustRead(t, conf))
	for _, bad := range []string{"pid ", "uwsgi_temp_path", "error_log"} {
		if strings.Contains(out, bad) {
			t.Errorf("%q was injected into a conf.d fragment, where it is invalid:\n%s", bad, out)
		}
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return b
}

// TestEnsureWritablePathsHandlesBraceOnNextLine covers the mandatecalc-ui
// failure. nginx accepts "http {", "http{" and "http" with the brace on the
// following line, and the project's hand-written config used the last. Matching
// only the first spelling made a complete config look like a conf.d fragment,
// so ensure_nginx_writable_paths returned without touching it — silently, since
// skipping a fragment is the normal case — and the image shipped with the two
// temp paths the config happened to omit still pointing at the build prefix. It
// then died on boot with the very error the function exists to prevent.
//
// The inserted directives must land *after* the opening brace: above it they
// would be read in the main context and nginx would refuse to start.
func TestEnsureWritablePathsHandlesBraceOnNextLine(t *testing.T) {
	const input = `worker_processes auto;
events
{
    worker_connections 1024;
}
http
{
    client_body_temp_path /tmp/client_body;
    proxy_temp_path /tmp/proxy_temp;
    fastcgi_temp_path /tmp/fastcgi_temp;
    server {
        listen 8080;
        root /usr/share/nginx/html;
    }
}
`
	dir := t.TempDir()
	conf := filepath.Join(dir, "nginx.conf")
	if err := os.WriteFile(conf, []byte(input), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	destdir := filepath.Join(dir, "destdir")
	if err := os.MkdirAll(filepath.Join(destdir, "usr/share/nginx/html"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	log, code := runHardenScript(t, "finalise_nginx_conf \"$1\"\n", conf, destdir)
	if code != 0 {
		t.Fatalf("finalise failed (%d):\n%s", code, log)
	}
	b, _ := os.ReadFile(conf)
	got := string(b)
	for _, d := range []string{
		"client_body_temp_path",
		"proxy_temp_path",
		"fastcgi_temp_path",
		"uwsgi_temp_path",
		"scgi_temp_path",
	} {
		if directiveLine(got, d) == "" {
			t.Errorf("%s is still left to the build prefix:\n%s", d, got)
		}
	}
	// Everything inserted belongs inside http{}, which starts at the brace.
	brace := strings.Index(got, "http\n{")
	if brace == -1 {
		t.Fatalf("the http block opener was rewritten:\n%s", got)
	}
	for _, d := range []string{"uwsgi_temp_path", "scgi_temp_path"} {
		if at := strings.Index(got, d); at < brace {
			t.Errorf("%s was inserted above the opening brace, nginx reads it in the main context:\n%s", d, got)
		}
	}
}
