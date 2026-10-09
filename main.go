package main

import (
	_ "embed"
	"fmt"
	"os"
	"runtime"
	"strings"
)

//go:embed VERSION
var rawVersion string

var version = strings.TrimSpace(rawVersion)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "validate-rules" {
		if err := runValidateRules(os.Args[2:], os.Stdout); err != nil {
			fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "generate-explanations" {
		if err := runGenerateExplanations(os.Args[2:]); err != nil {
			fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "export-review" {
		if err := runExportReview(os.Args[2:]); err != nil {
			fatal(err)
		}
		return
	}
	if len(os.Args) == 2 && (os.Args[1] == "version" || os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("px1 %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
		return
	}
	fmt.Fprintf(os.Stderr, "px1 %s - understand the code your agent wrote before you merge it\n\n", version)
	fmt.Fprintln(os.Stderr, "usage: px1 <export-review|generate-explanations|validate-rules> [options]")
	os.Exit(2)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "px1:", err)
	os.Exit(1)
}
