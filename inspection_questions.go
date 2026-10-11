package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
	"strings"
)

const (
	inspectionQuestionLimit = 100
	inspectionRelatedLimit  = 8
)

type staticInspectionQuestion struct {
	ID          string                      `json:"id"`
	Path        string                      `json:"path"`
	Line        int                         `json:"line,omitempty"`
	Kind        string                      `json:"kind"`
	Observation string                      `json:"observation"`
	Question    string                      `json:"question"`
	Related     []staticInspectionReference `json:"related"`
}

type staticInspectionReference struct {
	Path         string `json:"path"`
	Relationship string `json:"relationship"`
	InDiff       bool   `json:"inDiff"`
}

type staticInspectionAnalysis struct {
	Truncated bool     `json:"truncated"`
	Limits    []string `json:"limits"`
}

// inspectionQuestions turns deterministic evidence into bounded prompts for a
// human reviewer. Questions deliberately ask about behavior; they never claim
// to know author intent or whether the change is correct.
func inspectionQuestions(files []staticReviewFile, flags []reviewAttentionFlag, affected []staticAffectedFile) ([]staticInspectionQuestion, staticInspectionAnalysis) {
	questions := make([]staticInspectionQuestion, 0)
	analysis := staticInspectionAnalysis{Limits: []string{}}
	seen := map[string]bool{}
	relatedLimited := false
	changed := make(map[string]staticReviewFile, len(files))
	testsByStem := map[string][]staticReviewFile{}
	for _, file := range files {
		changed[file.Path] = file
		if file.Status != "D" && isTestPath(file.Path) {
			stem := companionStem(file.Path)
			testsByStem[stem] = append(testsByStem[stem], file)
		}
	}
	for stem := range testsByStem {
		sort.Slice(testsByStem[stem], func(i, j int) bool { return testsByStem[stem][i].Path < testsByStem[stem][j].Path })
	}
	add := func(kind, file string, line int, observation, question string, related []staticInspectionReference) {
		key := kind + "\x00" + file + "\x00" + fmt.Sprint(line) + "\x00" + observation
		if seen[key] {
			return
		}
		seen[key] = true
		if len(questions) >= inspectionQuestionLimit {
			analysis.Truncated = true
			return
		}
		sum := sha256.Sum256([]byte(key))
		if related == nil {
			related = []staticInspectionReference{}
		}
		questions = append(questions, staticInspectionQuestion{
			ID:          "inspect-" + hex.EncodeToString(sum[:8]),
			Path:        file,
			Line:        line,
			Kind:        kind,
			Observation: observation,
			Question:    question,
			Related:     related,
		})
	}

	for _, flag := range flags {
		if isTestPath(flag.Path) && flag.Rule != "deleted-test" {
			continue
		}
		observation, question, ok := inspectionQuestionForAttention(flag)
		if ok {
			add(flag.Rule, flag.Path, flag.Line, observation, question, nil)
		}
	}

	for _, source := range files {
		if source.Status == "D" || isTestPath(source.Path) {
			continue
		}
		related := make([]staticInspectionReference, 0)
		for _, test := range testsByStem[companionStem(source.Path)] {
			if !nearbyCompanionTest(source.Path, test.Path) {
				continue
			}
			if len(related) >= inspectionRelatedLimit {
				relatedLimited = true
				continue
			}
			related = append(related, staticInspectionReference{Path: test.Path, Relationship: "changed companion test", InDiff: true})
		}
		if len(related) > 0 {
			sort.Slice(related, func(i, j int) bool { return related[i].Path < related[j].Path })
			add("changed-test", source.Path, 0,
				"The implementation and a nearby companion test both changed in this diff.",
				"Which changed test demonstrates the new behavior, including its failure or boundary case?", related)
		}
	}

	for _, hint := range affected {
		if !isTestPath(hint.Path) {
			continue
		}
		byChangedPath := map[string]map[string]bool{}
		for _, evidence := range hint.Evidence {
			if evidence.Kind != "test" || changed[evidence.ChangedPath].Path == "" {
				continue
			}
			if byChangedPath[evidence.ChangedPath] == nil {
				byChangedPath[evidence.ChangedPath] = map[string]bool{}
			}
			byChangedPath[evidence.ChangedPath][evidence.Detail] = true
		}
		for changedPath, detailSet := range byChangedPath {
			details := make([]string, 0, len(detailSet))
			for detail := range detailSet {
				details = append(details, detail)
			}
			sort.Strings(details)
			observation := "An unchanged test is related to this changed file: " + strings.Join(details, "; ") + "."
			add("related-test", changedPath, 0, observation,
				"Does this existing test still describe the changed behavior, and which important path remains unverified?",
				[]staticInspectionReference{{Path: hint.Path, Relationship: "unchanged related test", InDiff: false}})
		}
	}

	if analysis.Truncated {
		analysis.Limits = append(analysis.Limits, fmt.Sprintf("inspection questions limited to %d", inspectionQuestionLimit))
	}
	if relatedLimited {
		analysis.Truncated = true
		analysis.Limits = append(analysis.Limits, fmt.Sprintf("related files limited to %d per inspection question", inspectionRelatedLimit))
	}
	sort.Slice(questions, func(i, j int) bool {
		if questions[i].Path != questions[j].Path {
			return questions[i].Path < questions[j].Path
		}
		if questions[i].Line != questions[j].Line {
			return questions[i].Line < questions[j].Line
		}
		return questions[i].ID < questions[j].ID
	})
	return questions, analysis
}

func inspectionQuestionForAttention(flag reviewAttentionFlag) (string, string, bool) {
	switch flag.Rule {
	case "public-api":
		return "An added route or exported declaration was observed: " + flag.Evidence + ".",
			"Who calls this interface, what should valid and invalid inputs do, and is compatibility preserved?", true
	case "authentication":
		return "The changed path contains an authentication or authorization term: " + flag.Path + ".",
			"What access decision changes here, and which allowed and denied cases demonstrate it?", true
	case "schema":
		return "The changed path is migration, schema, or SQL-related: " + flag.Path + ".",
			"What happens to existing data during deploy, rollback, and mixed-version operation?", true
	case "configuration":
		return "A configuration-related file changed: " + flag.Path + ".",
			"Which runtime behavior changes when this value is absent, defaulted, or overridden?", true
	case "dependency":
		return "A dependency manifest or lockfile changed: " + flag.Path + ".",
			"Which runtime behavior requires this dependency change, and do the manifest and lockfile agree?", true
	case "deleted-test":
		return "A test file was deleted: " + flag.Path + ".",
			"Which behavior did this test protect, and where is equivalent coverage now visible?", true
	case "executable-mode":
		return "The executable mode changed: " + flag.Evidence + ".",
			"How should this file be invoked after the mode change, and on which supported environments?", true
	default:
		return "", "", false
	}
}

func nearbyCompanionTest(source, test string) bool {
	if companionStem(source) != companionStem(test) {
		return false
	}
	sourceDir, testDir := path.Dir(source), path.Dir(test)
	return sourceDir == testDir || strings.HasPrefix(testDir, strings.TrimSuffix(sourceDir, "/")+"/")
}
