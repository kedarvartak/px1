// Package eval defines the auditable benchmark input used to gate review
// prompt and model changes.
package eval

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
)

type Dataset struct {
	Version int    `json:"version"`
	Cases   []Case `json:"cases"`
}
type Case struct {
	ID             string  `json:"id"`
	Kind           string  `json:"kind"`
	Repository     string  `json:"repository"`
	PullRequestURL string  `json:"pull_request_url"`
	BaseSHA        string  `json:"base_sha"`
	HeadSHA        string  `json:"head_sha"`
	BugFixSHA      string  `json:"bug_fix_sha,omitempty"`
	MutationID     string  `json:"mutation_id,omitempty"`
	CrossFile      bool    `json:"cross_file,omitempty"`
	Labels         []Label `json:"labels"`
}

// ValidateV2Coverage raises the seeded benchmark floor and requires cross-file
// cases to carry labels in multiple files so the designation is auditable.
func (d Dataset) ValidateV2Coverage() error {
	if err := d.Validate(); err != nil {
		return err
	}
	seeded, crossFile := 0, 0
	for _, c := range d.Cases {
		if c.Kind != "seeded" {
			continue
		}
		seeded++
		if !c.CrossFile {
			continue
		}
		paths := map[string]bool{}
		for _, label := range c.Labels {
			paths[label.Path] = true
		}
		if len(paths) < 2 {
			return fmt.Errorf("cross-file case %s requires labels in at least two files", c.ID)
		}
		crossFile++
	}
	if seeded < 100 || crossFile < 40 {
		return fmt.Errorf("v2 eval coverage is %d seeded and %d cross-file; require 100 and 40", seeded, crossFile)
	}
	return nil
}

type Label struct {
	Path      string `json:"path"`
	BugClass  string `json:"bug_class"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

func Decode(data []byte) (Dataset, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var dataset Dataset
	if err := decoder.Decode(&dataset); err != nil {
		return Dataset{}, fmt.Errorf("decode eval set: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Dataset{}, errors.New("eval set contains trailing data")
	}
	if err := dataset.Validate(); err != nil {
		return Dataset{}, err
	}
	return dataset, nil
}

func (d Dataset) Validate() error {
	if d.Version != 1 {
		return errors.New("eval set version must be 1")
	}
	seen := map[string]bool{}
	for i, c := range d.Cases {
		if c.ID == "" || seen[c.ID] {
			return fmt.Errorf("case %d has missing or duplicate ID", i)
		}
		seen[c.ID] = true
		if c.Kind != "historical" && c.Kind != "seeded" {
			return fmt.Errorf("case %s has unsupported kind", c.ID)
		}
		if !githubURL(c.Repository) || !githubURL(c.PullRequestURL) || !sha(c.BaseSHA) || !sha(c.HeadSHA) || c.BaseSHA == c.HeadSHA {
			return fmt.Errorf("case %s has invalid provenance", c.ID)
		}
		if c.Kind == "historical" && (!sha(c.BugFixSHA) || c.BugFixSHA == c.HeadSHA) {
			return fmt.Errorf("historical case %s requires a later bug-fix SHA", c.ID)
		}
		if c.Kind == "seeded" && strings.TrimSpace(c.MutationID) == "" {
			return fmt.Errorf("seeded case %s requires a mutation ID", c.ID)
		}
		if len(c.Labels) == 0 {
			return fmt.Errorf("case %s requires at least one bug label", c.ID)
		}
		for _, label := range c.Labels {
			if label.Path == "" || label.BugClass == "" || label.StartLine <= 0 || label.EndLine < label.StartLine {
				return fmt.Errorf("case %s has invalid label", c.ID)
			}
		}
	}
	return nil
}

// ValidateV1Coverage enforces the launch benchmark floor from the roadmap.
func (d Dataset) ValidateV1Coverage() error {
	if err := d.Validate(); err != nil {
		return err
	}
	historical, seeded, repos := 0, 0, map[string]bool{}
	for _, c := range d.Cases {
		repos[c.Repository] = true
		if c.Kind == "historical" {
			historical++
		} else {
			seeded++
		}
	}
	if historical < 50 || seeded < 20 || len(repos) < 10 {
		return fmt.Errorf("v1 eval coverage is %d historical, %d seeded, %d repositories; require 50, 20, 10", historical, seeded, len(repos))
	}
	return nil
}

func githubURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && strings.EqualFold(parsed.Host, "github.com") && strings.Trim(parsed.Path, "/") != ""
}
func sha(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range strings.ToLower(value) {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
