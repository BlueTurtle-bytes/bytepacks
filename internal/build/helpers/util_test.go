package helpers

import (
	"os"
	"path/filepath"
	"testing"
)

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
		if got := ApplyProjectTemplates(c.s, c.name); got != c.want {
			t.Errorf("ApplyProjectTemplates(%q, %q) = %q, want %q", c.s, c.name, got, c.want)
		}
	}
}

func TestReadProcfileCmd(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Procfile"), []byte("web: ./bin/server --port 8080\nworker: ./bin/worker\n"), 0o644)
	if got := ReadProcfileCmd(dir); got != "./bin/server --port 8080" {
		t.Errorf("got %q, want ./bin/server --port 8080", got)
	}
}

func TestReadProcfileCmdNoWebProcess(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Procfile"), []byte("worker: ./bin/worker\n"), 0o644)
	if got := ReadProcfileCmd(dir); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestReadProcfileCmdMissing(t *testing.T) {
	if got := ReadProcfileCmd(t.TempDir()); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestCacheVolumeName(t *testing.T) {
	cases := []struct {
		path, want string
	}{
		{"/home/build/.npm", "apexpack-cache-home-build--npm"},
		{"/home/build/go/pkg/mod", "apexpack-cache-home-build-go-pkg-mod"},
		{"/root/.m2", "apexpack-cache-root--m2"},
	}
	for _, c := range cases {
		if got := CacheVolumeName(c.path); got != c.want {
			t.Errorf("CacheVolumeName(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}
