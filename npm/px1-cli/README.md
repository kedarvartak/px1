# px1

px1 is a local-first review control plane for AI-generated code. Teams encode
their own standards in `.px1/rules.json`; px1 evaluates changed lines, shows
contextual findings and diffs, and can export a commit-pinned report for a
GitHub pull request. The core runs locally without an account, hosted service,
or LLM API key.

Optional AI providers can explain a finding, but the reviewer remains the
decision-maker and the evidence stays tied to the exact commit.

```sh
npx px1-cli            # review the current directory
npm install -g px1-cli # then: px1 /path/to/repo
```

The command is `px1`. It serves a local web UI and opens it in your browser;
nothing leaves the machine unless you explicitly configure an external
provider or publish a static report. Full documentation:
https://github.com/kedarvartak/px1
