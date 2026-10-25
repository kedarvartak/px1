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

func runValidateConfig(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("validate-config", flag.ContinueOnError)
	root := fs.String("root", ".", "repository root")
	target := fs.String("path", "README.md", "repository-relative path to resolve")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("validate-config does not accept positional arguments")
	}
	if _, err := resolveCascadingConfig(*target, map[string][]byte{}); err != nil {
		return err
	}
	files := map[string][]byte{}
	for _, candidate := range configCandidates(*target) {
		body, err := os.ReadFile(filepath.Join(*root, filepath.FromSlash(candidate)))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", candidate, err)
		}
		files[candidate] = body
	}
	resolved, err := resolveCascadingConfig(*target, files)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(resolved)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(encoded))
	return err
}
