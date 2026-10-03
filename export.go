package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// staticReviewReport is the portable, read-only representation consumed by
// the generated report. It deliberately contains commit identities so a
// published page cannot silently drift when a pull request receives another
// commit.
type staticReviewReport struct {
	Version      int                  `json:"version"`
	Repository   string               `json:"repository"`
	Base         string               `json:"base"`
	Head         string               `json:"head"`
	GeneratedAt  time.Time            `json:"generatedAt"`
	Files        []staticReviewFile   `json:"files"`
	RuleHits     []staticReviewHit    `json:"ruleHits"`
	Explanations []staticExplanation  `json:"explanations"`
	Verification verificationResponse `json:"verification"`
}

type staticReviewFile struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Diff   string `json:"diff"`
}

type staticReviewHit struct {
	Key     string                    `json:"key"`
	RuleID  string                    `json:"ruleId"`
	Message string                    `json:"message"`
	Source  string                    `json:"source"`
	Origin  string                    `json:"origin,omitempty"`
	Path    string                    `json:"path"`
	Line    int                       `json:"line"`
	Text    string                    `json:"text"`
	Context []staticReviewContextLine `json:"context,omitempty"`
}

type staticReviewContextLine struct {
	Number int    `json:"number"`
	Kind   string `json:"kind"` // added, context, or removed
	Text   string `json:"text"`
}

type staticReviewContextBlock struct {
	Lines []staticReviewContextLine
}

func runExportReview(args []string) error {
	fs := flag.NewFlagSet("export-review", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	base := fs.String("base", "", "base commit or ref (required)")
	head := fs.String("head", "HEAD", "head commit or ref")
	root := fs.String("root", ".", "repository root")
	out := fs.String("out", ".px1-review", "directory to write the static report")
	verificationFile := fs.String("verification-file", "", "optional verification JSON file to validate for the head commit")
	explanationsFile := fs.String("explanations-file", "", "optional AI explanation JSON file to validate for the head commit")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: px1 export-review --base <commit> [--head <commit>] [--root <repo>] [--verification-file <path>] [--explanations-file <path>] [--out <dir>]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*base) == "" {
		return errors.New("export-review requires --base")
	}

	repoRoot, err := filepath.Abs(*root)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}
	outDir, err := filepath.Abs(*out)
	if err != nil {
		return fmt.Errorf("resolve output: %w", err)
	}
	report, err := generateStaticReviewWithOptions(repoRoot, *base, *head, staticReviewOptions{VerificationFile: *verificationFile, ExplanationsFile: *explanationsFile})
	if err != nil {
		return err
	}
	if err := writeStaticReviewReport(outDir, report); err != nil {
		return err
	}
	fmt.Printf("static review report written to %s\n", filepath.Join(outDir, "index.html"))
	return nil
}

func generateStaticReview(root, base, head string) (staticReviewReport, error) {
	return generateStaticReviewWithOptions(root, base, head, staticReviewOptions{})
}

func generateStaticReviewWithVerification(root, base, head, verificationFile string) (staticReviewReport, error) {
	return generateStaticReviewWithOptions(root, base, head, staticReviewOptions{VerificationFile: verificationFile})
}

type staticReviewOptions struct {
	VerificationFile string
	ExplanationsFile string
}

