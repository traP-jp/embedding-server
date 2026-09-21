from __future__ import annotations

import warnings
from io import BytesIO
from typing import Any

from worker_config import Config

MAX_IMAGE_BYTES = 20 << 20
MAX_IMAGE_PIXELS = 20_000_000
MAX_IMAGES = 4


class ObjectStore:
    def __init__(self, config: Config) -> None:
        self.config = config
        self._client = self._new_s3_client()

    def read_images(self, image_objects: list[dict[str, Any]]) -> list[Any]:
        if not image_objects:
            return []
        if len(image_objects) > MAX_IMAGES:
            raise ValueError("too many image objects")

        images: list[Any] = []
        for idx, image_object in enumerate(image_objects):
            if not isinstance(image_object, dict):
                raise TypeError("image object must be an object")
            key = image_object.get("key")
            if not isinstance(key, str) or not key:
                raise TypeError("image object key must be a non-empty string")

            response = self._client.get_object(Bucket=self.config.s3_bucket, Key=key)
            body = response["Body"]
            try:
                if response.get("ContentLength", 0) > MAX_IMAGE_BYTES:
                    raise ValueError("image object exceeds 20 MiB")
                raw = body.read(MAX_IMAGE_BYTES + 1)
                if len(raw) > MAX_IMAGE_BYTES:
                    raise ValueError("image object exceeds 20 MiB")
            finally:
                body.close()
            images.append(_decode_image(raw, idx))

        return images

    def _new_s3_client(self) -> Any:
        if not (
            self.config.s3_endpoint_url
            and self.config.s3_bucket
            and self.config.s3_region
            and self.config.s3_access_key_id
            and self.config.s3_secret_access_key
        ):
            raise ValueError("missing S3 object storage configuration")

        import boto3
        from botocore.config import Config as BotoConfig

        return boto3.client(
            "s3",
            endpoint_url=self.config.s3_endpoint_url,
            region_name=self.config.s3_region,
            aws_access_key_id=self.config.s3_access_key_id,
            aws_secret_access_key=self.config.s3_secret_access_key,
            config=BotoConfig(s3={"addressing_style": "path"}),
        )


def _decode_image(raw: bytes, index: int) -> Any:
    from PIL import Image

    try:
        with warnings.catch_warnings():
            warnings.simplefilter("error", Image.DecompressionBombWarning)
            with Image.open(BytesIO(raw), formats=["PNG", "JPEG", "WEBP"]) as source:
                # 変換による画素バッファの確保前にサイズを検査する。
                if source.width * source.height > MAX_IMAGE_PIXELS:
                    raise ValueError("image exceeds 20 million pixels")
                return source.convert("RGB")
    except Exception as e:
        raise ValueError(f"failed to decode image object index={index}") from e
