# Releasing BananaGit (maintainers)

A release is a signed tag `vX.Y.Z.N` plus one binary per platform, published
on GitHub and on the mirror. Servers install only what the tag's signature
covers (see [updating.md](updating.md)).

## Once: a signing key

```sh
ssh-keygen -t ed25519 -f ~/.ssh/bananagit-release -C "BananaGit releases"
git config gpg.format ssh
git config user.signingkey ~/.ssh/bananagit-release.pub
```

Keep the private key off servers and out of CI. Publish the allowed-signers
line that server administrators pass to `--require-signatures`:

```sh
echo "$(git config user.email) $(cat ~/.ssh/bananagit-release.pub)"
```

Tell them its fingerprint (`ssh-keygen -lf ~/.ssh/bananagit-release.pub`)
through a second channel. On both forges, let only maintainers create `v*`
tags (GitHub: a tag ruleset; Forgejo/BananaGit: protected tags), because
anyone who can push tags can make servers skip to an older signed release.

## Each release

1. **Take in upstream.** Run `bananagit/scripts/sync-upstream.sh` (on GitHub
   the `bananagit-sync` workflow does it and opens a pull request). For a new
   series: `bananagit/scripts/sync-upstream.sh v1.28.0`. On conflicts, resolve
   them (ours are in the few files listed in the README) and commit. Push.
2. **Check it.** `TAGS="sqlite sqlite_unlock_notify" make build`, start
   `./gitea web`, look at the themes and the logo.
3. **Build and tag** from a clean, up-to-date `main`:

   ```sh
   bananagit/scripts/release.sh
   ```

   It builds `dist/bananagit-<version>-linux-amd64` and `-linux-arm64`
   (without SQLite unless you set `TAGS=sqlite CGO_ENABLED=1` on a host with a
   C cross-compiler), writes their SHA-256 into the tag message and creates
   the signed tag. `BANANAGIT_PLATFORMS` chooses other platforms.
4. **Publish**, tag first:

   ```sh
   git push origin v1.27.4.1
   ```

   Then create a release for that tag on GitHub and on the mirror and upload
   every file from `dist/` except `TAG_MESSAGE`, under the same names. The
   updater downloads `…/releases/download/<tag>/<file>` from whichever source
   answered, so both need the files.
5. **Verify** on any machine: `bananagit-update check` should name the new tag.

A second release of the same Gitea version (a BananaGit-only fix) gets the
next counter automatically: `v1.27.4.2`.
