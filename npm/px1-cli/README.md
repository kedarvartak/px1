# px1

Agents write the PR. px1 helps reviewers understand it.

We are building an HTML report linked from each GitHub PR: diffs, highlighted
code blocks, short AI explanation chips, and findings for rules your team keeps
in `.px1/rules.json`.

This package installs the existing local CLI. The PR report work is under
development; AI chips and arbitrary plain-language rules are planned. See the
[product vision and status](https://github.com/kedarvartak/px1/blob/master/docs/PRODUCT_VISION.md).

```sh
npx px1-cli            # review the current directory
npm install -g px1-cli # then: px1 /path/to/repo
```

The command is `px1`. It serves a local web UI and opens it in your browser;
nothing leaves the machine unless you explicitly configure an external
provider or publish a static report. Full documentation:
https://github.com/kedarvartak/px1
