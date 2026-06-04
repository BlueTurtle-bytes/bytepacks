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
	"sort"
	"strconv"
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
	// SBOM versions often use non-APK formats (e.g. "openssl-3.6.2", "v1.2.0",
	// "releases/gcc-16.1.0"). Normalize before comparing so we don't treat a
	// version-string-format difference as a "needs update".
	result := &Result{SBOMPath: sbomPath, IndexArch: arch}
	for name, currentVer := range installed {
		latestVer, ok := latest[name]
		if !ok {
			continue // not a Wolfi package (e.g. the app itself)
		}
		normalizedVer := normalizeSBOMVersion(name, currentVer)
		update := PackageUpdate{
			Name:           name,
			CurrentVersion: currentVer,
			LatestVersion:  latestVer,
			NeedsUpdate:    compareAPKVersion(latestVer, normalizedVer) > 0,
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
// Transitive packages — those present in the SBOM but not yet in image.packages —
// are appended as new pinned entries so future rebuilds lock in the latest Wolfi
// versions for all packages, not just CVE-affected ones.
//
// Comments and all other fields in the profile are preserved.
func ApplyToProfile(profilePath string, updates []PackageUpdate) ([]string, error) {
	// patchMap covers all packages with a newer Wolfi version available.
	// Used for both updating existing image.packages entries AND adding new
	// transitive pins — ensures the rebuilt image is fully at Wolfi-latest.
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
	lastPackageLine := -1
	packageIndent := "    " // fallback: 4 spaces
	alreadyListed := make(map[string]bool)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "packages:" {
			inPackages = true
			continue
		}

		// Exit the packages block when we hit a non-list, non-comment line.
		if inPackages && trimmed != "" && !strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(trimmed, "#") {
			inPackages = false
		}

		if !inPackages || !strings.HasPrefix(trimmed, "- ") {
			continue
		}

		lastPackageLine = i
		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if indent != "" {
			packageIndent = indent
		}

		pkgRef := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
		pkgName := pkgRef
		if idx := strings.Index(pkgRef, "="); idx != -1 {
			pkgName = pkgRef[:idx]
		}
		alreadyListed[pkgName] = true

		latestVer, needsPatch := patchMap[pkgName]
		if !needsPatch {
			continue
		}

		newLine := fmt.Sprintf("%s- %s=%s", indent, pkgName, latestVer)
		lines[i] = newLine
		applied = append(applied, fmt.Sprintf("%s  %s → %s=%s", pkgName, pkgRef, pkgName, latestVer))
	}

	// Append pinned entries for all transitive packages not yet in image.packages
	// that have a newer Wolfi version. Pinning everything (not just CVE packages)
	// makes the rebuilt image fully reproducible and ensures defense-in-depth even
	// when grype misses a CVE due to SBOM version format differences.
	if lastPackageLine >= 0 {
		var newPkgs []string
		for pkgName := range patchMap {
			if !alreadyListed[pkgName] {
				newPkgs = append(newPkgs, pkgName)
			}
		}
		sort.Strings(newPkgs)

		var newLines []string
		for _, pkgName := range newPkgs {
			latestVer := patchMap[pkgName]
			newLines = append(newLines, fmt.Sprintf("%s- %s=%s", packageIndent, pkgName, latestVer))
			applied = append(applied, fmt.Sprintf("%s  (transitive) → %s=%s", pkgName, pkgName, latestVer))
		}

		if len(newLines) > 0 {
			updated := make([]string, 0, len(lines)+len(newLines))
			updated = append(updated, lines[:lastPackageLine+1]...)
			updated = append(updated, newLines...)
			updated = append(updated, lines[lastPackageLine+1:]...)
			lines = updated
		}
	}

	if len(applied) == 0 {
		return nil, nil
	}

	if err := os.WriteFile(profilePath, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return nil, fmt.Errorf("writing profile: %w", err)
	}

	return applied, nil
}

// NormalizeSBOMFile reads an SPDX SBOM, normalizes each package's versionInfo
// to a standard APK version string (stripping "v", name prefixes, path
// prefixes), and writes the result to a temp file. Returns the temp path;
// caller must remove it when done.
//
// This is needed before passing the SBOM to grype: grype can only correlate
// packages against its CVE database when versions look like "3.6.2-r5", not
// "openssl-3.6.2" or "releases/gcc-16.1.0".
func NormalizeSBOMFile(sbomPath string) (string, error) {
	data, err := os.ReadFile(sbomPath)
	if err != nil {
		return "", err
	}

	// Use a generic map so every field we don't touch is preserved verbatim.
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("invalid SBOM JSON: %w", err)
	}

	if pkgs, ok := doc["packages"].([]interface{}); ok {
		for _, entry := range pkgs {
			p, ok := entry.(map[string]interface{})
			if !ok {
				continue
			}
			name, _ := p["name"].(string)
			ver, _ := p["versionInfo"].(string)
			if name != "" && ver != "" {
				p["versionInfo"] = normalizeSBOMVersion(name, ver)
			}
		}
	}

	normalized, err := json.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("serializing normalized SBOM: %w", err)
	}

	tmp, err := os.CreateTemp("", "sbom-normalized-*.json")
	if err != nil {
		return "", err
	}
	defer tmp.Close()

	if _, err := tmp.Write(normalized); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}

	return tmp.Name(), nil
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
// We keep the HIGHEST version seen for each package name — the index can contain
// multiple entries for the same package (e.g. different sub-packages or epochs),
// and "last entry wins" can produce a stale/older version.
func parseAPKINDEX(r io.Reader) (map[string]string, error) {
	latest := make(map[string]string)
	scanner := bufio.NewScanner(r)

	var currentName, currentVersion string
	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// End of a package entry — keep it only if it's the highest version seen.
			if currentName != "" && currentVersion != "" {
				if existing, ok := latest[currentName]; !ok || compareAPKVersion(currentVersion, existing) > 0 {
					latest[currentName] = currentVersion
				}
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
		if existing, ok := latest[currentName]; !ok || compareAPKVersion(currentVersion, existing) > 0 {
			latest[currentName] = currentVersion
		}
	}

	return latest, scanner.Err()
}

