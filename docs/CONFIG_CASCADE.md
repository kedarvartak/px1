---
title: Cascading configuration
section: concepts
kind: narrative
status: shipped
covers:
  - config_cascade.go
verified_at:
---

# Cascading configuration

`config_cascade.go` resolves the px1 settings that apply to one file. It reads
a `.px1/config.json` at the repository root and one in each directory on the
way down to the file, validates each, and merges them into a single result.

Status: shipped, through one command. `px1 validate-config` (in
`validate_config.go`, dispatched from `main.go`) calls
`resolveCascadingConfig` and prints the result as JSON; see the
[CLI reference](CLI_REFERENCE.md). That is the only caller. No review command
in the repository reads the resolved values, so today the cascade can be
checked and inspected but does not change what a review does.

## Which files are read

`configCandidates(target)` lists the candidate paths, root first. For the
target `src/api/handler.go` it returns:

```
.px1/config.json
src/.px1/config.json
src/api/.px1/config.json
```

A target in the repository root has one candidate, `.px1/config.json`.
Missing files are skipped. Files beside the cascade path (for example
`docs/.px1/config.json` for a target under `src/`) are never read.

## Target rules

`resolveCascadingConfig(target, files)` takes a repository-relative path and a
map from candidate path to file content. It rejects the target with
`config target must be a repository-relative file` when the target is empty,
is absolute, cleans to `.` or `..`, or starts with `../` after cleaning.

The target's directory may be at most `configDepthLimit` (20) levels deep.
Deeper targets fail with `config target exceeds directory depth limit`. The
target file itself does not have to exist.

## Fields

`decodeConfig` validates one file. The same rules are published as
`docs/config.schema.json`.

| Field | Type | Rule |
| --- | --- | --- |
| `version` | integer | Required; must be `1` |
| `strictness` | integer | 1 to 3 |
| `commentTypes` | array of strings | Each one of `logic`, `syntax`, `style`, `security`; no duplicates |
| `disabledRules` | array of strings | Each non-blank and at most 80 bytes; no duplicates |
| `maxInlineComments` | integer | 1 to 50 |
| `failOnP0` | boolean | |
| `context.repos` | array of strings | At most 10; each `owner/repository`; no duplicates, compared case-insensitively |

Each part of a `context.repos` entry must pass `configSlug`: non-empty, not
`.` or `..`, and only ASCII letters, digits, `-`, `_` and `.`. The test shows
`../private` being rejected.

File-level rules:

- A file must be non-empty and at most `configFileLimit`, 64 KiB
  (`64 << 10` bytes).
- Unknown fields are rejected (`DisallowUnknownFields`), at the top level and
  inside `context`.
- Anything after the first JSON value is rejected as
  `config contains trailing data`.

## Defaults

With no config files, the result is:

| Setting | Default |
| --- | --- |
| `strictness` | 2 |
| `commentTypes` | `logic`, `security`, `style`, `syntax` |
| `maxInlineComments` | 15 |
| `failOnP0` | false |
| `disabledRules` | none |
| `contextRepos` | none |

## Merge order and rules

Files are applied in candidate order: repository root first, then each
directory down to the one containing the target. A field that a file omits
leaves the current value unchanged. How a present field merges depends on the
field:

| Field | Merge rule | Effect |
| --- | --- | --- |
| `strictness` | Maximum of the default and every file | A deeper file can raise it, never lower it. No file can take it below the default of 2 |
| `failOnP0` | Logical OR | Once any file sets `true`, a deeper `false` does not turn it off |
| `maxInlineComments` | Last file wins | The deepest file that sets it decides |
| `commentTypes` | Last file wins, whole list replaced | Lists are not combined |
| `disabledRules` | Union across all files | Sorted; a deeper file cannot re-enable a rule |
| `context.repos` | Union across all files, lower-cased | Sorted; duplicates that differ only in case collapse |

The function's comment states the principle for the two gating settings:
they only become stricter.