func generateStaticReviewWithOptions(root, base, head string, options staticReviewOptions) (staticReviewReport, error) {
	baseSHA, err := resolveCommit(root, base)
	if err != nil {
		return staticReviewReport{}, err
	}
	headSHA, err := resolveCommit(root, head)
	if err != nil {
		return staticReviewReport{}, err
	}
	diff, err := gitDiffBetween(root, baseSHA, headSHA)
	if err != nil {
		return staticReviewReport{}, err
	}
	files, err := parseStaticReviewFiles(root, baseSHA, headSHA, diff)
	if err != nil {
		return staticReviewReport{}, err
	}
	rules, err := teamRulesAtCommit(root, headSHA)
	if err != nil {
		return staticReviewReport{}, err
	}
	hits, err := matchStaticRuleAdditions(rules, addedLinesByPath(diff))
	if err != nil {
		return staticReviewReport{}, err
	}
	staticHits := addStaticReviewContexts(hits, diff)
	verification, err := verificationAtCommit(root, headSHA)
	if options.VerificationFile != "" {
		verification, err = verificationAtFile(options.VerificationFile, headSHA)
	}
	if err != nil {
		return staticReviewReport{}, err
	}
	changed := make(map[string]bool, len(files))
	for _, file := range files {
		changed[file.Path] = true
	}
	explanations := []staticExplanation{}
	if options.ExplanationsFile != "" {
		explanations, err = explanationsAtFile(options.ExplanationsFile, headSHA, changed)
	}
	if err != nil {
		return staticReviewReport{}, err
	}
	return staticReviewReport{
		Version:      1,
		Repository:   filepath.Base(root),
		Base:         baseSHA,
		Head:         headSHA,
		GeneratedAt:  time.Now().UTC(),
		Files:        files,
		RuleHits:     staticHits,
		Explanations: explanations,
		Verification: verification,
	}, nil
}

func gitDiffBetween(root, base, head string) (string, error) {
	cmd := exec.Command("git", "-C", root, "diff", "--no-color", "--no-ext-diff", "--unified=3", base, head, "--")
	out, err := cmd.Output()
	if err == nil {
		return string(out), nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		// git diff uses exit status 1 to mean that differences exist when
		// --exit-code is enabled; tolerate it defensively for wrappers too.
		return string(out), nil
	}
	return "", fmt.Errorf("git diff %s..%s: %w", shortRevision(base), shortRevision(head), err)
}

func gitFileAtCommit(root, commit, rel string) ([]byte, bool, error) {
	out, err := exec.Command("git", "-C", root, "show", commit+":"+filepath.ToSlash(rel)).Output()
	if err == nil {
		return out, true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 128 {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("read %s at %s: %w", rel, shortRevision(commit), err)
}

func teamRulesAtCommit(root, head string) ([]reviewRule, error) {
	b, found, err := gitFileAtCommit(root, head, ruleTeamFile)
	if err != nil || !found {
		return nil, err
	}
	return parseTeamRules(b)
}

func verificationAtCommit(root, head string) (verificationResponse, error) {
	b, found, err := gitFileAtCommit(root, head, ".px1/verification.json")
	if err != nil {
		return verificationResponse{}, err
	}
	if !found {
		return verificationResponse{Available: false, Checks: []verificationCheck{}}, nil
	}
	return verificationFromBytes(b, head, ".px1/verification.json")
}

func verificationAtFile(path, head string) (verificationResponse, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return verificationResponse{}, fmt.Errorf("read verification file %s: %w", path, err)
	}
	return verificationFromBytes(b, head, filepath.ToSlash(path))
}

func verificationFromBytes(b []byte, head, label string) (verificationResponse, error) {
	var report verificationReport
	if err := json.Unmarshal(b, &report); err != nil {
		return verificationResponse{Available: false, Checks: []verificationCheck{}, Error: fmt.Sprintf("invalid %s: %v", label, err)}, nil
	}
	if err := validateVerificationReport(report, head); err != nil {
		return verificationResponse{Available: false, Source: report.Source, Revision: report.Revision, URL: report.URL, Checks: []verificationCheck{}, Error: err.Error()}, nil
	}
	return verificationResponse{Available: true, Source: report.Source, Revision: report.Revision, URL: report.URL, Checks: nonNilChecks(report.Checks)}, nil
}

func parseStaticReviewFiles(root, base, head, diff string) ([]staticReviewFile, error) {
	statusOut, err := exec.Command("git", "-C", root, "diff", "--name-status", "--find-renames", base, head, "--").Output()
	if err != nil {
		return nil, fmt.Errorf("list changed files: %w", err)
	}
	statuses := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(statusOut)), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 2 || fields[0] == "" {
			continue
		}
		status := string(fields[0][0])
		path := fields[len(fields)-1]
		statuses[filepath.ToSlash(path)] = status
	}

	diffs := map[string]string{}
	for _, block := range strings.Split(diff, "diff --git ") {
		if strings.TrimSpace(block) == "" {
			continue
		}
		block = "diff --git " + block
		path := staticDiffPath(block)
		if path != "" {
			diffs[path] = block
			if _, ok := statuses[path]; !ok {
				statuses[path] = staticDiffStatus(block)
			}
		}
	}
	paths := make([]string, 0, len(statuses))
	for p := range statuses {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	files := make([]staticReviewFile, 0, len(paths))
	for _, p := range paths {
		files = append(files, staticReviewFile{Path: p, Status: statuses[p], Diff: diffs[p]})
	}
	return files, nil
}

