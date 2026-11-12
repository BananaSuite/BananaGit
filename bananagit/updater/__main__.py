"""``bananagit-update``: the command-line interface (see bananagit/docs/updating.md)."""

from __future__ import annotations

import argparse
import json
import shutil
import subprocess
import sys
from pathlib import Path

from .manager import Manager, _write
from .release import UpdateError

UNIT = """# Managed by BananaSuite
[Unit]
Description=Optional BananaGit update
After=network-online.target

[Service]
Type=oneshot
ExecStart={command} --root {root} update --automatic
TimeoutStartSec=3600
"""
TIMER = """# Managed by BananaSuite
[Unit]
Description=Optional BananaGit update schedule

[Timer]
OnBootSec=5min
OnUnitActiveSec={interval}min
RandomizedDelaySec=120
Persistent=true

[Install]
WantedBy=timers.target
"""


def set_updates(manager: Manager, args: argparse.Namespace) -> dict:
    policy = manager.policy()
    if args.action == "status":
        return {**policy, "signatures_required": manager.signers() is not None}
    if args.require_signatures:
        shutil.copyfile(args.require_signatures, manager.config / "allowed_signers")
        (manager.config / "allowed_signers").chmod(0o600)
    enabled = args.action == "enable"
    if enabled and manager.signers() is None and not args.allow_unsigned:
        raise UpdateError("Automatic updates install new code as root with nobody reviewing it, so they require "
                          "signed releases: add --require-signatures ALLOWED_SIGNERS. To accept unsigned releases "
                          "anyway, add --allow-unsigned.")
    if args.interval is not None:
        if not 5 <= args.interval <= 10080:
            raise UpdateError("Choose a check interval between 5 minutes and one week.")
        policy["interval_minutes"] = args.interval
    if args.keep_backups is not None:
        if not 2 <= args.keep_backups <= 30:
            raise UpdateError("Keep between 2 and 30 backups.")
        policy["keep_backups"] = args.keep_backups
    policy.update(enabled=enabled, unsigned=enabled and manager.signers() is None)
    _write(manager.config / "updates.json", policy)
    units = Path("/etc/systemd/system")
    (units / "bananagit-update.service").write_text(UNIT.format(command=Path(sys.argv[0]).resolve(), root=manager.root))
    (units / "bananagit-update.timer").write_text(TIMER.format(interval=policy["interval_minutes"]))
    subprocess.run(["systemctl", "daemon-reload"], check=True)
    subprocess.run(["systemctl", "enable" if enabled else "disable", "--now", "bananagit-update.timer"], check=True)
    return policy


def main() -> int:
    parser = argparse.ArgumentParser(prog="bananagit-update", description=(
        "BananaGit updates. Automatic updates stay disabled until explicitly enabled."))
    parser.add_argument("--root", default="/opt/bananagit")
    commands = parser.add_subparsers(dest="command", required=True)
    commands.add_parser("check", help="Show what update would install, without installing")
    update = commands.add_parser("update", help="Back up, install the latest release, check health, roll back on failure")
    update.add_argument("--automatic", action="store_true", help=argparse.SUPPRESS)
    update.add_argument("--retry-failed", action="store_true")
    updates = commands.add_parser("updates", help="Opt into or out of automatic updates")
    updates.add_argument("action", choices=("status", "enable", "disable"))
    updates.add_argument("--interval", type=int, help="Check interval in minutes (5 to 10080)")
    updates.add_argument("--keep-backups", type=int, help="Pre-update backups to keep (2 to 30)")
    updates.add_argument("--require-signatures", type=Path, metavar="ALLOWED_SIGNERS")
    updates.add_argument("--allow-unsigned", action="store_true")
    args = parser.parse_args()
    manager = Manager(args.root)
    try:
        if args.command == "check":
            result = manager.check()
        elif args.command == "update":
            result = manager.update(automatic=args.automatic, retry_failed=args.retry_failed)
        else:
            result = set_updates(manager, args)
    except (UpdateError, subprocess.SubprocessError, OSError) as error:
        print(f"bananagit-update: {error}", file=sys.stderr)
        return 1
    print(json.dumps(result, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
