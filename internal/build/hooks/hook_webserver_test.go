package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
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
	if i := strings.Index(strings.ReplaceAll(s, "${{targets.destdir}}", ""), "{{"); i >= 0 {
		t.Errorf("unsubstituted template token remains: %q", s[i:min(i+40, len(s))])
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
		`grep -qE '^[[:space:]]*(events|http)[[:space:]]*\{' nginx.conf`,
		`cp nginx.conf "$DESTDIR/etc/nginx/apexpack.conf"`,
		`cp nginx.conf "$DESTDIR/etc/nginx/conf.d/default.conf"`,
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
