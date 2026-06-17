from __future__ import annotations

from io import BytesIO
from typing import Any

from worker_config import Config


class ObjectStore:
    def __init__(self, config: Config) -> None:
        self.config = config
        self._client = self._new_s3_client()

    def read_images(self, image_objects: list[dict[str, Any]]) -> list[Any]:
        if not image_objects:
            return []

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
                raw = body.read()
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
        image = Image.open(BytesIO(raw)).convert("RGB")
        image.load()
        return image
    except Exception as e:
        raise ValueError(f"failed to decode image object index={index}") from e
