# px1

Agents write the PR. px1 helps reviewers understand it.

px1 evaluates versioned `.px1/rules.json` policy, renders structured diffs and
contextual findings, and exports a commit-pinned HTML report for a GitHub pull
request. The core runs locally without an account, hosted service, or LLM API
key. Optional provider-generated explanations clarify changes; reviewers remain
the decision-makers.

See the [product vision and status](https://github.com/kedarvartak/px1/blob/master/docs/PRODUCT_VISION.md).

```sh
npx px1-cli            # review the current directory
npm install -g px1-cli # then: px1 /path/to/repo
```

The command is `px1`. It serves a local web UI and opens it in your browser;
nothing leaves the machine unless you explicitly configure an external
provider or publish a static report. Full documentation:
https://github.com/kedarvartak/px1
