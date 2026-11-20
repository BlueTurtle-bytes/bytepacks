package hooks

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/apexpack/apexpack/internal/build/templates"
	"github.com/apexpack/apexpack/internal/types"
)

// pythonSysDep names the Wolfi packages a Python distribution needs when pip has
// no matching wheel and falls back to compiling the extension from source.
//
// Build holds the headers and tooling the compile step needs; they stay in the
// melange build environment and never reach the final image. Runtime holds the
// shared libraries the compiled module links against, which must be present in
// the image or `import` fails at startup with a missing .so.
//
// Every package name here was verified against the Wolfi APKINDEX. Keys are
// normalised distribution names (lowercase, '_' and '.' folded to '-').
type pythonSysDep struct {
	Build   []string
	Runtime []string
}

var pythonSysDeps = map[string]pythonSysDep{
	// Kafka client — needs librdkafka headers; the .so links against librdkafka.
	"confluent-kafka": {Build: []string{"librdkafka-dev"}, Runtime: []string{"librdkafka"}},
	// XML/XSLT
	"lxml":   {Build: []string{"libxml2-dev", "libxslt-dev"}, Runtime: []string{"libxml2", "libxslt"}},
	"xmlsec": {Build: []string{"libxml2-dev", "libxslt-dev", "openssl-dev"}, Runtime: []string{"libxml2", "libxslt", "openssl"}},
	// PostgreSQL — psycopg2-binary ships wheels, but the source form needs pg_config.
	"psycopg2": {Build: []string{"postgresql-dev"}, Runtime: []string{"libpq"}},
	"psycopg":  {Build: []string{"postgresql-dev"}, Runtime: []string{"libpq"}},
	// MySQL / MariaDB
	"mysqlclient": {Build: []string{"mariadb-connector-c-dev"}, Runtime: []string{"mariadb-connector-c"}},
	// ODBC
	"pyodbc": {Build: []string{"unixodbc-dev"}, Runtime: []string{"unixodbc"}},
	// Imaging
	"pillow": {
		Build:   []string{"libjpeg-turbo-dev", "zlib-dev", "freetype-dev", "libwebp-dev"},
		Runtime: []string{"libjpeg-turbo", "zlib", "freetype", "libwebp"},
	},
	// Crypto / auth
	"cryptography": {Build: []string{"openssl-dev", "libffi-dev"}, Runtime: []string{"openssl", "libffi"}},
	"pynacl":       {Build: []string{"libsodium-dev"}, Runtime: []string{"libsodium"}},
	"gssapi":       {Build: []string{"krb5-dev"}, Runtime: []string{"krb5"}},
	"kerberos":     {Build: []string{"krb5-dev"}, Runtime: []string{"krb5"}},
	"pykerberos":   {Build: []string{"krb5-dev"}, Runtime: []string{"krb5"}},
	"sasl":         {Build: []string{"cyrus-sasl-mit-dev"}, Runtime: []string{"cyrus-sasl-mit-libs"}},
	"pure-sasl":    {Build: []string{"cyrus-sasl-mit-dev"}, Runtime: []string{"cyrus-sasl-mit-libs"}},
	// Compression
	"python-snappy": {Build: []string{"snappy-dev"}, Runtime: []string{"snappy"}},
	// FFI
	"cffi": {Build: []string{"libffi-dev"}, Runtime: []string{"libffi"}},
}

