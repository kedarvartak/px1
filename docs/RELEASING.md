# Releasing the px1 Actions

Consumers should pin px1 to an immutable release or maintained major tag.

1. Run `go test ./...`, `go test -race ./...`, and `go vet ./...`.
2. Generate and inspect a report from two fixture commits.
3. Update `VERSION` and commit it.
4. Create and push an immutable semantic-version tag such as `v1.2.0`.
5. Move the corresponding major tag, such as `v1`, only after the immutable
   release has passed its workflow checks.
6. Verify both `kedarvartak/px1@v1` and `kedarvartak/px1/history@v1` from a
   separate repository.

The project does not publish a local application, npm package, installer, or
cross-platform binary bundle.
