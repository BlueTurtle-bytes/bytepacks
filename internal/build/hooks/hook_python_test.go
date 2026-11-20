package hooks

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/apexpack/apexpack/internal/types"
)

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

func TestResolvePythonSysDepsRequirements(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "requirements.txt", `
# a comment
Django==4.2.11
confluent-kafka==2.3.0
lxml>=4.9.0
requests==2.31.0
-r other.txt
`)
	build, runtime := resolvePythonSysDeps(dir)
	wantBuild := []string{"librdkafka-dev", "libxml2-dev", "libxslt-dev"}
	wantRuntime := []string{"librdkafka", "libxml2", "libxslt"}
	if !reflect.DeepEqual(build, wantBuild) {
		t.Errorf("build = %v, want %v", build, wantBuild)
	}
	if !reflect.DeepEqual(runtime, wantRuntime) {
		t.Errorf("runtime = %v, want %v", runtime, wantRuntime)
	}
}

func TestResolvePythonSysDepsNormalisesNames(t *testing.T) {
	// PEP 503: Pillow, python_snappy and PyNaCl must all match their map keys.
	dir := t.TempDir()
	writeFile(t, dir, "requirements.txt", "Pillow~=10.2.0\npython_snappy==0.6.1\nPyNaCl==1.5.0\n")
	build, _ := resolvePythonSysDeps(dir)
	for _, want := range []string{"libjpeg-turbo-dev", "snappy-dev", "libsodium-dev"} {
		found := false
		for _, g := range build {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected %q in build deps, got %v", want, build)
		}
	}
}

func TestResolvePythonSysDepsPyprojectAndNoMatch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pyproject.toml", `
[tool.poetry.dependencies]
python = "^3.11"
confluent-kafka = "^2.3.0"
`)
	build, runtime := resolvePythonSysDeps(dir)
	if len(build) == 0 || build[0] != "librdkafka-dev" {
		t.Errorf("pyproject scan: build = %v, want librdkafka-dev", build)
	}
	if len(runtime) == 0 || runtime[0] != "librdkafka" {
		t.Errorf("pyproject scan: runtime = %v, want librdkafka", runtime)
	}

	// A project with no compiled dependencies must inject nothing.
	plain := t.TempDir()
	writeFile(t, plain, "requirements.txt", "Django==4.2.11\nrequests==2.31.0\n")
	b, r := resolvePythonSysDeps(plain)
	if len(b) != 0 || len(r) != 0 {
		t.Errorf("pure-python project injected build=%v runtime=%v, want none", b, r)
	}

	// A directory with no dependency files at all must not error.
	if b, r := resolvePythonSysDeps(t.TempDir()); len(b) != 0 || len(r) != 0 {
		t.Errorf("empty dir injected build=%v runtime=%v, want none", b, r)
	}
}

func TestAppendMissingDedupes(t *testing.T) {
	// A versioned entry still satisfies the bare name, so npm must not be re-added.
	got := appendMissing([]string{"busybox", "npm<11"}, []string{"npm", "git", "busybox"})
	want := []string{"busybox", "npm<11", "git"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("appendMissing = %v, want %v", got, want)
	}
}

func TestProjectDirJoinsSubpath(t *testing.T) {
	// A multi-project tree mounts the whole repo but builds one directory inside
	// it; the manifests live there, not at the root.
	root := t.TempDir()
	sub := filepath.Join(root, "services", "kafka-consumer")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, sub, "requirements.txt", "confluent-kafka==2.3.0\n")

	opts := types.BuildOptions{SourceDir: root, ProjectSubpath: "services/kafka-consumer"}
	if got := projectDir(opts); got != sub {
		t.Errorf("projectDir = %q, want %q", got, sub)
	}
	build, runtime := resolvePythonSysDeps(projectDir(opts))
	if !reflect.DeepEqual(build, []string{"librdkafka-dev"}) || !reflect.DeepEqual(runtime, []string{"librdkafka"}) {
		t.Errorf("subpath scan missed deps: build=%v runtime=%v", build, runtime)
	}

	// Without a subpath the root is scanned as-is, and finds nothing there.
	noSub := types.BuildOptions{SourceDir: root}
	if got := projectDir(noSub); got != root {
		t.Errorf("projectDir without subpath = %q, want %q", got, root)
	}
	if build, _ := resolvePythonSysDeps(projectDir(noSub)); len(build) != 0 {
		t.Errorf("root scan found %v, want none", build)
	}
}
