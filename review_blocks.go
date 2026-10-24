package main

import (
	"path/filepath"
	"regexp"
	"strings"

	hostedreview "px1/internal/hosted/review"
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
	workerFiles := make([]hostedreview.File, len(files))
	for i, file := range files {
		workerFiles[i].Path = file.Path
		for _, hunk := range file.Hunks {
			workerHunk := hostedreview.Hunk{}
			for _, line := range hunk.Lines {
				workerHunk.Lines = append(workerHunk.Lines, hostedreview.DiffLine{Kind: line.Kind, Text: line.Text})
			}
			workerFiles[i].Hunks = append(workerFiles[i].Hunks, workerHunk)
		}
	}
	workerBlocks := hostedreview.GroupBlocks(workerFiles)
	blocks := make([]staticReviewBlock, len(workerBlocks))
	for i, block := range workerBlocks {
		blocks[i] = staticReviewBlock{ID: block.ID, Title: block.Title, Summary: block.Summary, Paths: block.Paths}
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
