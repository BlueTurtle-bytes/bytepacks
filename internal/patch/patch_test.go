package patch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func sign(n int) int {
	if n > 0 {
		return 1
	}
	if n < 0 {
		return -1
	}
	return 0
}

// ── splitAPKRev ───────────────────────────────────────────────────────────────

func TestSplitAPKRev(t *testing.T) {
	cases := []struct {
		ver     string
		base    string
		rev     int
	}{
		{"1.2.3-r4", "1.2.3", 4},
		{"1.2.3-r0", "1.2.3", 0},
		{"1.2.3", "1.2.3", 0},
		{"2.54.0-r1", "2.54.0", 1},
		{"", "", 0},
	}
	for _, c := range cases {
		base, rev := splitAPKRev(c.ver)
		if base != c.base || rev != c.rev {
			t.Errorf("splitAPKRev(%q) = (%q, %d), want (%q, %d)",
				c.ver, base, rev, c.base, c.rev)
		}
	}
}

// ── compareAPKVersion ─────────────────────────────────────────────────────────

func TestCompareAPKVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int // sign: +1, 0, -1
	}{
		// equal
		{"1.2.3-r1", "1.2.3-r1", 0},
		{"1.2.3", "1.2.3", 0},
		// revision difference
		{"1.2.3-r2", "1.2.3-r1", +1},
		{"1.2.3-r1", "1.2.3-r2", -1},
		{"1.2.3-r0", "1.2.3-r1", -1},
		// minor version
		{"1.10.0-r0", "1.9.0-r0", +1},
		{"1.9.0-r0", "1.10.0-r0", -1},
		// major version
		{"2.0.0-r0", "1.9.9-r9", +1},
		// real-world git case
		{"2.54.0-r0", "2.50.0-r1", +1},
		{"2.50.0-r1", "2.54.0-r0", -1},
		// no revision vs with revision
		{"1.2.4", "1.2.3", +1},
	}
	for _, c := range cases {
		got := compareAPKVersion(c.a, c.b)
		if sign(got) != sign(c.want) {
			t.Errorf("compareAPKVersion(%q, %q) = %d (sign %+d), want sign %+d",
				c.a, c.b, got, sign(got), sign(c.want))
		}
	}
}

// ── normalizeSBOMVersion ──────────────────────────────────────────────────────

func TestNormalizeSBOMVersion(t *testing.T) {
	cases := []struct {
		name, ver, want string
	}{
		{"gcc", "releases/gcc-16.1.0", "16.1.0"},
		{"openssl", "openssl-3.6.2", "3.6.2"},
		{"brotli", "v1.2.0", "1.2.0"},
		{"git", "2.50.0-r1", "2.50.0-r1"},
		{"sqlite", "3.53.2", "3.53.2"},
		{"heimdal", "heimdal-7.8.0", "7.8.0"},
		{"e2fsprogs", "v1.47.4", "1.47.4"},
	}
	for _, c := range cases {
		got := normalizeSBOMVersion(c.name, c.ver)
		if got != c.want {
			t.Errorf("normalizeSBOMVersion(%q, %q) = %q, want %q",
				c.name, c.ver, got, c.want)
		}
	}
}

// ── parseAPKINDEX ─────────────────────────────────────────────────────────────

func TestParseAPKINDEX(t *testing.T) {
	// Two entries for git — the higher version should win.
	input := "P:git\nV:2.50.0-r1\nA:aarch64\n\n" +
		"P:git\nV:2.54.0-r1\nA:aarch64\n\n" +
		"P:openssl\nV:3.6.3-r3\nA:aarch64\n\n"

	got, err := parseAPKINDEX(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"git":     "2.54.0-r1",
		"openssl": "3.6.3-r3",
	}
	for pkg, ver := range want {
		if got[pkg] != ver {
			t.Errorf("pkg %q: got %q, want %q", pkg, got[pkg], ver)
		}
	}
}

