package main

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// staticReviewBlock links files that appear to participate in the same
// behavior change. It is deterministic navigation metadata, not an AI claim.
type staticReviewBlock struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	Paths   []string `json:"paths"`
}

var reviewBlockToken = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_]{3,}`)

var reviewBlockStopWords = map[string]bool{
	"async": true, "await": true, "break": true, "case": true, "class": true,
	"const": true, "continue": true, "default": true, "else": true, "error": true,
	"export": true, "false": true, "from": true, "function": true, "import": true,
	"interface": true, "package": true, "return": true, "string": true, "struct": true,
	"switch": true, "throw": true, "true": true, "undefined": true,
}

type reviewBlockSignals struct {
	path map[string]bool
	code map[string]bool
}

func relatedReviewBlocks(files []staticReviewFile) []staticReviewBlock {
	if len(files) < 2 {
		return []staticReviewBlock{}
	}
	signals := make([]reviewBlockSignals, len(files))
	for i, file := range files {
		signals[i] = blockSignals(file)
	}
	parent := make([]int, len(files))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	union := func(a, b int) {
		a, b = find(a), find(b)
		if a != b {
			parent[b] = a
		}
	}
	for i := range files {
		for j := i + 1; j < len(files); j++ {
			if shareSignal(signals[i], signals[j]) {
				union(i, j)
			}
		}
	}
	groups := map[int][]int{}
	for i := range files {
		groups[find(i)] = append(groups[find(i)], i)
	}
	blocks := make([]staticReviewBlock, 0, len(groups))
	for _, indexes := range groups {
		if len(indexes) < 2 {
			continue
		}
		paths := make([]string, 0, len(indexes))
		for _, index := range indexes {
			paths = append(paths, files[index].Path)
		}
		sort.Strings(paths)
		topic := groupTopic(indexes, signals)
		sum := sha256.Sum256([]byte(strings.Join(paths, "\x00")))
		blocks = append(blocks, staticReviewBlock{
			ID: fmt.Sprintf("change-%x", sum[:8]), Title: humanizeTopic(topic),
			Summary: fmt.Sprintf("%d related files share the %q change signal.", len(paths), topic), Paths: paths,
		})
	}
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].Paths[0] < blocks[j].Paths[0] })
	if blocks == nil {
		return []staticReviewBlock{}
	}
	return blocks
}

func blockSignals(file staticReviewFile) reviewBlockSignals {
	s := reviewBlockSignals{path: map[string]bool{}, code: map[string]bool{}}
	path := strings.TrimSuffix(filepath.Base(filepath.ToSlash(file.Path)), filepath.Ext(file.Path))
	for _, token := range reviewBlockToken.FindAllString(path, -1) {
		addBlockToken(s.path, token)
	}
	for _, hunk := range file.Hunks {
		for _, line := range hunk.Lines {
			if line.Kind == "context" {
				continue
			}
			for _, token := range reviewBlockToken.FindAllString(line.Text, -1) {
				addBlockToken(s.code, token)
			}
		}
	}
	return s
}

func addBlockToken(set map[string]bool, token string) {
	token = strings.ToLower(strings.Trim(token, "_"))
	if len(token) >= 4 && !reviewBlockStopWords[token] {
		set[token] = true
	}
}

func shareSignal(a, b reviewBlockSignals) bool {
	for token := range a.path {
		if b.path[token] {
			return true
		}
	}
	sharedCode := 0
	for token := range a.code {
		if b.code[token] {
			sharedCode++
			if sharedCode >= 2 {
				return true
			}
		}
	}
	return false
}

func groupTopic(indexes []int, signals []reviewBlockSignals) string {
	counts := map[string]int{}
	for _, index := range indexes {
		seen := map[string]bool{}
		for token := range signals[index].code {
			seen[token] = true
		}
		for token := range signals[index].path {
			seen[token] = true
			counts[token]++ // Prefer explicit path vocabulary on ties.
		}
		for token := range seen {
			counts[token]++
		}
	}
	topic, score := "related", -1
	for token, count := range counts {
		if count > score || (count == score && token < topic) {
			topic, score = token, count
		}
	}
	return topic
}

func humanizeTopic(topic string) string {
	if topic == "related" {
		return "Related changes"
	}
	return strings.ToUpper(topic[:1]) + topic[1:] + " changes"
}
