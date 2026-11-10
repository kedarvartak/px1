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
// has no page. It reads files only.
func runDocs(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: px1 docs <check|gen> [--root dir]")
	}
	fs := flag.NewFlagSet("docs "+args[0], flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", ".", "repository root")
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
	}
	return fmt.Errorf("unknown docs command %q: use check or gen", args[0])
}
