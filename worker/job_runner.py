from __future__ import annotations

import logging
import os
from typing import Any

import httpx

from embedding_engine import EmbeddingEngine
from ocr_engine import OcrEngine
from worker_api import ApiClient

log = logging.getLogger("worker")


def run_job(api: ApiClient, embedder: EmbeddingEngine, ocr: OcrEngine, job: dict[str, Any]) -> None:
    job_id = job.get("id")
    payload = job.get("payload")
    if not isinstance(job_id, str):
        raise ValueError("claim response missing string id")
    if not isinstance(payload, dict):
        log.error("invalid payload job_id=%s type=%s", job_id, type(payload))
        fail_safely(api, job_id)
        return

    try:
        items = build_embedding_items(payload, ocr)
        log.info("job start id=%s items=%s", job_id, len(items))
        vector = embedder.embed(items)
        api.complete(job_id, vector)
        log.info("job complete id=%s dim=%s", job_id, len(vector))
    except Exception as e:
        log.exception("job failed id=%s error=%s", job_id, e)
        fail_safely(api, job_id)


def build_embedding_items(payload: dict[str, Any], ocr: OcrEngine) -> list[dict[str, Any]]:
    text = payload.get("text")
    if text is not None and not isinstance(text, str):
        raise TypeError("payload.text must be a string")
    base_text = (text or "").strip()

    image_paths = payload.get("image_paths") or []
    if not isinstance(image_paths, list):
        raise TypeError("payload.image_paths must be a list")

    if not base_text and not image_paths:
        raise ValueError("payload requires text or image_paths")

    if not image_paths:
        return [{"text": base_text}]

    items: list[dict[str, Any]] = []
    for image_path in image_paths:
        if not isinstance(image_path, str) or not image_path:
            raise TypeError("image path must be a non-empty string")
        if not os.path.exists(image_path):
            raise FileNotFoundError(f"image not found: {image_path}")

        text_parts = [base_text] if base_text else []
        ocr_text = ocr.read_image_text(image_path)
        if ocr_text:
            text_parts.append(f"[OCR]\n{ocr_text}")

        item: dict[str, Any] = {"image": image_path}
        if text_parts:
            item["text"] = "\n\n".join(text_parts)
        items.append(item)

    return items


def fail_safely(api: ApiClient, job_id: str) -> None:
    try:
        api.fail(job_id)
    except httpx.HTTPStatusError as e:
        log.error(
            "fail job_id=%s http=%s body=%s",
            job_id,
            e.response.status_code,
            e.response.content[:500],
        )
    except httpx.RequestError as e:
        log.error("fail job_id=%s request error=%s", job_id, e)
