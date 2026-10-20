#!/usr/bin/env python3
"""Fetch the original MiSans Web WOFF2 slices pinned in the application manifest.

Valid cached files never require network access. --source-dir copies a previously
fetched directory for offline builds; --output selects the destination directory.
Each replacement is checked before an atomic rename, so a failed refresh cannot
replace a usable font with an incomplete or unexpected response.
"""
from __future__ import annotations

import argparse
from concurrent.futures import ThreadPoolExecutor, as_completed
import hashlib
from http.client import HTTPException
import json
import os
from pathlib import Path
import re
import sys
import tempfile
import time
from typing import BinaryIO
from urllib.parse import urlparse
import urllib.request

REPO_ROOT = Path(__file__).resolve().parents[1]
DEFAULT_MANIFEST = REPO_ROOT / "frontend/public/fonts/misans-manifest.json"
DEFAULT_OUTPUT = REPO_ROOT / "frontend/public/fonts/misans"
MAX_FONT_BYTES = 2 * 1024 * 1024
SUPPORTED_WEIGHTS = {400, 500, 650, 700}
BASENAME = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]*\.woff2\Z")
SHA256 = re.compile(r"[0-9a-f]{64}\Z")
UNICODE_RANGE = re.compile(r"U\+[0-9a-fA-F]{1,6}(?:-[0-9a-fA-F]{1,6})?(?:,\s*U\+[0-9a-fA-F]{1,6}(?:-[0-9a-fA-F]{1,6})?)*\Z")


def load_manifest(path: Path) -> dict:
    manifest = json.loads(path.read_text(encoding="utf-8"))
    if (not isinstance(manifest, dict) or type(manifest.get("schemaVersion")) is not int
            or manifest["schemaVersion"] != 1):
        raise ValueError("Unsupported MiSans manifest schema")
    if not isinstance(manifest.get("version"), str) or not manifest["version"]:
        raise ValueError("Manifest must identify its pinned font version")
    if manifest.get("family") != "MiSans" or manifest.get("fontDisplay") != "optional":
        raise ValueError("Manifest must describe MiSans with optional font display")
    weights = manifest.get("weights")
    if (not isinstance(weights, list) or not weights
            or any(type(weight) is not int or weight not in SUPPORTED_WEIGHTS for weight in weights)
            or len(weights) != len(set(weights))):
        raise ValueError("Manifest has invalid CSS weights")
    files = manifest.get("files")
    if not isinstance(files, list) or not 1 <= len(files) <= 1024:
        raise ValueError("Manifest must contain a bounded, nonempty font file list")
    seen = set()
    for item in files:
        if not isinstance(item, dict):
            raise ValueError("Invalid font manifest entry")
        name = item.get("path")
        if not isinstance(name, str) or not BASENAME.fullmatch(name) or name in seen:
            raise ValueError(f"Unsafe or duplicate font basename: {name!r}")
        seen.add(name)
        if type(item.get("size")) is not int or not 4 <= item["size"] <= MAX_FONT_BYTES:
            raise ValueError(f"Invalid font size: {name}")
        if not isinstance(item.get("sha256"), str) or not SHA256.fullmatch(item["sha256"]):
            raise ValueError(f"Invalid SHA-256: {name}")
        weight = item.get("weight")
        if type(weight) is not int or weight not in weights:
            raise ValueError(f"Invalid CSS weight: {name}")
        if not isinstance(item.get("preload"), bool):
            raise ValueError(f"Invalid preload flag: {name}")
        if not isinstance(item.get("unicodeRange"), str) or not UNICODE_RANGE.fullmatch(item["unicodeRange"]):
            raise ValueError(f"Invalid unicode range: {name}")
        url = item.get("url")
        if not isinstance(url, str):
            raise ValueError(f"Invalid official font URL: {name}")
        parsed = urlparse(url)
        if (parsed.scheme != "https" or parsed.netloc != "cdn-file.hyperos.mi.com"
                or parsed.path != f"/mi-font-service/misans/{weight}/{name}"
                or parsed.params or parsed.query or parsed.fragment):
            raise ValueError(f"Unexpected official font URL: {name}")
    return manifest


