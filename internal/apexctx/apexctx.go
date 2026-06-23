// Package apexctx manages the .apexpack/context.json file written by each
// apexpack command as a side effect of its work. The file is a single source
// of truth that grows as the pipeline progresses: detect writes runtime and
// git info, build writes artifact paths, scan writes CVE counts, and so on.
//
// Any tool — Tekton task, GitHub Action, local developer — can read the file
// with "cat .apexpack/context.json | jq ." to see the full build state without
// threading values through CI params.
package apexctx

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const contextFile = ".apexpack/context.json"

// Context is the full build context written to .apexpack/context.json.
// Fields are populated incrementally: detect → build → scan → patch → push.
// Any field left empty means that stage hasn't run yet.
type Context struct {
	SchemaVersion string   `json:"_schema_version"`
	UpdatedBy     []string `json:"_updated_by"`

	// Set by detect
	ProjectName     string `json:"project_name,omitempty"`
	Runtime         string `json:"runtime,omitempty"`
	Framework       string `json:"framework,omitempty"`
	Confidence      string `json:"confidence,omitempty"`
	LanguageVersion string `json:"language_version,omitempty"`
	Arch            string `json:"arch,omitempty"`
	ArchRID         string `json:"arch_rid,omitempty"`

	GitURL    string `json:"git_url,omitempty"`
	GitBranch string `json:"git_branch,omitempty"`
	GitCommit string `json:"git_commit,omitempty"`

	AutoPatch    string `json:"auto_patch,omitempty"`
	PatchPersist string `json:"patch_persist,omitempty"`

	// Set by build
	Version      string `json:"version,omitempty"`
	Image        string `json:"image,omitempty"`
	ImageTarball string `json:"image_tarball,omitempty"`
	SBOMPath     string `json:"sbom_path,omitempty"`
	APKPath      string `json:"apk_path,omitempty"`

	// Set by scan
	ScanResult     string `json:"scan_result,omitempty"`
	ScanFailOn     string `json:"scan_fail_on,omitempty"`
	ScanCritical   string `json:"scan_critical,omitempty"`
	ScanHigh       string `json:"scan_high,omitempty"`
	ScanMedium     string `json:"scan_medium,omitempty"`
	ScanLow        string `json:"scan_low,omitempty"`
	ScanReportPath string `json:"scan_report_path,omitempty"`

	// Set by patch
	PatchesApplied  string   `json:"patches_applied,omitempty"`
	PatchedPackages []string `json:"patched_packages,omitempty"`

	// Set by rescan (second scan after patching)
	RescanResult   string `json:"rescan_result,omitempty"`
	RescanCritical string `json:"rescan_critical,omitempty"`
	RescanHigh     string `json:"rescan_high,omitempty"`
	RescanMedium   string `json:"rescan_medium,omitempty"`
	RescanLow      string `json:"rescan_low,omitempty"`

	// Set by push
	PushedImage string   `json:"pushed_image,omitempty"`
	PushedTags  []string `json:"pushed_tags,omitempty"`
}

// AppendStage records which command last updated the file.
func (c *Context) AppendStage(stage string) {
	c.UpdatedBy = append(c.UpdatedBy, stage)
}

// Load reads context.json from sourceDir/.apexpack/context.json.
// Returns an empty Context (not an error) when the file doesn't exist yet —
// this is the normal state before the first command runs.
func Load(sourceDir string) (*Context, error) {
	path := filepath.Join(sourceDir, contextFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Context{SchemaVersion: "1"}, nil
	}
	if err != nil {
		return nil, err
	}
	var ctx Context
	if err := json.Unmarshal(data, &ctx); err != nil {
		return nil, err
	}
	if ctx.SchemaVersion == "" {
		ctx.SchemaVersion = "1"
	}
	return &ctx, nil
}

// Save writes ctx to sourceDir/.apexpack/context.json atomically using a
// temp file + rename so a crash mid-write never leaves a partial file.
func Save(sourceDir string, ctx *Context) error {
	dir := filepath.Join(sourceDir, ".apexpack")
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return err
	}
	data, err := json.MarshalIndent(ctx, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "context-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// 0644 so nonroot steps (scan, patch) can read what a root build step wrote.
	tmp.Chmod(0o644) //nolint:errcheck
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, filepath.Join(dir, "context.json"))
}