The union of `context.repos` is checked again after merging. If the resolved
list has more than 10 repositories the call fails with
`resolved context.repos cannot exceed 10 repositories`, even when every file
is within its own limit of 10.

Every file that was found and applied is recorded, in order, in `sources`.

## Worked example

From `TestResolveCascadingConfigKeepsGatesStrict`, resolving
`src/api/handler.go` with three files:

| File | Content |
| --- | --- |
| `.px1/config.json` | `{"version":1,"strictness":3,"failOnP0":true,"maxInlineComments":10,"disabledRules":["root-rule"]}` |
| `src/.px1/config.json` | `{"version":1,"strictness":1,"failOnP0":false,"maxInlineComments":20,"commentTypes":["logic"],"disabledRules":["child-rule"]}` |
| `src/api/.px1/config.json` | `{"version":1,"maxInlineComments":5}` |

Result:

| Setting | Value | Why |
| --- | --- | --- |
| `strictness` | 3 | Root sets 3; the `src` value of 1 is lower and ignored |
| `failOnP0` | true | Root sets `true`; the `src` value of `false` does not undo it |
| `maxInlineComments` | 5 | Deepest file wins over 10 and 20 |
| `commentTypes` | `logic` | `src` replaces the default list; `src/api` does not set it |
| `disabledRules` | `child-rule`, `root-rule` | Union of both lists, sorted |
| `sources` | all three paths | Root first |

A second test, `TestCascadingConfigCollectsBoundedContextRepositories`, has
`Acme/API` in the root file and `acme/api`, `acme/web` in `src`. The resolved
`contextRepos` is `acme/api`, `acme/web`.

## Output

`resolvedConfig` is what `px1 validate-config` prints, as one line of JSON
with the keys `sources`, `strictness`, `commentTypes`, `disabledRules`,
`maxInlineComments`, `failOnP0` and `contextRepos`. `contextRepos` is omitted
when empty. Note the output key is `contextRepos`, while the input field is
`context.repos`.

## Errors

Any invalid file stops resolution. The error is prefixed with the path of the
file that failed, for example `src/.px1/config.json: version must be 1`.
There is no partial result and no fallback to defaults.

| Message | Cause |
| --- | --- |
| `config must be non-empty and at most 64 KiB` | Empty or oversized file |
| `config contains trailing data` | More than one JSON value |
| `version must be 1` | Missing or different `version` |
| `strictness must be between 1 and 3` | Out of range |
| `maxInlineComments must be between 1 and 50` | Out of range |
| `commentTypes contains an invalid or duplicate value` | Unknown or repeated type |
| `disabledRules contains an invalid id` | Blank or longer than 80 bytes |
| `disabledRules contains a duplicate id` | Repeated within one file |
| `context.repos cannot exceed 10 repositories` | More than 10 in one file |
| `context.repos contains an invalid or duplicate owner/repository` | Bad shape or repeated entry |

Unknown fields and malformed JSON return the JSON decoder's own error.

## How `px1 validate-config` uses it

`runValidateConfig` takes `--root` (default `.`) and `--path` (default
`README.md`) and accepts no positional arguments.

1. It calls `resolveCascadingConfig` with an empty file map. This validates
   the target before any file is read, so an absolute path or one that
   escapes the root is rejected first.
2. It reads each candidate from disk under `--root`. A file that does not
   exist is skipped; any other read error fails as `read PATH: ...`.
3. It resolves again with the files it found and prints the JSON.

The files are read from the working tree at `--root`, not from a git commit.

## Not implemented

- No command other than `validate-config` reads the resolved configuration.
- No environment variable, flag or organisation-level layer; the cascade is
  only the `.px1/config.json` files on the path to the target.
- No way for a deeper directory to lower `strictness`, clear `failOnP0`,
  re-enable a disabled rule or remove a context repository.
- The schema file is not loaded by the code; the Go checks are separate and
  slightly stricter (they also reject whitespace-only rule ids and `.` or `..`
  as a repository owner or name).
