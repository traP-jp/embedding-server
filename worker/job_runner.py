from __future__ import annotations

import logging
import os
import time
from dataclasses import dataclass
from typing import Any

import httpx

from embedding_engine import EmbeddingEngine
from ocr_engine import OcrEngine
from worker_api import ApiClient

log = logging.getLogger("worker")


@dataclass
class BuildStats:
    ocr_chars: int = 0
    ocr_elapsed_sec: float = 0.0


def run_job(api: ApiClient, embedder: EmbeddingEngine, ocr: OcrEngine, job: dict[str, Any]) -> None:
    job_id = job.get("id")
    payload = job.get("payload")
    if not isinstance(job_id, str):
        raise ValueError("claim response missing string id")
    if not isinstance(payload, dict):
        log.error("invalid payload job_id=%s type=%s", job_id, type(payload))
        fail_safely(api, job_id)
        return

    started = time.perf_counter()
    try:
        item, build_stats = build_embedding_item(payload, ocr, job_id)

        embedding_started = time.perf_counter()
        vector = embedder.embed(item)
        embedding_elapsed = time.perf_counter() - embedding_started

        complete_started = time.perf_counter()
        api.complete(job_id, vector)
        report_elapsed = time.perf_counter() - complete_started

        log.info(
            (
                "job completed id=%s text_chars=%s image_count=%s "
                "ocr_chars=%s vector_dim=%s "
                "ocr_elapsed_sec=%.3f embedding_elapsed_sec=%.3f "
                "report_elapsed_sec=%.3f total_elapsed_sec=%.3f"
            ),
            job_id,
            _payload_text_chars(payload),
            _count_images(item),
            build_stats.ocr_chars,
            len(vector),
            build_stats.ocr_elapsed_sec,
            embedding_elapsed,
            report_elapsed,
            time.perf_counter() - started,
        )
    except Exception as e:
        log.exception("job failed id=%s elapsed_sec=%.3f error=%s", job_id, time.perf_counter() - started, e)
        fail_safely(api, job_id)


def build_embedding_item(
    payload: dict[str, Any],
    ocr: OcrEngine,
    job_id: str = "",
) -> tuple[dict[str, Any], BuildStats]:
    stats = BuildStats()
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
        return {"text": base_text}, stats

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
        ocr_elapsed = time.perf_counter() - ocr_started
        stats.ocr_chars += len(ocr_text)
        stats.ocr_elapsed_sec += ocr_elapsed
        log.debug(
            "job image ocr completed id=%s index=%s chars=%s elapsed_sec=%.3f preview=%r",
            job_id,
            idx,
            len(ocr_text),
            ocr_elapsed,
            _preview(ocr_text),
        )
        if ocr_text:
            label = "[OCR]" if len(image_paths) == 1 else f"[OCR image {idx}]"
            text_parts.append(f"{label}\n{ocr_text}")

    item: dict[str, Any] = {"image": validated_image_paths}
    if text_parts:
        item["text"] = text_parts

    return item, stats


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


def _count_images(item: dict[str, Any]) -> int:
    image = item.get("image")
    if isinstance(image, list):
        return len(image)
    if image:
        return 1
    return 0


def _payload_text_chars(payload: dict[str, Any]) -> int:
    text = payload.get("text")
    if isinstance(text, str):
        return len(text)
    return 0


def _preview(text: str, limit: int = 120) -> str:
    text = " ".join(text.split())
    if len(text) <= limit:
        return text
    return text[:limit] + "..."
