package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"px1/internal/docs"
)

// runDocs maintains the documentation under docs/. "px1 docs gen" writes the
// reference pages, the manifest and the index from the code and the pages;
// "px1 docs check" fails when they are out of date, a link is broken, or code
// has no page; "px1 docs impact" lists the pages whose covered code a diff
// touches; "px1 docs apply" applies edits to those pages when each one stays
// inside a section and quotes the code it rests on; "px1 docs site" renders
// the pages as a static website. It reads repository files only and runs
// nothing from them.
func runDocs(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: px1 docs <apply|check|gen|impact|site> [--root dir]")
	}
	fs := flag.NewFlagSet("docs "+args[0], flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", ".", "repository root")
	out := fs.String("out", "", "site: directory to write the site to, which must be empty or not exist (required); impact: optional file to write instead of stdout")
	sourceURL := fs.String("source-url", "", "site, apply: web address of the repository tree at one commit (on GitHub, the blob address); links to code point there")
	base := fs.String("base", "", "impact, apply: base commit or ref (required)")
	editsFile := fs.String("edits", "", "apply: JSON file holding a list of edits (required)")
	dryRun := fs.Bool("dry-run", false, "apply: check the edits and report, but write nothing")
	head := fs.String("head", "HEAD", "impact: head commit or ref; the pages are read from the working tree, which must be at this commit")
	asJSON := fs.Bool("json", false, "impact, apply: write JSON instead of Markdown")
	impactFile := fs.String("impact", "", "site: impact JSON written by docs impact --json; marks the pages a pull request affects")
	commit := fs.String("commit", "", "site: the commit the site is built from, shown on each page")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	switch args[0] {
	case "check":
		problems, err := docs.Check(*root)
		if err != nil {
			return err
		}
		for _, problem := range problems {
			fmt.Fprintln(stdout, problem)
		}
		if len(problems) > 0 {
			return fmt.Errorf("docs check found %d problem(s)", len(problems))
		}
		fmt.Fprintln(stdout, "docs check: ok")
		return nil
	case "gen":
		changed, err := docs.WriteGenerated(*root)
		if err != nil {
			return err
		}
		for _, rel := range changed {
			fmt.Fprintln(stdout, "wrote", rel)
		}
		if len(changed) == 0 {
			fmt.Fprintln(stdout, "docs gen: up to date")
		}
		return nil
	case "impact":
		impact, err := docsImpact(*root, *base, *head)
		if err != nil {
			return err
		}
		text := impact.Markdown()
		if *asJSON {
			encoded, err := json.MarshalIndent(impact, "", "  ")
			if err != nil {
				return err
			}
			text = string(encoded) + "\n"
		}
		if *out != "" {
			return os.WriteFile(*out, []byte(text), 0o644)
		}
		_, err = io.WriteString(stdout, text)
		return err
	case "apply":
		if *editsFile == "" {
			return errors.New("docs apply requires --edits")
		}
		data, err := os.ReadFile(*editsFile)
		if err != nil {
			return err
		}
		var edits []docs.Edit
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&edits); err != nil {
			return fmt.Errorf("%s: %w", *editsFile, err)
		}
		impact, err := docsImpact(*root, *base, *head)
		if err != nil {
			return err
		}
		// Quotes are checked against the files on disk, so the disk must
		// hold the commit the impact was computed for.
		if err := docsTreeAtHead(*root, impact.Head); err != nil {
			return err
		}
		results, err := applyDocsEdits(*root, impact, edits, *dryRun)
		if err != nil {
			return err
		}
		text := docs.EditsMarkdown(results, *sourceURL)
		if *asJSON {
			encoded, err := json.MarshalIndent(results, "", "  ")
			if err != nil {
				return err
			}
			text = string(encoded) + "\n"
		}
		if _, err := io.WriteString(stdout, text); err != nil {
			return err
		}
		refused := 0
		for _, result := range results {
			if !result.Applied {
				refused++
			}
		}
		if refused > 0 {
			return fmt.Errorf("%d of %d edit(s) refused", refused, len(results))
		}
		return nil
	case "site":
		if *out == "" {
			return errors.New("docs site requires --out")
		}
		fonts := map[string][]byte{}
		entries, err := staticReportAssets.ReadDir("static_report/fonts")
		if err != nil {
			return err
		}
		for _, entry := range entries {
			data, err := staticReportAssets.ReadFile("static_report/fonts/" + entry.Name())
			if err != nil {
				return err
			}
			fonts[entry.Name()] = data
		}
		opts := docs.SiteOptions{SourceURL: *sourceURL, Commit: *commit, Fonts: fonts}
		if *impactFile != "" {
			data, err := os.ReadFile(*impactFile)
			if err != nil {
				return err
			}
			opts.Impact = &docs.Impact{}
			if err := json.Unmarshal(data, opts.Impact); err != nil {
				return fmt.Errorf("%s: %w", *impactFile, err)
			}
		}
		n, err := docs.WriteSite(*root, *out, opts)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "wrote %d files to %s\n", n, *out)
		return nil
	}
	return fmt.Errorf("unknown docs command %q: use apply, check, gen, impact or site", args[0])
}

