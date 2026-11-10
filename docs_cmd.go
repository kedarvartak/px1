package main

import (
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
// touches; "px1 docs site" renders the pages as a static website. It reads
// repository files only and runs nothing from them.
func runDocs(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: px1 docs <check|gen|impact|site> [--root dir]")
	}
	fs := flag.NewFlagSet("docs "+args[0], flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", ".", "repository root")
	out := fs.String("out", "", "site: directory to write the site to, which must be empty or not exist (required); impact: optional file to write instead of stdout")
	sourceURL := fs.String("source-url", "", "site: web address of the repository tree at one commit (on GitHub, the blob address); links to code point there")
	base := fs.String("base", "", "impact: base commit or ref (required)")
	head := fs.String("head", "HEAD", "impact: head commit or ref; the pages are read from the working tree, which must be at this commit")
	asJSON := fs.Bool("json", false, "impact: write JSON instead of Markdown")
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
	return fmt.Errorf("unknown docs command %q: use check, gen, impact or site", args[0])
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
