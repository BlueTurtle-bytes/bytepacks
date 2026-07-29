package config

import (
	"strings"
	"testing"

	"github.com/apexpack/apexpack/internal/types"
)

func TestBuildHealthCheckHTTPDefaults(t *testing.T) {
	hc := &types.HealthCheckConfig{HTTP: &types.HTTPHealthCheck{}}
	got := BuildHealthCheck(hc)
	if got == nil {
		t.Fatal("expected non-nil ApkoHealthCheck")
	}
	if len(got.Command) < 2 || got.Command[0] != "CMD" {
		t.Errorf("Command[0] should be CMD, got %v", got.Command)
	}
	wantURL := "http://localhost:8080/"
	if got.Command[len(got.Command)-1] != wantURL {
		t.Errorf("Command URL: got %q, want %q", got.Command[len(got.Command)-1], wantURL)
	}
	if got.Interval != "30s" {
		t.Errorf("Interval: got %q, want 30s", got.Interval)
	}
	if got.Timeout != "5s" {
		t.Errorf("Timeout: got %q, want 5s", got.Timeout)
	}
	if got.StartPeriod != "10s" {
		t.Errorf("StartPeriod: got %q, want 10s", got.StartPeriod)
	}
	if got.Retries != 3 {
		t.Errorf("Retries: got %d, want 3", got.Retries)
	}
}

func TestBuildHealthCheckHTTPCustom(t *testing.T) {
	hc := &types.HealthCheckConfig{
		HTTP:        &types.HTTPHealthCheck{Path: "/health", Port: 9090},
		Interval:    "60s",
		Timeout:     "10s",
		StartPeriod: "30s",
		Retries:     5,
	}
	got := BuildHealthCheck(hc)
	if got == nil {
		t.Fatal("expected non-nil ApkoHealthCheck")
	}
	wantURL := "http://localhost:9090/health"
	if got.Command[len(got.Command)-1] != wantURL {
		t.Errorf("URL: got %q, want %q", got.Command[len(got.Command)-1], wantURL)
	}
	if got.Interval != "60s" {
		t.Errorf("Interval: got %q, want 60s", got.Interval)
	}
	if got.Retries != 5 {
		t.Errorf("Retries: got %d, want 5", got.Retries)
	}
}

func TestBuildHealthCheckTCP(t *testing.T) {
	hc := &types.HealthCheckConfig{TCP: &types.TCPHealthCheck{Port: 5432}}
	got := BuildHealthCheck(hc)
	if got == nil {
		t.Fatal("expected non-nil ApkoHealthCheck")
	}
	if got.Command[0] != "CMD-SHELL" {
		t.Errorf("Command[0]: got %q, want CMD-SHELL", got.Command[0])
	}
	if !strings.Contains(got.Command[1], "5432") {
		t.Errorf("TCP command should mention port 5432: %q", got.Command[1])
	}
}

func TestBuildHealthCheckNilWhenNoCheck(t *testing.T) {
	hc := &types.HealthCheckConfig{}
	if got := BuildHealthCheck(hc); got != nil {
		t.Errorf("expected nil for empty config, got %v", got)
	}
}

func TestEnsurePackageAddsNew(t *testing.T) {
	pkgs := []string{"ca-certificates", "wolfi-baselayout"}
	got := EnsurePackage(pkgs, "wget")
	if len(got) != 3 || got[2] != "wget" {
		t.Errorf("expected wget appended, got %v", got)
	}
}

func TestEnsurePackageNoDuplicate(t *testing.T) {
	pkgs := []string{"ca-certificates", "wget"}
	got := EnsurePackage(pkgs, "wget")
	if len(got) != 2 {
		t.Errorf("expected no duplicate, got %v", got)
	}
}

func TestEnsurePackageSkipsPinned(t *testing.T) {
	pkgs := []string{"wget=1.21.4-r0"}
	got := EnsurePackage(pkgs, "wget")
	if len(got) != 1 {
		t.Errorf("pinned wget should not be duplicated, got %v", got)
	}
}