// reRequirement pulls the distribution name off a requirements.txt line,
// discarding version specifiers, extras, environment markers and comments.
var reRequirement = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]*)`)

// normalizeDistName applies PEP 503 name normalisation.
func normalizeDistName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, ".", "-")
	return s
}

// projectDir is the directory the build actually targets. SourceDir is the
// mounted root, which for a multi-project tree holds no manifest of its own, so
// the subpath has to be joined on before anything looks for one.
func projectDir(opts types.BuildOptions) string {
	if opts.ProjectSubpath == "" {
		return opts.SourceDir
	}
	return filepath.Join(opts.SourceDir, opts.ProjectSubpath)
}

// scanRequirementNames collects distribution names declared by a Python project.
// It reads the files pip and the lock-based managers actually install from, and
// deliberately does not try to parse full TOML — a substring scan is enough to
// decide whether a system library is needed, and a false positive only adds a
// build dependency that goes unused.
func scanRequirementNames(srcDir string) map[string]bool {
	found := make(map[string]bool)

	// requirements*.txt at the project root.
	matches, _ := filepath.Glob(filepath.Join(srcDir, "requirements*.txt"))
	for _, path := range matches {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
				continue
			}
			if m := reRequirement.FindStringSubmatch(line); m != nil {
				found[normalizeDistName(m[1])] = true
			}
		}
		f.Close()
	}

	// pyproject.toml / Pipfile / poetry.lock: match known names as whole tokens.
	for _, name := range []string{"pyproject.toml", "Pipfile", "poetry.lock", "uv.lock"} {
		data, err := os.ReadFile(filepath.Join(srcDir, name))
		if err != nil {
			continue
		}
		haystack := normalizeDistName(string(data))
		for dist := range pythonSysDeps {
			if strings.Contains(haystack, `"`+dist+`"`) || strings.Contains(haystack, `'`+dist+`'`) ||
				strings.Contains(haystack, "\n"+dist+" ") || strings.Contains(haystack, "\n"+dist+"=") {
				found[dist] = true
			}
		}
	}
	return found
}

// resolvePythonSysDeps returns the extra build dependencies and image packages a
// project needs for its compiled dependencies, as sorted, de-duplicated lists.
func resolvePythonSysDeps(srcDir string) (build []string, runtime []string) {
	declared := scanRequirementNames(srcDir)
	buildSet, runtimeSet := map[string]bool{}, map[string]bool{}
	for dist := range declared {
		dep, ok := pythonSysDeps[dist]
		if !ok {
			continue
		}
		for _, b := range dep.Build {
			buildSet[b] = true
		}
		for _, r := range dep.Runtime {
			runtimeSet[r] = true
		}
	}
	for b := range buildSet {
		build = append(build, b)
	}
	for r := range runtimeSet {
		runtime = append(runtime, r)
	}
	sort.Strings(build)
	sort.Strings(runtime)
	return build, runtime
}

// appendMissing adds each candidate to list if not already present.
func appendMissing(list []string, candidates []string) []string {
	have := make(map[string]bool, len(list))
	for _, p := range list {
		have[p] = true
		// A versioned entry such as "npm<11" still satisfies the bare name.
		if i := strings.IndexAny(p, "<>=~"); i > 0 {
			have[p[:i]] = true
		}
	}
	for _, c := range candidates {
		if !have[c] {
			list = append(list, c)
			have[c] = true
		}
	}
	return list
}

// reportSysDeps prints what was injected so the choice is visible in build logs.
func reportSysDeps(build, runtime []string) {
	if len(build) == 0 && len(runtime) == 0 {
		return
	}
	fmt.Printf("  → python system deps: build=[%s] image=[%s]\n",
		strings.Join(build, " "), strings.Join(runtime, " "))
}

type pythonHook struct{}

func (pythonHook) PatchMelange(cfg *types.MelangeConfig, p *types.Profile, opts types.BuildOptions) error {
	// Compiled dependencies (confluent-kafka, lxml, psycopg2, ...) need their
	// system headers present before pip falls back to a source build. Done ahead
	// of the registry check so it still applies when no private registry is set.
	buildDeps, runtimeDeps := resolvePythonSysDeps(projectDir(opts))
	if len(buildDeps) > 0 {
		cfg.Environment.Contents.Packages = appendMissing(cfg.Environment.Contents.Packages, buildDeps)
	}
	reportSysDeps(buildDeps, runtimeDeps)

	if p.Build.PypiRegistryURL == "" || os.Getenv("ARTI_USER") == "" {
		fmt.Printf("  → pypi registry: skipped (pypi_registry_url or ARTI_USER not set)\n")
		return nil
	}

	for _, key := range []string{"ARTI_USER", "ARTI_PASSWORD", "ARTI_REPO"} {
		if val := os.Getenv(key); val != "" {
			if cfg.Environment.Env == nil {
				cfg.Environment.Env = make(map[string]string)
			}
			if _, exists := cfg.Environment.Env[key]; !exists {
				cfg.Environment.Env[key] = val
			}
		}
	}

	registryURL := strings.TrimRight(p.Build.PypiRegistryURL, "/") + "/" + os.Getenv("ARTI_REPO")

	// pip.conf index-url embeds credentials: https://user:pass@host/path/repo/simple
	parsed, err := url.Parse(registryURL)
	if err != nil {
		return fmt.Errorf("invalid pypi_registry_url: %w", err)
	}
	registryHost := parsed.Hostname()
	parsed.User = url.UserPassword(os.Getenv("ARTI_USER"), os.Getenv("ARTI_PASSWORD"))
	registryURLWithAuth := parsed.String()

	r := strings.NewReplacer(
		"{{REGISTRY_URL}}", registryURL,
		"{{REGISTRY_URL_WITH_AUTH}}", registryURLWithAuth,
		"{{REGISTRY_HOST}}", registryHost,
		"{{ARTI_USER}}", os.Getenv("ARTI_USER"),
		"{{ARTI_PASSWORD}}", os.Getenv("ARTI_PASSWORD"),
	)

	pipConfTmpl, err := templates.LoadPipConfTemplate(opts.ProfilesDir)
	if err != nil {
		return err
	}
	pypircTmpl, err := templates.LoadPypircTemplate(opts.ProfilesDir)
	if err != nil {
		return err
	}

	pipConfContent := r.Replace(pipConfTmpl)
	pypircContent := r.Replace(pypircTmpl)

	fmt.Printf("  → pypi registry: %s\n", registryURL)
	fmt.Printf("  → pip.conf:\n%s\n", pipConfContent)

	const pipConfPath = "/home/build/.config/pip/pip.conf"
	// Configure Poetry http-basic credentials for every [[tool.poetry.source]] entry.
	// awk extracts the name field from each source section; poetry config stores it
	// in ~/.config/pypoetry/auth.toml which Poetry reads before resolving packages.
	poetryCredStep := `if [ -f pyproject.toml ] && command -v poetry >/dev/null 2>&1; then
  awk '/^\[\[tool\.poetry\.source\]\]/{f=1} f && /^name\s*=/{gsub(/.*=\s*"|"\s*$/,""); print; f=0}' pyproject.toml 2>/dev/null \
    | while IFS= read -r src; do poetry config "http-basic.$src" "$ARTI_USER" "$ARTI_PASSWORD" 2>/dev/null || true; done
