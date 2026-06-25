package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apexpack/apexpack/internal/types"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func golangProfile() *types.Profile {
	return &types.Profile{
		Runtime: "golang",
		Build: types.BuildConfig{
			Dependencies: []string{"go", "git"},
			Command:      "go build -o ${{targets.destdir}}/usr/bin/{APP_NAME} .",
		},
		Image: types.ImageConfig{
			Packages:   []string{"ca-certificates"},
			Entrypoint: "/usr/bin/{APP_NAME}",
		},
	}
}

// ── javaHomeDirVersion / fixJavaHome ──────────────────────────────────────────

func TestJavaHomeDirVersion(t *testing.T) {
	cases := []struct{ major, want string }{
		{"8", "1.8"},
		{"17", "17"},
		{"21", "21"},
		{"11", "11"},
	}
	for _, c := range cases {
		if got := javaHomeDirVersion(c.major); got != c.want {
			t.Errorf("javaHomeDirVersion(%q) = %q, want %q", c.major, got, c.want)
		}
	}
}

func TestFixJavaHomeJava8(t *testing.T) {
	env := map[string]string{
		"JAVA_HOME": "/usr/lib/jvm/java-8-openjdk",
		"OTHER":     "unchanged",
	}
	got := fixJavaHome(env, "java", "8")
	if got["JAVA_HOME"] != "/usr/lib/jvm/java-1.8-openjdk" {
		t.Errorf("JAVA_HOME: got %q, want /usr/lib/jvm/java-1.8-openjdk", got["JAVA_HOME"])
	}
	if got["OTHER"] != "unchanged" {
		t.Errorf("OTHER should be unchanged: %q", got["OTHER"])
	}
}

func TestFixJavaHomeJava17NoChange(t *testing.T) {
	env := map[string]string{"JAVA_HOME": "/usr/lib/jvm/java-17-openjdk"}
	got := fixJavaHome(env, "java", "17")
	if got["JAVA_HOME"] != "/usr/lib/jvm/java-17-openjdk" {
		t.Errorf("Java 17 should be unchanged: %q", got["JAVA_HOME"])
	}
}

func TestFixJavaHomeNonJavaRuntime(t *testing.T) {
	env := map[string]string{"SOME_HOME": "/usr/lib/jvm/java-8-openjdk"}
	got := fixJavaHome(env, "golang", "8")
	if got["SOME_HOME"] != "/usr/lib/jvm/java-8-openjdk" {
		t.Errorf("non-java runtime should not be modified: %q", got["SOME_HOME"])
	}
}

// ── vsub ──────────────────────────────────────────────────────────────────────

func TestVsub(t *testing.T) {
	cases := []struct {
		s, token, version, want string
	}{
		{"openjdk-{JAVA_VERSION}-jre", "{JAVA_VERSION}", "21", "openjdk-21-jre"},
		{"node-{NODE_VERSION}", "{NODE_VERSION}", "20", "node-20"},
		{"no token here", "{JAVA_VERSION}", "21", "no token here"},
		{"empty token", "", "21", "empty token"},
		{"empty version", "{JAVA_VERSION}", "", "empty version"},
		{"", "{JAVA_VERSION}", "21", ""},
	}
	for _, c := range cases {
		if got := vsub(c.s, c.token, c.version); got != c.want {
			t.Errorf("vsub(%q, %q, %q) = %q, want %q", c.s, c.token, c.version, got, c.want)
		}
	}
}

func TestVsubSlice(t *testing.T) {
	in := []string{"openjdk-{JAVA_VERSION}-jre", "ca-certificates", "maven-{JAVA_VERSION}"}
	got := vsubSlice(in, "{JAVA_VERSION}", "17")
	want := []string{"openjdk-17-jre", "ca-certificates", "maven-17"}
	for i, g := range got {
		if g != want[i] {
			t.Errorf("[%d] got %q, want %q", i, g, want[i])
		}
	}
}

func TestVsubSliceNoTokenReturnsOriginal(t *testing.T) {
	in := []string{"busybox", "ca-certificates"}
	got := vsubSlice(in, "", "21")
	if &got[0] == &in[0] {
		// slice header may differ but underlying array should be same (original returned)
	}
	if got[0] != in[0] || got[1] != in[1] {
		t.Errorf("expected original slice contents, got %v", got)
	}
}

