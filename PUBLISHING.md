# Publishing & Release Guide

This document outlines the step-by-step instructions for preparing, testing, and publishing a new release of `px1`.

## 1. Prerequisites

Before cutting a new release, ensure you have:

- Git with push and tag permissions for `kedarvartak/px1`.
- Go (version 1.24+).
- Node.js (v18+ or v20+) or Bun for bundling frontend assets.
- A clean working tree with all tests passing.
- For npm publishing: an `NPM_TOKEN` repository secret (an npm automation token for
  an account that owns `px1-cli` and the `px1-cli-*` platform packages). Without it
  the release still publishes binaries; the npm step logs that it is skipping.

## 2. Pre-Release Verification

Run the test suite and verify asset bundling:

```bash
# 1. Run unit and integration tests
make test

# 2. Build local binary and verify sanity
make build
./px1 --version
```

## 3. Release Methods
### Method 1: Automated Release via Makefile (Recommended)

The project includes an automated `make publish` target in the [Makefile](Makefile) that:

1. Updates the [VERSION](VERSION) file.
1. Bundles web assets and cross-compiles binaries into `dist/`.
1. Commits `VERSION`.
1. Creates an annotated Git tag `v<version>`.

#### Step 1: Run publish command

Specify the version without a leading `v`:

```bash
make publish 0.2.0
```

#### Step 2: Push commit and tags to GitHub

```bash
git push origin master --tags
```

### Method 2: Manual Release via Git

```bash
# 1. Update VERSION file
echo "0.2.0" > VERSION

# 2. Bundle frontend assets and compile release binaries
make dist

# 3. Commit version bump
git add VERSION
git commit -m "Release v0.2.0"

# 4. Tag the release
git tag -fa v0.2.0 -m "Release v0.2.0"

# 5. Push to GitHub
git push origin master
git push origin v0.2.0
```

### Method 3: GitHub Actions Workflow Dispatch

1. Navigate to Actions -> Release on GitHub: `https://github.com/kedarvartak/px1/actions/workflows/release.yml`
1. Click Run workflow.
1. Enter the version tag (e.g. `v0.2.0`).
1. Trigger the workflow.

## 4. npm Packages

The release workflow publishes px1 to npm as well, so `npx px1-cli` runs the
version that was just tagged.

The layout is the one esbuild uses: a wrapper package, `px1-cli`, that carries
the `px1` command, plus one package per platform (`px1-cli-linux-x64`,
`px1-cli-darwin-arm64`, …) holding nothing but the binary. Each platform package
declares its own `os` and `cpu`, so npm installs only the matching one and skips
the rest. Nothing is downloaded after install, which is what keeps px1 working
behind a proxy, from an offline cache, and under `npm ci --ignore-scripts`.

- `scripts/build-npm.js` lays the packages out in `dist-npm/` from the binaries
  in `dist/`. `make npm` runs both steps.
- The workflow publishes the platform packages first, then the wrapper: npm
  rejects a version whose optional dependencies do not exist yet.
- A version already on the registry is skipped rather than failing, so a release
  can be re-run.
- `--provenance` links each package to the workflow run, which is why the job
  asks for `id-token: write`.

To rehearse locally without publishing:

```bash
make npm
cd dist-npm/px1-cli && npm pack            # inspect the tarball
npm install -g ./dist-npm/px1-cli-linux-x64 ./dist-npm/px1-cli
px1 --version
```

## 5. Post-Release Verification

1. Verify GitHub Actions workflow completion on the Actions tab.
1. Confirm artifacts on the [Releases](https://github.com/kedarvartak/px1/releases) page (cross-platform binaries and `checksums.txt`). The self-updater requires this file and verifies the selected binary against it before execution.
1. Verify the installer script:
  ```bash
  curl -fsSL https://raw.githubusercontent.com/kedarvartak/px1/master/install.sh | sh
  ```
1. Verify self-update functionality:
  ```bash
  px1 --update
  ```
1. Verify the npm packages:
  ```bash
  npx px1-cli@latest --version
  ```
