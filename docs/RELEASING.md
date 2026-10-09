# Releasing the px1 Actions

Consumers should pin px1 to an immutable release or maintained major tag.

An Action release is distinct from the public product launch. Before announcing
the broader launch, check the human-review, evidence, privacy, and pilot criteria
in [ROADMAP.md](../ROADMAP.md). Product messaging should lead with understanding
the code your agent wrote before merging, as described in the product vision.

1. Update `VERSION` in a normal pull request and merge it after checks pass.
2. Open **Actions → Release px1 Actions → Run workflow** on the default branch.
3. Enter the matching immutable tag, such as `v1.2.0`.
4. The workflow runs tests, race tests, vet, and Action contract tests.
5. It creates the immutable tag and release, then moves the maintained major
   tag (`v1`) to the same tested commit.
6. Verify both `kedarvartak/px1@v1` and `kedarvartak/px1/history@v1` from a
   separate repository.

The workflow refuses malformed versions, a tag that already exists, or a tag
that does not match `VERSION`. Immutable version tags are never replaced; only
the documented major compatibility tag moves.

The project does not publish a local application, npm package, installer, or
cross-platform binary bundle.
