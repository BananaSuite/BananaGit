"""Find, verify and download BananaGit releases.

A release is a tag ``vX.Y.Z.N`` (Gitea's version plus BananaGit's own
counter, so it never collides with an upstream tag) on the update source: the GitHub repository or
its mirror on our Forgejo. Any Git URL works; several are tried in order.

With signatures required, a release is an annotated tag SSH-signed by a key
in the allowed-signers file, for exactly its own name. The tag message lists
the SHA-256 of every binary of the release, so the signature covers the
binary that gets installed and not only the source it was built from.
"""

from __future__ import annotations

import hashlib
import os
import re
import subprocess
import urllib.request
from pathlib import Path

RELEASE_TAG = re.compile(r"v(\d{1,9})\.(\d{1,9})\.(\d{1,9})\.(\d{1,9})")
CHECKSUM_LINE = re.compile(r"sha256 ([0-9a-f]{64})  ([A-Za-z0-9._-]+)")
MAX_CANDIDATES = 20
MAX_BINARY_BYTES = 512 * 1024 * 1024
_SSH_ONLY = ("-c", "gpg.format=ssh", "-c", "gpg.openpgp.program=false", "-c", "gpg.x509.program=false")
_SSH_SIGNATURE = "-----BEGIN SSH SIGNATURE-----"


class UpdateError(RuntimeError):
    """The update cannot go on; nothing was changed unless the message says so."""


def version_of(tag: str) -> tuple[int, int, int, int] | None:
    match = RELEASE_TAG.fullmatch(tag)
    return tuple(int(part or 0) for part in match.groups()) if match else None  # type: ignore[return-value]


def valid_url(url: str) -> str:
    if not re.fullmatch(r"https://[A-Za-z0-9.-]+(:\d+)?/[A-Za-z0-9._/-]+", url) or ".." in url:
        raise UpdateError(f"Use an HTTPS repository URL without credentials: {url!r}")
    return url.removesuffix("/")


class Source:
    """The update source: repository URLs and the bare cache tags are fetched into."""

    def __init__(self, root: Path, urls: list[str], signers: Path | None):
        self.root, self.urls, self.signers = Path(root), [valid_url(url) for url in urls], signers
        self.git_dir = self.root / "repository.git"
        self.skipped: list[str] = []

    def git(self, *arguments: str, check: bool = True) -> subprocess.CompletedProcess:
        environment = {"PATH": os.environ.get("PATH", "/usr/bin:/bin"), "LANG": "C.UTF-8", "GIT_TERMINAL_PROMPT": "0",
                       "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null", "HOME": str(self.root / "config")}
        command = ["git", "-c", "core.hooksPath=/dev/null", "-c", "credential.helper=", "-c", "protocol.ext.allow=never",
                   "-c", "http.followRedirects=false", "-c", "transfer.fsckObjects=true",
                   "--git-dir", str(self.git_dir), *arguments]
        result = subprocess.run(command, env=environment, capture_output=True, text=True, timeout=300, check=False)
        if check and result.returncode:
            raise UpdateError(f"Git {arguments[0]} failed. Check the update source and the network.")
        return result

    def initialize(self) -> None:
        if not self.git_dir.exists():
            self.git_dir.mkdir(mode=0o700, parents=True)
            self.git("init", "--bare", "--quiet")

    def tags(self, url: str) -> list[str]:
        """Release tags of *url*, highest first."""
        listing = self.git("ls-remote", "--tags", "--refs", url, "refs/tags/v*").stdout
        names = [line.partition("\trefs/tags/")[2] for line in listing.splitlines()]
        return sorted((name for name in names if version_of(name)), key=version_of, reverse=True)

    def verify(self, url: str, tag: str) -> str:
        """Fetch *tag*, check its signature (when required) and return the tag message."""
        self.git("fetch", "--quiet", "--no-tags", "--depth", "1", url, "refs/tags/" + tag)
        tag_object = self.git("rev-parse", "FETCH_HEAD").stdout.strip()
        if self.git("cat-file", "-t", tag_object).stdout.strip() != "tag":
            raise UpdateError(f"Release {tag} is a lightweight tag: a release is an annotated tag whose message "
                              "lists the checksums of its binaries.")
        body = self.git("cat-file", "tag", tag_object).stdout
        if "tag " + tag not in body.split("\n\n", 1)[0].splitlines():
            raise UpdateError(f"Release {tag} is a tag object made for another name, so it releases nothing.")
        if self.signers is None:
            return body
        blocks = [line for line in body.splitlines() if line.startswith("-----BEGIN ")]
        if not blocks or blocks[-1] != _SSH_SIGNATURE:
            raise UpdateError(f"Release {tag} is not SSH-signed (git tag -s with gpg.format=ssh).")
        if self.git(*_SSH_ONLY, "-c", f"gpg.ssh.allowedSignersFile={self.signers}", "verify-tag", tag_object,
                    check=False).returncode:
            raise UpdateError(f"Release {tag} is not signed by a key in the allowed signers file.")
        return body

    def latest(self) -> tuple[str, str, str]:
        """``(url, tag, tag message)`` of the highest release that verifies, from the first source that answers."""
        self.initialize()
        problems = []
        for url in self.urls:
            try:
                tags = self.tags(url)
            except (UpdateError, subprocess.TimeoutExpired) as error:
                problems.append(f"{url}: {error}")
                continue
            if not tags:
                problems.append(f"{url}: no release tag (vX.Y.Z.N)")
                continue
            self.skipped = []
            for tag in tags[:MAX_CANDIDATES]:
                try:
                    return url, tag, self.verify(url, tag)
                except UpdateError as error:
                    self.skipped.append(tag)
                    problems.append(f"{url}: {error}")
        raise UpdateError("No release could be used. " + " | ".join(problems[:5]))


def asset_name(tag: str, platform: str) -> str:
    return f"bananagit-{tag.removeprefix('v')}-{platform}"


def expected_checksum(message: str, asset: str) -> str:
    for digest, name in CHECKSUM_LINE.findall(message):
        if name == asset:
            return digest
    raise UpdateError(f"The release's tag message has no checksum for {asset}.")


def download(url: str, tag: str, asset: str, digest: str, destination: Path) -> None:
    """Download *asset* of release *tag* (GitHub and Forgejo share the URL layout) and check its SHA-256."""
    address = f"{url.removesuffix('.git')}/releases/download/{tag}/{asset}"
    sha, size = hashlib.sha256(), 0
    request = urllib.request.Request(address, headers={"User-Agent": "bananagit-updater"})
    with urllib.request.urlopen(request, timeout=60) as response, destination.open("wb") as target:  # noqa: S310
        while chunk := response.read(1 << 20):
            size += len(chunk)
            if size > MAX_BINARY_BYTES:
                raise UpdateError("The release binary is larger than expected.")
            sha.update(chunk)
            target.write(chunk)
    if sha.hexdigest() != digest:
        destination.unlink(missing_ok=True)
        raise UpdateError(f"{asset} does not match the checksum in the release tag. Nothing was installed.")
    destination.chmod(0o755)
