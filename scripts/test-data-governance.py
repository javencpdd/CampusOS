#!/usr/bin/env python3
"""Prove built-in branding stays static while package governance remains strict."""
import importlib.util
import json
from pathlib import Path
import struct
import tempfile
import unittest
from unittest import mock
import zlib

SPEC = importlib.util.spec_from_file_location("governance", Path(__file__).with_name("check-data-governance.py"))
GOVERNANCE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(GOVERNANCE)


def png(width=1, height=1, *, compressed=None, depth=8, color=6, interlace=0):
    def chunk(kind, data):
        return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))
    if compressed is None:
        compressed = zlib.compress(b"\0\0\0\0\xff")
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, depth, color, 0, 0, interlace)) + chunk(b"IDAT", compressed) + chunk(b"IEND", b"")


class ResourceGovernanceTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        old = GOVERNANCE.ROOT, GOVERNANCE.DATA
        self.addCleanup(lambda: self.restore(old))
        GOVERNANCE.ROOT, GOVERNANCE.DATA = self.root, self.root / "data"
        self.resources = GOVERNANCE.DATA / "resources"
        for kind in GOVERNANCE.RESOURCE_TYPES:
            (self.resources / kind).mkdir(parents=True)
        self.branding = self.resources / "branding"
        self.branding.mkdir()
        self.logo = self.branding / "default-logo.png"
        self.logo.write_bytes(png())

    def restore(self, old):
        GOVERNANCE.ROOT, GOVERNANCE.DATA = old

    def errors(self):
        errors = []
        GOVERNANCE.validate_resources(errors)
        return errors

    def test_static_logo_and_optional_legacy_layout(self):
        self.assertEqual([], self.errors())
        self.logo.unlink()
        self.branding.rmdir()
        self.assertEqual([], self.errors())

    def test_unknown_files_directories_and_runtime_markers(self):
        for name in ("extra.png", "plugin.yaml", "go.mod", "resource.json", "subdirectory"):
            with self.subTest(name=name):
                path = self.branding / name
                path.mkdir() if name == "subdirectory" else path.write_text("untrusted")
                self.assertTrue(self.errors())
                path.rmdir() if path.is_dir() else path.unlink()

    def test_symlinked_logo_or_group(self):
        target = self.root / "outside.png"
        target.write_bytes(png())
        self.logo.unlink()
        self.logo.symlink_to(target)
        self.assertTrue(self.errors())
        self.logo.unlink()
        self.branding.rmdir()
        self.branding.symlink_to(self.root, target_is_directory=True)
        self.assertTrue(self.errors())

    def test_missing_or_directory_logo(self):
        self.logo.unlink()
        self.assertTrue(self.errors())
        self.logo.mkdir()
        self.assertTrue(self.errors())

    def test_non_png_truncated_crc_dimension_and_size(self):
        for data in (b"not PNG", png()[:24], png()[:-1], png() + b"trailer", png(1025), png(height=0), b"x" * (2 * 1024 * 1024 + 1)):
            with self.subTest(length=len(data)):
                self.logo.write_bytes(data)
                self.assertTrue(self.errors())
        damaged = bytearray(png())
        damaged[29] ^= 1
        self.logo.write_bytes(damaged)
        self.assertTrue(self.errors())

    def test_png_zlib_must_be_complete_and_have_no_extra_stream(self):
        compressed = zlib.compress(b"\0\0\0\0\xff")
        for invalid in (b"not zlib", compressed[:-1], compressed + b"trailer", compressed + zlib.compress(b"")):
            with self.subTest(compressed=invalid):
                self.logo.write_bytes(png(compressed=invalid))
                self.assertTrue(self.errors())

    def test_png_rows_must_have_exact_pixels_and_valid_filters(self):
        for pixels in (b"\0", b"\0\0\0\0", b"\5\0\0\0\xff", b"\xff\0\0\0\xff"):
            with self.subTest(pixels=pixels):
                self.logo.write_bytes(png(compressed=zlib.compress(pixels)))
                self.assertTrue(self.errors())

    def test_png_decompression_stays_within_ihdr_bound(self):
        # A tiny valid IHDR must never cause its highly compressed IDAT to be
        # expanded in full. Assert the actual zlib cap, and forbid an uncapped
        # flush, as well as rejecting the image.
        self.logo.write_bytes(png(compressed=zlib.compress(b"\0" * (2 * 1024 * 1024))))
        decoder = zlib.decompressobj()
        limits = []

        class BoundedDecoder:
            def decompress(self, data, max_length=0):
                limits.append(max_length)
                return decoder.decompress(data, max_length)

            def flush(self, *args):
                raise AssertionError("PNG decoder must not flush without an output cap")

            def __getattr__(self, name):
                return getattr(decoder, name)

        with mock.patch.object(GOVERNANCE.zlib, "decompressobj", return_value=BoundedDecoder()):
            self.assertTrue(self.errors())
        self.assertEqual([6], limits)

    def test_png_dimension_bounds_packed_pixels_and_adam7(self):
        # 1024x1024 monochrome rows are 128 bytes plus one filter byte.
        # Adam7 on 8x8 RGBA has pass rows 1,1,1,2,2,4,4: 256+15 bytes.
        images = (
            png(1024, 1024, depth=1, color=0, compressed=zlib.compress(b"\0" * (129 * 1024))),
            png(interlace=1),
            png(8, 8, interlace=1, compressed=zlib.compress(b"\0" * 271)),
        )
        for image in images:
            with self.subTest(length=len(image)):
                self.logo.write_bytes(image)
                self.assertEqual([], self.errors())
        self.logo.write_bytes(png(8, 8, interlace=1, compressed=zlib.compress(b"\0" * 270)))
        self.assertTrue(self.errors())
        bad_filter = bytearray(271)
        bad_filter[5] = 5  # First row of the second Adam7 pass.
        self.logo.write_bytes(png(8, 8, interlace=1, compressed=zlib.compress(bad_filter)))
        self.assertTrue(self.errors())

    def test_unknown_resource_kind_still_rejected(self):
        (self.resources / "undeclared").mkdir()
        self.assertTrue(any("undeclared resource kinds" in error for error in self.errors()))

    def test_package_manifest_checksum_and_runtime_still_enforced(self):
        package = self.resources / "themes" / "example"
        package.mkdir()
        (package / "style.css").write_text("body{}")
        self.assertTrue(any("no resource.json" in error for error in self.errors()))
        manifest = {"schema": "campusos.resource/v1", "id": "example", "type": "theme", "entry": "style.css", "checksum": GOVERNANCE.directory_checksum(package)}
        (package / "resource.json").write_text(json.dumps(manifest))
        self.assertEqual([], self.errors())
        (package / "style.css").write_text("body{color:red}")
        self.assertTrue(any("checksum mismatch" in error for error in self.errors()))
        (package / "plugin.yaml").write_text("runtime: wasm")
        self.assertTrue(any("runtime marker" in error for error in self.errors()))


if __name__ == "__main__":
    unittest.main()
