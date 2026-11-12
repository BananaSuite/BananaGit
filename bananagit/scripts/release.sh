#!/bin/sh
# Build the release binaries of HEAD and create its signed release tag.
#
#   bananagit/scripts/release.sh            # next free tag for the Gitea version main is on
#   bananagit/scripts/release.sh v1.27.3.2  # an explicit tag
#
# BananaGit tags are vX.Y.Z.N: Gitea's version plus our own counter (v1.27.3.1,
# v1.27.3.2, ...), so they never collide with upstream's vX.Y.Z tags.
#
# The tag message lists the SHA-256 of every binary, so the tag's SSH signature
# (git config gpg.format ssh, user.signingkey) covers what servers install.
# Nothing is pushed: push the tag and upload dist/* to the release yourself
# (GitHub and the mirror), or let the release workflow do it.
set -eu
cd "$(git rev-parse --show-toplevel)"
upstream=$(cat bananagit/UPSTREAM_VERSION)
count=$(git tag --list "$upstream.*" | wc -l | tr -d ' ')
tag=${1:-$upstream.$((count + 1))}
version=${tag#v}
test -z "$(git status --porcelain)" || { echo "Commit your changes first." >&2; exit 1; }
rm -rf dist && mkdir dist
for platform in ${BANANAGIT_PLATFORMS:-linux-amd64 linux-arm64}; do
  # Pure-Go build (no cgo): PostgreSQL and MySQL. Add TAGS=sqlite on a host with a C cross-compiler.
  GOOS=${platform%-*} GOARCH=${platform#*-} CGO_ENABLED=${CGO_ENABLED:-0} TAGS="${TAGS:-}" \
    GITEA_VERSION="$version" EXECUTABLE="dist/bananagit-$version-$platform" make build
done
{
  echo "BananaGit $version (Gitea ${upstream#v})"
  echo
  for file in dist/bananagit-*; do
    echo "sha256 $(shasum -a 256 "$file" | cut -d' ' -f1)  $(basename "$file")"
  done
} > dist/TAG_MESSAGE
git tag -s "$tag" -F dist/TAG_MESSAGE
echo "Created signed tag $tag. Binaries are in dist/."
