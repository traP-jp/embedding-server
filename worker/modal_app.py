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
DEFAULT_MAX_JOBS_PER_RUN = int(os.environ.get("MODAL_MAX_JOBS_PER_RUN", "0"))  # 0 = キュー空まで
# process_queue の Modal function timeout（秒）。deploy 時に反映。
DEFAULT_FUNCTION_TIMEOUT_SECONDS = int(os.environ.get("MODAL_FUNCTION_TIMEOUT_SECONDS", str(60 * 60 * 3)))
DEFAULT_SCALEDOWN_WINDOW_SECONDS = int(os.environ.get("MODAL_SCALEDOWN_WINDOW_SECONDS", "30"))
# 0 = ソフトな時間上限なし（Modal function timeout までキューを空にする）
DEFAULT_WORKER_RUN_SECONDS = int(os.environ.get("MODAL_WORKER_RUN_SECONDS", "0"))
DEFAULT_GPU = os.environ.get("MODAL_GPU", "T4")


def _worker_dependencies() -> list[str]:
    candidates = [
        WORKER_DIR / "pyproject.toml",
        Path("/app/pyproject.toml"),
        Path(__file__).resolve().parent / "pyproject.toml",
    ]
    pyproject_path = next((path for path in candidates if path.exists()), None)
    # run_batch など worker をマウントしないコンテナでも module import が通るようにする。
    # 画像ビルド自体は deploy 時にローカルの pyproject で解決される。
    if pyproject_path is None:
        return [
            "torch>=2.7.0",
            "torchvision>=0.22.0",
            "accelerate>=1.0.0",
            "bitsandbytes>=0.46.1",
            "numpy>=1.26.0",
            "qwen-vl-utils>=0.0.14",
            "transformers>=4.57.0,<5",
            "yomitoku>=0.13.0",
            "boto3>=1.42.0,<2",
            "httpx>=0.28.0,<1",
            "pillow>=10.0.0",
            "pydantic-settings>=2,<3",
        ]

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
        "MODAL_TRIGGER_TOKEN",
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
    gpu=DEFAULT_GPU,
    volumes={CACHE_VOLUME_DIR: cache_volume},
    env=worker_env,
    secrets=[worker_secret],
    timeout=DEFAULT_FUNCTION_TIMEOUT_SECONDS,
    scaledown_window=DEFAULT_SCALEDOWN_WINDOW_SECONDS,
    max_containers=int(os.environ.get("MODAL_MAX_CONTAINERS", "1")),
)
def process_queue(max_jobs: int = DEFAULT_MAX_JOBS_PER_RUN) -> int:
    return _process_queue_impl(max_jobs, DEFAULT_WORKER_RUN_SECONDS)


def _process_queue_impl(max_jobs: int, run_seconds: int) -> int:
    started = time.perf_counter()
    import httpx
    from job_runner import claim_jobs, run_jobs

    log = logging.getLogger("worker")
    config = _load_config()
    api = _load_api()
    batch_size = config.embedding_batch_size
    # max_jobs<=0 / run_seconds<=0 は「キューが空になるまで」
    job_limit = max_jobs if max_jobs > 0 else None
    deadline = time.monotonic() + run_seconds if run_seconds > 0 else None

    completed = 0
    while True:
        if job_limit is not None and completed >= job_limit:
            break
        if deadline is not None and time.monotonic() >= deadline:
            break

        claim_n = batch_size
        if job_limit is not None:
            claim_n = min(batch_size, job_limit - completed)

        try:
            # 起動条件は画像キューだが、起きている間は text もついでに消化する
            # （text は 1 件あたり <1s。省略時 = text+image、サーバー側で text 優先）。
            jobs = claim_jobs(api, claim_n)
        except httpx.HTTPStatusError as e:
            log.error("claim http=%s body=%s", e.response.status_code, e.response.content[:500])
            break
        except httpx.RequestError as e:
            log.error("claim request error=%s", e)
            break

        # push 起動前提: キューが空なら待たずに終了する。
        if not jobs:
            break

        if completed == 0:
            log.info(
                "modal first batch claimed size=%s elapsed_sec=%.3f",
                len(jobs),
                time.perf_counter() - started,
            )
        embedder, ocr, object_store = _load_job_components()
        if completed == 0:
            log.info(
                "modal first batch ready size=%s batch_size=%s drain_all=%s elapsed_sec=%.3f",
                len(jobs),
                batch_size,
                job_limit is None,
                time.perf_counter() - started,
            )
        completed += run_jobs(api, embedder, ocr, object_store, jobs)

    log.info(
        "modal queue pass completed jobs=%s max_jobs=%s batch_size=%s run_seconds=%s elapsed_sec=%.3f",
        completed,
        max_jobs,
        batch_size,
        run_seconds,
        time.perf_counter() - started,
    )
    return completed


# Go から閾値到達時に叩く薄い起動口。GPU は使わず process_queue を spawn するだけ。
# fastapi_endpoint は関数ごとに固定 URL を出す（ASGI の POST リダイレクト問題を避ける）。
trigger_image = modal.Image.debian_slim(python_version="3.12").uv_pip_install("fastapi[standard]>=0.115.0")


@app.function(
    image=trigger_image,
    secrets=[worker_secret],
    timeout=60,
    scaledown_window=10,
)
@modal.fastapi_endpoint(method="POST")
def run_batch(token: str = "") -> dict[str, Any]:
    import fastapi

    expected = os.environ.get("MODAL_TRIGGER_TOKEN", "").strip()
    if not expected:
        raise fastapi.HTTPException(status_code=500, detail="MODAL_TRIGGER_TOKEN is not configured")
    if token != expected:
        raise fastapi.HTTPException(status_code=401, detail="unauthorized")

    call = process_queue.spawn()  # デフォルトでキュー空までドレイン
    return {"status": "started", "call_id": call.object_id}


if os.environ.get("MODAL_ENABLE_SCHEDULE") == "1":

    @app.function(
        image=worker_image,
        gpu=DEFAULT_GPU,
        volumes={CACHE_VOLUME_DIR: cache_volume},
        env=worker_env,
        secrets=[worker_secret],
        timeout=60 * 60,
        scaledown_window=DEFAULT_SCALEDOWN_WINDOW_SECONDS,
        max_containers=1,
        schedule=modal.Period(minutes=int(os.environ.get("MODAL_POLL_MINUTES", "1"))),
    )
    def poll_queue() -> int:
        return _process_queue_impl(DEFAULT_MAX_JOBS_PER_RUN, DEFAULT_WORKER_RUN_SECONDS)


@app.local_entrypoint()
def run(max_jobs: int = DEFAULT_MAX_JOBS_PER_RUN) -> None:
    started = time.perf_counter()
    completed = process_queue.remote(max_jobs)
    print(f"completed={completed} elapsed_sec={time.perf_counter() - started:.3f}")
