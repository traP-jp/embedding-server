from __future__ import annotations

import json
import logging
import signal
import time
from typing import Any

import httpx

from embedding_engine import EmbeddingEngine
from job_runner import claim_jobs, run_jobs
from ocr_engine import OcrEngine
from worker_api import ApiClient
from worker_config import Config
from worker_object_store import ObjectStore

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
logging.getLogger("httpx").setLevel(logging.WARNING)
log = logging.getLogger("worker")

_stop = False


def _handle_signal(signum: int, _frame: Any) -> None:
    global _stop
    log.info("signal %s, draining current batch", signum)
    _stop = True


def main() -> None:
    signal.signal(signal.SIGTERM, _handle_signal)
    signal.signal(signal.SIGINT, _handle_signal)

    try:
        config = Config()
    except ValueError as e:
        log.error("missing worker configuration: %s", e)
        raise SystemExit(1)

    log.info("worker components init started")
    api = ApiClient(config.api_base_url)
    object_store = ObjectStore(config)
    ocr = OcrEngine(config)
    embedder = EmbeddingEngine(config)
    log.info(
        "worker components init completed embedding_batch_size=%s",
        config.embedding_batch_size,
    )

    while not _stop:
        try:
            try:
                jobs = claim_jobs(api, config.embedding_batch_size)
            except httpx.HTTPStatusError as e:
                log.error("claim http=%s body=%s", e.response.status_code, e.response.content[:500])
                time.sleep(2)
                continue
            except httpx.RequestError as e:
                log.error("claim request error=%s", e)
                time.sleep(2)
                continue

            if not jobs:
                time.sleep(config.poll_interval_seconds)
                continue

            run_jobs(api, embedder, ocr, object_store, jobs)
        except json.JSONDecodeError as e:
            log.warning("invalid json from api: %s", e)
            time.sleep(1)
        except Exception as e:
            log.exception("worker loop error=%s", e)
            time.sleep(2)

    log.info("exit")


if __name__ == "__main__":
    main()
