package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"px1/internal/docs"
)

// runDocs maintains the documentation under docs/. "px1 docs gen" writes the
// reference pages, the manifest and the index from the code and the pages;
// "px1 docs check" fails when they are out of date, a link is broken, or code
// has no page; "px1 docs site" renders the pages as a static website. It reads
// repository files only and runs nothing from them.
func runDocs(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: px1 docs <check|gen|site> [--root dir]")
	}
	fs := flag.NewFlagSet("docs "+args[0], flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", ".", "repository root")
	out := fs.String("out", "", "site: directory to write the site to; it must be empty or not exist (required)")
	sourceURL := fs.String("source-url", "", "site: web address of the repository tree at one commit (on GitHub, the blob address); links to code point there")
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
		n, err := docs.WriteSite(*root, *out, docs.SiteOptions{SourceURL: *sourceURL, Commit: *commit, Fonts: fonts})
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "wrote %d files to %s\n", n, *out)
		return nil
	}
	return fmt.Errorf("unknown docs command %q: use check, gen or site", args[0])
}
