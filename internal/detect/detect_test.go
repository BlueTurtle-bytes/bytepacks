package detect

import (
	"os"
	"path/filepath"
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

func makeProfile(runtime string, files []string, confidence float64) *types.Profile {
	return &types.Profile{
		Runtime: runtime,
		Detect:  types.DetectConfig{Files: files, Confidence: confidence},
	}
}

// ── javaMajor ─────────────────────────────────────────────────────────────────

func TestJavaMajor(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		{"17", "17"},
		{"17.0.1", "17"},
		{"21.0.3", "21"},
		{"1.8", "8"},
		{"1.8.0_292", "8"},
		{"1.11", "11"},
		{"adoptopenjdk-17.0.1+12", "17"},
		{"21.0.3-graal", "21"},
		{"", ""},
		{"abc", ""},
	}
	for _, c := range cases {
		if got := javaMajor(c.input); got != c.want {
			t.Errorf("javaMajor(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// ── pythonMinorVersion ────────────────────────────────────────────────────────

func TestPythonMinorVersion(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		{"3.11.0", "3.11"},
		{"3.11", "3.11"},
		{"3.12.1", "3.12"},
		{"python3.11", "3.11"},
		{"python-3.11.0", "3.11"},
		{"2.7.18", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := pythonMinorVersion(c.input); got != c.want {
			t.Errorf("pythonMinorVersion(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// ── toolVersionsEntry ─────────────────────────────────────────────────────────

func TestToolVersionsEntry(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".tool-versions",
		"java adoptopenjdk-17.0.1+12\nnodejs 20.11.0\npython 3.11.0\n")

	cases := []struct {
		tool, want string
	}{
		{"java", "adoptopenjdk-17.0.1+12"},
		{"nodejs", "20.11.0"},
		{"python", "3.11.0"},
		{"ruby", ""},
	}
	for _, c := range cases {
		if got := toolVersionsEntry(dir, c.tool); got != c.want {
			t.Errorf("tool=%q: got %q, want %q", c.tool, got, c.want)
		}
	}
}

func TestToolVersionsEntryMissing(t *testing.T) {
	if got := toolVersionsEntry(t.TempDir(), "java"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// ── detectGoVersion ───────────────────────────────────────────────────────────

func TestDetectGoVersion(t *testing.T) {
	cases := []struct {
		gomod, want string
	}{
		{"module x\n\ngo 1.21\n", "1.21"},
		{"module x\n\ngo 1.24\n", "1.24"},
		{"module x\n", ""},
	}
	for _, c := range cases {
		dir := t.TempDir()
		writeFile(t, dir, "go.mod", c.gomod)
		if got := detectGoVersion(dir); got != c.want {
			t.Errorf("gomod=%q: got %q, want %q", c.gomod, got, c.want)
		}
	}
}

func TestDetectGoVersionMissing(t *testing.T) {
	if got := detectGoVersion(t.TempDir()); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// ── detectNodeVersion ─────────────────────────────────────────────────────────

func TestDetectNodeVersionNvmrc(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".nvmrc", "v20.11.0\n")
	if got := detectNodeVersion(dir); got != "20" {
		t.Errorf("got %q, want 20", got)
	}
}

func TestDetectNodeVersionNodeVersion(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".node-version", "18\n")
	if got := detectNodeVersion(dir); got != "18" {
		t.Errorf("got %q, want 18", got)
	}
}

func TestDetectNodeVersionPackageJSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"engines":{"node":">=20"}}`)
	if got := detectNodeVersion(dir); got != "20" {
		t.Errorf("got %q, want 20", got)
	}
}

func TestDetectNodeVersionToolVersions(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".tool-versions", "nodejs 20.11.0\n")
	if got := detectNodeVersion(dir); got != "20" {
		t.Errorf("got %q, want 20", got)
	}
}

// ── detectJavaVersion ─────────────────────────────────────────────────────────

func TestDetectJavaVersionFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".java-version", "17.0.1\n")
	if got := detectJavaVersion(dir); got != "17" {
		t.Errorf("got %q, want 17", got)
	}
}

func TestDetectJavaVersionSDKManrc(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".sdkmanrc", "java=21.0.3-tem\n")
	if got := detectJavaVersion(dir); got != "21" {
		t.Errorf("got %q, want 21", got)
	}
}

func TestDetectJavaVersionPomXML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pom.xml",
		`<project><properties><maven.compiler.source>17</maven.compiler.source></properties></project>`)
	if got := detectJavaVersion(dir); got != "17" {
		t.Errorf("got %q, want 17", got)
	}
}

func TestDetectJavaVersionGradle(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "build.gradle", `sourceCompatibility = '21'`)
	if got := detectJavaVersion(dir); got != "21" {
		t.Errorf("got %q, want 21", got)
	}
}

func TestDetectJavaVersionOldStyle(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".java-version", "1.8\n")
	if got := detectJavaVersion(dir); got != "8" {
		t.Errorf("got %q, want 8", got)
	}
}

// ── detectPythonVersion ───────────────────────────────────────────────────────

func TestDetectPythonVersionFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".python-version", "3.11.0\n")
	if got := detectPythonVersion(dir); got != "3.11" {
		t.Errorf("got %q, want 3.11", got)
	}
}

func TestDetectPythonVersionPyproject(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pyproject.toml", `[project]
requires-python = ">=3.12"
`)
	if got := detectPythonVersion(dir); got != "3.12" {
		t.Errorf("got %q, want 3.12", got)
	}
}

func TestDetectPythonVersionRuntimeTxt(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "runtime.txt", "python-3.11.2\n")
	if got := detectPythonVersion(dir); got != "3.11" {
		t.Errorf("got %q, want 3.11", got)
	}
}

// ── detectDotnetVersion ───────────────────────────────────────────────────────

func TestDetectDotnetVersionGlobalJSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global.json", `{"sdk":{"version":"8.0.100"}}`)
	if got := detectDotnetVersion(dir); got != "8" {
		t.Errorf("got %q, want 8", got)
	}
}

func TestDetectDotnetVersionCsproj(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "App.csproj",
		`<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>`)
	if got := detectDotnetVersion(dir); got != "8" {
		t.Errorf("got %q, want 8", got)
	}
}

// ── Run / Best ────────────────────────────────────────────────────────────────

func TestBestMatchesCorrectRuntime(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module x\n\ngo 1.21\n")

	profiles := []*types.Profile{
		makeProfile("golang", []string{"go.mod"}, 0.9),
		makeProfile("node", []string{"package.json"}, 0.9),
	}

	result := Best(profiles, dir)
	if result == nil {
		t.Fatal("expected a match, got nil")
	}
	if result.Profile.Runtime != "golang" {
		t.Errorf("got runtime %q, want golang", result.Profile.Runtime)
	}
}

func TestBestReturnsNilWhenNoMatch(t *testing.T) {
	dir := t.TempDir()
	profiles := []*types.Profile{
		makeProfile("golang", []string{"go.mod"}, 0.9),
	}
	if result := Best(profiles, dir); result != nil {
		t.Errorf("expected nil, got %+v", result)
	}
}

func TestRunRankedByConfidence(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module x\n\ngo 1.21\n")
	writeFile(t, dir, "package.json", `{}`)

	profiles := []*types.Profile{
		makeProfile("node", []string{"package.json"}, 0.7),
		makeProfile("golang", []string{"go.mod"}, 0.9),
	}

	results := Run(profiles, dir)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Profile.Runtime != "golang" {
		t.Errorf("highest confidence should be golang, got %q", results[0].Profile.Runtime)
	}
}

func TestRunContentBoostsConfidence(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module x\n\ngo 1.21\n")
	writeFile(t, dir, "README.md", "uses grpc")

	p := &types.Profile{
		Runtime: "golang",
		Detect: types.DetectConfig{
			Files:      []string{"go.mod"},
			Confidence: 0.8,
			Content: []types.ContentRule{
				{File: "README.md", Contains: "grpc", BoostConfidence: 0.1},
			},
		},
	}

	results := Run([]*types.Profile{p}, dir)
	if len(results) != 1 {
		t.Fatal("expected 1 result")
	}
	if results[0].Confidence != 0.9 {
		t.Errorf("confidence = %.2f, want 0.90", results[0].Confidence)
	}
}

func TestRunConfidenceCappedAt1(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module x\n\ngo 1.21\n")
	writeFile(t, dir, "a.txt", "x")
	writeFile(t, dir, "b.txt", "x")

	p := &types.Profile{
		Runtime: "golang",
		Detect: types.DetectConfig{
			Files:      []string{"go.mod"},
			Confidence: 0.9,
			Content: []types.ContentRule{
				{File: "a.txt", Contains: "x", BoostConfidence: 0.2},
				{File: "b.txt", Contains: "x", BoostConfidence: 0.2},
			},
		},
	}

	results := Run([]*types.Profile{p}, dir)
	if len(results) != 1 {
		t.Fatal("expected 1 result")
	}
	if results[0].Confidence > 1.0 {
		t.Errorf("confidence %f exceeds 1.0", results[0].Confidence)
	}
}
