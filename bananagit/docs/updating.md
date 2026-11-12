# Updating BananaGit

`bananagit-update` updates a BananaGit server the way `bananawiki update`
updates a wiki. It needs Python 3.9+, Git and systemd, and runs as root.

```sh
sudo bananagit-update check      # what update would install, without installing
sudo bananagit-update update
```

## What an update does

1. **Find the release.** The update source's release tags (`vX.Y.Z.N`) are
   listed with a hardened Git (no hooks, credential helpers or redirects) and
   the highest one is taken; pre-release and other tags are ignored. The
   sources in `config/settings.json` (`urls`) are tried in order: by default
   GitHub, then the mirror on `git.officinatecnologica.eu`. An installed
   version that is the same or newer ends here (`current`): BananaGit never
   downgrades, because Gitea's database migrations only go forward.
2. **Verify it.** With signatures required, the release must be an annotated
   tag SSH-signed by a key in `config/allowed_signers`, for its own name. Tags
   that do not verify are skipped and listed (`skipped_releases`), up to 20.
3. **Download.** The binary for this machine
   (`bananagit-<version>-<os>-<arch>`) comes from the release's downloads on
   the same forge and must match the SHA-256 written in the tag message. The
   signature on the tag therefore covers the binary. The server keeps running.
4. **Back up.** The service stops. The database is copied (SQLite: the file;
   PostgreSQL: `pg_dump -Fc`) and `bananagit dump` writes a full archive, both
   in `backups/before-update-<time>/`.
5. **Switch.** `current` points to the new binary and the service starts; the
   new release migrates the database as it starts.
6. **Health check.** `/api/healthz` must report `pass` within 3 minutes. If
   not, the service stops, `current` points back, the database copy is
   restored and the old release starts: the result is `rolled_back` and the
   release is recorded as failed.
7. **Clean up.** Backups beyond the retention count (3) and releases other
   than the new and the previous one are removed.

Every run appends its result to `config/history.jsonl`.

## Automatic updates

Off until you turn them on:

```sh
sudo bananagit-update updates enable --require-signatures ALLOWED_SIGNERS --interval 60 --keep-backups 3
sudo bananagit-update updates status
sudo bananagit-update updates disable
```

`bananagit-update.timer` runs `update --automatic`, which does nothing while
the service was stopped on purpose and never retries a release that failed
(run `update --retry-failed` yourself). Enabling them requires signature
checks; `--allow-unsigned` accepts unsigned releases, and then anyone who can
push tags to the source can run code on the server as root.
`ALLOWED_SIGNERS` holds the maintainers' lines (`email ssh-ed25519 AAAA…`);
verify the fingerprints through a second channel.

## Same as BananaWiki, and different

Same: release tags only, highest that verifies; SSH-signed annotated tags
checked against an allowed-signers file with OpenPGP disabled; unsigned tags
skipped and reported; backup before the switch; health check and rollback;
failed releases not retried automatically; timer off by default and refusing
to start without signatures; one JSON result per run.

Different, because BananaGit is one Go binary with a database:

| BananaWiki | BananaGit |
| --- | --- |
| Deploys the source at the tag (`git archive`) and builds a virtual environment from hash-pinned wheels | Downloads a built binary; its SHA-256 is in the signed tag message. Dependencies are pinned at build time by `go.sum` and `package-lock.json` |
| Tags `vX.Y.Z` | Tags `vX.Y.Z.N` (Gitea's version plus our counter) |
| Requires a fast-forward of the installed commit | Requires a higher version; never downgrades |
| Live snapshot of files, short stop to refresh it | Stops first, then copies the database and runs `dump`; downtime includes the backup |
| Rollback restores the snapshot and the old release | Rollback restores the database copy and the old binary. Repositories and attachments are not touched by an update, so they are not restored |
| One source, with token or SSH key for private ones | Several public HTTPS sources tried in order (GitHub, mirror) |
| Encrypted backups to a private Git repository | Not included: `dump` archives stay on the server; copy them elsewhere yourself |
| The new release runs its own preflight | No preflight |

MySQL servers are refused before anything stops: the updater restores only
SQLite and PostgreSQL.

## Installing the updater

On a server that runs BananaGit under systemd:

```sh
sudo mkdir -p /opt/bananagit/{config,releases/1.27.3.1,backups}
sudo cp bananagit-1.27.3.1-linux-amd64 /opt/bananagit/releases/1.27.3.1/bananagit
sudo ln -s releases/1.27.3.1/bananagit /opt/bananagit/current
sudo cp -r bananagit/updater bananagit/bananagit-update /opt/bananagit/
sudo ln -s /opt/bananagit/bananagit-update /usr/local/bin/bananagit-update
```

Write `/opt/bananagit/config/settings.json` with what differs from the
defaults and with the installed version, for example:

```json
{"version": "1.27.3.1", "user": "git", "app_ini": "/etc/gitea/app.ini"}
```

| Key | Default | Meaning |
| --- | --- | --- |
| `version` | (empty) | The installed BananaGit version; the updater writes it after each update |
| `urls` | GitHub, then the mirror | Update sources (HTTPS Git URLs), tried in order |
| `service` | `bananagit` | The systemd service to stop and start |
| `user` | `git` | The account the service runs as (runs `dump`) |
| `app_ini` | `/etc/bananagit/app.ini` | The configuration file; its `[database]` section says what to back up |
| `health_url` | `http://127.0.0.1:3000/api/healthz` | Must report `pass` after an update |
| `health_timeout` | `180` | Seconds to wait for it |

Point the service's `ExecStart` at `/opt/bananagit/current web --config …`.

## Files the updater keeps

Under `/opt/bananagit/config/`: `settings.json` (above), `updates.json` (the
automatic-update policy), `allowed_signers` (trusted keys), `failed.json` (the
release that last failed, and why) and `history.jsonl` (one line per run).

## When something goes wrong

* **`rolled_back`:** the old release runs again with the database as it was.
  The reason is in the result and in `failed.json`; the service's own log
  (`journalctl -u bananagit`) says why the new release did not start.
  Automatic updates skip that release until a newer one appears or you run
  `sudo bananagit-update update --retry-failed`.
* **Rolling back by hand:** stop the service, point `current` at the previous
  binary in `releases/`, restore `backups/before-update-<time>/database.*`
  (copy the SQLite file back, or `pg_restore --clean`), start the service and
  set `version` in `settings.json` back.
* **`No release could be used`:** the message lists each source and why: no
  network, no `vX.Y.Z.N` tag yet, or tags that are not signed by a key in
  `allowed_signers`.
