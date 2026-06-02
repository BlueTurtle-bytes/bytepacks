// Package patch scans a built image SBOM for CVE-affected packages,
// fetches the latest patched versions from the Wolfi package index,
// and updates language profiles to pin the patched versions.
package patch

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

// PackageUpdate describes one package that needs patching.
type PackageUpdate struct {
	Name           string
	CurrentVersion string // version in the last built image (from SBOM)
	LatestVersion  string // latest available in the Wolfi index
	NeedsUpdate    bool   // LatestVersion != CurrentVersion
	CVEs           []string
	Severity       string // highest CVE severity for this package
}

// Result is the output of Check().
type Result struct {
	Updates   []PackageUpdate
	SBOMPath  string
	IndexArch string
}

// Check reads the SBOM, fetches the Wolfi package index, and returns a list
// of packages that have a newer version available (with CVE annotations
// from grype where a grype binary is available).
func Check(sbomPath, arch string) (*Result, error) {
	if arch == "" {
		arch = "x86_64"
	}

	// Step 1: parse the SBOM to get currently installed package versions.
	installed, err := parseSBOM(sbomPath)
	if err != nil {
		return nil, fmt.Errorf("parsing SBOM: %w", err)
	}

	// Step 2: fetch the Wolfi package index to find latest available versions.
	fmt.Printf("  → Fetching Wolfi package index (%s)...\n", arch)
	latest, err := fetchLatestVersions(arch)
	if err != nil {
		return nil, fmt.Errorf("fetching Wolfi index: %w", err)
	}

	// Step 3: run grype to identify CVE-affected packages (best-effort).
	cveMap := make(map[string][]string)
	severityMap := make(map[string]string)
	if grype, lookErr := exec.LookPath("grype"); lookErr == nil {
		cveMap, severityMap = runGrype(grype, sbomPath)
	} else {
		fmt.Println("  → grype not found — skipping CVE cross-reference (install: brew install grype)")
	}

	// Step 4: compare installed vs latest for each package.
	result := &Result{SBOMPath: sbomPath, IndexArch: arch}
	for name, currentVer := range installed {
		latestVer, ok := latest[name]
		if !ok {
			continue // not a Wolfi package (e.g. the app itself)
		}
		update := PackageUpdate{
			Name:           name,
			CurrentVersion: currentVer,
			LatestVersion:  latestVer,
			NeedsUpdate:    latestVer != currentVer,
			CVEs:           cveMap[name],
			Severity:       severityMap[name],
		}
		// Only include packages that are outdated OR have CVEs.
		if update.NeedsUpdate || len(update.CVEs) > 0 {
			result.Updates = append(result.Updates, update)
		}
	}

	return result, nil
}

// ApplyToProfile reads a profile YAML file and updates image.packages entries
// to pin the patched versions for each affected package.
//
// Floating entries (e.g. "ca-certificates-bundle") are updated to pinned entries
// (e.g. "ca-certificates-bundle=20260413-r1"). Already-pinned entries have their
// version bumped to the latest.
//
// Comments and all other fields in the profile are preserved.
func ApplyToProfile(profilePath string, updates []PackageUpdate) ([]string, error) {
	// Build a lookup: package name → latest version.
	patchMap := make(map[string]string)
	for _, u := range updates {
		if u.NeedsUpdate {
			patchMap[u.Name] = u.LatestVersion
		}
	}
	if len(patchMap) == 0 {
		return nil, nil
	}

	data, err := os.ReadFile(profilePath)
	if err != nil {
		return nil, fmt.Errorf("reading profile: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	var applied []string
	inPackages := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect entry into the image.packages: list.
		if trimmed == "packages:" {
			// Check if the previous non-empty line is "image:" context.
			// Simple heuristic: any "packages:" we find is the image packages.
			inPackages = true
			continue
		}

		// Detect exit from the packages list — a non-list line.
		if inPackages && trimmed != "" && !strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(trimmed, "#") {
			inPackages = false
		}

		if !inPackages {
			continue
		}
		if !strings.HasPrefix(trimmed, "- ") {
			continue
		}

		// Extract the package reference from the list item.
		// Could be "- pkgname" or "- pkgname=version"
		pkgRef := strings.TrimPrefix(trimmed, "- ")
		pkgRef = strings.TrimSpace(pkgRef)

		// Strip any existing version pin to get the base name.
		pkgName := pkgRef
		if idx := strings.Index(pkgRef, "="); idx != -1 {
			pkgName = pkgRef[:idx]
		}

		latestVer, needsPatch := patchMap[pkgName]
		if !needsPatch {
			continue
		}

		// Replace the line with the pinned version.
		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		newLine := fmt.Sprintf("%s- %s=%s", indent, pkgName, latestVer)
		lines[i] = newLine
		applied = append(applied, fmt.Sprintf("%s  %s → %s=%s", pkgName, pkgRef, pkgName, latestVer))
	}

	if len(applied) == 0 {
		return nil, nil
	}

	if err := os.WriteFile(profilePath, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return nil, fmt.Errorf("writing profile: %w", err)
	}

	return applied, nil
}

// ============================================================================
// SBOM parsing — reads the SPDX JSON SBOM produced by apko
// ============================================================================

type spdxDoc struct {
	Packages []spdxPackage `json:"packages"`
}

type spdxPackage struct {
	Name        string `json:"name"`
	VersionInfo string `json:"versionInfo"`
}

// parseSBOM returns a map of packageName → installedVersion from an SPDX SBOM.
func parseSBOM(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc spdxDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("invalid SBOM JSON: %w", err)
	}
	installed := make(map[string]string)
	for _, p := range doc.Packages {
		if p.VersionInfo != "" && p.VersionInfo != "NOASSERTION" {
			installed[p.Name] = p.VersionInfo
		}
	}
	return installed, nil
}

