package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type ruleValidationSummary struct {
	Valid    bool   `json:"valid"`
	File     string `json:"file"`
	Rules    int    `json:"rules"`
	Info     int    `json:"info"`
	Warnings int    `json:"warnings"`
	Errors   int    `json:"errors"`
}

func runValidateRules(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("validate-rules", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	file := fs.String("file", ruleTeamFile, "rules JSON file")
	jsonOutput := fs.Bool("json", false, "write a machine-readable validation summary")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("validate-rules does not accept positional arguments")
	}
	summary, err := validateRulesPath(*file)
	if err != nil {
		return err
	}
	if *jsonOutput {
		encoded, err := json.Marshal(summary)
		if err != nil {
			return fmt.Errorf("encode validation summary: %w", err)
		}
		_, err = fmt.Fprintln(out, string(encoded))
		return err
	}
	_, err = fmt.Fprintf(out, "%s is valid: %d rules (%d error, %d warning, %d info)\n", summary.File, summary.Rules, summary.Errors, summary.Warnings, summary.Info)
	return err
}

func validateRulesPath(path string) (ruleValidationSummary, error) {
	clean, err := filepath.Abs(path)
	if err != nil {
		return ruleValidationSummary{}, fmt.Errorf("resolve rules file: %w", err)
	}
	body, err := os.ReadFile(clean)
	if err != nil {
		return ruleValidationSummary{}, fmt.Errorf("read rules file %s: %w", path, err)
	}
	rules, err := parseTeamRules(body)
	if err != nil {
		return ruleValidationSummary{}, err
	}
	summary := ruleValidationSummary{Valid: true, File: filepath.ToSlash(path), Rules: len(rules)}
	for _, rule := range rules {
		switch rule.Severity {
		case "error":
			summary.Errors++
		case "info":
			summary.Info++
		default:
			summary.Warnings++
		}
	}
	return summary, nil
}
