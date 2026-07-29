package runner

import "testing"

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

func TestArchToDockerPlatform(t *testing.T) {
	if got := archToDockerPlatform("aarch64"); got != "linux/arm64" {
		t.Errorf("aarch64: got %q", got)
	}
	if got := archToDockerPlatform("x86_64"); got != "linux/amd64" {
		t.Errorf("x86_64: got %q", got)
	}
}
