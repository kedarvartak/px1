package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateRulesCommandReportsPolicySummary(t *testing.T) {
	file := filepath.Join(t.TempDir(), "rules.json")
	body := `{"version":1,"rules":[{"id":"no-console","pattern":"console\\.log","message":"Use logger","severity":"error"},{"pattern":"TODO","message":"Resolve TODO"}]}`
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runValidateRules([]string{"--file", file, "--json"}, &out); err != nil {
		t.Fatal(err)
	}
	var summary ruleValidationSummary
	if err := json.Unmarshal(out.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if !summary.Valid || summary.Rules != 2 || summary.Errors != 1 || summary.Warnings != 1 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestValidateRulesCommandRejectsBrokenPolicy(t *testing.T) {
	file := filepath.Join(t.TempDir(), "rules.json")
	if err := os.WriteFile(file, []byte(`{"rules":[{"pattern":"(","message":"broken"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := runValidateRules([]string{"--file", file}, &out)
	if err == nil || !strings.Contains(err.Error(), "pattern") || out.Len() != 0 {
		t.Fatalf("error = %v, output = %q", err, out.String())
	}
}
