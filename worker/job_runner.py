from __future__ import annotations

import logging
import os
import time
from collections.abc import Iterator
from contextlib import contextmanager
from dataclasses import dataclass, field
from typing import Any

import httpx

from embedding_engine import EmbeddingEngine
from ocr_engine import OcrEngine
from worker_api import ApiClient

log = logging.getLogger("worker")

# jobのログを記録するためのクラス
@dataclass
class JobMetrics:
    job_id: str
    text_chars: int
    images: int = 0
    ocr_chars: int = 0
    ocr_sec: float = 0.0
    embed_sec: float = 0.0
    report_sec: float = 0.0
    started: float = field(default_factory=time.perf_counter, init=False)

    @contextmanager
    def measure_embedding(self) -> Iterator[None]:
        started = time.perf_counter()
        try:
            yield
        finally:
            self.embed_sec = time.perf_counter() - started

    @contextmanager
    def measure_report(self) -> Iterator[None]:
        started = time.perf_counter()
        try:
            yield
        finally:
            self.report_sec = time.perf_counter() - started

    def start_ocr(self) -> float:
        return time.perf_counter()

    def finish_ocr(self, index: int, text: str, started: float) -> None:
        elapsed_sec = time.perf_counter() - started
        self.ocr_chars += len(text)
        self.ocr_sec += elapsed_sec
        log.debug(
            "job image ocr completed id=%s index=%s chars=%s elapsed_sec=%.3f preview=%r",
            self.job_id,
            index,
            len(text),
            elapsed_sec,
            _preview(text),
        )

    def set_images(self, images: int) -> None:
        self.images = images

    def total_elapsed_sec(self) -> float:
        return time.perf_counter() - self.started

    def log_completed(self, vector_dim: int) -> None:
        log.info(
            (
                "job completed id=%s text_chars=%s images=%s "
                "ocr_chars=%s dim=%s "
                "ocr_sec=%.3f embed_sec=%.3f "
                "report_sec=%.3f total_sec=%.3f"
            ),
            self.job_id,
            self.text_chars,
            self.images,
            self.ocr_chars,
            vector_dim,
            self.ocr_sec,
            self.embed_sec,
            self.report_sec,
            self.total_elapsed_sec(),
        )

    def log_failed(self, error: Exception) -> None:
        log.exception(
            "job failed id=%s elapsed_sec=%.3f error=%s",
            self.job_id,
            self.total_elapsed_sec(),
            error,
        )


def run_job(api: ApiClient, embedder: EmbeddingEngine, ocr: OcrEngine, job: dict[str, Any]) -> None:
    job_id = job.get("id")
    payload = job.get("payload")
    if not isinstance(job_id, str):
        raise ValueError("claim response missing string id")
    if not isinstance(payload, dict):
        log.error("invalid payload job_id=%s type=%s", job_id, type(payload))
        fail_safely(api, job_id)
        return

    metrics = JobMetrics(job_id=job_id, text_chars=_payload_text_chars(payload))
    try:
        item = build_embedding_item(payload, ocr, metrics)

        with metrics.measure_embedding():
            vector = embedder.embed(item)

        with metrics.measure_report():
            api.complete(job_id, vector)

        metrics.log_completed(len(vector))
    except Exception as e:
        metrics.log_failed(e)
        fail_safely(api, job_id)


def build_embedding_item(
    payload: dict[str, Any],
    ocr: OcrEngine,
    metrics: JobMetrics,
) -> dict[str, Any]:
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

        ocr_started = metrics.start_ocr()
        ocr_text = ocr.read_image_text(image_path)
        metrics.finish_ocr(idx, ocr_text, ocr_started)
        if ocr_text:
            label = "[OCR]" if len(image_paths) == 1 else f"[OCR image {idx}]"
            text_parts.append(f"{label}\n{ocr_text}")

    item: dict[str, Any] = {"image": validated_image_paths}
    metrics.set_images(len(validated_image_paths))
    if text_parts:
        item["text"] = text_parts

    return item


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