func TestParseAPKINDEXKeepsHighestVersion(t *testing.T) {
	// Older entry appears last — must still pick the highest version.
	input := "P:zlib\nV:1.3.2-r3\nA:aarch64\n\n" +
		"P:zlib\nV:1.3.1-r0\nA:aarch64\n\n"

	got, err := parseAPKINDEX(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if got["zlib"] != "1.3.2-r3" {
		t.Errorf("zlib: got %q, want 1.3.2-r3", got["zlib"])
	}
}

func TestParseAPKINDEXEmpty(t *testing.T) {
	got, err := parseAPKINDEX(strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

// ── ApplyToProfile ────────────────────────────────────────────────────────────

func writeProfile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readProfile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestApplyToProfileUpdatesPinnedVersion(t *testing.T) {
	path := writeProfile(t, `image:
  packages:
    - busybox
    - git=2.50.0-r1
    - ca-certificates
`)
	updates := []PackageUpdate{
		{Name: "git", CurrentVersion: "2.50.0-r1", LatestVersion: "2.54.0-r1", NeedsUpdate: true},
	}

	applied, err := ApplyToProfile(path, updates)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) == 0 {
		t.Fatal("expected at least one applied change")
	}

	out := readProfile(t, path)
	if !strings.Contains(out, "git=2.54.0-r1") {
		t.Errorf("expected git=2.54.0-r1 in output:\n%s", out)
	}
	if strings.Contains(out, "git=2.50.0-r1") {
		t.Errorf("old version should be gone:\n%s", out)
	}
}

func TestApplyToProfilePinsFloatingPackage(t *testing.T) {
	path := writeProfile(t, `image:
  packages:
    - busybox
    - openssl
`)
	updates := []PackageUpdate{
		{Name: "openssl", CurrentVersion: "3.6.3-r2", LatestVersion: "3.6.3-r3", NeedsUpdate: true},
	}

	_, err := ApplyToProfile(path, updates)
	if err != nil {
		t.Fatal(err)
	}

	out := readProfile(t, path)
	if !strings.Contains(out, "openssl=3.6.3-r3") {
		t.Errorf("expected floating package pinned:\n%s", out)
	}
}

func TestApplyToProfileAppendsTransitive(t *testing.T) {
	path := writeProfile(t, `image:
  packages:
    - busybox
    - git=2.50.0-r1
`)
	updates := []PackageUpdate{
		{Name: "git", CurrentVersion: "2.50.0-r1", LatestVersion: "2.54.0-r1", NeedsUpdate: true},
		{Name: "openssl", CurrentVersion: "3.6.3-r2", LatestVersion: "3.6.3-r3", NeedsUpdate: true},
	}

	applied, err := ApplyToProfile(path, updates)
	if err != nil {
		t.Fatal(err)
	}

	out := readProfile(t, path)
	if !strings.Contains(out, "openssl=3.6.3-r3") {
		t.Errorf("expected transitive package appended:\n%s", out)
	}
	if len(applied) < 2 {
		t.Errorf("expected at least 2 applied entries, got %d", len(applied))
	}
}

func TestApplyToProfileNothingToDoWhenUpToDate(t *testing.T) {
	path := writeProfile(t, `image:
  packages:
    - git=2.54.0-r1
`)
	updates := []PackageUpdate{
		{Name: "git", CurrentVersion: "2.54.0-r1", LatestVersion: "2.54.0-r1", NeedsUpdate: false},
	}

	applied, err := ApplyToProfile(path, updates)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 0 {
		t.Errorf("expected no changes, got %v", applied)
	}
}

func TestApplyToProfilePreservesComments(t *testing.T) {
	path := writeProfile(t, `# runtime profile
image:
  packages:
    # core packages
    - busybox
    - git=2.50.0-r1
`)
	updates := []PackageUpdate{
		{Name: "git", CurrentVersion: "2.50.0-r1", LatestVersion: "2.54.0-r1", NeedsUpdate: true},
	}

	_, err := ApplyToProfile(path, updates)
	if err != nil {
		t.Fatal(err)
	}

	out := readProfile(t, path)
	if !strings.Contains(out, "# runtime profile") {
		t.Errorf("top-level comment removed:\n%s", out)
	}
	if !strings.Contains(out, "# core packages") {
		t.Errorf("inline comment removed:\n%s", out)
	}
}

func TestApplyToProfileEmptyUpdates(t *testing.T) {
	path := writeProfile(t, `image:
  packages:
    - busybox
`)
	applied, err := ApplyToProfile(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 0 {
		t.Errorf("expected no changes with nil updates, got %v", applied)
	}
}

// ── parseSBOM ─────────────────────────────────────────────────────────────────

func TestParseSBOM(t *testing.T) {
	sbom := `{
  "packages": [
    {"name": "git", "versionInfo": "2.50.0-r1"},
    {"name": "busybox", "versionInfo": "1.36.1-r5"},
    {"name": "noversion", "versionInfo": "NOASSERTION"},
    {"name": "empty", "versionInfo": ""}
  ]
}`
	dir := t.TempDir()
	path := filepath.Join(dir, "sbom.json")
	if err := os.WriteFile(path, []byte(sbom), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := parseSBOM(path)
	if err != nil {
		t.Fatal(err)
	}

	if got["git"] != "2.50.0-r1" {
		t.Errorf("git: got %q, want 2.50.0-r1", got["git"])
	}
	if got["busybox"] != "1.36.1-r5" {
		t.Errorf("busybox: got %q, want 1.36.1-r5", got["busybox"])
	}
	if _, ok := got["noversion"]; ok {
		t.Error("NOASSERTION entry should be excluded")
	}
	if _, ok := got["empty"]; ok {
		t.Error("empty version entry should be excluded")
	}
}

func TestParseSBOMInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sbom.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := parseSBOM(path); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseSBOMMissing(t *testing.T) {
	if _, err := parseSBOM("/nonexistent/sbom.json"); err == nil {
		t.Error("expected error for missing file")
	}
}