func TestVsubMap(t *testing.T) {
	in := map[string]string{
		"JAVA_HOME": "/usr/lib/jvm/openjdk-{JAVA_VERSION}",
		"APP_ENV":   "production",
	}
	got := vsubMap(in, "{JAVA_VERSION}", "21")
	if got["JAVA_HOME"] != "/usr/lib/jvm/openjdk-21" {
		t.Errorf("JAVA_HOME: got %q", got["JAVA_HOME"])
	}
	if got["APP_ENV"] != "production" {
		t.Errorf("APP_ENV: got %q", got["APP_ENV"])
	}
}

func TestVsubMapEmptyReturnsOriginal(t *testing.T) {
	in := map[string]string{"K": "V"}
	got := vsubMap(in, "", "21")
	if got["K"] != "V" {
		t.Errorf("expected original map, got %v", got)
	}
}

// ── langVersionToken ──────────────────────────────────────────────────────────

func TestLangVersionToken(t *testing.T) {
	cases := []struct {
		runtime, want string
	}{
		{"java", "{JAVA_VERSION}"},
		{"node", "{NODE_VERSION}"},
		{"python", "{PYTHON_VERSION}"},
		{"dotnet", "{DOTNET_VERSION}"},
		{"golang", "{GO_VERSION}"},
		{"unknown", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := langVersionToken(c.runtime); got != c.want {
			t.Errorf("langVersionToken(%q) = %q, want %q", c.runtime, got, c.want)
		}
	}
}

// ── resolveVersion ────────────────────────────────────────────────────────────

func TestResolveVersion(t *testing.T) {
	cases := []struct {
		runtime, detected, want string
	}{
		{"java", "17", "17"},
		{"java", "", "21"},   // default
		{"node", "", "20"},   // default
		{"python", "", "3.12"}, // default
		{"dotnet", "", "8"},  // default
		{"golang", "", ""},   // no default
		{"golang", "1.24", "1.24"},
		{"unknown", "", ""},
	}
	for _, c := range cases {
		if got := resolveVersion(c.runtime, c.detected); got != c.want {
			t.Errorf("resolveVersion(%q, %q) = %q, want %q", c.runtime, c.detected, got, c.want)
		}
	}
}

// ── validateRuntimeVersion ────────────────────────────────────────────────────

func TestValidateRuntimeVersion(t *testing.T) {
	// runtimes without constraints pass any version
	for _, rt := range []string{"golang", "java", "node", "python"} {
		if err := validateRuntimeVersion(rt, "any-version"); err != nil {
			t.Errorf("runtime=%q: unexpected error: %v", rt, err)
		}
	}

	// dotnet: only specific versions are in Wolfi
	for _, v := range []string{"8", "9", "10"} {
		if err := validateRuntimeVersion("dotnet", v); err != nil {
			t.Errorf("dotnet version %q should be valid, got error: %v", v, err)
		}
	}
	if err := validateRuntimeVersion("dotnet", "6"); err == nil {
		t.Error("dotnet version 6 should be invalid")
	}
	if err := validateRuntimeVersion("dotnet", "7"); err == nil {
		t.Error("dotnet version 7 should be invalid")
	}
}

// ── applyProjectTemplates ─────────────────────────────────────────────────────

func TestApplyProjectTemplates(t *testing.T) {
	cases := []struct {
		s, name, want string
	}{
		{"/usr/bin/{APP_NAME}", "myapp", "/usr/bin/myapp"},
		{"go build -o ${{targets.destdir}}/usr/bin/{APP_NAME} .", "svc", "go build -o ${{targets.destdir}}/usr/bin/svc ."},
		{"no token", "myapp", "no token"},
		{"", "myapp", ""},
	}
	for _, c := range cases {
		if got := applyProjectTemplates(c.s, c.name); got != c.want {
			t.Errorf("applyProjectTemplates(%q, %q) = %q, want %q", c.s, c.name, got, c.want)
		}
	}
}

// ── SanitizeImageName ─────────────────────────────────────────────────────────

func TestSanitizeImageName(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		{"myapp", "myapp"},
		{"MyApp", "myapp"},
		{"my-app", "my-app"},
		{"my_app", "my_app"},
		{"my app", "my-app"},
		{"My App/v2", "my-app-v2"},
		{"--myapp--", "myapp"},
		{"..myapp..", "myapp"},
		{"UPPER_CASE", "upper_case"},
		{"", ""},
	}
	for _, c := range cases {
		if got := SanitizeImageName(c.input); got != c.want {
			t.Errorf("SanitizeImageName(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// ── cacheVolumeName ───────────────────────────────────────────────────────────

func TestCacheVolumeName(t *testing.T) {
	cases := []struct {
		path, want string
	}{
		{"/home/build/.npm", "apexpack-cache-home-build--npm"},
		{"/home/build/go/pkg/mod", "apexpack-cache-home-build-go-pkg-mod"},
		{"/root/.m2", "apexpack-cache-root--m2"},
	}
	for _, c := range cases {
		if got := cacheVolumeName(c.path); got != c.want {
			t.Errorf("cacheVolumeName(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// ── melangeArch ───────────────────────────────────────────────────────────────

func TestMelangeArchOverride(t *testing.T) {
	if got := melangeArch("x86_64"); got != "x86_64" {
		t.Errorf("got %q, want x86_64", got)
	}
	if got := melangeArch("aarch64"); got != "aarch64" {
		t.Errorf("got %q, want aarch64", got)
	}
}

func TestMelangeArchEmpty(t *testing.T) {
	got := melangeArch("")
	if got != "aarch64" && got != "x86_64" {
		t.Errorf("melangeArch(\"\") = %q, want aarch64 or x86_64", got)
	}
}

// ── archToDockerPlatform ──────────────────────────────────────────────────────

func TestArchToDockerPlatform(t *testing.T) {
	if got := archToDockerPlatform("aarch64"); got != "linux/arm64" {
		t.Errorf("aarch64: got %q", got)
	}
	if got := archToDockerPlatform("x86_64"); got != "linux/amd64" {
		t.Errorf("x86_64: got %q", got)
	}
}

// ── resolveOverride ───────────────────────────────────────────────────────────

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

	// most specific: framework+pm wins
	got, ok := resolveOverride(p, "nextjs", "pnpm")
	if !ok || got.Command != "pnpm next build" {
		t.Errorf("framework+pm: got %q ok=%v, want pnpm next build", got.Command, ok)
	}

	// pm fallback when no framework+pm entry
	got, ok = resolveOverride(p, "remix", "pnpm")
	if !ok || got.Command != "pnpm build" {
		t.Errorf("pm fallback: got %q ok=%v, want pnpm build", got.Command, ok)
	}

	// framework fallback when no pm
	got, ok = resolveOverride(p, "nextjs", "")
	if !ok || got.Command != "next build" {
		t.Errorf("framework fallback: got %q ok=%v, want next build", got.Command, ok)
	}

	// no match
	_, ok = resolveOverride(p, "unknown", "yarn")
	if ok {
		t.Error("expected no match for unknown framework+pm")
	}

	// no frameworks defined
	_, ok = resolveOverride(&types.Profile{}, "nextjs", "pnpm")
	if ok {
		t.Error("expected no match when no frameworks defined")
	}
}

// ── readProcfileCmd ───────────────────────────────────────────────────────────

func TestReadProcfileCmd(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Procfile", "web: ./bin/server --port 8080\nworker: ./bin/worker\n")

	if got := readProcfileCmd(dir); got != "./bin/server --port 8080" {
		t.Errorf("got %q, want ./bin/server --port 8080", got)
	}
}

func TestReadProcfileCmdNoWebProcess(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Procfile", "worker: ./bin/worker\n")

	if got := readProcfileCmd(dir); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestReadProcfileCmdMissing(t *testing.T) {
	if got := readProcfileCmd(t.TempDir()); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// ── applyDefaults ─────────────────────────────────────────────────────────────

func TestApplyDefaultsVersion(t *testing.T) {
	opts := applyDefaults(Options{SourceDir: "/tmp/myapp"})
	if opts.Version != "0.0.1" {
		t.Errorf("Version: got %q, want 0.0.1", opts.Version)
	}
}

func TestApplyDefaultsProjectName(t *testing.T) {
	opts := applyDefaults(Options{SourceDir: "/tmp/my-project"})
	if opts.ProjectName != "my-project" {
		t.Errorf("ProjectName: got %q, want my-project", opts.ProjectName)
	}
}

func TestApplyDefaultsExplicitProjectName(t *testing.T) {
	opts := applyDefaults(Options{SourceDir: "/tmp/x", ProjectName: "My App"})
	if opts.ProjectName != "my-app" {
		t.Errorf("ProjectName: got %q, want my-app", opts.ProjectName)
	}
}

func TestApplyDefaultsOutputDir(t *testing.T) {
	opts := applyDefaults(Options{SourceDir: "/tmp/myapp"})
	if opts.OutputDir != "/tmp/myapp/.apexpack-output" {
		t.Errorf("OutputDir: got %q", opts.OutputDir)
	}
}

func TestApplyDefaultsExplicitOutputDir(t *testing.T) {
	opts := applyDefaults(Options{SourceDir: "/tmp/myapp", OutputDir: "/custom/out"})
	if opts.OutputDir != "/custom/out" {
		t.Errorf("OutputDir should not be overridden: got %q", opts.OutputDir)
	}
}

// ── Plan ──────────────────────────────────────────────────────────────────────

func TestPlanGolang(t *testing.T) {
	dir := t.TempDir()
	plan, err := Plan(golangProfile(), Options{
		SourceDir:   dir,
		ProjectName: "myapp",
		Version:     "1.0.0",
	})
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}

	if plan.ProjectName != "myapp" {
		t.Errorf("ProjectName: got %q, want myapp", plan.ProjectName)
	}
	if plan.Version != "1.0.0" {
		t.Errorf("Version: got %q, want 1.0.0", plan.Version)
	}
	if plan.Melange.Package.Name != "myapp" {
		t.Errorf("Melange.Package.Name: got %q, want myapp", plan.Melange.Package.Name)
	}

	// project name and wolfi-baselayout must be in the apko package list
	packages := strings.Join(plan.Apko.Contents.Packages, " ")
	if !strings.Contains(packages, "myapp") {
		t.Errorf("apko packages should contain project name: %v", plan.Apko.Contents.Packages)
	}
	if !strings.Contains(packages, "wolfi-baselayout") {
		t.Errorf("apko packages should contain wolfi-baselayout: %v", plan.Apko.Contents.Packages)
	}
}

func TestPlanEntrypointSubstitution(t *testing.T) {
	dir := t.TempDir()
	plan, err := Plan(golangProfile(), Options{
		SourceDir:   dir,
		ProjectName: "mysvc",
	})
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if plan.Apko.Entrypoint.Command != "/usr/bin/mysvc" {
		t.Errorf("entrypoint: got %q, want /usr/bin/mysvc", plan.Apko.Entrypoint.Command)
	}
}

func TestPlanMelangeBuildCommand(t *testing.T) {
	dir := t.TempDir()
	plan, err := Plan(golangProfile(), Options{
		SourceDir:   dir,
		ProjectName: "mybin",
	})
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if len(plan.Melange.Pipeline) == 0 {
		t.Fatal("expected at least one pipeline step")
	}
	cmd := plan.Melange.Pipeline[len(plan.Melange.Pipeline)-1].Runs
	if !strings.Contains(cmd, "mybin") {
		t.Errorf("build command should contain project name: %q", cmd)
	}
}

func TestPlanDotnetUnsupportedVersionErrors(t *testing.T) {
	p := &types.Profile{
		Runtime: "dotnet",
		Build: types.BuildConfig{
			Dependencies: []string{"dotnet-{DOTNET_VERSION}-sdk"},
			Command:      "dotnet publish",
		},
		Image: types.ImageConfig{
			Packages: []string{"aspnet-{DOTNET_VERSION}-runtime"},
		},
	}
	_, err := Plan(p, Options{
		SourceDir:       t.TempDir(),
		ProjectName:     "myapi",
		LanguageVersion: "6", // not in Wolfi
	})
	if err == nil {
		t.Error("expected error for unsupported dotnet version")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("error should mention 'unsupported': %v", err)
	}
}

func TestPlanDotnetVersionSubstitution(t *testing.T) {
	p := &types.Profile{
		Runtime: "dotnet",
		Build: types.BuildConfig{
			Dependencies: []string{"dotnet-{DOTNET_VERSION}-sdk"},
			Command:      "dotnet publish",
		},
		Image: types.ImageConfig{
			Packages: []string{"aspnet-{DOTNET_VERSION}-runtime"},
		},
	}
	plan, err := Plan(p, Options{
		SourceDir:       t.TempDir(),
		ProjectName:     "myapi",
		LanguageVersion: "8",
	})
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}

	packages := strings.Join(plan.Apko.Contents.Packages, " ")
	if !strings.Contains(packages, "aspnet-8-runtime") {
		t.Errorf("expected aspnet-8-runtime in packages: %v", plan.Apko.Contents.Packages)
	}
}

func TestPlanProcfileFallback(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Procfile", "web: ./bin/server\n")

	p := &types.Profile{
		Runtime: "golang",
		Build:   types.BuildConfig{Command: "go build ."},
		Image:   types.ImageConfig{Packages: []string{"ca-certificates"}},
		// no entrypoint set — should fall back to Procfile
	}
	plan, err := Plan(p, Options{SourceDir: dir, ProjectName: "app"})
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if plan.Apko.Entrypoint.Command != "./bin/server" {
		t.Errorf("entrypoint: got %q, want ./bin/server", plan.Apko.Entrypoint.Command)
	}
}

func TestPlanJava8EntrypointUsesLegacyPath(t *testing.T) {
	p := &types.Profile{
		Runtime: "java",
		Build: types.BuildConfig{
			Dependencies: []string{"openjdk-{JAVA_VERSION}"},
			Command:      "mvn package",
			Env:          map[string]string{"JAVA_HOME": "/usr/lib/jvm/java-{JAVA_VERSION}-openjdk"},
		},
		Image: types.ImageConfig{
			Packages:   []string{"openjdk-{JAVA_VERSION}-jre"},
			Entrypoint: "java",
			Env:        map[string]string{"JAVA_HOME": "/usr/lib/jvm/java-{JAVA_VERSION}-openjdk"},
		},
	}
	plan, err := Plan(p, Options{
		SourceDir:       t.TempDir(),
		ProjectName:     "myapp",
		LanguageVersion: "8",
	})
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	// Entrypoint must use the 1.8 directory name, not 8
	if plan.Apko.Entrypoint.Command != "/usr/lib/jvm/java-1.8-openjdk/bin/java" {
		t.Errorf("entrypoint: got %q, want /usr/lib/jvm/java-1.8-openjdk/bin/java",
			plan.Apko.Entrypoint.Command)
	}
	// JAVA_HOME must also be corrected in the image env
	if plan.Apko.Environment["JAVA_HOME"] != "/usr/lib/jvm/java-1.8-openjdk" {
		t.Errorf("JAVA_HOME: got %q, want /usr/lib/jvm/java-1.8-openjdk",
			plan.Apko.Environment["JAVA_HOME"])
	}
	// Build env must also be corrected
	if plan.Melange.Environment.Env["JAVA_HOME"] != "/usr/lib/jvm/java-1.8-openjdk" {
		t.Errorf("build JAVA_HOME: got %q, want /usr/lib/jvm/java-1.8-openjdk",
			plan.Melange.Environment.Env["JAVA_HOME"])
	}
}

func TestPlanDefaultRunAs(t *testing.T) {
	plan, err := Plan(golangProfile(), Options{
		SourceDir:   t.TempDir(),
		ProjectName: "app",
	})
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if plan.Apko.Accounts.RunAs != "65532" {
		t.Errorf("RunAs: got %q, want 65532", plan.Apko.Accounts.RunAs)
	}
}