// compareAPKVersion returns >0 if a is newer than b, 0 if equal, <0 if older.
// Handles the Alpine/Wolfi `VERSION-rREVISION` format.
func compareAPKVersion(a, b string) int {
	if a == b {
		return 0
	}
	aBase, aRev := splitAPKRev(a)
	bBase, bRev := splitAPKRev(b)
	if aBase != bBase {
		aParts := strings.Split(aBase, ".")
		bParts := strings.Split(bBase, ".")
		for i := 0; i < len(aParts) && i < len(bParts); i++ {
			an, aErr := strconv.Atoi(aParts[i])
			bn, bErr := strconv.Atoi(bParts[i])
			if aErr == nil && bErr == nil {
				if an != bn {
					return an - bn
				}
			} else if cmp := strings.Compare(aParts[i], bParts[i]); cmp != 0 {
				return cmp
			}
		}
		return len(aParts) - len(bParts)
	}
	return aRev - bRev
}

// splitAPKRev splits "1.2.3-r4" into ("1.2.3", 4). Missing revision → 0.
func splitAPKRev(ver string) (string, int) {
	if i := strings.LastIndex(ver, "-r"); i != -1 {
		if rev, err := strconv.Atoi(ver[i+2:]); err == nil {
			return ver[:i], rev
		}
	}
	return ver, 0
}

// normalizeSBOMVersion strips non-APK prefixes that SBOM generators sometimes
// embed in the version field, so version comparisons work correctly:
//   - "releases/gcc-16.1.0"  → "16.1.0"  (path + name prefix)
//   - "openssl-3.6.2"        → "3.6.2"   (name prefix)
//   - "v1.2.0"               → "1.2.0"   (v-prefix)
func normalizeSBOMVersion(name, ver string) string {
	// Strip path prefix (e.g. "releases/")
	if i := strings.LastIndex(ver, "/"); i != -1 {
		ver = ver[i+1:]
	}
	// Strip leading "v"
	ver = strings.TrimPrefix(ver, "v")
	// Strip "<name>-" prefix (e.g. "openssl-" from "openssl-3.6.2")
	ver = strings.TrimPrefix(ver, name+"-")
	return ver
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
