package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestInspectionQuestionsSeparateObservedEvidenceFromReviewerQuestions(t *testing.T) {
	files := []staticReviewFile{
		{Path: "src/service.ts", Status: "M"},
		{Path: "src/service.test.ts", Status: "M"},
		{Path: "src/auth/session.ts", Status: "M"},
	}
	flags := []reviewAttentionFlag{
		{Rule: "public-api", Path: "src/service.ts", Line: 12, Evidence: "export function createOrder()"},
		{Rule: "authentication", Path: "src/auth/session.ts", Evidence: "src/auth/session.ts"},
		{Rule: "large-diff", Path: "src/service.ts", Evidence: "500 added lines"},
	}
	questions, analysis := inspectionQuestions(files, flags)
	if analysis.Truncated || len(questions) != 3 {
		t.Fatalf("questions = %#v, analysis = %#v", questions, analysis)
	}
	for _, question := range questions {
		if question.ID == "" || question.Observation == "" || !strings.HasSuffix(question.Question, "?") {
			t.Fatalf("incomplete question: %#v", question)
		}
		if question.Kind == "large-diff" {
			t.Fatalf("large diffs should not become behavioral claims: %#v", question)
		}
	}
	var testQuestion staticInspectionQuestion
	for _, question := range questions {
		if question.Kind == "changed-test" {
			testQuestion = question
		}
	}
	if len(testQuestion.Related) != 1 || testQuestion.Related[0].Path != "src/service.test.ts" || !testQuestion.Related[0].InDiff {
		t.Fatalf("changed test references = %#v", testQuestion.Related)
	}
}

func TestInspectionQuestionsExposeOutputLimit(t *testing.T) {
	flags := make([]reviewAttentionFlag, inspectionQuestionLimit+1)
	files := make([]staticReviewFile, inspectionQuestionLimit+1)
	for i := range flags {
		file := fmt.Sprintf("config/%03d.json", i)
		files[i] = staticReviewFile{Path: file, Status: "M"}
		flags[i] = reviewAttentionFlag{Rule: "configuration", Path: file, Evidence: file}
	}
	questions, analysis := inspectionQuestions(files, flags)
	if len(questions) != inspectionQuestionLimit || !analysis.Truncated || len(analysis.Limits) != 1 {
		t.Fatalf("questions = %d, analysis = %#v", len(questions), analysis)
	}
}

func TestInspectionQuestionsBoundRelatedChangedTests(t *testing.T) {
	files := []staticReviewFile{{Path: "src/service.ts", Status: "M"}}
	for i := 0; i < inspectionRelatedLimit+2; i++ {
		files = append(files, staticReviewFile{Path: fmt.Sprintf("src/tests/%02d/service.test.ts", i), Status: "M"})
	}
	questions, analysis := inspectionQuestions(files, nil)
	if len(questions) != 1 || len(questions[0].Related) != inspectionRelatedLimit {
		t.Fatalf("questions = %#v", questions)
	}
	if !analysis.Truncated || len(analysis.Limits) != 1 || !strings.Contains(analysis.Limits[0], "related files") {
		t.Fatalf("analysis = %#v", analysis)
	}
}
