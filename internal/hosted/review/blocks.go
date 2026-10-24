package review

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type DiffLine struct{ Kind, Text string }
type Hunk struct{ Lines []DiffLine }
type File struct {
	Path  string
	Hunks []Hunk
}
type Block struct {
	ID, Title, Summary string
	Paths              []string
}

var tokenPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_]{3,}`)
var stopWords = map[string]bool{
	"async": true, "await": true, "break": true, "case": true, "class": true,
	"const": true, "continue": true, "default": true, "else": true, "error": true,
	"export": true, "false": true, "from": true, "function": true, "import": true,
	"interface": true, "package": true, "return": true, "string": true, "struct": true,
	"switch": true, "throw": true, "true": true, "undefined": true,
}

type signals struct{ path, code map[string]bool }

func GroupBlocks(files []File) []Block {
	if len(files) < 2 {
		return []Block{}
	}
	all := make([]signals, len(files))
	parent := make([]int, len(files))
	for i, file := range files {
		all[i] = fileSignals(file)
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	for i := range files {
		for j := i + 1; j < len(files); j++ {
			if shared(all[i], all[j]) {
				parent[find(j)] = find(i)
			}
		}
	}
	groups := map[int][]int{}
	for i := range files {
		groups[find(i)] = append(groups[find(i)], i)
	}
	out := []Block{}
	for _, indexes := range groups {
		if len(indexes) < 2 {
			continue
		}
		paths := make([]string, 0, len(indexes))
		for _, i := range indexes {
			paths = append(paths, files[i].Path)
		}
		sort.Strings(paths)
		topic := topic(indexes, all)
		digest := sha256.Sum256([]byte(strings.Join(paths, "\x00")))
		out = append(out, Block{ID: fmt.Sprintf("change-%x", digest[:8]), Title: strings.ToUpper(topic[:1]) + topic[1:] + " changes", Summary: fmt.Sprintf("%d related files share the %q change signal.", len(paths), topic), Paths: paths})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Paths[0] < out[j].Paths[0] })
	return out
}

func fileSignals(file File) signals {
	s := signals{map[string]bool{}, map[string]bool{}}
	base := strings.TrimSuffix(filepath.Base(filepath.ToSlash(file.Path)), filepath.Ext(file.Path))
	for _, value := range tokenPattern.FindAllString(base, -1) {
		add(s.path, value)
	}
	for _, hunk := range file.Hunks {
		for _, line := range hunk.Lines {
			if line.Kind != "context" {
				for _, value := range tokenPattern.FindAllString(line.Text, -1) {
					add(s.code, value)
				}
			}
		}
	}
	return s
}
func add(set map[string]bool, value string) {
	value = strings.ToLower(strings.Trim(value, "_"))
	if len(value) >= 4 && !stopWords[value] {
		set[value] = true
	}
}
func shared(a, b signals) bool {
	for v := range a.path {
		if b.path[v] {
			return true
		}
	}
	count := 0
	for v := range a.code {
		if b.code[v] {
			count++
			if count >= 2 {
				return true
			}
		}
	}
	return false
}
func topic(indexes []int, all []signals) string {
	counts := map[string]int{}
	for _, i := range indexes {
		seen := map[string]bool{}
		for v := range all[i].code {
			seen[v] = true
		}
		for v := range all[i].path {
			seen[v] = true
			counts[v]++
		}
		for v := range seen {
			counts[v]++
		}
	}
	best, score := "related", -1
	for v, n := range counts {
		if n > score || (n == score && v < best) {
			best, score = v, n
		}
	}
	return best
}
