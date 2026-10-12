# Private-code delivery

Use [`examples/px1-review-private.yml`](../examples/px1-review-private.yml) for
repositories whose source should not be published through a public GitHub Pages
site. The workflow uploads the static bundle as a GitHub Actions artifact and
links it from the pull request.

The download requires a signed-in GitHub user with read access to the repository,
as described in GitHub's [artifact download documentation](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/download-workflow-artifacts).
The example retains each artifact for 30 days. After downloading and unzipping
the artifact, open `index.html`; it makes no network requests.

## Privacy check

The reusable Action runs `px1 privacy-check` by default. It blocks delivery when
the generated report contains a high-confidence secret signature or a changed
path commonly used for credentials, such as `.env`, a private key, or a package
manager credentials file. Findings name the signature or path but never print
the detected value.

This bounded check can miss secrets and can produce false positives. It does not
remove content, redact values, or grant access control. Fix or remove sensitive
material before delivery. `privacy-check: false` exists for reviewed false
positives and emits a warning; it must not be treated as a safe-publication flag.

## Model transfer

AI explanations are off unless `explanation-model` is set. Enabling them sends
bounded, non-excluded pull-request diff content to the configured model endpoint.
The Action warns before transfer. `explanation-exclude-globs` reduces the paths
sent to the model, but it is not redaction and excluded files still appear in
the report bundle.

## Integrity

Every bundle contains `checksums.sha256` for `index.html` and `review.json`.
GitHub's uploaded archive also has an artifact digest shown in the private
workflow's PR comment. These hashes can detect changed bytes; they do not prove
who generated or approved the report.

## Public Pages

The Pages template remains available for repositories that deliberately choose
that delivery model. Confirm the site's effective visibility before enabling it:
a private source repository does not by itself guarantee a private Pages site.
GitHub documents the applicable [Pages visibility and access-control behavior](https://docs.github.com/en/enterprise-cloud@latest/pages/getting-started-with-github-pages/changing-the-visibility-of-your-github-pages-site).
