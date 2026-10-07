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
	Pattern   string     `json:"pattern"`
	Glob      string     `json:"glob,omitempty"`
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
	Key     string `json:"key"`
	RuleID  string `json:"ruleId"`
	Message string `json:"message"`
	Source  string `json:"source"`
	Origin  string `json:"origin,omitempty"`
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Text    string `json:"text"`
}

type ruleInput struct {
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
	return in, nil
}

func parseTeamRules(b []byte) ([]reviewRule, error) {
	var file struct {
		Rules []ruleInput `json:"rules"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		return nil, fmt.Errorf("%s: %w", ruleTeamFile, err)
	}
	out := make([]reviewRule, 0, len(file.Rules))
	for i, in := range file.Rules {
		valid, err := validateRule(in)
		if err != nil {
			return out, fmt.Errorf("%s rule %d: %w", ruleTeamFile, i+1, err)
		}
		out = append(out, reviewRule{ID: "team-" + strconv.Itoa(i+1), Pattern: valid.Pattern, Glob: valid.Glob, Message: valid.Message, Origin: valid.Origin, Enabled: true, Source: "team"})
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
