# BananaGit

BananaGit is Officina Tecnologica's fork of [Gitea](https://about.gitea.com), in
the colours of the other Banana tools and with an updater that works like
BananaWiki's. Apart from what is listed below it is Gitea, unchanged: the same
features, database and `app.ini`.

Contributions go to <https://github.com/BananaSuite/BananaGit>;
`git.officinatecnologica.eu` carries a mirror. Changes that are not about
BananaGit's name, look or updater belong upstream, in Gitea.

## What differs from Gitea

Everything of ours is in this short list, so merging an upstream release stays easy.

| What | Where |
| --- | --- |
| Themes `bananagit-auto`, `bananagit-light`, `bananagit-dark` (new files that import Gitea's themes and override colours only) | `web_src/css/themes/theme-bananagit-*.css` |
| Default theme, page author and description | `modules/setting/ui.go` (3 lines) |
| Default name "BananaGit" | `modules/setting/server.go` (1 line) |
| Footer: "Powered by Gitea (BananaGit)" | `templates/base/footer_content.tmpl` (1 line) |
| Logo and favicon: the suite's banana | `assets/*.svg`, `public/assets/img/{logo,favicon,apple-touch-icon,avatar_default}.*`, made by `bananagit/branding/make-logo.py` |
| Updater, release, sync and migration scripts, these docs | `bananagit/`, `.github/workflows/bananagit-sync.yml` |
| Gitea's own CI workflows, issue templates and funding file are removed | `.github/` |
| A three-line note on top of the README; the default theme named in the example configuration | `README.md`, `custom/conf/app.example.ini` |

An administrator can still change name, theme and logo as in Gitea
(`APP_NAME`, `[ui] DEFAULT_THEME`, `custom/public/assets/img/`).

## Following Gitea

To stay small, this repository does not carry Gitea's history: `main` starts
from a snapshot of one Gitea release (named in `bananagit/UPSTREAM_VERSION`)
with our commits on top. `bananagit/scripts/sync-upstream.sh` fetches the next
Gitea release, applies the difference to `main` with a three-way merge and
commits it. On GitHub a daily workflow (`bananagit-sync`) runs it for patch
releases of the same series and opens a pull request. A maintainer merges it and runs:

```sh
bananagit/scripts/release.sh     # builds the binaries, creates the signed tag v1.27.4.1
```

BananaGit tags are `vX.Y.Z.N`: Gitea's version plus our own counter. Servers
with automatic updates then install it (see [docs/updating.md](docs/updating.md)).
The tag is signed by a maintainer on purpose: a signing key in CI would let
anyone who takes over the workflow release code that every server installs as
root. A new minor series is applied by hand:
`bananagit/scripts/sync-upstream.sh v1.28.0`.

## Documentation

* [docs/updating.md](docs/updating.md): how servers update, automatic updates, settings, what to do when an update fails.
* [docs/releasing.md](docs/releasing.md): for maintainers: signing key, building, tagging and publishing a release.
* [docs/migrating-from-forgejo.md](docs/migrating-from-forgejo.md): moving a Forgejo server to BananaGit.

## Coming from Forgejo

See [docs/migrating-from-forgejo.md](docs/migrating-from-forgejo.md).

## Building and trying it

```sh
TAGS="sqlite sqlite_unlock_notify" make build
./gitea web        # http://localhost:3000
```

Needs Go and Node.js as Gitea does. `make build` names the binary `gitea`
(upstream's default); releases are named `bananagit-<version>-<platform>`.
A local build reports its commit as the version; add
`GITEA_VERSION=1.27.3` to `make build` for a proper one.

## Licence and names

BananaGit keeps Gitea's **MIT licence** and copyright notices (see `LICENSE`);
our additions are under the same licence. It is not AGPL like the other Banana
tools, because the upstream code stays under its own licence.

BananaGit is not Gitea and is not endorsed by the Gitea project or Gitea
Limited: it has its own name and its own logo (the BananaSuite banana, by Luca
Zani), and says "Powered by Gitea" where Gitea does. The Gitea logo is no
longer used as the application's logo; the files of it that remain in the tree
are upstream's.
