#!/usr/bin/env python3

import base64
import json
import os
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
    def test_resolve_config_prefers_tuzhi_over_cpa(self) -> None:
        env = {
            "TUZHI_KEY": "tuzhi-key",
            "TUZHI_IMAGE_MODEL": "tuzhi-model",
            "TUZHI_IMAGE_GEN_BASE": "https://example.invalid/v1/images/generations/",
            "TUZHI_IMAGE_EDIT_BASE": "https://example.invalid/v1/images/edits/",
            "CPA_API_KEY": "cpa-key",
            "CPA_API_BASE": "https://cpa.invalid/v1",
            "CPA_IMAGE_MODEL": "gpt-image-2",
        }
        with mock.patch.dict(os.environ, env, clear=True):
            config = generate_image.resolve_config()

        self.assertEqual(config.provider, "tuzhi")
        self.assertEqual(config.api_key, "tuzhi-key")
        self.assertEqual(config.model, "tuzhi-model")
        self.assertEqual(config.generation_url, "https://example.invalid/v1/images/generations")
        self.assertEqual(config.edit_url, "https://example.invalid/v1/images/edits")

    def test_resolve_config_rejects_partial_tuzhi_without_cpa_fallback(self) -> None:
        env = {
            "TUZHI_KEY": "tuzhi-key",
            "CPA_API_KEY": "cpa-key",
            "CPA_API_BASE": "https://cpa.invalid/v1",
            "CPA_IMAGE_MODEL": "gpt-image-2",
        }
        with mock.patch.dict(os.environ, env, clear=True):
            with self.assertRaisesRegex(RuntimeError, "TUZHI_IMAGE_MODEL"):
                generate_image.resolve_config()

    def test_resolve_config_falls_back_to_cpa(self) -> None:
        env = {
            "CPA_API_KEY": "cpa-key",
            "CPA_API_BASE": "https://cpa.invalid/v1/",
            "CPA_IMAGE_MODEL": "gpt-image-2",
        }
        with mock.patch.dict(os.environ, env, clear=True):
            config = generate_image.resolve_config()

        self.assertEqual(config.provider, "cpa")
        self.assertEqual(config.api_key, "cpa-key")
        self.assertEqual(config.api_base, "https://cpa.invalid/v1")
        self.assertEqual(config.model, "gpt-image-2")

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

    def test_build_provider_request_uses_exact_tuzhi_generation_url(self) -> None:
        config = generate_image.ImageProviderConfig(
            provider="tuzhi",
            api_key="tuzhi-key",
            model="tuzhi-model",
            generation_url="https://example.invalid/custom/generate",
            edit_url="https://example.invalid/custom/edit",
        )

        url, body, content_type = generate_image.build_provider_request(
            config,
            {"prompt": "cat", "aspect_ratio": "16:9"},
            None,
        )

        self.assertEqual(url, "https://example.invalid/custom/generate")
        self.assertEqual(content_type, "application/json")
        payload = json.loads(body.decode("utf-8"))
        self.assertEqual(payload["model"], "tuzhi-model")
        self.assertEqual(payload["size"], "1536x864")

    def test_build_provider_request_uses_exact_tuzhi_edit_url(self) -> None:
        config = generate_image.ImageProviderConfig(
            provider="tuzhi",
            api_key="tuzhi-key",
            model="tuzhi-model",
            generation_url="https://example.invalid/custom/generate",
            edit_url="https://example.invalid/custom/edit",
        )
        with tempfile.NamedTemporaryFile(suffix=".png", delete=False) as handle:
            handle.write(TINY_PNG_BYTES)
            image_path = Path(handle.name)

        try:
            url, body, content_type = generate_image.build_provider_request(
                config,
                {"prompt": "edit this", "aspect_ratio": "1:1"},
                image_path,
            )
        finally:
            image_path.unlink(missing_ok=True)

        self.assertEqual(url, "https://example.invalid/custom/edit")
        self.assertIn("multipart/form-data;", content_type)
        self.assertIn(b'name="model"', body)
        self.assertIn(b"tuzhi-model", body)
        self.assertIn(b'name="image[]"; filename="', body)
        self.assertIn(TINY_PNG_BYTES, body)


if __name__ == "__main__":
    unittest.main()
