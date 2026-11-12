<p align="center">
  <img src="assets/logo.svg" alt="BananaGit logo" width="96">
</p>

# BananaGit

BananaGit is a self-hosted Git service: repository hosting, code review, issues,
wiki, package registry and CI/CD. It is [Officina Tecnologica](https://git.officinatecnologica.eu)'s
fork of [Gitea](https://about.gitea.com), in the BananaSuite colours and with
its own signed, self-rolling-back updater.

Apart from the short list below it **is** Gitea: the same features, database,
`app.ini` and API. Gitea's [documentation](https://docs.gitea.com/) applies unchanged.

- [Quickstart](#quickstart)
- [What changes between Gitea and BananaGit](#what-changes-between-gitea-and-bananagit)
- [Install](#install)
- [Update](#update)
- [Following Gitea](#following-gitea)
- [Build from source](#build-from-source)
- [Coming from Gitea or Forgejo](#coming-from-gitea-or-forgejo)
- [Contributing](#contributing)
- [Licence and names](#licence-and-names)

## Quickstart

Try it on your own machine in a few minutes. Needs Git, Go and Node.js with
pnpm (see [docs/build-setup.md](docs/build-setup.md)).

```sh
git clone https://github.com/BananaSuite/BananaGit.git
cd BananaGit
TAGS="sqlite sqlite_unlock_notify" make build
./gitea web
```

Open <http://localhost:3000>, keep SQLite as the database, press install, and
register: the first account is the administrator. Everything it writes stays
in this folder (`data/`, `custom/`). For a real server, see [Install](#install).

## What changes between Gitea and BananaGit

| Area | Gitea | BananaGit |
| --- | --- | --- |
| Look | Gitea's themes | Adds `bananagit-auto`, `bananagit-light` and `bananagit-dark` (Gitea's themes with other colours) and makes `bananagit-auto` the default |
| Name | Default `APP_NAME` is "Gitea" | Default is "BananaGit"; the footer says "Powered by Gitea (BananaGit)" |
| Logo | Gitea's tea cup | The BananaSuite banana (logo, favicon, default avatar) |
| Updates | Replace the binary yourself | `bananagit-update`: verifies a signed release, backs up, switches, checks health and rolls back on failure; optional automatic updates |
| Versions | `vX.Y.Z` | `vX.Y.Z.N`: Gitea's version plus our counter, e.g. `v1.27.3.1` |
| Release binaries | Many platforms, SQLite included | `linux-amd64` and `linux-arm64`, built without SQLite by default |
| Forgejo | No supported path | A tested, rehearsable in-place migration |
| Repository | Full history, Gitea's CI | A snapshot of one Gitea release with our commits on top; Gitea's CI workflows, issue templates and funding file are removed |

Everything else, including every setting, is Gitea's. An administrator can
still change name, theme and logo as in Gitea (`APP_NAME`, `[ui] DEFAULT_THEME`,
`custom/public/assets/img/`).

The exact files we touch, and how upstream releases are merged in, are in
[bananagit/README.md](bananagit/README.md). The Gitea release this tree is
based on is named in [bananagit/UPSTREAM_VERSION](bananagit/UPSTREAM_VERSION).

## Install

These steps set up a Linux server with systemd, using the paths the updater
expects by default. For anything else (other init systems, reverse proxies,
HTTPS, mail), follow [Gitea's installation guide](https://docs.gitea.com/category/installation)
with the BananaGit binary in place of `gitea`.

### Requirements

- Linux on amd64 or arm64, with systemd
- Git
- A PostgreSQL database and user for BananaGit. Release binaries are built
  without SQLite by default, and the updater cannot back up MySQL
- Python 3.9+ (only for the updater)

### 1. Create the user and directories

```sh
sudo adduser --system --group --shell /bin/bash --home /home/git git
sudo mkdir -p /opt/bananagit/{config,backups,releases} /var/lib/bananagit/{custom,data,log} /etc/bananagit
sudo chown -R git:git /var/lib/bananagit
sudo chown root:git /etc/bananagit
sudo chmod 770 /etc/bananagit
```

### 2. Download and check a release

Pick the newest version on the [releases page](https://github.com/BananaSuite/BananaGit/releases)
and set `ARCH` to `amd64` or `arm64`.

```sh
VERSION=1.27.3.1
ARCH=amd64
git clone --depth 1 --branch "v$VERSION" https://github.com/BananaSuite/BananaGit.git
curl -fLO "https://github.com/BananaSuite/BananaGit/releases/download/v$VERSION/bananagit-$VERSION-linux-$ARCH"
git -C BananaGit tag -l --format='%(contents)' "v$VERSION"   # the expected SHA-256
sha256sum "bananagit-$VERSION-linux-$ARCH"                    # must match
```

### 3. Put the binary in place

```sh
sudo install -D -m 755 "bananagit-$VERSION-linux-$ARCH" "/opt/bananagit/releases/$VERSION/bananagit"
sudo ln -sfn "releases/$VERSION/bananagit" /opt/bananagit/current
```

### 4. Add the service

The unit in [bananagit/contrib/bananagit.service](bananagit/contrib/bananagit.service)
uses the paths above. Edit it first if your database is not a local PostgreSQL.

```sh
sudo cp BananaGit/bananagit/contrib/bananagit.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now bananagit
```

### 5. Finish in the browser

Open `http://<server>:3000`, fill in the database and the administrator
account, and submit. Then stop the installer from being able to rewrite the
configuration:

```sh
sudo chmod 750 /etc/bananagit
sudo chmod 640 /etc/bananagit/app.ini
```

### 6. Install the updater

```sh
sudo cp -r BananaGit/bananagit/updater BananaGit/bananagit/bananagit-update /opt/bananagit/
sudo ln -s /opt/bananagit/bananagit-update /usr/local/bin/bananagit-update
echo "{\"version\": \"$VERSION\"}" | sudo tee /opt/bananagit/config/settings.json
sudo bananagit-update check
```

`check` should print the installed and the latest version. If your service
name, user, `app.ini` path or port differ from the ones above, add them to
`settings.json` as described in [bananagit/docs/updating.md](bananagit/docs/updating.md#installing-the-updater).

## Update

```sh
sudo bananagit-update check      # what update would install, without installing
sudo bananagit-update update     # back up, install, check health, roll back on failure
```

Automatic updates are off until you enable them, and require signed releases:

```sh
sudo bananagit-update updates enable --require-signatures ALLOWED_SIGNERS --interval 60
```

`ALLOWED_SIGNERS` holds the maintainers' release keys; verify their
fingerprints through a second channel. What an update does step by step, every
setting and what to do when one fails are in [bananagit/docs/updating.md](bananagit/docs/updating.md).

## Following Gitea

BananaGit stays a soft fork: a new Gitea release is applied to this tree
without anyone writing code.

```sh
bananagit/scripts/sync-upstream.sh           # newest patch release of the current series
bananagit/scripts/sync-upstream.sh v1.28.0   # a new series
bananagit/scripts/diff-upstream.sh           # what still differs from Gitea
```

On GitHub the `bananagit-sync` workflow does the first one daily, builds the
result and opens a pull request. Files we replace entirely (the README, the
logo, `.github/`) are listed in [bananagit/OURS](bananagit/OURS) and skipped,
so they cannot conflict; the few lines we change inside Gitea's files are the
only place a conflict can come from. Details: [bananagit/README.md](bananagit/README.md#following-gitea).

## Build from source

Needs Go and Node.js with pnpm, as Gitea does (see [docs/build-setup.md](docs/build-setup.md)).

```sh
TAGS="sqlite sqlite_unlock_notify" make build
./gitea web        # http://localhost:3000
```

`make build` names the binary `gitea` (upstream's default); releases are named
`bananagit-<version>-<platform>`. A local build reports its commit as the
version; add `GITEA_VERSION=1.27.3` to `make build` for a proper one.
[docs/development.md](docs/development.md) covers the development environment,
linting and testing.

## Coming from Gitea or Forgejo

- **Gitea:** swap the binary; the database, `app.ini` and data stay as they
  are. Steps and what users notice: [bananagit/docs/migrating-from-gitea.md](bananagit/docs/migrating-from-gitea.md).
- **Forgejo:** possible in place but one-way, and it needs preparation. Rehearse
  it on a copy first: [bananagit/docs/migrating-from-forgejo.md](bananagit/docs/migrating-from-forgejo.md).

## Documentation

| Topic | Where |
| --- | --- |
| Using and administering the server | [docs.gitea.com](https://docs.gitea.com/) |
| Files that differ from Gitea, following upstream | [bananagit/README.md](bananagit/README.md) |
| Updating, automatic updates, recovery | [bananagit/docs/updating.md](bananagit/docs/updating.md) |
| Releasing (maintainers) | [bananagit/docs/releasing.md](bananagit/docs/releasing.md) |
| Migrating from Gitea | [bananagit/docs/migrating-from-gitea.md](bananagit/docs/migrating-from-gitea.md) |
| Migrating from Forgejo | [bananagit/docs/migrating-from-forgejo.md](bananagit/docs/migrating-from-forgejo.md) |

## Contributing

Contributions go to <https://github.com/BananaSuite/BananaGit>;
`git.officinatecnologica.eu` carries a mirror. Changes that are not about
BananaGit's name, look or updater belong upstream, in
[Gitea](https://github.com/go-gitea/gitea).

Security problems in the updater or the branding: report them privately to the
maintainers of this repository. Problems in Gitea itself go to Gitea: see [SECURITY.md](SECURITY.md).

## Licence and names

BananaGit keeps Gitea's **MIT licence** and copyright notices (see [LICENSE](LICENSE));
our additions are under the same licence.

BananaGit is not Gitea and is not endorsed by the Gitea project or Gitea
Limited. It has its own name and logo (the BananaSuite banana, by Luca Zani)
and says "Powered by Gitea" where Gitea does.
