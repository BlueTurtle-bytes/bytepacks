// Package detect matches language profiles against a source directory.
package detect

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/apexpack/apexpack/internal/types"
)

// Run checks every profile against srcDir and returns all matches,
// sorted by confidence (highest first).
//
// A profile matches when at least one file from detect.files or detect.patterns
// is found. Content rules boost the confidence score and set the framework field.
func Run(profiles []*types.Profile, srcDir string) []types.DetectResult {
	var results []types.DetectResult

	for _, p := range profiles {
		result, matched := matchProfile(p, srcDir)
		if matched {
			results = append(results, result)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Confidence > results[j].Confidence
	})

	return results
}

// Best returns the single highest-confidence match, or nil if nothing matched.
func Best(profiles []*types.Profile, srcDir string) *types.DetectResult {
	results := Run(profiles, srcDir)
	if len(results) == 0 {
		return nil
	}
	return &results[0]
}

// matchProfile checks whether a single profile matches srcDir.
func matchProfile(p *types.Profile, srcDir string) (types.DetectResult, bool) {
	var matchedFiles []string

	for _, filename := range p.Detect.Files {
		if fileExists(filepath.Join(srcDir, filename)) {
			matchedFiles = append(matchedFiles, filename)
		}
	}

	for _, pattern := range p.Detect.Patterns {
		matches, err := filepath.Glob(filepath.Join(srcDir, pattern))
		if err == nil && len(matches) > 0 {
			matchedFiles = append(matchedFiles, filepath.Base(matches[0]))
		}
	}

	if len(matchedFiles) == 0 {
		return types.DetectResult{}, false
	}

	confidence := p.Detect.Confidence
	if confidence == 0 {
		confidence = 0.8
	}

	var matchedContent []string
	var framework string
	for _, rule := range p.Detect.Content {
		if contentMatches(srcDir, rule.File, rule.Contains) {
			confidence += rule.BoostConfidence
			matchedContent = append(matchedContent, rule.File+":"+rule.Contains)
			if framework == "" && rule.Framework != "" {
				framework = rule.Framework
			}
		}
	}

	var packageManager string
	for _, rule := range p.Detect.PackageManagers {
		if fileExists(filepath.Join(srcDir, rule.File)) {
			packageManager = rule.Manager
			break
		}
	}

	if confidence > 1.0 {
		confidence = 1.0
	}

	return types.DetectResult{
		Profile:        p,
		Confidence:     confidence,
		MatchedFiles:   matchedFiles,
		MatchedContent: matchedContent,
		Framework:      framework,
		PackageManager: packageManager,
	}, true
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func contentMatches(srcDir, filename, contains string) bool {
	data, err := os.ReadFile(filepath.Join(srcDir, filename))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), contains)
}
