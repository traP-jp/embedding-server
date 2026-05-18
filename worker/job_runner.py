from __future__ import annotations

import logging
import os
import time
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
        started = time.perf_counter()
        log.info(
            "job start id=%s text_present=%s text_chars=%s image_count=%s",
            job_id,
            isinstance(payload.get("text"), str) and bool(payload.get("text")),
            len(payload.get("text", "")) if isinstance(payload.get("text"), str) else 0,
            len(payload.get("image_paths") or []) if isinstance(payload.get("image_paths") or [], list) else -1,
        )
        item = build_embedding_item(payload, ocr, job_id)
        log.info(
            "job input built id=%s text_parts=%s image_count=%s",
            job_id,
            _count_text_parts(item),
            _count_images(item),
        )
        vector = embedder.embed(item)
        api.complete(job_id, vector)
        log.info("job complete id=%s dim=%s elapsed_sec=%.3f", job_id, len(vector), time.perf_counter() - started)
    except Exception as e:
        log.exception("job failed id=%s error=%s", job_id, e)
        fail_safely(api, job_id)


def build_embedding_item(payload: dict[str, Any], ocr: OcrEngine, job_id: str = "") -> dict[str, Any]:
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
        return {"text": base_text}

    validated_image_paths: list[str] = []
    text_parts = [base_text] if base_text else []
    for idx, image_path in enumerate(image_paths, start=1):
        if not isinstance(image_path, str) or not image_path:
            raise TypeError("image path must be a non-empty string")
        if not os.path.exists(image_path):
            raise FileNotFoundError(f"image not found: {image_path}")
        validated_image_paths.append(image_path)

        ocr_started = time.perf_counter()
        ocr_text = ocr.read_image_text(image_path)
        log.info(
            "job image ocr completed id=%s index=%s chars=%s elapsed_sec=%.3f",
            job_id,
            idx,
            len(ocr_text),
            time.perf_counter() - ocr_started,
        )
        if ocr_text:
            label = "[OCR]" if len(image_paths) == 1 else f"[OCR image {idx}]"
            text_parts.append(f"{label}\n{ocr_text}")

    item: dict[str, Any] = {"image": validated_image_paths}
    if text_parts:
        item["text"] = text_parts

    return item


def fail_safely(api: ApiClient, job_id: str) -> None:
    try:
        log.warning("job report failure start id=%s", job_id)
        api.fail(job_id)
        log.warning("job report failure complete id=%s", job_id)
    except httpx.HTTPStatusError as e:
        log.error(
            "fail job_id=%s http=%s body=%s",
            job_id,
            e.response.status_code,
            e.response.content[:500],
        )
    except httpx.RequestError as e:
        log.error("fail job_id=%s request error=%s", job_id, e)


def _count_text_parts(item: dict[str, Any]) -> int:
    text = item.get("text")
    if isinstance(text, list):
        return len(text)
    if isinstance(text, str) and text:
        return 1
    return 0


def _count_images(item: dict[str, Any]) -> int:
    image = item.get("image")
    if isinstance(image, list):
        return len(image)
    if image:
        return 1
    return 0
