package main

import (
	"regexp"
	"strings"
	"testing"
)

func TestArtifactRulesRejectUselessPatterns(t *testing.T) {
	for _, in := range []ruleInput{
		{Pattern: "", Message: "missing"},
		{Pattern: ".*", Message: "matches everything"},
		{Pattern: "(", Message: "invalid regex"},
		{Pattern: `\bfetch\(`, Message: "valid", Glob: strings.Repeat("a", ruleGlobMax+1)},
	} {
		if _, err := validateRule(in); err == nil {
			t.Fatalf("accepted invalid rule: %#v", in)
		}
	}
}

func TestArtifactRulesParseRepositoryPolicy(t *testing.T) {
	rules, err := parseTeamRules([]byte(`{"rules":[{"pattern":"\\bfetch\\(","glob":"*.ts","message":"Use apiClient"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].ID != "team-1" || rules[0].Source != "team" || !rules[0].Enabled {
		t.Fatalf("rules = %#v", rules)
	}
}

func TestArtifactRulesParseRichTeamPolicy(t *testing.T) {
	rules, err := parseTeamRules([]byte(`{
  "version": 1,
  "rules": [{
    "id": "no-console",
    "title": "Use the shared logger",
    "description": "Application logs need standard redaction and routing.",
    "severity": "error",
    "globs": ["src/**/*.ts", "src/**/*.tsx"],
    "match": {"kind": "text", "pattern": "console.log("},
    "message": "Replace direct console output with the shared logger."
  }]
}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 {
		t.Fatalf("rules = %#v", rules)
	}
	rule := rules[0]
	if rule.ID != "no-console" || rule.Title != "Use the shared logger" || rule.Severity != "error" {
		t.Fatalf("rule metadata = %#v", rule)
	}
	if rule.Why != "Application logs need standard redaction and routing." || len(rule.Globs) != 2 {
		t.Fatalf("rule context = %#v", rule)
	}
	if matched, _ := regexp.MatchString(rule.Pattern, "console.log(value)"); !matched {
		t.Fatalf("text matcher was not escaped correctly: %q", rule.Pattern)
	}
}

func TestArtifactRulesRejectDuplicateIDsAndUnsupportedVersions(t *testing.T) {
	for _, body := range []string{
		`{"version":2,"rules":[]}`,
		`{"version":1,"rules":[{"id":"same","pattern":"one","message":"one"},{"id":"same","pattern":"two","message":"two"}]}`,
	} {
		if _, err := parseTeamRules([]byte(body)); err == nil {
			t.Fatalf("accepted invalid policy: %s", body)
		}
	}
}
