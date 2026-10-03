#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: review-report-history.sh <restore|publish> [site-directory]" >&2
  exit 2
}

action="${1:-}"
site_dir="${2:-site}"
remote="${PX1_REVIEW_HISTORY_REMOTE:-origin}"
history_branch="${PX1_REVIEW_HISTORY_BRANCH:-px1-review-reports}"

[[ "$action" == "restore" || "$action" == "publish" ]] || usage
[[ "$history_branch" =~ ^[A-Za-z0-9][A-Za-z0-9._/-]*$ ]] || {
  echo "invalid history branch: $history_branch" >&2
  exit 2
}
[[ "$history_branch" != *..* && "$history_branch" != */ && "$history_branch" != *//* ]] || {
  echo "invalid history branch: $history_branch" >&2
  exit 2
}

git_dir="$(git rev-parse --absolute-git-dir)"
history_ref="refs/remotes/${remote}/${history_branch}"

validate_history_tree() {
  local entry metadata mode type path
  while IFS= read -r -d '' entry; do
    metadata="${entry%%$'\t'*}"
    path="${entry#*$'\t'}"
    mode="${metadata%% *}"
    metadata="${metadata#* }"
    type="${metadata%% *}"
    if [[ "$mode" != "100644" || "$type" != "blob" || ! "$path" =~ ^reviews/[0-9a-f]{40}/index\.html$ ]]; then
      echo "refusing unexpected history entry: $path" >&2
      exit 1
    fi
  done < <(git --git-dir="$git_dir" ls-tree -rz "$history_ref")
}

validate_site() {
  local site_abs="$1" file relative found=false
  if find -P "$site_abs" -type l -print -quit | grep -q .; then
    echo "refusing symlink in review history" >&2
    exit 1
  fi
  while IFS= read -r -d '' file; do
    found=true
    relative="${file#"$site_abs"/}"
    if [[ ! "$relative" =~ ^reviews/[0-9a-f]{40}/index\.html$ ]]; then
      echo "refusing unexpected site entry: $relative" >&2
      exit 1
    fi
  done < <(find -P "$site_abs" -type f -print0)
  [[ "$found" == true ]] || {
    echo "review history is empty" >&2
    exit 1
  }
}

if [[ "$action" == "restore" ]]; then
  mkdir -p "$site_dir"
  if git --git-dir="$git_dir" ls-remote --exit-code --heads "$remote" "$history_branch" >/dev/null 2>&1; then
    git --git-dir="$git_dir" fetch --no-tags "$remote" \
      "refs/heads/${history_branch}:${history_ref}"
    validate_history_tree
    git --git-dir="$git_dir" archive "$history_ref" | tar -x -C "$site_dir"
  fi
  exit 0
fi

[[ -d "$site_dir" ]] || {
  echo "site directory does not exist: $site_dir" >&2
  exit 1
}
site_abs="$(cd "$site_dir" && pwd -P)"
validate_site "$site_abs"

index_dir="$(mktemp -d)"
index_file="$index_dir/index"
trap 'rm -f "$index_file"; rmdir "$index_dir"' EXIT

GIT_INDEX_FILE="$index_file" git --git-dir="$git_dir" read-tree --empty
GIT_INDEX_FILE="$index_file" git --git-dir="$git_dir" --work-tree="$site_abs" add -A
tree="$(GIT_INDEX_FILE="$index_file" git --git-dir="$git_dir" write-tree)"

parent_args=()
if git --git-dir="$git_dir" show-ref --verify --quiet "$history_ref"; then
  parent_args=(-p "$(git --git-dir="$git_dir" rev-parse "$history_ref")")
fi

export GIT_AUTHOR_NAME="${GIT_AUTHOR_NAME:-px1 review reports}"
export GIT_AUTHOR_EMAIL="${GIT_AUTHOR_EMAIL:-px1-review-reports@users.noreply.github.com}"
export GIT_COMMITTER_NAME="${GIT_COMMITTER_NAME:-$GIT_AUTHOR_NAME}"
export GIT_COMMITTER_EMAIL="${GIT_COMMITTER_EMAIL:-$GIT_AUTHOR_EMAIL}"
commit="$(printf 'Preserve px1 review reports\n' | git --git-dir="$git_dir" commit-tree "$tree" "${parent_args[@]}")"
git --git-dir="$git_dir" push "$remote" "$commit:refs/heads/${history_branch}"
