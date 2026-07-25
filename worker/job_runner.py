from __future__ import annotations

import logging
import time
from collections.abc import Iterator
from contextlib import contextmanager
from dataclasses import dataclass, field
from typing import Any

import httpx

from embedding_engine import EmbeddingEngine
from ocr_engine import OcrEngine
from worker_api import ApiClient
from worker_object_store import ObjectStore

log = logging.getLogger("worker")


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


@dataclass
class _PreparedJob:
    """OCR・画像読み込みまで済ませた、推論待ちジョブ。"""

    job_id: str
    item: dict[str, Any]
    metrics: JobMetrics


def claim_jobs(api: ApiClient, max_jobs: int) -> list[dict[str, Any]]:
    # キューが空になるか max_jobs に達するまで claim を繰り返す。
    # 満杯を待たず、取れた分だけで返す。
    if max_jobs < 1:
        return []
    jobs: list[dict[str, Any]] = []
    for _ in range(max_jobs):
        job = api.claim()
        if job is None:
            break
        jobs.append(job)
    return jobs


def run_jobs(
    api: ApiClient,
    embedder: EmbeddingEngine,
    ocr: OcrEngine,
    object_store: ObjectStore,
    jobs: list[dict[str, Any]],
) -> int:
    # 1) 各ジョブを前処理（失敗したものは個別 fail）
    # 2) 成功分だけ embed_many で一括推論
    # 3) ベクトルを1件ずつ complete
    prepared: list[_PreparedJob] = []
    for job in jobs:
        prepared_job = _prepare_job(api, ocr, object_store, job)
        if prepared_job is not None:
            prepared.append(prepared_job)

    if not prepared:
        return 0

    try:
        embed_started = time.perf_counter()
        # GPU 上では複数入力をまとめて process する。
        vectors = embedder.embed_many([job.item for job in prepared])
        embed_sec = time.perf_counter() - embed_started
    except Exception as e:
        # 推論全体が落ちたら、このバッチ分はすべて fail。
        for job in prepared:
            job.metrics.embed_sec = time.perf_counter() - job.metrics.started
            job.metrics.log_failed(e)
            fail_safely(api, job.job_id)
        return 0

    if len(vectors) != len(prepared):
        error = ValueError(f"embedding batch size mismatch: got {len(vectors)}, want {len(prepared)}")
        for job in prepared:
            job.metrics.embed_sec = embed_sec
            job.metrics.log_failed(error)
            fail_safely(api, job.job_id)
        return 0

    completed = 0
    for job, vector in zip(prepared, vectors, strict=True):
        # embed_sec はバッチ全体の壁時計時間を各ジョブに共有する。
        job.metrics.embed_sec = embed_sec
        try:
            with job.metrics.measure_report():
                api.complete(job.job_id, vector)
            job.metrics.log_completed(len(vector))
            completed += 1
        except Exception as e:
            job.metrics.log_failed(e)
            fail_safely(api, job.job_id)
    return completed


def _prepare_job(
    api: ApiClient,
    ocr: OcrEngine,
    object_store: ObjectStore,
    job: dict[str, Any],
) -> _PreparedJob | None:
    # 画像取得と OCR まで。推論は run_jobs 側でまとめて行う。
    job_id = job.get("id")
    payload = job.get("payload")
    if not isinstance(job_id, str):
        raise ValueError("claim response missing string id")
    if not isinstance(payload, dict):
        log.error("invalid payload job_id=%s type=%s", job_id, type(payload))
        fail_safely(api, job_id)
        return None

    metrics = JobMetrics(job_id=job_id, text_chars=_payload_text_chars(payload))
    try:
        images = get_images(payload, object_store)
        item = add_ocr_text(payload, images, ocr, metrics)
        return _PreparedJob(job_id=job_id, item=item, metrics=metrics)
    except Exception as e:
        metrics.log_failed(e)
        fail_safely(api, job_id)
        return None


def get_images(
    payload: dict[str, Any],
    object_store: ObjectStore,
) -> list[Any]:
    image_objects = payload.get("image_objects") or []
    if not image_objects:
        return []
    if not isinstance(image_objects, list):
        raise TypeError("payload.image_objects must be a list")

    return object_store.read_images(image_objects)

def add_ocr_text(
    payload: dict[str, Any],
    images: list[Any],
    ocr: OcrEngine,
    metrics: JobMetrics,
) -> dict[str, Any]:
    text = payload.get("text")

    if text is not None and not isinstance(text, str):
        raise TypeError("payload.text must be a string")

    base_text = (text or "").strip()

    if not base_text and not images:
        raise ValueError("payload requires text or image_objects")

    if not images:
        return {"text": base_text}

    text_parts = [base_text] if base_text else []
    for idx, image in enumerate(images):
        ocr_started = metrics.start_ocr()
        ocr_text = ocr.read_image_text(image)
        metrics.finish_ocr(idx, ocr_text, ocr_started)
        if ocr_text:
            label = "[OCR]" if len(images) == 1 else f"[OCR image {idx}]"
            text_parts.append(f"{label}\n{ocr_text}")

    item: dict[str, Any] = {"image": images}
    metrics.set_images(len(images))
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