def matches_font(path: Path, item: dict) -> bool:
    try:
        if path.is_symlink() or not path.is_file() or path.stat().st_size != item["size"]:
            return False
        # Bound the read even if a cached file changes while being checked.
        with path.open("rb") as handle:
            payload = handle.read(item["size"] + 1)
        return (len(payload) == item["size"] and payload[:4] == b"wOF2"
                and hashlib.sha256(payload).hexdigest() == item["sha256"])
    except OSError:
        return False


def write_verified(source: BinaryIO, output: Path, item: dict) -> None:
    target = output / item["path"]
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(dir=output, prefix=f".{item['path']}.", suffix=".tmp", delete=False) as handle:
            temporary = Path(handle.name)
            # Read at most one byte beyond the pinned size, never an unbounded response.
            payload = source.read(item["size"] + 1)
            if len(payload) != item["size"]:
                raise ValueError(f"Size mismatch for {item['path']}: expected {item['size']}, got {len(payload)}")
            if payload[:4] != b"wOF2":
                raise ValueError(f"Invalid WOFF2 signature: {item['path']}")
            if hashlib.sha256(payload).hexdigest() != item["sha256"]:
                raise ValueError(f"SHA-256 mismatch: {item['path']}")
            handle.write(payload)
        os.replace(temporary, target)
        temporary = None
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def ensure_font(item: dict, output: Path, *, source_dir: Path | None = None,
                force: bool = False, retries: int = 3, timeout: float = 45) -> bool:
    """Return True for an intact cached font, False for a verified replacement."""
    if not force and matches_font(output / item["path"], item):
        return True
    if source_dir is not None:
        with (source_dir / item["path"]).open("rb") as source:
            write_verified(source, output, item)
        return False
    for attempt in range(retries):
        try:
            request = urllib.request.Request(item["url"], headers={"Accept-Encoding": "identity"})
            with urllib.request.urlopen(request, timeout=timeout) as response:
                write_verified(response, output, item)
            return False
        except (OSError, ValueError, HTTPException) as error:
            if attempt == retries - 1:
                raise RuntimeError(f"Could not fetch {item['path']}: {error}") from error
            time.sleep(min(2 ** attempt, 4))
    raise AssertionError("At least one download attempt is required")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Fetch and verify pinned official MiSans WOFF2 slices")
    parser.add_argument("--manifest", type=Path, default=DEFAULT_MANIFEST)
    parser.add_argument("--output", type=Path, default=DEFAULT_OUTPUT, help="Destination directory")
    parser.add_argument("--source-dir", type=Path, help="Offline directory containing original WOFF2 files")
    parser.add_argument("--force", action="store_true", help="Refresh even intact cached files")
    parser.add_argument("--workers", type=int, default=6)
    parser.add_argument("--retries", type=int, default=3)
    parser.add_argument("--timeout", type=float, default=45)
    args = parser.parse_args(argv)
    if not 1 <= args.workers <= 16 or not 1 <= args.retries <= 5 or not 0 < args.timeout <= 120:
        parser.error("Use workers 1–16, retries 1–5, and timeout > 0 and <= 120 seconds")
    try:
        manifest = load_manifest(args.manifest)
        output = args.output.resolve()
        output.mkdir(parents=True, exist_ok=True)
        source_dir = args.source_dir.resolve() if args.source_dir is not None else None
        cached = 0
        errors = []
        with ThreadPoolExecutor(max_workers=args.workers) as pool:
            futures = {
                pool.submit(ensure_font, item, output, source_dir=source_dir,
                            force=args.force, retries=args.retries, timeout=args.timeout): item["path"]
                for item in manifest["files"]
            }
            for future in as_completed(futures):
                try:
                    cached += int(future.result())
                except (OSError, ValueError, RuntimeError) as error:
                    errors.append(f"{futures[future]}: {error}")
        if errors:
            raise RuntimeError("\n".join(sorted(errors)))
        count = len(manifest["files"])
        total = sum(item["size"] for item in manifest["files"])
        print(f"MiSans Web {manifest['version']}: {count} WOFF2 files verified, "
              f"{cached} cached, {count - cached} fetched, {total:,} bytes ({output})")
        return 0
    except (OSError, ValueError, KeyError, RuntimeError) as error:
        print(f"MiSans font fetch failed: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
