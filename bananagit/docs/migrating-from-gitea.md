# Moving a Gitea server to BananaGit

BananaGit is Gitea with other defaults, so a Gitea server becomes a BananaGit
server by swapping the binary. The database, `app.ini`, repositories and data
directory stay where they are and are used as they are. Coming from Forgejo is
a different matter: see [migrating-from-forgejo.md](migrating-from-forgejo.md).

This follows from the code being the same; it has not been rehearsed on a real
Gitea server the way the Forgejo move was. Take the backup in step 1.

## Which release

Pick a BananaGit release whose Gitea version (the first three numbers of
`vX.Y.Z.N`) is **the same as or newer than** the Gitea you run. Never older:
database migrations only go forward and an older binary refuses a newer
database. A newer one migrates the database as it starts, exactly as a Gitea
upgrade would; read Gitea's release notes for the versions you skip.

Release binaries are built without SQLite unless a maintainer builds them with
it. A server on SQLite needs a binary built with `TAGS="sqlite sqlite_unlock_notify"`
(see the README).

## Steps

1. **Back up.** Stop Gitea, then keep a `gitea dump` or a copy of the
   database, `app.ini` and the data directory, and keep the Gitea binary.
2. **Put the BananaGit binary in place** as in the README's install steps 2
   and 3 (`/opt/bananagit/releases/<version>/bananagit` and the `current` link).
3. **Point the service at it.** Either change `ExecStart` in your existing
   unit to `/opt/bananagit/current web --config <your app.ini>` and keep its
   name, user and paths, or install `bananagit/contrib/bananagit.service` and
   set `User`, `GITEA_WORK_DIR` and `--config` to what Gitea used.
4. **Start it** and check `/api/healthz`, sign in, open a repository, clone
   and push.
5. **Install the updater** ([updating.md](updating.md)). In
   `/opt/bananagit/config/settings.json` set `service`, `user` and `app_ini`
   to the ones you kept, for example:

   ```json
   {"version": "1.27.3.1", "service": "gitea", "user": "git", "app_ini": "/etc/gitea/app.ini"}
   ```

## What changes for users

* **Name.** If `app.ini` sets `APP_NAME`, nothing changes. If it does not, the
  site is now called "BananaGit".
* **Theme.** Existing accounts keep the theme they have (Gitea stores one per
  account when it is created, and the Gitea themes are all still there); they
  can switch to a BananaGit theme in their settings. New accounts and
  signed-out visitors get `bananagit-auto`, unless `app.ini` sets
  `[ui] DEFAULT_THEME`.
* **Logo.** The banana, unless you have your own in `custom/public/assets/img/`.
* Anything in `custom/` (templates, public files) keeps working.

## Going back to Gitea

Stop the service, point it at the Gitea binary of **the same Gitea version**
as the BananaGit release you ran (the database is identical) and start it.
Users who picked a `bananagit-*` theme fall back to Gitea's default.
