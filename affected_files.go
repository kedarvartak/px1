package main

import (
	"fmt"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strings"
)

const (
	affectedMaxFiles      = 2000
	affectedMaxFileBytes  = 256 << 10
	affectedMaxTotalBytes = 8 << 20
	affectedMaxHints      = 100
)

type staticAffectedFile struct {
	Path     string                   `json:"path"`
	Evidence []staticAffectedEvidence `json:"evidence"`
}

type staticAffectedAnalysis struct {
	RepositoryPaths int      `json:"repositoryPaths"`
	CandidatePaths  int      `json:"candidatePaths"`
	FilesScanned    int      `json:"filesScanned"`
	BytesScanned    int64    `json:"bytesScanned"`
	Truncated       bool     `json:"truncated"`
	Limits          []string `json:"limits"`
}

type staticAffectedEvidence struct {
	Kind        string `json:"kind"` // import, test, config, or dependency
	ChangedPath string `json:"changedPath"`
	Detail      string `json:"detail"`
}

var importReference = regexp.MustCompile(`^(?:import\s+(?:[^;]*?\s+from\s+)?|export\s+[^;]*?\s+from\s+|(?:const|let|var)\s+[^=]+?=\s*require\s*\(\s*|require\s*\(\s*)["']([^"']+)["']`)

// affectedFileHints finds unchanged files that may deserve a follow-up look.
// It reads bounded blobs from the pinned head commit and never executes them.
func affectedFileHints(root, head string, changedFiles []staticReviewFile) ([]staticAffectedFile, staticAffectedAnalysis, error) {
	changed := make(map[string]bool, len(changedFiles))
	changedPaths := make([]string, 0, len(changedFiles))
	for _, file := range changedFiles {
		changed[file.Path] = true
		changedPaths = append(changedPaths, file.Path)
	}
	sort.Strings(changedPaths)

	out, err := exec.Command("git", "-C", root, "ls-tree", "-r", "--name-only", "-z", head).Output()
	if err != nil {
		return nil, staticAffectedAnalysis{}, fmt.Errorf("list files for affected-file hints: %w", err)
	}
	paths := strings.Split(strings.TrimRight(string(out), "\x00"), "\x00")
	if len(paths) == 1 && paths[0] == "" {
		paths = nil
	}
	analysis := staticAffectedAnalysis{RepositoryPaths: len(paths), Limits: []string{}}
	if len(paths) > affectedMaxFiles {
		analysis.Truncated = true
		analysis.Limits = append(analysis.Limits, fmt.Sprintf("repository scan limited to the first %d paths", affectedMaxFiles))
		paths = paths[:affectedMaxFiles]
	}

	byPath := map[string][]staticAffectedEvidence{}
	hintsLimited := false
	evidenceLimited := false
	add := func(candidate string, evidence staticAffectedEvidence) {
		if candidate == "" || changed[candidate] {
			return
		}
		if len(byPath) >= affectedMaxHints && byPath[candidate] == nil {
			hintsLimited = true
			return
		}
		if len(byPath[candidate]) >= 8 {
			evidenceLimited = true
			return
		}
		for _, existing := range byPath[candidate] {
			if existing.Kind == evidence.Kind && existing.ChangedPath == evidence.ChangedPath && existing.Detail == evidence.Detail {
				return
			}
		}
		byPath[candidate] = append(byPath[candidate], evidence)
	}

	remaining := int64(affectedMaxTotalBytes)
	skippedContent := 0
	for _, candidate := range paths {
		if candidate == "" {
			continue
		}
		candidate = path.Clean(candidate)
		if changed[candidate] {
			continue
		}
		for _, changedPath := range changedPaths {
			if isTestPath(candidate) && companionStem(candidate) == companionStem(changedPath) {
				add(candidate, staticAffectedEvidence{Kind: "test", ChangedPath: changedPath, Detail: "test filename matches the changed source file"})
			}
			if isProjectConfig(candidate) && pathIsAncestor(path.Dir(candidate), changedPath) {
				add(candidate, staticAffectedEvidence{Kind: "config", ChangedPath: changedPath, Detail: "configuration file is in an ancestor directory of the changed file"})
			}
			if dependencyPair(changedPath, candidate) {
				add(candidate, staticAffectedEvidence{Kind: "dependency", ChangedPath: changedPath, Detail: "dependency manifest and lockfile are paired in the same directory"})
			}
		}
		if !supportsImportHints(candidate) {
			continue
		}
		analysis.CandidatePaths++

		content, ok, reason, readErr := affectedBlob(root, head, candidate, &remaining)
		if readErr != nil {
			return nil, staticAffectedAnalysis{}, readErr
		}
		if !ok {
			if reason != "" {
				skippedContent++
			}
			continue
		}
		analysis.FilesScanned++
		analysis.BytesScanned += int64(len(content))
		targets := importTargets(candidate, string(content))
		for _, changedPath := range changedPaths {
			if targetsChangedPath(targets, changedPath) {
				kind := "import"
				detail := "imports the changed file"
				if isTestPath(candidate) {
					kind, detail = "test", "test imports the changed file"
				}
				add(candidate, staticAffectedEvidence{Kind: kind, ChangedPath: changedPath, Detail: detail})
			}
		}
	}
	if skippedContent > 0 {
		analysis.Truncated = true
		analysis.Limits = append(analysis.Limits, fmt.Sprintf("content unavailable for %d candidate paths because of binary or byte limits", skippedContent))
	}
	if hintsLimited {
		analysis.Truncated = true
		analysis.Limits = append(analysis.Limits, fmt.Sprintf("hint output limited to %d files", affectedMaxHints))
	}
	if evidenceLimited {
		analysis.Truncated = true
		analysis.Limits = append(analysis.Limits, "evidence output limited to 8 relationships per hinted file")
	}

	hints := make([]staticAffectedFile, 0, len(byPath))
	for candidate, evidence := range byPath {
		sort.Slice(evidence, func(i, j int) bool {
			if evidence[i].Kind != evidence[j].Kind {
				return evidence[i].Kind < evidence[j].Kind
			}
			return evidence[i].ChangedPath < evidence[j].ChangedPath
		})
		hints = append(hints, staticAffectedFile{Path: candidate, Evidence: evidence})
	}
	sort.Slice(hints, func(i, j int) bool { return hints[i].Path < hints[j].Path })
	if hints == nil {
		return []staticAffectedFile{}, analysis, nil
	}
	return hints, analysis, nil
}

