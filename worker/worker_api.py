from __future__ import annotations

import logging
import time
from typing import Any

import httpx

log = logging.getLogger("worker")


class ApiClient:
    def __init__(self, base_url: str) -> None:
        self.base_url = base_url

    # jobの取得
    def claim(self) -> dict[str, Any] | None:
        response = self._post_json("/internal/worker/jobs/claim", None)
        if response.status_code == 204 or not response.content:
            return None
        job = response.json()
        if not isinstance(job, dict):
            raise ValueError(f"claim returned non-object JSON: {type(job)}")
        return job

    # embedding完了報告
    def complete(self, job_id: str, vector: list[float]) -> None:
        self._post_json(
            f"/internal/worker/jobs/{job_id}/complete",
            {"result": {"vector": vector}},
        )

    # embedding失敗報告
    def fail(self, job_id: str) -> None:
        self._post_json(f"/internal/worker/jobs/{job_id}/fail", None)

    def _post_json(self, path: str, body: dict[str, Any] | None) -> httpx.Response:
        request_kwargs: dict[str, Any] = {}
        if body is not None:
            request_kwargs["json"] = body

        started = time.perf_counter()
        try:
            response = httpx.post(
                self.base_url + path,
                **request_kwargs,
            )
        except httpx.RequestError as e:
            log.error(
                "api request failed method=POST path=%s elapsed_sec=%.3f error=%s",
                path,
                time.perf_counter() - started,
                e,
            )
            raise

        elapsed = time.perf_counter() - started
        if response.is_error:
            log.error(
                "api request http error method=POST path=%s status=%s elapsed_sec=%.3f body=%s",
                path,
                response.status_code,
                elapsed,
                response.content[:500],
            )
        else:
            log.debug(
                "api request completed method=POST path=%s status=%s elapsed_sec=%.3f",
                path,
                response.status_code,
                elapsed,
            )
        response.raise_for_status()
        return response
