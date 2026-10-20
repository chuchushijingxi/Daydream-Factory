"""Network-free integrity and recovery checks for the pinned MiSans fetcher.

The tiny wOF2-prefixed payloads exercise transport validation, not font rendering.
Run: python3 -m unittest discover -s scripts -p 'test_fetch_misans.py' -v
"""
from __future__ import annotations

import contextlib
import copy
import hashlib
from http.client import IncompleteRead
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from urllib.error import URLError

spec = importlib.util.spec_from_file_location("fetch_misans", Path(__file__).with_name("fetch-misans.py"))
fetch = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fetch)


class FetchMiSansTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.output = self.root / "output"
        self.source = self.root / "source"
        self.source.mkdir()
        self.payload = b"wOF2fixture-pinned-original"
        self.item = {
            "path": "cs.fixture.0.woff2",
            "url": "https://cdn-file.hyperos.mi.com/mi-font-service/misans/400/cs.fixture.0.woff2",
            "size": len(self.payload),
            "sha256": hashlib.sha256(self.payload).hexdigest(),
            "weight": 400,
            "unicodeRange": "U+20,U+4e00-4e01",
            "preload": True,
        }
        self.manifest = {
            "schemaVersion": 1,
            "family": "MiSans",
            "version": "4.003",
            "fontDisplay": "optional",
            "weights": [400],
            "files": [self.item],
        }
        self.manifest_path = self.root / "manifest.json"
        self.write_manifest()
        self.source_file = self.source / self.item["path"]
        self.source_file.write_bytes(self.payload)
        self.target = self.output / self.item["path"]

    def write_manifest(self):
        self.manifest_path.write_text(json.dumps(self.manifest), encoding="utf-8")

    def run_fetch(self, *extra):
        stdout, stderr = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            result = fetch.main(["--manifest", str(self.manifest_path), "--output", str(self.output), *extra])
        return result, stdout.getvalue(), stderr.getvalue()

    def assert_no_temporary_files(self):
        self.assertEqual(list(self.output.glob(".*.tmp")), [])

    def test_offline_copy_is_verified_and_cached_without_source_or_network(self):
        with patch.object(fetch.urllib.request, "urlopen", side_effect=AssertionError("network forbidden")):
            status, stdout, stderr = self.run_fetch("--source-dir", str(self.source))
            self.assertEqual((status, stderr), (0, ""))
            self.assertIn("0 cached, 1 fetched", stdout)
            self.assertEqual(self.target.read_bytes(), self.payload)
            self.source_file.unlink()
            status, stdout, stderr = self.run_fetch()
            self.assertEqual((status, stderr), (0, ""))
            self.assertIn("1 cached, 0 fetched", stdout)
        self.assert_no_temporary_files()

    def test_same_size_corrupt_cache_is_recovered_from_offline_source(self):
        self.output.mkdir()
        self.target.write_bytes(b"x" * len(self.payload))
        status, _, stderr = self.run_fetch("--source-dir", str(self.source))
        self.assertEqual((status, stderr), (0, ""))
        self.assertEqual(self.target.read_bytes(), self.payload)
        self.assert_no_temporary_files()

    def test_missing_offline_resource_fails_without_creating_a_font(self):
        self.source_file.unlink()
        status, _, stderr = self.run_fetch("--source-dir", str(self.source))
        self.assertEqual(status, 1)
        self.assertIn(self.item["path"], stderr)
        self.assertFalse(self.target.exists())
        self.assert_no_temporary_files()

    def test_failed_forced_refresh_preserves_existing_valid_file(self):
        self.output.mkdir()
        self.target.write_bytes(self.payload)
        self.source_file.write_bytes(b"x" * len(self.payload))
        status, _, stderr = self.run_fetch("--source-dir", str(self.source), "--force")
        self.assertEqual(status, 1)
        self.assertIn("WOFF2 signature", stderr)
        self.assertEqual(self.target.read_bytes(), self.payload)
        self.assert_no_temporary_files()

    def test_invalid_download_never_replaces_existing_file(self):
        self.output.mkdir()
        self.target.write_bytes(self.payload)
        candidates = {
            "oversize": self.payload + b"unbounded response",
            "truncated": self.payload[:-1],
            "signature": b"OTTO" + self.payload[4:],
            "digest": self.payload[:-1] + b"X",
        }
        for label, payload in candidates.items():
            with self.subTest(label=label):
                with patch.object(fetch.urllib.request, "urlopen", return_value=io.BytesIO(payload)):
                    with self.assertRaises(RuntimeError):
                        fetch.ensure_font(self.item, self.output, force=True, retries=1)
                self.assertEqual(self.target.read_bytes(), self.payload)
                self.assert_no_temporary_files()

    def test_transient_network_failure_retries_then_verifies(self):
        self.output.mkdir()
        with patch.object(fetch.urllib.request, "urlopen", side_effect=[URLError("temporary"), io.BytesIO(self.payload)]) as request:
            with patch.object(fetch.time, "sleep") as sleep:
                cached = fetch.ensure_font(self.item, self.output, retries=2, timeout=7)
        self.assertFalse(cached)
        self.assertEqual(request.call_count, 2)
        self.assertEqual(request.call_args.kwargs["timeout"], 7)
        sleep.assert_called_once_with(1)
        self.assertEqual(self.target.read_bytes(), self.payload)

    def test_network_failure_keeps_valid_cache_when_forced(self):
        self.output.mkdir()
        self.target.write_bytes(self.payload)
        with patch.object(fetch.urllib.request, "urlopen", side_effect=URLError("offline")) as request:
            with patch.object(fetch.time, "sleep"):
                with self.assertRaises(RuntimeError):
                    fetch.ensure_font(self.item, self.output, force=True, retries=2)
        self.assertEqual(request.call_count, 2)
        self.assertEqual(self.target.read_bytes(), self.payload)
        self.assert_no_temporary_files()

    def test_incomplete_http_response_is_retried_and_temporary_file_removed(self):
        self.output.mkdir()
        response = io.BytesIO(self.payload)
        with patch.object(response, "read", side_effect=IncompleteRead(b"partial", 10)):
            with patch.object(fetch.urllib.request, "urlopen", side_effect=[response, io.BytesIO(self.payload)]) as request:
                with patch.object(fetch.time, "sleep"):
                    self.assertFalse(fetch.ensure_font(self.item, self.output, retries=2))
        self.assertEqual(request.call_count, 2)
        self.assertEqual(self.target.read_bytes(), self.payload)
        self.assert_no_temporary_files()

    def test_unsafe_paths_and_duplicate_files_are_rejected_before_fetch(self):
        for path in ("../escape.woff2", "/absolute.woff2", "dir/font.woff2", "dir\\font.woff2", ".hidden.woff2"):
            with self.subTest(path=path):
                self.manifest["files"] = [{**self.item, "path": path}]
                self.write_manifest()
                with self.assertRaisesRegex(ValueError, "basename"):
                    fetch.load_manifest(self.manifest_path)
        self.manifest["files"] = [self.item, copy.deepcopy(self.item)]
        self.write_manifest()
        with self.assertRaisesRegex(ValueError, "duplicate"):
            fetch.load_manifest(self.manifest_path)
        self.assertFalse(self.output.exists())

    def test_invalid_size_hash_weight_and_range_metadata_are_rejected(self):
        cases = [
            ("size", 0), ("size", True), ("size", fetch.MAX_FONT_BYTES + 1),
            ("sha256", "bad"), ("sha256", "a" * 63),
            ("weight", 330), ("weight", True),
            ("unicodeRange", "U+20; src: url(other)"), ("preload", "yes"),
        ]
        for field, value in cases:
            with self.subTest(field=field, value=value):
                self.manifest["files"] = [{**self.item, field: value}]
                self.write_manifest()
                with self.assertRaises(ValueError):
                    fetch.load_manifest(self.manifest_path)

    def test_nonofficial_or_mismatched_source_urls_are_rejected(self):
        for url in (
            self.item["url"].replace("https:", "http:"),
            self.item["url"].replace("cdn-file.hyperos.mi.com", "example.com"),
            self.item["url"].replace("/400/", "/500/"),
            self.item["url"] + "?replace=1",
            self.item["url"] + "#extra",
        ):
            with self.subTest(url=url):
                self.manifest["files"] = [{**self.item, "url": url}]
                self.write_manifest()
                with self.assertRaisesRegex(ValueError, "font URL"):
                    fetch.load_manifest(self.manifest_path)

    def test_existing_symlink_is_replaced_without_writing_its_destination(self):
        self.output.mkdir()
        outside = self.root / "outside.woff2"
        outside.write_bytes(self.payload)
        self.target.symlink_to(outside)
        self.assertFalse(fetch.matches_font(self.target, self.item))
        status, _, stderr = self.run_fetch("--source-dir", str(self.source))
        self.assertEqual((status, stderr), (0, ""))
        self.assertFalse(self.target.is_symlink())
        self.assertEqual(outside.read_bytes(), self.payload)
        self.assertEqual(self.target.read_bytes(), self.payload)

    def test_response_read_is_bounded_by_pinned_size(self):
        self.output.mkdir()
        source = io.BytesIO(self.payload + b"x" * 100_000)
        with patch.object(source, "read", wraps=source.read) as read:
            with self.assertRaisesRegex(ValueError, "Size mismatch"):
                fetch.write_verified(source, self.output, self.item)
        read.assert_called_once_with(len(self.payload) + 1)
        self.assertFalse(self.target.exists())
        self.assert_no_temporary_files()


if __name__ == "__main__":
    unittest.main()
