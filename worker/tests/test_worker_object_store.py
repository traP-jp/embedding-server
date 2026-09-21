from io import BytesIO
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

from PIL import Image

from worker_object_store import ObjectStore, _decode_image


class ObjectStoreTest(unittest.TestCase):
    def test_bounds_stream_read_and_closes_on_failure(self) -> None:
        store = ObjectStore.__new__(ObjectStore)
        store.config = SimpleNamespace(s3_bucket="test")
        store._client = Mock()
        for content_length in (None, 9):
            with self.subTest(content_length=content_length):
                body = Mock()
                body.read.return_value = b"x" * 9
                response = {"Body": body}
                if content_length is not None:
                    response["ContentLength"] = content_length
                store._client.get_object.return_value = response
                with patch("worker_object_store.MAX_IMAGE_BYTES", 8):
                    with self.assertRaises(ValueError):
                        store.read_images([{"key": "image"}])
                body.close.assert_called_once()
                if content_length is None:
                    body.read.assert_called_once_with(9)
                else:
                    body.read.assert_not_called()

    def test_decodes_supported_image(self) -> None:
        raw = BytesIO()
        Image.new("RGB", (2, 3)).save(raw, format="PNG")
        image = _decode_image(raw.getvalue(), 0)
        self.assertEqual(image.size, (2, 3))
        self.assertEqual(image.mode, "RGB")

    def test_rejects_pixel_limit_before_conversion(self) -> None:
        raw = BytesIO()
        Image.new("RGB", (3, 3)).save(raw, format="PNG")
        with patch("worker_object_store.MAX_IMAGE_PIXELS", 8), patch.object(Image.Image, "convert") as convert:
            with self.assertRaises(ValueError):
                _decode_image(raw.getvalue(), 0)
            convert.assert_not_called()

    def test_rejects_decompression_warning(self) -> None:
        raw = BytesIO()
        Image.new("RGB", (3, 3)).save(raw, format="PNG")
        with patch.object(Image, "MAX_IMAGE_PIXELS", 8):
            with self.assertRaises(ValueError):
                _decode_image(raw.getvalue(), 0)

    def test_rejects_unsupported_format(self) -> None:
        raw = BytesIO()
        Image.new("RGB", (2, 3)).save(raw, format="BMP")
        with self.assertRaises(ValueError):
            _decode_image(raw.getvalue(), 0)

    def test_rejects_excess_images_before_download(self) -> None:
        store = ObjectStore.__new__(ObjectStore)
        with self.assertRaises(ValueError):
            store.read_images([{"key": "image"}] * 5)
