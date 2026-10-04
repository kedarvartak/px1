package main

import (
	"bytes"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// The static report is one self-contained HTML file: stylesheet, script,
// fonts and report JSON are all inlined, so a commit-pinned page loads with no
// network requests and keeps working offline or from an archived Pages site.
//
//go:embed static_report/report.css static_report/report.js static_report/fonts/*.woff2
var staticReportAssets embed.FS

func staticReportFontCSS() (string, error) {
	faces := []struct{ family, file, weight string }{
		{"Report Sans", "plex-sans-var.woff2", "400 600"},
		{"Report Mono", "plex-mono-400.woff2", "400"},
		{"Report Mono", "plex-mono-500.woff2", "500"},
	}
	var css strings.Builder
	for _, face := range faces {
		b, err := fs.ReadFile(staticReportAssets, "static_report/fonts/"+face.file)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&css, "@font-face{font-family:'%s';font-style:normal;font-weight:%s;font-display:swap;src:url(data:font/woff2;base64,%s) format('woff2')}\n",
			face.family, face.weight, base64.StdEncoding.EncodeToString(b))
	}
	return css.String(), nil
}

func renderStaticReviewHTML(report staticReviewReport) ([]byte, error) {
	// json.Marshal escapes <, > and & so the data cannot close its script tag.
	data, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}
	fonts, err := staticReportFontCSS()
	if err != nil {
		return nil, err
	}
	css, err := fs.ReadFile(staticReportAssets, "static_report/report.css")
	if err != nil {
		return nil, err
	}
	js, err := fs.ReadFile(staticReportAssets, "static_report/report.js")
	if err != nil {
		return nil, err
	}
	var page bytes.Buffer
	page.WriteString(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Review report</title><script>try{if(localStorage.getItem('px1:theme')==='light')document.documentElement.dataset.theme='light'}catch(e){}</script><style>
`)
	page.WriteString(fonts)
	page.Write(css)
	page.WriteString(`</style></head><body><div id="app"><p class="noscript">Loading review…</p></div>
<script type="application/json" id="report-data">`)
	page.Write(data)
	page.WriteString(`</script><script>
`)
	page.Write(js)
	page.WriteString(`</script></body></html>`)
	return page.Bytes(), nil
}

// staticAttention runs the same attention rules as the local review session
// against the content of two commits, so a published report flags what the
// live review would.
func staticAttention(root, base, head string) ([]reviewAttentionFlag, error) {
	out, err := exec.Command("git", "-C", root, "diff", "--raw", "--no-abbrev", "--find-renames", "-z", base, head, "--").Output()
	if err != nil {
		return nil, fmt.Errorf("list attention candidates: %w", err)
	}
	candidates, err := staticAttentionCandidates(root, base, head, out)
	if err != nil {
		return nil, err
	}
	flags := detectReviewAttention(candidates, nil).Flags
	if flags == nil {
		flags = []reviewAttentionFlag{}
	}
	return flags, nil
}

// staticAttentionCandidates parses `git diff --raw -z` output: a metadata
// record ":<old mode> <new mode> <old sha> <new sha> <status>" followed by one
// path, or two for renames and copies.
func staticAttentionCandidates(root, base, head string, raw []byte) ([]attentionCandidate, error) {
	fields := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	var candidates []attentionCandidate
	var err error
	remaining := int64(attentionMaxContentBytes)
	for i := 0; i < len(fields) && len(candidates) < attentionMaxFiles; {
		meta := strings.Fields(strings.TrimPrefix(fields[i], ":"))
		if len(meta) < 5 {
			return nil, errors.New("unexpected git diff --raw output")
		}
		status := meta[4][:1]
		oldPath, newPath := "", ""
		if status == "R" || status == "C" {
			if i+2 >= len(fields) {
				return nil, errors.New("unexpected git diff --raw output")
			}
			oldPath, newPath = fields[i+1], fields[i+2]
			i += 3
		} else {
			if i+1 >= len(fields) {
				return nil, errors.New("unexpected git diff --raw output")
			}
			oldPath, newPath = fields[i+1], fields[i+1]
			i += 2
		}
		deleted := status == "D"
		path := newPath
		if deleted {
			path = oldPath
		}
		candidate := attentionCandidate{
			Path:         filepath.ToSlash(path),
			Deleted:      deleted,
			BaselineMode: gitRawMode(meta[0]),
			CurrentMode:  gitRawMode(meta[1]),
		}
		available := true
		if status != "A" {
			var ok bool
			if candidate.Baseline, ok, err = attentionBlob(root, base, oldPath, &remaining); err != nil {
				return nil, err
			}
			available = available && ok
		}
		if !deleted {
			var ok bool
			if candidate.Current, ok, err = attentionBlob(root, head, newPath, &remaining); err != nil {
				return nil, err
			}
			available = available && ok
		}
		candidate.ContentAvailable = available && !containsNUL(candidate.Baseline) && !containsNUL(candidate.Current)
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Path < candidates[j].Path })
	return candidates, nil
}

// attentionBlob reads one file at a commit unless it is larger than the live
// review would read, or would push the total past the content budget. Skipped
// files still get the path-based rules; they only lose the content-based ones.
func attentionBlob(root, rev, path string, remaining *int64) ([]byte, bool, error) {
	out, err := exec.Command("git", "-C", root, "cat-file", "-s", rev+":"+filepath.ToSlash(path)).Output()
	if err != nil {
		return nil, false, fmt.Errorf("size of %s at %s: %w", path, shortRevision(rev), err)
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return nil, false, err
	}
	if size > attentionMaxFileBytes || size > *remaining {
		return nil, false, nil
	}
	b, found, err := gitFileAtCommit(root, rev, path)
	if err != nil || !found {
		return nil, false, err
	}
	*remaining -= size
	return b, true, nil
}

func gitRawMode(octal string) fs.FileMode {
	n, err := strconv.ParseUint(octal, 8, 32)
	if err != nil {
		return 0
	}
	return fs.FileMode(n & 0o777)
}
