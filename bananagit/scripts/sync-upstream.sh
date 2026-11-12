#!/bin/sh
# Bring main up to a newer upstream Gitea release.
#
#   bananagit/scripts/sync-upstream.sh            # newest patch release of the vX.Y series main is on
#   bananagit/scripts/sync-upstream.sh v1.28.0    # a specific upstream tag (a new series)
#
# This repository does not carry Gitea's history, to stay small. The script
# fetches only the two upstream releases involved (the one main is based on,
# named in bananagit/UPSTREAM_VERSION, and the new one), applies the
# difference between them to main with a three-way merge and commits it.
# Gitea's own CI workflows (.github/) are not part of this repository and are
# left out. Prints the upstream tag it applied, or nothing when main is
# already there. On conflicts it stops with the conflicts marked: resolve
# them (ours are in the few files listed in bananagit/README.md), then
# `git add -A && git commit`. Nothing is pushed and no release is tagged.
set -eu
cd "$(git rev-parse --show-toplevel)"
UPSTREAM=${BANANAGIT_UPSTREAM:-https://github.com/go-gitea/gitea.git}
test -z "$(git status --porcelain)" || { echo "Commit your changes first." >&2; exit 1; }
current=$(cat bananagit/UPSTREAM_VERSION)
series=${current%.*}
target=${1:-$(git ls-remote --tags --refs "$UPSTREAM" "refs/tags/$series.*" | sed 's|.*refs/tags/||' \
  | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -1)}
if [ "$target" = "$current" ]; then
  exit 0
fi
git fetch --quiet --no-tags --depth 1 "$UPSTREAM" \
  "+refs/tags/$current:refs/bananagit/old" "+refs/tags/$target:refs/bananagit/new"
cleanup() { git update-ref -d refs/bananagit/old; git update-ref -d refs/bananagit/new; }
trap cleanup EXIT
echo "$target" > bananagit/UPSTREAM_VERSION
git add bananagit/UPSTREAM_VERSION
if ! git diff --binary refs/bananagit/old refs/bananagit/new -- . ':(exclude).github' | git apply --quiet --3way --index >&2; then
  echo "Conflicts applying Gitea $target: resolve them, then git add -A && git commit -m 'Update to Gitea $target'." >&2
  exit 1
fi
git commit --quiet -m "Update to Gitea $target

Applied the difference between upstream tags $current and $target."
echo "$target"
