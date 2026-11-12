"""Install a release: back up, switch the binary, check health, roll back on failure.

Layout under the root (``/opt/bananagit`` by default)::

    config/      settings.json, updates.json, allowed_signers, failed.json, history.jsonl
    releases/    one binary per installed version
    current ->   the running binary (the systemd unit's ExecStart)
    backups/     before-update-<time>/ : the database copy and a `dump` archive
"""

from __future__ import annotations

import configparser
import json
import os
import platform as host
import shutil
import subprocess
import time
import urllib.request
from pathlib import Path

from .release import Source, UpdateError, asset_name, download, expected_checksum, version_of

DEFAULTS = {
    "urls": ["https://github.com/BananaSuite/BananaGit.git",
             "https://git.officinatecnologica.eu/BananaSuite/BananaGit.git"],
    "service": "bananagit", "user": "git", "app_ini": "/etc/bananagit/app.ini",
    "health_url": "http://127.0.0.1:3000/api/healthz", "health_timeout": 180, "version": "",
}


def _read(path: Path, default: dict) -> dict:
    return {**default, **json.loads(path.read_text())} if path.exists() else dict(default)


def _write(path: Path, value: dict) -> None:
    temporary = path.with_suffix(".tmp")
    temporary.write_text(json.dumps(value, indent=2) + "\n")
    temporary.chmod(0o600)
    temporary.replace(path)


