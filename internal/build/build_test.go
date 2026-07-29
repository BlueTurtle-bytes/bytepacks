package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apexpack/apexpack/internal/types"
)

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

// ── Plan integration tests ────────────────────────────────────────────────────

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
	plan, err := Plan(golangProfile(), Options{SourceDir: dir, ProjectName: "mysvc"})
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if plan.Apko.Entrypoint.Command != "/usr/bin/mysvc" {
		t.Errorf("entrypoint: got %q, want /usr/bin/mysvc", plan.Apko.Entrypoint.Command)
	}
}

func TestPlanMelangeBuildCommand(t *testing.T) {
	dir := t.TempDir()
	plan, err := Plan(golangProfile(), Options{SourceDir: dir, ProjectName: "mybin"})
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
		Build:   types.BuildConfig{Dependencies: []string{"dotnet-{DOTNET_VERSION}-sdk"}, Command: "dotnet publish"},
		Image:   types.ImageConfig{Packages: []string{"aspnet-{DOTNET_VERSION}-runtime"}},
	}
	_, err := Plan(p, Options{SourceDir: t.TempDir(), ProjectName: "myapi", LanguageVersion: "6"})
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
		Build:   types.BuildConfig{Dependencies: []string{"dotnet-{DOTNET_VERSION}-sdk"}, Command: "dotnet publish"},
		Image:   types.ImageConfig{Packages: []string{"aspnet-{DOTNET_VERSION}-runtime"}},
	}
	plan, err := Plan(p, Options{SourceDir: t.TempDir(), ProjectName: "myapi", LanguageVersion: "8"})
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
	plan, err := Plan(p, Options{SourceDir: t.TempDir(), ProjectName: "myapp", LanguageVersion: "8"})
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if plan.Apko.Entrypoint.Command != "/usr/lib/jvm/java-1.8-openjdk/bin/java" {
		t.Errorf("entrypoint: got %q, want /usr/lib/jvm/java-1.8-openjdk/bin/java",
			plan.Apko.Entrypoint.Command)
	}
	if plan.Apko.Environment["JAVA_HOME"] != "/usr/lib/jvm/java-1.8-openjdk" {
		t.Errorf("JAVA_HOME: got %q, want /usr/lib/jvm/java-1.8-openjdk",
			plan.Apko.Environment["JAVA_HOME"])
	}
	if plan.Melange.Environment.Env["JAVA_HOME"] != "/usr/lib/jvm/java-1.8-openjdk" {
		t.Errorf("build JAVA_HOME: got %q, want /usr/lib/jvm/java-1.8-openjdk",
			plan.Melange.Environment.Env["JAVA_HOME"])
	}
}

func TestPlanDefaultRunAs(t *testing.T) {
	plan, err := Plan(golangProfile(), Options{SourceDir: t.TempDir(), ProjectName: "app"})
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if plan.Apko.Accounts.RunAs != "65532" {
		t.Errorf("RunAs: got %q, want 65532", plan.Apko.Accounts.RunAs)
	}
}

func TestPlanHTTPHealthCheckAddsWget(t *testing.T) {
	p := profileWithHTTPHealthCheck(3000, "/")
	plan, err := Plan(p, Options{SourceDir: t.TempDir(), ProjectName: "webapi"})
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if plan.HealthCheck == nil {
		t.Fatal("expected HealthCheck to be set in build plan")
	}
	pkgs := strings.Join(plan.Apko.Contents.Packages, " ")
	if !strings.Contains(pkgs, "wget") {
		t.Errorf("wget should be auto-added for HTTP health check, packages: %v",
			plan.Apko.Contents.Packages)
	}
}

func TestPlanHTTPHealthCheckURL(t *testing.T) {
	plan, err := Plan(profileWithHTTPHealthCheck(8080, "/healthz"), Options{SourceDir: t.TempDir(), ProjectName: "svc"})
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	hc := plan.HealthCheck
	if hc == nil {
		t.Fatal("HealthCheck is nil")
	}
	wantURL := "http://localhost:8080/healthz"
	if hc.Command[len(hc.Command)-1] != wantURL {
		t.Errorf("URL: got %q, want %q", hc.Command[len(hc.Command)-1], wantURL)
	}
}

func TestPlanDisabledHealthCheckSkipped(t *testing.T) {
	p := &types.Profile{
		Runtime: "golang",
		Build:   types.BuildConfig{Dependencies: []string{"go"}, Command: "go build ."},
		Image: types.ImageConfig{
			Packages:   []string{"ca-certificates"},
			Entrypoint: "/usr/bin/myapp",
			HealthCheck: &types.HealthCheckConfig{
				HTTP:     &types.HTTPHealthCheck{Port: 8080},
				Disabled: true,
			},
		},
	}
	plan, err := Plan(p, Options{SourceDir: t.TempDir(), ProjectName: "myapp"})
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if plan.HealthCheck != nil {
		t.Error("disabled health check should not be set in build plan")
	}
}

func TestPlanNoHealthCheckByDefault(t *testing.T) {
	plan, err := Plan(golangProfile(), Options{SourceDir: t.TempDir(), ProjectName: "mytool"})
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if plan.HealthCheck != nil {
		t.Error("golang profile has no health check by default, expected nil")
	}
}

func profileWithHTTPHealthCheck(port int, path string) *types.Profile {
	return &types.Profile{
		Runtime: "node",
		Build:   types.BuildConfig{Dependencies: []string{"nodejs"}, Command: "npm install"},
		Image: types.ImageConfig{
			Packages:   []string{"nodejs-20", "ca-certificates"},
			Entrypoint: "node",
			HealthCheck: &types.HealthCheckConfig{
				HTTP: &types.HTTPHealthCheck{Port: port, Path: path},
			},
		},
	}
}
