package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	rulePatternMax    = 300
	ruleGlobMax       = 200
	ruleMessageMax    = 400
	ruleMaxHits       = 500
	ruleMaxHitsPerKey = 20
	ruleTeamFile      = ".px1/rules.json"
)

var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

type reviewRule struct {
	ID        string     `json:"id"`
	Title     string     `json:"title,omitempty"`
	Why       string     `json:"why,omitempty"`
	Severity  string     `json:"severity,omitempty"`
	MatchKind string     `json:"matchKind,omitempty"`
	Pattern   string     `json:"pattern"`
	Glob      string     `json:"glob,omitempty"`
	Globs     []string   `json:"globs,omitempty"`
	Message   string     `json:"message"`
	Enabled   bool       `json:"enabled"`
	Source    string     `json:"source"`
	Origin    string     `json:"origin,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	Hits      int        `json:"hits"`
	LastHitAt *time.Time `json:"lastHitAt,omitempty"`
	HitKeys   []string   `json:"hitKeys,omitempty"`
}

type ruleHit struct {
	Key      string `json:"key"`
	RuleID   string `json:"ruleId"`
	Message  string `json:"message"`
	Title    string `json:"title,omitempty"`
	Why      string `json:"why,omitempty"`
	Severity string `json:"severity,omitempty"`
	Source   string `json:"source"`
	Origin   string `json:"origin,omitempty"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Text     string `json:"text"`
}

type ruleInput struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Severity    string   `json:"severity"`
	Languages   []string `json:"languages"`
	Globs       []string `json:"globs"`
	Match       struct {
		Kind    string `json:"kind"`
		Pattern string `json:"pattern"`
	} `json:"match"`
	Why     string `json:"why"`
	Pattern string `json:"pattern"`
	Glob    string `json:"glob"`
	Message string `json:"message"`
	Origin  string `json:"origin"`
}

func globRegexp(glob string) (*regexp.Regexp, error) {
	glob = strings.TrimSpace(glob)
	if glob == "" {
		return nil, nil
	}
	if !strings.Contains(glob, "/") {
		glob = "**/" + glob
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(glob); i++ {
		switch c := glob[i]; {
		case strings.HasPrefix(glob[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case strings.HasPrefix(glob[i:], "**"):
			b.WriteString(".*")
			i++
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

func validateRule(in ruleInput) (ruleInput, error) {
	in.ID = strings.TrimSpace(in.ID)
	in.Title = clip(in.Title, ruleMessageMax)
	in.Description = clip(in.Description, ruleMessageMax)
	in.Why = clip(in.Why, ruleMessageMax)
	in.Severity = strings.ToLower(strings.TrimSpace(in.Severity))
	in.Match.Kind = strings.ToLower(strings.TrimSpace(in.Match.Kind))
	in.Match.Pattern = strings.TrimSpace(in.Match.Pattern)
	if in.Match.Pattern != "" {
		if in.Pattern != "" {
			return in, errors.New("use pattern or match, not both")
		}
		in.Pattern = in.Match.Pattern
		if in.Match.Kind == "text" || in.Match.Kind == "block-text" {
			in.Pattern = regexp.QuoteMeta(in.Pattern)
		} else if in.Match.Kind != "regex" && in.Match.Kind != "block-regex" {
			return in, errors.New("match.kind must be regex, text, block-regex, or block-text")
		}
	} else if in.Match.Kind != "" {
		return in, errors.New("match.pattern is required")
	}
	in.Pattern = strings.TrimSpace(in.Pattern)
	in.Glob = strings.TrimSpace(in.Glob)
	in.Message = clip(in.Message, ruleMessageMax)
	in.Origin = clip(in.Origin, ruleGlobMax)
	if in.Pattern == "" || len(in.Pattern) > rulePatternMax {
		return in, fmt.Errorf("pattern must be between 1 and %d bytes", rulePatternMax)
	}
	re, err := regexp.Compile(in.Pattern)
	if err != nil {
		return in, fmt.Errorf("pattern: %w", err)
	}
	if re.MatchString("") {
		return in, errors.New("pattern matches every line")
	}
	if len(in.Glob) > ruleGlobMax {
		return in, fmt.Errorf("glob must be at most %d bytes", ruleGlobMax)
	}
	if _, err := globRegexp(in.Glob); err != nil {
		return in, fmt.Errorf("glob: %w", err)
	}
	if in.Message == "" {
		return in, errors.New("rule needs a message")
	}
	if in.ID != "" {
		validID := regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,79}$`)
		if !validID.MatchString(in.ID) {
			return in, errors.New("id must contain only letters, numbers, dot, underscore, or dash")
		}
	}
	if in.Severity == "" {
		in.Severity = "warning"
	}
	if in.Match.Kind == "" {
		in.Match.Kind = "regex"
	}
	if in.Severity != "info" && in.Severity != "warning" && in.Severity != "error" {
		return in, errors.New("severity must be info, warning, or error")
	}
	if in.Title == "" {
		in.Title = in.Message
	}
	if in.Why == "" {
		in.Why = in.Description
	}
	if len(in.Globs) == 0 && in.Glob != "" {
		in.Globs = []string{in.Glob}
	}
	if len(in.Globs) > 20 {
		return in, errors.New("globs must contain at most 20 entries")
	}
	for i := range in.Globs {
		in.Globs[i] = strings.TrimSpace(in.Globs[i])
		if len(in.Globs[i]) > ruleGlobMax {
			return in, fmt.Errorf("globs[%d] must be at most %d bytes", i, ruleGlobMax)
		}
		if _, err := globRegexp(in.Globs[i]); err != nil {
			return in, fmt.Errorf("globs[%d]: %w", i, err)
		}
	}
	return in, nil
}

func parseTeamRules(b []byte) ([]reviewRule, error) {
	var file struct {
		Version int         `json:"version"`
		Rules   []ruleInput `json:"rules"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		return nil, fmt.Errorf("%s: %w", ruleTeamFile, err)
	}
	if file.Version != 0 && file.Version != 1 {
		return nil, fmt.Errorf("%s: unsupported version %d", ruleTeamFile, file.Version)
	}
	out := make([]reviewRule, 0, len(file.Rules))
	seen := map[string]bool{}
	for i, in := range file.Rules {
		valid, err := validateRule(in)
		if err != nil {
			return out, fmt.Errorf("%s rule %d: %w", ruleTeamFile, i+1, err)
		}
		id := valid.ID
		if id == "" {
			id = "team-" + strconv.Itoa(i+1)
		}
		if seen[id] {
			return out, fmt.Errorf("%s rule %d: duplicate id %q", ruleTeamFile, i+1, id)
		}
		seen[id] = true
		out = append(out, reviewRule{ID: id, Title: valid.Title, Why: valid.Why, Severity: valid.Severity, MatchKind: valid.Match.Kind, Pattern: valid.Pattern, Glob: valid.Glob, Globs: valid.Globs, Message: valid.Message, Origin: valid.Origin, Enabled: true, Source: "team"})
	}
	return out, nil
}

func ruleHitKey(ruleID, path, text string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return ruleID + ":" + path + ":" + hex.EncodeToString(sum[:8])
}

func clip(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return strings.TrimSpace(s[:max]) + "…"
}