fi`

	step := "mkdir -p /home/build/.config/pip\n" +
		"cat >> " + pipConfPath + " << APEXPACK_PIPCONF_EOF\n" +
		pipConfContent +
		"APEXPACK_PIPCONF_EOF\n" +
		"cat >> /home/build/.pypirc << APEXPACK_PYPIRC_EOF\n" +
		pypircContent +
		"APEXPACK_PYPIRC_EOF\n" +
		poetryCredStep

	// Set explicit env vars so pip finds the config regardless of HOME.
	if cfg.Environment.Env == nil {
		cfg.Environment.Env = make(map[string]string)
	}
	cfg.Environment.Env["PIP_CONFIG_FILE"] = pipConfPath
	cfg.Environment.Env["PIP_INDEX_URL"] = registryURLWithAuth + "/simple"

	cfg.Pipeline = append([]types.MelangePipeline{{Runs: step}}, cfg.Pipeline...)
	return nil
}

func (pythonHook) PatchApko(cfg *types.ApkoConfig, p *types.Profile, opts types.BuildOptions) error {
	// Shared libraries a source-built extension links against must also be in the
	// final image, or importing the module fails at startup with a missing .so.
	if _, runtimeDeps := resolvePythonSysDeps(projectDir(opts)); len(runtimeDeps) > 0 {
		cfg.Contents.Packages = appendMissing(cfg.Contents.Packages, runtimeDeps)
	}
	return nil
}