func staticDiffPath(diff string) string {
	var deleted string
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ b/"):
			return filepath.ToSlash(strings.TrimPrefix(line, "+++ b/"))
		case strings.HasPrefix(line, "--- a/"):
			deleted = filepath.ToSlash(strings.TrimPrefix(line, "--- a/"))
		}
	}
	return deleted
}

func staticDiffStatus(diff string) string {
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "new file mode") {
			return "A"
		}
		if strings.HasPrefix(line, "deleted file mode") {
			return "D"
		}
	}
	return "M"
}

func addedLinesByPath(diff string) map[string]map[int]string {
	out := map[string]map[int]string{}
	for _, block := range strings.Split(diff, "diff --git ") {
		if strings.TrimSpace(block) == "" {
			continue
		}
		block = "diff --git " + block
		file := staticDiffPath(block)
		if file == "" || strings.HasPrefix(file, ".px1/") {
			continue
		}
		added := map[int]string{}
		line := 0
		for _, raw := range strings.Split(block, "\n") {
			if m := hunkHeader.FindStringSubmatch(raw); m != nil {
				line, _ = strconv.Atoi(m[1])
				continue
			}
			if line == 0 || raw == "" || strings.HasPrefix(raw, `\\`) || strings.HasPrefix(raw, "+++") || strings.HasPrefix(raw, "---") {
				continue
			}
			switch raw[0] {
			case '+':
				added[line] = raw[1:]
				line++
			case ' ':
				line++
			}
		}
		out[file] = added
	}
	return out
}

func addStaticReviewContexts(hits []ruleHit, diff string) []staticReviewHit {
	blocks := staticContextBlocksByPath(diff)
	out := make([]staticReviewHit, 0, len(hits))
	for _, hit := range hits {
		staticHit := staticReviewHit{
			Key:     hit.Key,
			RuleID:  hit.RuleID,
			Message: hit.Message,
			Source:  hit.Source,
			Origin:  hit.Origin,
			Path:    hit.Path,
			Line:    hit.Line,
			Text:    hit.Text,
		}
		for _, block := range blocks[hit.Path] {
			target := -1
			for i, line := range block.Lines {
				if line.Kind == "added" && line.Number == hit.Line {
					target = i
					break
				}
			}
			if target < 0 {
				continue
			}
			start, end := target-2, target+3
			if start < 0 {
				start = 0
			}
			if end > len(block.Lines) {
				end = len(block.Lines)
			}
			staticHit.Context = append(staticHit.Context, block.Lines[start:end]...)
			break
		}
		out = append(out, staticHit)
	}
	return out
}

