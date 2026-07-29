package hooks

import "testing"

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
	got := fixJavaHome(env, "8")
	if got["JAVA_HOME"] != "/usr/lib/jvm/java-1.8-openjdk" {
		t.Errorf("JAVA_HOME: got %q, want /usr/lib/jvm/java-1.8-openjdk", got["JAVA_HOME"])
	}
	if got["OTHER"] != "unchanged" {
		t.Errorf("OTHER should be unchanged: %q", got["OTHER"])
	}
}

func TestFixJavaHomeJava17NoChange(t *testing.T) {
	env := map[string]string{"JAVA_HOME": "/usr/lib/jvm/java-17-openjdk"}
	got := fixJavaHome(env, "17")
	if got["JAVA_HOME"] != "/usr/lib/jvm/java-17-openjdk" {
		t.Errorf("Java 17 should be unchanged: %q", got["JAVA_HOME"])
	}
}

func TestFixJavaHomeNilEnv(t *testing.T) {
	got := fixJavaHome(nil, "8")
	if got != nil {
		t.Errorf("nil env should return nil, got %v", got)
	}
}
