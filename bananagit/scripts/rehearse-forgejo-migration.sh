#!/bin/sh
# Rehearse moving a Forgejo server to BananaGit, on this machine, from a dump.
#
#   On the Forgejo server (as its user; works for SQLite, PostgreSQL and MySQL):
#     forgejo dump --config /etc/forgejo/app.ini --database sqlite3 --skip-log --file forgejo-dump.zip
#   Here:
#     bananagit/scripts/rehearse-forgejo-migration.sh forgejo-dump.zip [WORKDIR] [PORT]
#
# Unpacks the dump into WORKDIR, loads the database into SQLite, starts this
# checkout's BananaGit (./gitea, build it first) on 127.0.0.1:PORT and reports
# what the migration did. The copy cannot reach the outside: no mail, mirrors,
# webhooks, Actions or scheduled jobs. The live server is never contacted.
# It rehearses the data, not the live database engine: a PostgreSQL or MySQL
# server is rehearsed here on SQLite.
set -eu
dump=$(cd "$(dirname "$1")" && pwd)/$(basename "$1")
repo=$(git -C "$(dirname "$0")" rev-parse --show-toplevel)
work=${2:-$PWD/forgejo-rehearsal}
port=${3:-3090}
test -x "$repo/gitea" || { echo "Build BananaGit first: TAGS=\"sqlite sqlite_unlock_notify\" make build" >&2; exit 1; }
test ! -e "$work" || { echo "$work exists; remove it or name another folder." >&2; exit 1; }
mkdir -p "$work/custom/conf" && chmod 700 "$work" && cd "$work"
unzip -q "$dump"
test -f forgejo-db.sql || { echo "No forgejo-db.sql in the dump: was it made with 'forgejo dump'?" >&2; exit 1; }
mkdir -p data && rm -f data/rehearsal.db
sqlite3 data/rehearsal.db < forgejo-db.sql
counts() {
  for table in user repository issue pull_request comment release team webhook access_token public_key attachment package lfs_meta_object two_factor webauthn_credential notification watch star; do
    printf '%s=%s\n' "$table" "$(sqlite3 data/rehearsal.db "select count(*) from \`$table\`" 2>/dev/null || echo missing)"
  done
}
counts > before.txt
sqlite3 data/rehearsal.db < "$repo/bananagit/migration/forgejo-pre.sql"
sqlite3 data/rehearsal.db "select version from version" > version-before.txt
# The rehearsal's app.ini: the server's secrets (they decrypt two-factor and
# OAuth data), everything else local and unable to reach the outside.
python3 - "$work" "$port" "$repo" <<'EOF'
import configparser, sys
work, port, repo = sys.argv[1:]
old = configparser.ConfigParser(interpolation=None, strict=False)
old.read_string("[DEFAULT]\n" + open(f"{work}/app.ini").read())
keep = {section: dict(old[section]) for section in ("security", "oauth2") if old.has_section(section)}
for section in keep.values():
    for key in old.defaults():
        section.pop(key, None)
keep.setdefault("security", {}).update(install_lock="true", allowed_host_list="loopback")
new = {
    "": {"WORK_PATH": work, "RUN_MODE": "prod"},
    "server": {"HTTP_ADDR": "127.0.0.1", "HTTP_PORT": port, "ROOT_URL": f"http://127.0.0.1:{port}/",
               "DISABLE_SSH": "true", "STATIC_ROOT_PATH": repo, "APP_DATA_PATH": f"{work}/data", "OFFLINE_MODE": "true"},
    "database": {"DB_TYPE": "sqlite3", "PATH": f"{work}/data/rehearsal.db"},
    "repository": {"ROOT": f"{work}/repos"},
    **keep,
    "mailer": {"ENABLED": "false"}, "mirror": {"ENABLED": "false"}, "actions": {"ENABLED": "false"},
    "cron": {"ENABLED": "false"},
    "cron.update_checker": {"ENABLED": "false"}, "federation": {"ENABLED": "false"},
    "service": {"DISABLE_REGISTRATION": "true"}, "log": {"LEVEL": "Warn"},
}
with open(f"{work}/custom/conf/app.ini", "w") as out:
    for section, values in new.items():
        if section:
            out.write(f"\n[{section}]\n")
        for key, value in values.items():
            out.write(f"{key.upper()} = {value}\n")
EOF
chmod 600 custom/conf/app.ini
GITEA_WORK_DIR="$work" nohup "$repo/gitea" web --config "$work/custom/conf/app.ini" > "$work/server.log" 2>&1 &
echo $! > server.pid
healthy=no
for _ in $(seq 1 100); do
  if curl -fs -m 3 "http://127.0.0.1:$port/api/healthz" 2>/dev/null | grep -q '"status": "pass"'; then healthy=yes; break; fi
  kill -0 "$(cat server.pid)" 2>/dev/null || break
  sleep 3
done
if [ "$healthy" = yes ]; then sqlite3 data/rehearsal.db < "$repo/bananagit/migration/forgejo-post.sql"; fi
counts > after.txt
echo "== Rehearsal in $work"
echo "Health check: $healthy"
echo "Database version: $(cat version-before.txt) -> $(sqlite3 data/rehearsal.db 'select version from version')"
echo "== Rows before and after (they should match)"
paste -d' ' before.txt after.txt | awk '{split($1,a,"="); split($2,b,"="); printf "%-22s %8s %8s %s\n", a[1], a[2], b[2], (a[2]==b[2] ? "" : "<-- differs")}'
echo "== Errors in the log: $(grep -c -E '\[(E|F)\]' server.log || true)"
grep -E '\[(E|F)\]' server.log | sed -E 's/^[0-9/: ]+//' | cut -c1-220 | sort | uniq -c | sort -rn | head -15
if [ "$healthy" = yes ]; then
  echo "== Running at http://127.0.0.1:$port/ : sign in with a real account and look around."
  echo "   Stop it with: kill $(cat server.pid)"
else
  echo "== It did not start. The end of the log:"; tail -20 server.log
fi
