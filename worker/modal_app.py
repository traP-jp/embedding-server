from __future__ import annotations

import logging
import os
import time
import tomllib
from pathlib import Path
from typing import Any

import modal

APP_NAME = "embedding-worker"
WORKER_DIR = Path(__file__).resolve().parent
CACHE_VOLUME_DIR = "/root/cache-volume"
CACHE_DIR = f"{CACHE_VOLUME_DIR}/xdg"
HF_CACHE_DIR = f"{CACHE_VOLUME_DIR}/huggingface"
LOCAL_DIR_IGNORE = [
    ".venv",
    ".venv/**",
    "__pycache__",
    "__pycache__/**",
    "**/__pycache__",
    "**/__pycache__/**",
    "*.pyc",
    ".pytest_cache",
    ".pytest_cache/**",
    ".mypy_cache",
    ".mypy_cache/**",
    ".ruff_cache",
    ".ruff_cache/**",
    "*.egg-info",
    "*.egg-info/**",
]
DEFAULT_MAX_JOBS_PER_RUN = 30
DEFAULT_SCALEDOWN_WINDOW_SECONDS = 30
DEFAULT_WORKER_RUN_SECONDS = int(os.environ.get("MODAL_WORKER_RUN_SECONDS", "300"))
DEFAULT_IDLE_POLL_SECONDS = float(os.environ.get("MODAL_IDLE_POLL_SECONDS", "2"))


def _worker_dependencies() -> list[str]:
    pyproject_path = WORKER_DIR / "pyproject.toml"
    if not pyproject_path.exists():
        pyproject_path = Path("/app/pyproject.toml")

    with pyproject_path.open("rb") as f:
        project = tomllib.load(f)["project"]

    return [
        "torch>=2.7.0",
        "torchvision>=0.22.0",
        *project.get("dependencies", []),
        *project.get("optional-dependencies", {}).get("runtime", []),
    ]


worker_image = (
    modal.Image.debian_slim(python_version="3.12")
    .apt_install("git", "libglib2.0-0", "libgl1")
    .uv_pip_install(*_worker_dependencies())
    .env(
        {
            "PYTHONUNBUFFERED": "1",
            "PYTHONPATH": "/app",
            "HF_HUB_DISABLE_XET": "1",
            "HF_XET_DISABLE": "1",
            "PYTORCH_CUDA_ALLOC_CONF": "expandable_segments:True",
        }
    )
    .add_local_dir(WORKER_DIR, remote_path="/app", ignore=LOCAL_DIR_IGNORE)
)

app = modal.App(APP_NAME)
cache_volume = modal.Volume.from_name("embedding-worker-cache", create_if_missing=True)
worker_env = {
    "XDG_CACHE_HOME": CACHE_DIR,
    "HF_HOME": HF_CACHE_DIR,
    "TRANSFORMERS_CACHE": f"{HF_CACHE_DIR}/transformers",
}
worker_secret = modal.Secret.from_name(
    "embedding-worker",
    required_keys=[
        "WORKER_API_MODE",
        "API_BASE_URL",
        "S3_ENDPOINT_URL",
        "S3_BUCKET",
        "S3_REGION",
        "S3_ACCESS_KEY_ID",
        "S3_SECRET_ACCESS_KEY",
        "ATTN_IMPLEMENTATION",
        "POLL_INTERVAL_SECONDS",
        "EMBEDDING_WORKER_FAKE",
        "FAKE_EMBEDDING_DIM",
        "MODEL_DEVICE_MAP",
        "MODEL_MAX_MEMORY_CUDA",
        "MODEL_MAX_MEMORY_CPU",
        "EMBEDDING_MAX_PIXELS",
        "TORCH_DTYPE",
        "QUANTIZATION",
        "BNB_4BIT_QUANT_TYPE",
        "BNB_4BIT_USE_DOUBLE_QUANT",
        "BNB_4BIT_COMPUTE_DTYPE",
        "OCR_ENABLED",
        "OCR_DEVICE",
        "OCR_SCALE",
        "OCR_REC_THRESHOLD",
        "OCR_DET_THRESHOLD",
        "OCR_MAX_CHARS",
        "OCR_VISUALIZE",
    ],
)

_config: Any | None = None
_api: Any | None = None
_components: tuple[Any, Any, Any] | None = None


