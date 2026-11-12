#!/bin/sh
# List how this tree differs from the Gitea release it is based on.
#
#   bananagit/scripts/diff-upstream.sh          # file names and line counts
#   bananagit/scripts/diff-upstream.sh -p       # the full diff
#
# Leaves out bananagit/ and the files in bananagit/OURS. What remains should be
# the short list in bananagit/README.md: anything else is drift that will
# conflict with a later Gitea release.
set -eu
cd "$(git rev-parse --show-toplevel)"
UPSTREAM=${BANANAGIT_UPSTREAM:-https://github.com/go-gitea/gitea.git}
git fetch --quiet --no-tags --depth 1 "$UPSTREAM" "+refs/tags/$(cat bananagit/UPSTREAM_VERSION):refs/bananagit/base"
trap 'git update-ref -d refs/bananagit/base' EXIT
set -- "${1:---stat}" refs/bananagit/base -- . ':(exclude)bananagit'
while read -r path; do
  case $path in ''|'#'*) ;; *) set -- "$@" ":(exclude)$path" ;; esac
done < bananagit/OURS
git --no-pager diff "$@"
