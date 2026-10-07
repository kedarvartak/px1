package main

import (
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
