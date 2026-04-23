#!/usr/bin/env python3

import base64
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import generate_image


TINY_PNG_BASE64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/aRsAAAAASUVORK5CYII="
TINY_PNG_BYTES = base64.b64decode(TINY_PNG_BASE64)


class FakeResponse:
    def __init__(self, data: bytes) -> None:
        self._data = data

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc, tb) -> bool:
        return False

    def read(self) -> bytes:
        return self._data


class GenerateImageMaterializeTests(unittest.TestCase):
    def test_materialize_images_copies_base64_outputs_into_output_dir(self) -> None:
        with tempfile.TemporaryDirectory() as output_dir:
            temp_images = generate_image.collect_images({"b64_json": TINY_PNG_BASE64})
            try:
                saved = generate_image.materialize_images(temp_images, output_dir, "slide-01", 1.0)
            finally:
                for item in temp_images:
                    path = Path(item)
                    if path.exists():
                        path.unlink()

            self.assertEqual(len(saved), 1)
            saved_path = Path(saved[0])
            self.assertTrue(saved_path.is_file())
            self.assertEqual(saved_path.parent, Path(output_dir).resolve())
            self.assertEqual(saved_path.name, "slide-01.png")
            self.assertEqual(saved_path.read_bytes(), TINY_PNG_BYTES)

    def test_materialize_images_downloads_url_outputs_into_output_dir(self) -> None:
        captured = {}

        def fake_urlopen(request, timeout=0):
            captured["url"] = request.full_url
            captured["user_agent"] = request.get_header("User-Agent") or request.get_header("User-agent")
            captured["timeout"] = timeout
            return FakeResponse(TINY_PNG_BYTES)

        with tempfile.TemporaryDirectory() as output_dir, mock.patch(
            "generate_image.urllib.request.urlopen",
            side_effect=fake_urlopen,
        ):
            saved = generate_image.materialize_images(
                ["https://example.com/generated/rendered.webp"],
                output_dir,
                "slide-02",
                2.5,
            )

            self.assertEqual(len(saved), 1)
            saved_path = Path(saved[0])
            self.assertTrue(saved_path.is_file())
            self.assertEqual(saved_path.parent, Path(output_dir).resolve())
            self.assertEqual(saved_path.name, "slide-02.webp")
            self.assertEqual(saved_path.read_bytes(), TINY_PNG_BYTES)
            self.assertEqual(captured["url"], "https://example.com/generated/rendered.webp")
            self.assertEqual(captured["user_agent"], generate_image.DEFAULT_USER_AGENT)
            self.assertEqual(captured["timeout"], 2.5)


class GenerateImageRequestTests(unittest.TestCase):
    def test_build_request_uses_chat_completions_for_non_image_api_model(self) -> None:
        endpoint, body, content_type = generate_image.build_request(
            "test-model",
            {"prompt": "cat", "aspect_ratio": "1:1"},
            None,
        )

        self.assertEqual(endpoint, "/chat/completions")
        self.assertEqual(content_type, "application/json")

        payload = json.loads(body.decode("utf-8"))
        self.assertEqual(payload["model"], "test-model")
        self.assertIn("messages", payload)
        self.assertEqual(payload["messages"][0]["content"][1]["text"], "aspect_ratio: 1:1")

    def test_build_request_uses_images_generation_for_image_api_model(self) -> None:
        endpoint, body, content_type = generate_image.build_request(
            "gpt-image-2",
            {"prompt": "cat", "aspect_ratio": "16:9"},
            None,
        )

        self.assertEqual(endpoint, "/images/generations")
        self.assertEqual(content_type, "application/json")

        payload = json.loads(body.decode("utf-8"))
        self.assertEqual(payload["model"], "gpt-image-2")
        self.assertEqual(payload["size"], "1536x864")
        self.assertIn("Target aspect ratio: 16:9", payload["prompt"])

    def test_build_request_uses_images_edits_for_image_api_model(self) -> None:
        with tempfile.NamedTemporaryFile(suffix=".png", delete=False) as handle:
            handle.write(TINY_PNG_BYTES)
            image_path = Path(handle.name)

        try:
            endpoint, body, content_type = generate_image.build_request(
                "gpt-image-2",
                {"prompt": "edit this", "aspect_ratio": "1:1"},
                image_path,
            )
        finally:
            image_path.unlink(missing_ok=True)

        self.assertEqual(endpoint, "/images/edits")
        self.assertIn("multipart/form-data;", content_type)
        self.assertIn(b'name="model"', body)
        self.assertIn(b"gpt-image-2", body)
        self.assertIn(b'name="image[]"; filename="', body)
        self.assertIn(TINY_PNG_BYTES, body)


if __name__ == "__main__":
    unittest.main()
