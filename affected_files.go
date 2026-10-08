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

type staticAffectedEvidence struct {
	Kind        string `json:"kind"` // import, test, config, or dependency
	ChangedPath string `json:"changedPath"`
	Detail      string `json:"detail"`
}

var importReference = regexp.MustCompile(`(?m)(?:\bfrom\s+|\brequire\s*\(\s*|\bimport\s+(?:[^\n]*?\s+from\s+)?)["']([^"']+)["']`)

// affectedFileHints finds unchanged files that may deserve a follow-up look.
// It reads bounded blobs from the pinned head commit and never executes them.
func affectedFileHints(root, head string, changedFiles []staticReviewFile) ([]staticAffectedFile, error) {
	changed := make(map[string]bool, len(changedFiles))
	changedPaths := make([]string, 0, len(changedFiles))
	for _, file := range changedFiles {
		changed[file.Path] = true
		changedPaths = append(changedPaths, file.Path)
	}
	sort.Strings(changedPaths)

	out, err := exec.Command("git", "-C", root, "ls-tree", "-r", "--name-only", "-z", head).Output()
	if err != nil {
		return nil, fmt.Errorf("list files for affected-file hints: %w", err)
	}
	paths := strings.Split(strings.TrimRight(string(out), "\x00"), "\x00")
	if len(paths) > affectedMaxFiles {
		paths = paths[:affectedMaxFiles]
	}

	byPath := map[string][]staticAffectedEvidence{}
	add := func(candidate string, evidence staticAffectedEvidence) {
		if candidate == "" || changed[candidate] || len(byPath) >= affectedMaxHints && byPath[candidate] == nil {
			return
		}
		if len(byPath[candidate]) >= 8 {
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
				add(candidate, staticAffectedEvidence{Kind: "config", ChangedPath: changedPath, Detail: "configuration file applies to the changed file's directory"})
			}
			if dependencyPair(changedPath, candidate) {
				add(candidate, staticAffectedEvidence{Kind: "dependency", ChangedPath: changedPath, Detail: "dependency manifest and lockfile should stay in sync"})
			}
		}

		content, ok, readErr := affectedBlob(root, head, candidate, &remaining)
		if readErr != nil {
			return nil, readErr
		}
		if !ok {
			continue
		}
		for _, changedPath := range changedPaths {
			if importsChangedPath(candidate, string(content), changedPath) {
				kind := "import"
				detail := "imports the changed file"
				if isTestPath(candidate) {
					kind, detail = "test", "test imports the changed file"
				}
				add(candidate, staticAffectedEvidence{Kind: kind, ChangedPath: changedPath, Detail: detail})
			}
		}
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
		return []staticAffectedFile{}, nil
	}
	return hints, nil
}

func affectedBlob(root, head, file string, remaining *int64) ([]byte, bool, error) {
	size, err := gitBlobSize(root, head, file)
	if err != nil {
		return nil, false, err
	}
	if size > affectedMaxFileBytes || size > *remaining {
		return nil, false, nil
	}
	b, found, err := gitFileAtCommit(root, head, file)
	if err != nil || !found {
		return nil, false, err
	}
	if containsNUL(b) {
		return nil, false, nil
	}
	*remaining -= size
	return b, true, nil
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

func importsChangedPath(importer, content, changed string) bool {
	changedNoExt := strings.TrimSuffix(changed, path.Ext(changed))
	changedNoExt = strings.TrimSuffix(changedNoExt, "/index")
	changedDir := path.Dir(changed)
	for _, match := range importReference.FindAllStringSubmatch(content, -1) {
		spec := strings.TrimSuffix(match[1], path.Ext(match[1]))
		var target string
		if strings.HasPrefix(spec, ".") {
			target = path.Clean(path.Join(path.Dir(importer), spec))
		} else {
			target = strings.TrimPrefix(strings.ReplaceAll(spec, ".", "/"), "/")
		}
		target = strings.TrimSuffix(target, "/index")
		if target == changedNoExt || (changedDir != "." && strings.HasSuffix(target, "/"+changedDir)) {
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
	return strings.HasPrefix(base, "tsconfig") || strings.HasPrefix(base, ".eslintrc") || strings.HasPrefix(base, "vite.config.") || strings.HasPrefix(base, "webpack.config.") || base == "dockerfile" || base == "compose.yml" || base == "compose.yaml"
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