class Manager:
    def __init__(self, root: str | os.PathLike[str]):
        self.root = Path(root)
        self.config = self.root / "config"

    def settings(self) -> dict:
        return _read(self.config / "settings.json", DEFAULTS)

    def policy(self) -> dict:
        return _read(self.config / "updates.json", {"enabled": False, "interval_minutes": 60, "keep_backups": 3,
                                                    "unsigned": False})

    def signers(self) -> Path | None:
        path = self.config / "allowed_signers"
        return path if path.exists() else None

    def event(self, outcome: str, **details) -> dict:
        record = {"time": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), "outcome": outcome, **details}
        with (self.config / "history.jsonl").open("a") as log:
            log.write(json.dumps(record) + "\n")
        return record

    # The service

    def run(self, *command: str, user: str | None = None, check: bool = True) -> subprocess.CompletedProcess:
        prefix = ["sudo", "-u", user, "--"] if user else []
        return subprocess.run([*prefix, *command], capture_output=True, text=True, timeout=3600, check=check)

    def healthy(self, settings: dict) -> bool:
        deadline = time.monotonic() + int(settings["health_timeout"])
        while time.monotonic() < deadline:
            try:
                with urllib.request.urlopen(settings["health_url"], timeout=5) as response:  # noqa: S310
                    if response.status == 200 and json.load(response).get("status") == "pass":
                        return True
            except (OSError, ValueError):
                pass
            time.sleep(3)
        return False

    def switch(self, version: str) -> None:
        link = self.root / "current.new"
        link.unlink(missing_ok=True)
        link.symlink_to(Path("releases") / version / "bananagit")
        link.replace(self.root / "current")

    # Backup and restore of the database

    def database(self, settings: dict) -> dict:
        parser = configparser.ConfigParser(interpolation=None, strict=False)
        parser.read_string("[DEFAULT]\n" + Path(settings["app_ini"]).read_text())
        return dict(parser["database"]) if parser.has_section("database") else {}

    def backup(self, settings: dict, binary: Path) -> Path:
        """With the service stopped: a restorable copy of the database, then Gitea's own full dump."""
        folder = self.root / "backups" / ("before-update-" + time.strftime("%Y%m%d-%H%M%S", time.gmtime()))
        folder.mkdir(mode=0o700, parents=True)
        database = self.database(settings)
        kind = database.get("db_type", "sqlite3")
        if kind == "sqlite3":
            shutil.copy2(database["path"], folder / "database.sqlite3")
        elif kind == "postgres":
            with (folder / "database.pgdump").open("wb") as target:
                subprocess.run(["sudo", "-u", "postgres", "pg_dump", "-Fc", database["name"]], stdout=target,
                               check=True, timeout=3600)
        else:
            raise UpdateError(f"Automatic rollback knows SQLite and PostgreSQL, not {kind}. Nothing was changed.")
        shutil.chown(folder, settings["user"])
        self.run(str(binary), "dump", "--config", settings["app_ini"], "--file", str(folder / "dump.zip"),
                 "--skip-log", user=settings["user"])
        return folder

    def restore_database(self, settings: dict, folder: Path) -> None:
        database = self.database(settings)
        if (folder / "database.sqlite3").exists():
            shutil.copy2(folder / "database.sqlite3", database["path"])
            shutil.chown(database["path"], settings["user"])
        else:
            self.run("pg_restore", "--clean", "--if-exists", "-d", database["name"], str(folder / "database.pgdump"),
                     user="postgres")

    def prune(self, keep: int, protect: Path) -> None:
        folders = sorted((self.root / "backups").glob("before-update-*"), reverse=True)
        for folder in folders[keep:]:
            if folder != protect and folder.is_dir() and not folder.is_symlink():
                shutil.rmtree(folder)

    # Update

    def check(self) -> dict:
        settings = self.settings()
        source = Source(self.root, settings["urls"], self.signers())
        url, tag, _message = source.latest()
        return {"installed": settings["version"], "latest": tag, "source": url, "skipped_releases": source.skipped,
                "update": (version_of(tag) or ()) > (version_of("v" + settings["version"]) or ())}

    def update(self, *, automatic: bool = False, retry_failed: bool = False) -> dict:
        settings, policy = self.settings(), self.policy()
        if automatic and not policy["enabled"]:
            return {"outcome": "disabled"}
        service = settings["service"]
        if automatic and self.run("systemctl", "is-active", "--quiet", service, check=False).returncode:
            return self.event("skipped", reason="The service was stopped on purpose.")
        source = Source(self.root, settings["urls"], self.signers())
        url, tag, message = source.latest()
        version, installed = tag.removeprefix("v"), settings["version"]
        if (version_of(tag) or ()) <= (version_of("v" + installed) or ()):
            # Never downgrade: Gitea's database migrations only go forward.
            return self.event("current", version=installed, latest=tag, skipped_releases=source.skipped)
        failed = _read(self.config / "failed.json", {})
        if automatic and failed.get("version") == version and not retry_failed:
            return self.event("paused", version=version, reason=f"This release failed before: {failed.get('reason')}")
        kind = self.database(settings).get("db_type", "sqlite3")
        if kind not in ("sqlite3", "postgres"):
            raise UpdateError(f"Automatic rollback knows SQLite and PostgreSQL, not {kind}. Nothing was changed.")
        machine = {"x86_64": "amd64", "aarch64": "arm64", "arm64": "arm64"}.get(host.machine(), host.machine())
        asset = asset_name(tag, f"{host.system().lower()}-{machine}")
        release = self.root / "releases" / version
        release.mkdir(mode=0o755, parents=True, exist_ok=True)
        try:
            # Prepared next to the running release; the service keeps serving.
            download(url, tag, asset, expected_checksum(message, asset), release / "bananagit")
        except Exception as error:
            shutil.rmtree(release, ignore_errors=True)
            self.event("failed", version=version, reason=str(error))
            raise
        previous = (self.root / "current").resolve()
        self.run("systemctl", "stop", service)
        try:
            backup = self.backup(settings, previous)
        except Exception as error:
            self.run("systemctl", "start", service)  # nothing was switched
            self.event("failed", version=version, reason=f"Backup failed: {error}")
            raise
        try:
            self.switch(version)
            self.run("systemctl", "start", service)  # the new binary migrates the database as it starts
            if not self.healthy(settings):
                raise UpdateError("The new release did not pass its health check.")
        except Exception as error:
            self.run("systemctl", "stop", service, check=False)
            self.switch(previous.parent.name)
            self.restore_database(settings, backup)
            self.run("systemctl", "start", service)
            _write(self.config / "failed.json", {"version": version, "reason": str(error)[:2000]})
            self.event("rolled_back", version=version, restored_version=installed, backup=str(backup),
                       reason=str(error)[:2000], healthy=self.healthy(settings))
            raise
        (self.config / "failed.json").unlink(missing_ok=True)
        _write(self.config / "settings.json", {**settings, "version": version})
        self.prune(int(policy["keep_backups"]), backup)
        for old in (self.root / "releases").iterdir():
            if old.name not in {version, installed}:
                shutil.rmtree(old, ignore_errors=True)
        return self.event("complete", version=version, previous_version=installed, source=url, backup=str(backup),
                          skipped_releases=source.skipped)