func affectedBlob(root, head, file string, remaining *int64) ([]byte, bool, string, error) {
	size, err := gitBlobSize(root, head, file)
	if err != nil {
		return nil, false, "", err
	}
	if size > affectedMaxFileBytes || size > *remaining {
		return nil, false, "byte limit", nil
	}
	b, found, err := gitFileAtCommit(root, head, file)
	if err != nil || !found {
		return nil, false, "", err
	}
	if containsNUL(b) {
		return nil, false, "binary", nil
	}
	*remaining -= size
	return b, true, "", nil
}

func gitBlobSize(root, rev, file string) (int64, error) {
	out, err := exec.Command("git", "-C", root, "cat-file", "-s", rev+":"+file).Output()
	if err != nil {
		return 0, fmt.Errorf("size of %s at %s: %w", file, shortRevision(rev), err)
	}
	var size int64
	if _, err := fmt.Sscan(strings.TrimSpace(string(out)), &size); err != nil {
		return 0, fmt.Errorf("parse size of %s: %w", file, err)
	}
	return size, nil
}

func importTargets(importer, content string) []string {
	targets := make([]string, 0)
	for _, line := range strings.Split(content, "\n") {
		match := importReference.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		spec := strings.TrimSuffix(match[1], path.Ext(match[1]))
		var target string
		if strings.HasPrefix(spec, ".") {
			target = path.Clean(path.Join(path.Dir(importer), spec))
		} else {
			target = strings.TrimPrefix(strings.ReplaceAll(spec, ".", "/"), "/")
		}
		target = strings.TrimSuffix(target, "/index")
		targets = append(targets, target)
	}
	return targets
}

func supportsImportHints(file string) bool {
	switch strings.ToLower(path.Ext(file)) {
	case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts":
		return true
	default:
		return false
	}
}

func targetsChangedPath(targets []string, changed string) bool {
	changedNoExt := strings.TrimSuffix(changed, path.Ext(changed))
	changedNoExt = strings.TrimSuffix(changedNoExt, "/index")
	for _, target := range targets {
		if target == changedNoExt {
			return true
		}
	}
	return false
}

func isTestPath(file string) bool {
	base := strings.ToLower(path.Base(file))
	return strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") || strings.HasSuffix(strings.TrimSuffix(base, path.Ext(base)), "_test") || strings.Contains(strings.ToLower(file), "/test/") || strings.Contains(strings.ToLower(file), "/tests/")
}

func companionStem(file string) string {
	base := strings.ToLower(strings.TrimSuffix(path.Base(file), path.Ext(file)))
	for _, suffix := range []string{".test", ".spec", "_test", "-test"} {
		base = strings.TrimSuffix(base, suffix)
	}
	return base
}

func isProjectConfig(file string) bool {
	base := strings.ToLower(path.Base(file))
	return base == "tsconfig.json" || strings.HasPrefix(base, "tsconfig.") && strings.HasSuffix(base, ".json") ||
		base == ".eslintrc" || strings.HasPrefix(base, ".eslintrc.") || strings.HasPrefix(base, "vite.config.") ||
		strings.HasPrefix(base, "webpack.config.") || base == "dockerfile" || base == "compose.yml" || base == "compose.yaml"
}

func pathIsAncestor(dir, file string) bool {
	return dir == "." || file == dir || strings.HasPrefix(file, strings.TrimSuffix(dir, "/")+"/")
}

func dependencyPair(changed, candidate string) bool {
	pairs := map[string][]string{
		"package.json":   {"package-lock.json", "npm-shrinkwrap.json", "yarn.lock", "pnpm-lock.yaml"},
		"go.mod":         {"go.sum"},
		"Cargo.toml":     {"Cargo.lock"},
		"pyproject.toml": {"poetry.lock", "uv.lock"},
		"Gemfile":        {"Gemfile.lock"},
	}
	changedBase, candidateBase := path.Base(changed), path.Base(candidate)
	if path.Dir(changed) != path.Dir(candidate) {
		return false
	}
	for manifest, locks := range pairs {
		for _, lock := range locks {
			if changedBase == manifest && candidateBase == lock || changedBase == lock && candidateBase == manifest {
				return true
			}
		}
	}
	return false
}
