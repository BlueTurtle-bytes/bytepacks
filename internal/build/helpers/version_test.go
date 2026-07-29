package helpers

import "testing"

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
		if got := Vsub(c.s, c.token, c.version); got != c.want {
			t.Errorf("Vsub(%q, %q, %q) = %q, want %q", c.s, c.token, c.version, got, c.want)
		}
	}
}

func TestVsubSlice(t *testing.T) {
	in := []string{"openjdk-{JAVA_VERSION}-jre", "ca-certificates", "maven-{JAVA_VERSION}"}
	got := VsubSlice(in, "{JAVA_VERSION}", "17")
	want := []string{"openjdk-17-jre", "ca-certificates", "maven-17"}
	for i, g := range got {
		if g != want[i] {
			t.Errorf("[%d] got %q, want %q", i, g, want[i])
		}
	}
}

func TestVsubSliceNoTokenReturnsOriginal(t *testing.T) {
	in := []string{"busybox", "ca-certificates"}
	got := VsubSlice(in, "", "21")
	if got[0] != in[0] || got[1] != in[1] {
		t.Errorf("expected original slice contents, got %v", got)
	}
}

func TestVsubMap(t *testing.T) {
	in := map[string]string{
		"JAVA_HOME": "/usr/lib/jvm/openjdk-{JAVA_VERSION}",
		"APP_ENV":   "production",
	}
	got := VsubMap(in, "{JAVA_VERSION}", "21")
	if got["JAVA_HOME"] != "/usr/lib/jvm/openjdk-21" {
		t.Errorf("JAVA_HOME: got %q", got["JAVA_HOME"])
	}
	if got["APP_ENV"] != "production" {
		t.Errorf("APP_ENV: got %q", got["APP_ENV"])
	}
}

func TestVsubMapEmptyReturnsOriginal(t *testing.T) {
	in := map[string]string{"K": "V"}
	got := VsubMap(in, "", "21")
	if got["K"] != "V" {
		t.Errorf("expected original map, got %v", got)
	}
}

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
		if got := LangVersionToken(c.runtime); got != c.want {
			t.Errorf("LangVersionToken(%q) = %q, want %q", c.runtime, got, c.want)
		}
	}
}

func TestResolveVersion(t *testing.T) {
	cases := []struct {
		runtime, detected, want string
	}{
		{"java", "17", "17"},
		{"java", "", "21"},
		{"node", "", "20"},
		{"python", "", "3.12"},
		{"dotnet", "", "8"},
		{"golang", "", ""},
		{"golang", "1.24", "1.24"},
		{"unknown", "", ""},
	}
	for _, c := range cases {
		if got := ResolveVersion(c.runtime, c.detected); got != c.want {
			t.Errorf("ResolveVersion(%q, %q) = %q, want %q", c.runtime, c.detected, got, c.want)
		}
	}
}

func TestValidateRuntimeVersion(t *testing.T) {
	for _, rt := range []string{"golang", "java", "node", "python"} {
		if err := ValidateRuntimeVersion(rt, "any-version"); err != nil {
			t.Errorf("runtime=%q: unexpected error: %v", rt, err)
		}
	}
	for _, v := range []string{"8", "9", "10"} {
		if err := ValidateRuntimeVersion("dotnet", v); err != nil {
			t.Errorf("dotnet version %q should be valid, got error: %v", v, err)
		}
	}
	if err := ValidateRuntimeVersion("dotnet", "6"); err == nil {
		t.Error("dotnet version 6 should be invalid")
	}
	if err := ValidateRuntimeVersion("dotnet", "7"); err == nil {
		t.Error("dotnet version 7 should be invalid")
	}
}