// ============================================================================
// Wolfi APKINDEX parsing — fetches latest package versions from Wolfi
// ============================================================================

// fetchLatestVersions downloads and parses the Wolfi APKINDEX for the given
// architecture, returning a map of packageName → latestVersion.
func fetchLatestVersions(arch string) (map[string]string, error) {
	url := fmt.Sprintf("https://packages.wolfi.dev/os/%s/APKINDEX.tar.gz", arch)

	resp, err := http.Get(url) //nolint:gosec // URL is constructed from a known safe constant
	if err != nil {
		return nil, fmt.Errorf("downloading APKINDEX: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("APKINDEX returned HTTP %d", resp.StatusCode)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("decompressing APKINDEX: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading APKINDEX tar: %w", err)
		}
		if hdr.Name == "APKINDEX" {
			return parseAPKINDEX(tr)
		}
	}
	return nil, fmt.Errorf("APKINDEX file not found in archive")
}

// parseAPKINDEX reads the APKINDEX file format.
// Each package entry is a block of "KEY:value" lines separated by blank lines.
// We keep the LATEST version seen for each package name (last entry wins).
func parseAPKINDEX(r io.Reader) (map[string]string, error) {
	latest := make(map[string]string)
	scanner := bufio.NewScanner(r)

	var currentName, currentVersion string
	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// End of a package entry — record it.
			if currentName != "" && currentVersion != "" {
				latest[currentName] = currentVersion
			}
			currentName = ""
			currentVersion = ""
			continue
		}

		if rest, ok := strings.CutPrefix(line, "P:"); ok {
			currentName = strings.TrimSpace(rest)
		}
		if rest, ok := strings.CutPrefix(line, "V:"); ok {
			currentVersion = strings.TrimSpace(rest)
		}
	}
	// Flush last entry.
	if currentName != "" && currentVersion != "" {
		latest[currentName] = currentVersion
	}

	return latest, scanner.Err()
}

// ============================================================================
// Grype — CVE cross-referencing
// ============================================================================

type grypeMatch struct {
	Vulnerability struct {
		ID       string `json:"id"`
		Severity string `json:"severity"`
	} `json:"vulnerability"`
	Artifact struct {
		Name string `json:"name"`
	} `json:"artifact"`
}

type grypeOutput struct {
	Matches []grypeMatch `json:"matches"`
}

// runGrype runs grype in JSON mode and returns per-package CVE lists.
func runGrype(grypePath, sbomPath string) (map[string][]string, map[string]string) {
	cves := make(map[string][]string)
	severity := make(map[string]string)

	out, err := exec.Command(grypePath,
		fmt.Sprintf("sbom:%s", sbomPath),
		"--output", "json",
	).Output()
	if err != nil && len(out) == 0 {
		return cves, severity
	}

	var result grypeOutput
	if err := json.Unmarshal(out, &result); err != nil {
		return cves, severity
	}

	severityRank := map[string]int{
		"Critical": 4, "High": 3, "Medium": 2, "Low": 1, "Negligible": 0,
	}

	for _, m := range result.Matches {
		name := m.Artifact.Name
		id := m.Vulnerability.ID
		sev := m.Vulnerability.Severity

		cves[name] = append(cves[name], id)

		if severityRank[sev] > severityRank[severity[name]] {
			severity[name] = sev
		}
	}
	return cves, severity
}