func staticContextBlocksByPath(diff string) map[string][]staticReviewContextBlock {
	out := map[string][]staticReviewContextBlock{}
	for _, rawBlock := range strings.Split(diff, "diff --git ") {
		if strings.TrimSpace(rawBlock) == "" {
			continue
		}
		block := "diff --git " + rawBlock
		file := staticDiffPath(block)
		if file == "" || strings.HasPrefix(file, ".px1/") {
			continue
		}
		lineNumber := 0
		var lines []staticReviewContextLine
		flush := func() {
			if len(lines) > 0 {
				out[file] = append(out[file], staticReviewContextBlock{Lines: lines})
				lines = nil
			}
		}
		for _, raw := range strings.Split(block, "\n") {
			if m := hunkHeader.FindStringSubmatch(raw); m != nil {
				flush()
				lineNumber, _ = strconv.Atoi(m[1])
				continue
			}
			if lineNumber == 0 || raw == "" || strings.HasPrefix(raw, `\`) {
				continue
			}
			switch raw[0] {
			case '+':
				if strings.HasPrefix(raw, "+++") {
					continue
				}
				lines = append(lines, staticReviewContextLine{Number: lineNumber, Kind: "added", Text: raw[1:]})
				lineNumber++
			case '-':
				if strings.HasPrefix(raw, "---") {
					continue
				}
				lines = append(lines, staticReviewContextLine{Kind: "removed", Text: raw[1:]})
			case ' ':
				lines = append(lines, staticReviewContextLine{Number: lineNumber, Kind: "context", Text: raw[1:]})
				lineNumber++
			}
		}
		flush()
	}
	return out
}

func matchStaticRuleAdditions(rules []reviewRule, additions map[string]map[int]string) ([]ruleHit, error) {
	type compiledRule struct {
		rule reviewRule
		re   *regexp.Regexp
		glob *regexp.Regexp
	}
	active := make([]compiledRule, 0, len(rules))
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		re, err := regexp.Compile(rule.Pattern)
		if err != nil {
			continue
		}
		glob, err := globRegexp(rule.Glob)
		if err != nil {
			continue
		}
		active = append(active, compiledRule{rule: rule, re: re, glob: glob})
	}
	paths := make([]string, 0, len(additions))
	for p := range additions {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	hits := make([]ruleHit, 0)
	for _, p := range paths {
		for _, c := range active {
			if c.glob != nil && !c.glob.MatchString(p) && !c.glob.MatchString(filepath.Base(p)) {
				continue
			}
			lines := make([]int, 0, len(additions[p]))
			for line := range additions[p] {
				lines = append(lines, line)
			}
			sort.Ints(lines)
			perKey := map[string]int{}
			for _, line := range lines {
				text := additions[p][line]
				if !c.re.MatchString(text) {
					continue
				}
				key := ruleHitKey(c.rule.ID, p, text)
				if perKey[key] >= ruleMaxHitsPerKey {
					continue
				}
				perKey[key]++
				hits = append(hits, ruleHit{Key: key, RuleID: c.rule.ID, Message: c.rule.Message, Source: c.rule.Source, Origin: c.rule.Origin, Path: p, Line: line, Text: clip(text, 300)})
				if len(hits) >= ruleMaxHits {
					return hits, nil
				}
			}
		}
	}
	return hits, nil
}

func renderStaticReviewHTML(report staticReviewReport) ([]byte, error) {
	data, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}
	const pagePrefix = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>px1 static review</title><style>
:root{color-scheme:dark;--bg:#101318;--panel:#171b22;--border:#2b3340;--text:#e8edf3;--muted:#9aa6b2;--accent:#7dd3fc;--bad:#fca5a5;--good:#86efac}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text);font:15px/1.5 system-ui,sans-serif}main{max-width:1100px;margin:0 auto;padding:32px 20px}h1,h2{line-height:1.2}h1{margin:0 0 8px}h2{margin:28px 0 12px;font-size:20px}.meta,.card{background:var(--panel);border:1px solid var(--border);border-radius:10px;padding:16px}.meta{color:var(--muted)}.meta code{color:var(--accent);word-break:break-all}.stats{display:flex;gap:10px;flex-wrap:wrap;margin:18px 0}.stat{background:var(--panel);border:1px solid var(--border);border-radius:8px;padding:10px 14px}.stat strong{display:block;font-size:20px}.finding{border-left:3px solid var(--bad);margin:10px 0;padding:10px 12px;background:var(--panel)}.finding code{color:var(--accent)}.finding pre{margin:10px 0 0;border:1px solid var(--border)}.explanation{margin:10px 0;padding:12px;background:var(--panel);border:1px solid var(--border);border-radius:8px}.chip{color:var(--accent);border-color:#31536a;background:#102331;font-weight:600}.explanation p{margin:10px 0 0}.muted{color:var(--muted)}.ok{color:var(--good)}.bad{color:var(--bad)}details{background:var(--panel);border:1px solid var(--border);border-radius:8px;margin:10px 0}summary{cursor:pointer;padding:12px;font-weight:600}pre{overflow:auto;margin:0;padding:14px;background:#0b0d10;color:#d5dee8;font:13px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace}button{background:none;border:1px solid var(--border);color:var(--text);border-radius:5px;padding:3px 8px;cursor:pointer}
</style></head><body><main id="app"><p class="muted">Loading review…</p></main>
<script type="application/json" id="report-data">`
	const pageSuffix = `</script><script>
const report=JSON.parse(document.getElementById('report-data').textContent), app=document.getElementById('app');
const esc=(value)=>String(value??''); const text=(tag,value,cls)=>{const n=document.createElement(tag);n.textContent=esc(value);if(cls)n.className=cls;return n};
const code=(value)=>text('code',value); const section=(title)=>{const n=text('h2',title);app.append(n);return n};
const header=text('h1','px1 static review');app.append(header);const repo=text('div',report.repository,'muted');app.append(repo);
const meta=text('div');meta.className='meta';meta.append(text('div','Base: '),code(report.base),text('div','Head: '),code(report.head),text('div','Generated: '+report.generatedAt));app.append(meta);
const stats=text('div');stats.className='stats';for(const [label,value] of [['Changed files',report.files.length],['Rule findings',report.ruleHits.length],['AI explanations',report.explanations.length],['CI checks',report.verification.checks.length]]){const s=text('div');s.className='stat';s.append(text('strong',value),text('span',label,'muted'));stats.append(s)}app.append(stats);
section('Team-rule findings');if(!report.ruleHits.length)app.append(text('p','No team-rule findings on added lines.','ok'));for(const hit of report.ruleHits){const n=text('div');n.className='finding';n.append(text('div',hit.message),text('div',hit.path+':'+hit.line+' — '),code(hit.text));if(hit.context?.length){const lines=hit.context.map((line)=>{const marker=line.kind==='added'?'+':line.kind==='removed'?'-':' ';const number=line.number?String(line.number).padStart(4,' '):'    ';return marker+' '+number+' | '+line.text}).join('\n');n.append(text('pre',lines));}app.append(n)}
section('AI explanations');if(!report.explanations.length)app.append(text('p','No AI explanations were supplied for this commit.','muted'));for(const item of report.explanations){const n=text('div');n.className='explanation';const chip=text('button',item.title,'chip');chip.type='button';chip.setAttribute('aria-expanded','false');const where=text('span',' '+item.path+':'+item.lineStart+(item.lineEnd!==item.lineStart?'-'+item.lineEnd:''),'muted');const detail=text('p',item.summary);detail.hidden=true;chip.addEventListener('click',()=>{detail.hidden=!detail.hidden;chip.setAttribute('aria-expanded',String(!detail.hidden))});n.append(chip,where,detail);app.append(n)}
section('Verification');const v=text('div');v.className='card';if(report.verification.available){v.append(text('div','Verified by '+report.verification.source+' for '+report.verification.revision,'ok'));for(const check of report.verification.checks){const row=text('div',check.name+': '+check.status);if(check.url){row.append(text('span',' '),code(check.url))}v.append(row)}}else{v.append(text('div',report.verification.error||'No verification report is attached.','muted'))}app.append(v);
section('Changed files');if(!report.files.length)app.append(text('p','No changed files.','muted'));for(const file of report.files){const d=document.createElement('details'),s=text('summary',file.status+' '+file.path);d.append(s);if(file.diff){const pre=text('pre',file.diff);d.append(pre)}else d.append(text('p','No textual diff available.','muted'));app.append(d)}
</script></body></html>`
	return []byte(pagePrefix + string(data) + pageSuffix), nil
}

func writeStaticReviewReport(outDir string, report staticReviewReport) error {
	b, err := renderStaticReviewHTML(report)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "index.html"), b, 0o644); err != nil {
		return fmt.Errorf("write static report: %w", err)
	}
	return nil
}
