# Moving a Forgejo server to BananaGit

BananaGit is based on Gitea. Forgejo was forked from Gitea 1.22 and has
changed its database on its own since, and neither project supports opening a
recent Forgejo database with Gitea. It can still be done in place, and this
page says what was tested, what is lost and how to rehearse it. **Rehearse on
a copy of the real server before touching it: the move is one-way.** Going
back means restoring the Forgejo backup taken in step 1.

## What was tested

On 2026-11-12, a `forgejo dump` of a Forgejo 16.0.7 test instance was moved to
BananaGit (Gitea 1.27.3) with `bananagit/scripts/rehearse-forgejo-migration.sh`.
The instance held two users, an organisation with a team and a private
repository, a repository with a merged and an open pull request, a review,
comments, a label, a milestone, a release, a wiki, a webhook, a branch
protection, an access token, an SSH key, a star, watches, a user block and a
generic package.

* **Without preparation the move fails.** Forgejo dropped two columns of the
  `notification` table; as soon as that table has rows, Gitea's migration 309
  cannot add them back and BananaGit does not start. `migration/forgejo-pre.sql`
  adds them first.
* With it, Gitea ran its migrations (database version 305 to 343), the health
  check passed, every row count matched and nothing was logged as an error.
* Everything listed above was read back through the API and the web pages,
  the package file downloaded, and `git clone` returned all commits.
* `forgejo-pre.sql` also turns webhooks of type `forgejo` into `gitea` and
  resets `forgejo-*` themes to the default; `migration/forgejo-post.sql`
  copies user blocks into Gitea's table.

Not tested: a database migrated on PostgreSQL or MySQL itself (the rehearsal
loads any server's dump into SQLite), Actions runs and runners, LFS, two-factor
and passkeys, OAuth applications, mirrors, SSH access, and anything at the
size of the real server. The rehearsal on the real dump is how you find out
what else needs fixing.

## What does not come along

Gitea ignores what Forgejo added. After the move the data is still in the
database, but nothing uses it:

* Federation, quotas, repository flags and abuse reports (tables `federat*`,
  `quota_*`, `forgejo_repo_flag`, `abuse_report*`).
* "Remember this device" sign-ins (`forgejo_auth_token`): everyone signs in
  again.
* Pronouns, per-kind watch choices (issues, pull requests, releases), access
  tokens limited to chosen repositories, a wiki on a branch other than the
  default one, and Forgejo's extra details on Actions runs (columns Gitea does
  not read).
* Forgejo-only `app.ini` settings are ignored.

## Steps

1. **Back up Forgejo.** Stop it, then copy the database (SQLite file, or
   `pg_dump -Fc`), `app.ini` and the data directory, and keep the Forgejo
   binary. This is the only way back.
2. **Rehearse on a copy.** On the server, as Forgejo's user:

   ```sh
   forgejo dump --config /etc/forgejo/app.ini --database sqlite3 --skip-log --file forgejo-dump.zip
   ```

   Copy the file to a machine with a BananaGit checkout and run:

   ```sh
   bananagit/scripts/rehearse-forgejo-migration.sh forgejo-dump.zip
   ```

   It starts a private copy on `http://127.0.0.1:3090` that cannot send mail,
   mirror, call webhooks or run Actions, and prints the row counts before and
   after and every error. Sign in and look around. The dump holds every
   repository, password hash and secret of the server: keep it private and
   delete it and the rehearsal folder afterwards.
3. **Prepare the live database** (service stopped):

   ```sh
   sqlite3 /var/lib/forgejo/data/forgejo.db < bananagit/migration/forgejo-pre.sql   # or: psql -d forgejo -f …
   ```

4. **Settle the paths in `app.ini`** so BananaGit finds what Forgejo wrote:

   ```ini
   [repository]
   ROOT = /var/lib/forgejo/data/forgejo-repositories   ; wherever they are now
   ```

5. **Start BananaGit** with the same `app.ini`, user and data directory. It
   migrates the database as it starts; read the log to the end of the start.
6. **Run `migration/forgejo-post.sql`** the same way as step 3.
7. **Check** `/api/healthz`, sign in, open a repository, an issue and a pull
   request, clone and push over HTTP and SSH, run an Actions job if you use
   them, and look for `[E]` lines in the log. Then install the updater
   ([updating.md](updating.md)).