// applyDocsEdits applies the edits, or only plans them on a dry run.
func applyDocsEdits(root string, impact docs.Impact, edits []docs.Edit, dryRun bool) ([]docs.EditResult, error) {
	if dryRun {
		results, _, err := docs.PlanEdits(root, impact, edits)
		return results, err
	}
	return docs.ApplyEdits(root, impact, edits)
}

// docsTreeAtHead returns an error unless the working tree is the head
// commit exactly: checked out there, with no tracked file modified, staged
// or deleted. Untracked files are allowed; they are in no commit and no page
// covers them in the impact.
func docsTreeAtHead(root, head string) error {
	if current, err := resolveCommit(root, "HEAD"); err != nil || current != head {
		return errors.New("the working tree is read and edited, so it must be checked out at --head")
	}
	out, err := exec.Command("git", "-C", root, "-c", "core.fsmonitor=false", "status", "--porcelain", "--untracked-files=no").Output()
	if err != nil {
		return fmt.Errorf("git status failed: %w", err)
	}
	if len(strings.TrimSpace(string(out))) > 0 {
		return errors.New("the working tree has uncommitted changes to tracked files; commit or stash them first, so that quotes are checked against the head commit")
	}
	return nil
}

// docsImpact asks git which files differ between two commits and maps them to
// the pages in the working tree. Git only reads: no hooks, filters or
// external diff programs run for a name-only diff with these settings.
func docsImpact(root, base, head string) (docs.Impact, error) {
	if base == "" {
		return docs.Impact{}, errors.New("docs impact requires --base")
	}
	baseSHA, err := resolveCommit(root, base)
	if err != nil {
		return docs.Impact{}, err
	}
	headSHA, err := resolveCommit(root, head)
	if err != nil {
		return docs.Impact{}, err
	}
	out, err := exec.Command("git", "-C", root, "-c", "core.fsmonitor=false", "diff", "--name-only", "--no-renames", "--no-ext-diff", "-z", baseSHA, headSHA, "--").Output()
	if err != nil {
		return docs.Impact{}, fmt.Errorf("git diff failed: %w", err)
	}
	var changed []string
	for _, file := range strings.Split(string(out), "\x00") {
		if file != "" {
			changed = append(changed, file)
		}
	}
	pages, problems, err := docs.LoadPages(root)
	if err != nil {
		return docs.Impact{}, err
	}
	if len(problems) > 0 {
		return docs.Impact{}, fmt.Errorf("%s (and %d more): run px1 docs check", problems[0], len(problems)-1)
	}
	impact := docs.ComputeImpact(pages, changed)
	impact.Base, impact.Head = baseSHA, headSHA
	return impact, nil
}