def _configure_logging() -> None:
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
    logging.getLogger("httpx").setLevel(logging.WARNING)


def _load_config() -> Any:
    global _config
    if _config is not None:
        return _config

    from worker_config import Config

    _configure_logging()
    _config = Config()
    return _config


def _load_api() -> Any:
    global _api
    if _api is not None:
        return _api

    from worker_api import ApiClient

    config = _load_config()
    _api = ApiClient(config.api_base_url)
    return _api


def _load_job_components() -> tuple[Any, Any, Any]:
    global _components
    if _components is not None:
        return _components

    started = time.perf_counter()
    from embedding_engine import EmbeddingEngine
    from ocr_engine import OcrEngine
    from worker_object_store import ObjectStore

    config = _load_config()
    log = logging.getLogger("worker")
    log.info("modal job components init started")
    object_store = ObjectStore(config)
    ocr = OcrEngine(config)
    embedder = EmbeddingEngine(config)
    _components = (embedder, ocr, object_store)
    log.info("modal job components ready elapsed_sec=%.3f", time.perf_counter() - started)
    return _components


@app.function(
    image=worker_image,
    gpu=os.environ.get("MODAL_GPU", "A10"),
    volumes={CACHE_VOLUME_DIR: cache_volume},
    env=worker_env,
    secrets=[worker_secret],
    timeout=60 * 60,
    scaledown_window=DEFAULT_SCALEDOWN_WINDOW_SECONDS,
    max_containers=int(os.environ.get("MODAL_MAX_CONTAINERS", "1")),
)
def process_queue(max_jobs: int = DEFAULT_MAX_JOBS_PER_RUN) -> int:
    return _process_queue_impl(max_jobs, DEFAULT_WORKER_RUN_SECONDS, DEFAULT_IDLE_POLL_SECONDS)


def _process_queue_impl(max_jobs: int, run_seconds: int, idle_poll_seconds: float) -> int:
    started = time.perf_counter()
    import httpx
    from job_runner import run_job

    log = logging.getLogger("worker")
    api = _load_api()

    deadline = time.monotonic() + run_seconds
    completed = 0
    while completed < max_jobs and time.monotonic() < deadline:
        try:
            job = api.claim()
        except httpx.HTTPStatusError as e:
            log.error("claim http=%s body=%s", e.response.status_code, e.response.content[:500])
            break
        except httpx.RequestError as e:
            log.error("claim request error=%s", e)
            break

        if job is None:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                break
            time.sleep(min(idle_poll_seconds, remaining))
            continue

        if completed == 0:
            log.info("modal first job claimed elapsed_sec=%.3f", time.perf_counter() - started)
        embedder, ocr, object_store = _load_job_components()
        if completed == 0:
            log.info("modal first job ready elapsed_sec=%.3f", time.perf_counter() - started)
        run_job(api, embedder, ocr, object_store, job)
        completed += 1

    log.info(
        "modal queue pass completed jobs=%s max_jobs=%s run_seconds=%s elapsed_sec=%.3f",
        completed,
        max_jobs,
        run_seconds,
        time.perf_counter() - started,
    )
    return completed


if os.environ.get("MODAL_ENABLE_SCHEDULE") == "1":

    @app.function(
        image=worker_image,
        gpu=os.environ.get("MODAL_GPU", "A10"),
        volumes={CACHE_VOLUME_DIR: cache_volume},
        env=worker_env,
        secrets=[worker_secret],
        timeout=60 * 60,
        scaledown_window=DEFAULT_SCALEDOWN_WINDOW_SECONDS,
        max_containers=1,
        schedule=modal.Period(minutes=int(os.environ.get("MODAL_POLL_MINUTES", "1"))),
    )
    def poll_queue() -> int:
        return _process_queue_impl(DEFAULT_MAX_JOBS_PER_RUN, DEFAULT_WORKER_RUN_SECONDS, DEFAULT_IDLE_POLL_SECONDS)


@app.local_entrypoint()
def run(max_jobs: int = DEFAULT_MAX_JOBS_PER_RUN) -> None:
    started = time.perf_counter()
    completed = process_queue.remote(max_jobs)
    print(f"completed={completed} elapsed_sec={time.perf_counter() - started:.3f}")
