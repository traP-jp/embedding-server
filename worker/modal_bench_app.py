"""Modal 上で画像埋め込みの起動込みコスト・時間を測る専用 App。

使い方:
  mise run modal-cost-bench
  # GPU を絞る例: BENCH_GPUS=A10 mise run modal-cost-bench
  # OCR を切る例: BENCH_OCR_ENABLED=false mise run modal-cost-bench

fixtures は benchmark/fixtures/ の画像を使う（先に fetch-traq-samples）。
デフォルトは OCR あり（本番ジョブに近い経路）。
"""

from __future__ import annotations

import json
import logging
import os
import time
import tomllib
from io import BytesIO
from pathlib import Path
from typing import Any

import modal

APP_NAME = "embedding-worker-bench"
WORKER_DIR = Path(__file__).resolve().parent
FIXTURES_DIR = WORKER_DIR.parent / "benchmark" / "fixtures"
RESULTS_DIR = WORKER_DIR.parent / "benchmark"
CACHE_VOLUME_DIR = "/root/cache-volume"
DEFAULT_GPUS = ("A10", "T4")
GPU_USD_PER_SEC = {
    "A10": 0.000306,
    "T4": 0.000164,
    "L4": 0.000222,
}
JPY_PER_USD = float(os.environ.get("BENCH_JPY_PER_USD", "150"))
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


def _worker_dependencies() -> list[str]:
    pyproject_path = WORKER_DIR / "pyproject.toml"
    if not pyproject_path.exists():
        # modal run 時、リモートではスクリプトが /root に載り worker 本体は /app
        pyproject_path = Path("/app/pyproject.toml")

    with pyproject_path.open("rb") as f:
        project = tomllib.load(f)["project"]
    return [
        "torch>=2.7.0",
        "torchvision>=0.22.0",
        *project.get("dependencies", []),
        *project.get("optional-dependencies", {}).get("runtime", []),
    ]


def _bench_worker_env(*, max_memory_cuda: str, torch_dtype: str) -> dict[str, str]:
    max_pixels = os.environ.get("BENCH_MAX_PIXELS", "1048576")
    return {
        "XDG_CACHE_HOME": f"{CACHE_VOLUME_DIR}/xdg",
        "HF_HOME": f"{CACHE_VOLUME_DIR}/huggingface",
        "TRANSFORMERS_CACHE": f"{CACHE_VOLUME_DIR}/huggingface/transformers",
        "WORKER_API_MODE": "url",
        "API_BASE_URL": "http://127.0.0.1:9",
        "S3_ENDPOINT_URL": "http://127.0.0.1:9",
        "S3_BUCKET": "bench",
        "S3_REGION": "auto",
        "S3_ACCESS_KEY_ID": "bench",
        "S3_SECRET_ACCESS_KEY": "bench",
        "ATTN_IMPLEMENTATION": "sdpa",
        "POLL_INTERVAL_SECONDS": "1",
        "EMBEDDING_WORKER_FAKE": "false",
        "FAKE_EMBEDDING_DIM": "1024",
        "MODEL_DEVICE_MAP": "gpu",
        "MODEL_MAX_MEMORY_CUDA": max_memory_cuda,
        "MODEL_MAX_MEMORY_CPU": "24GiB",
        "EMBEDDING_MAX_PIXELS": max_pixels,
        "EMBEDDING_BATCH_SIZE": os.environ.get("BENCH_BATCH_SIZE", "1"),
        "TORCH_DTYPE": torch_dtype,
        "QUANTIZATION": "4bit",
        "BNB_4BIT_QUANT_TYPE": "nf4",
        "BNB_4BIT_USE_DOUBLE_QUANT": "true",
        "BNB_4BIT_COMPUTE_DTYPE": torch_dtype,
        "OCR_ENABLED": os.environ.get("BENCH_OCR_ENABLED", "true"),
        "OCR_DEVICE": os.environ.get("BENCH_OCR_DEVICE", "cuda"),
        "OCR_SCALE": os.environ.get("BENCH_OCR_SCALE", "2"),
        "OCR_REC_THRESHOLD": "0.2",
        "OCR_DET_THRESHOLD": "0.2",
        "OCR_MAX_CHARS": "256",
        "OCR_VISUALIZE": "false",
        "PYTHONUNBUFFERED": "1",
        "PYTHONPATH": "/app",
        "HF_HUB_DISABLE_XET": "1",
        "HF_XET_DISABLE": "1",
        "PYTORCH_CUDA_ALLOC_CONF": "expandable_segments:True",
    }


def _memory_for_gpu(gpu: str) -> str:
    if gpu.upper().startswith("T4"):
        return os.environ.get("BENCH_MAX_MEMORY_CUDA_T4", "14GiB")
    return os.environ.get("BENCH_MAX_MEMORY_CUDA_A10", "20GiB")


def _dtype_for_gpu(gpu: str) -> str:
    # T4 (Turing) は bf16 が遅く、fp16 が本命。A10 は bf16 で問題なし。
    if gpu.upper().startswith("T4"):
        return os.environ.get("BENCH_TORCH_DTYPE_T4", "float16")
    return os.environ.get("BENCH_TORCH_DTYPE_A10", "bfloat16")


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

# ローカル定義時だけ fixtures を載せる（リモート再 import 時は /fixtures パスが無い）
if FIXTURES_DIR.exists():
    bench_image = worker_image.add_local_dir(
        FIXTURES_DIR,
        remote_path="/fixtures",
        ignore=["manifest.json", ".gitkeep"],
    )
else:
    bench_image = worker_image

app = modal.App(APP_NAME)
cache_volume = modal.Volume.from_name("embedding-worker-cache", create_if_missing=True)


def _configure_logging() -> None:
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")


def _list_fixture_images(root: Path) -> list[Path]:
    files = sorted(
        p
        for p in root.iterdir()
        if p.is_file() and p.suffix.lower() in {".png", ".jpg", ".jpeg", ".webp"}
    )
    if not files:
        raise FileNotFoundError(f"no fixture images in {root}")
    return files


def _run_benchmark(gpu: str) -> dict[str, Any]:
    from PIL import Image

    from embedding_engine import EmbeddingEngine
    from ocr_engine import OcrEngine
    from worker_config import Config

    _configure_logging()
    log = logging.getLogger("modal-bench")
    images = _list_fixture_images(Path("/fixtures"))
    log.info(
        "bench start gpu=%s images=%s batch_size=%s ocr=%s max_pixels=%s dtype=%s",
        gpu,
        len(images),
        os.environ.get("EMBEDDING_BATCH_SIZE", "1"),
        os.environ.get("OCR_ENABLED", "false"),
        os.environ.get("EMBEDDING_MAX_PIXELS"),
        os.environ.get("TORCH_DTYPE"),
    )

    wall_started = time.perf_counter()
    config = Config()

    ocr_load_started = time.perf_counter()
    ocr = OcrEngine(config)
    ocr_load_sec = time.perf_counter() - ocr_load_started

    embed_load_started = time.perf_counter()
    embedder = EmbeddingEngine(config)
    embed_load_sec = time.perf_counter() - embed_load_started
    load_sec = ocr_load_sec + embed_load_sec

    batch_size = max(1, config.embedding_batch_size)
    per_image: list[dict[str, Any]] = []
    per_batch: list[dict[str, Any]] = []
    embed_total_sec = 0.0
    ocr_total_sec = 0.0

    # 本番と同様: 画像ごとに OCR → text 付き item を作り、その後バッチ推論。
    prepared: list[tuple[Path, dict[str, Any], int, float, int]] = []
    for path in images:
        raw = path.read_bytes()
        pil = Image.open(BytesIO(raw)).convert("RGB")
        ocr_started = time.perf_counter()
        ocr_text = ocr.read_image_text(pil)
        ocr_sec = time.perf_counter() - ocr_started
        ocr_total_sec += ocr_sec
        item: dict[str, Any] = {"image": [pil]}
        if ocr_text:
            item["text"] = [f"[OCR]\n{ocr_text}"]
        prepared.append((path, item, len(raw), ocr_sec, len(ocr_text)))
        log.info(
            "bench ocr file=%s ocr_sec=%.3f chars=%s",
            path.name,
            ocr_sec,
            len(ocr_text),
        )

    for batch_index in range(0, len(prepared), batch_size):
        batch = prepared[batch_index : batch_index + batch_size]
        items = [item for _, item, _, _, _ in batch]
        started = time.perf_counter()
        vectors = embedder.embed_many(items)
        elapsed = time.perf_counter() - started
        embed_total_sec += elapsed
        per_item_sec = elapsed / len(batch)
        per_batch.append(
            {
                "batch_index": batch_index // batch_size,
                "size": len(batch),
                "embed_sec": round(elapsed, 3),
                "embed_sec_per_image": round(per_item_sec, 3),
            }
        )
        for (path, _, nbytes, ocr_sec, ocr_chars), vector in zip(batch, vectors, strict=True):
            per_image.append(
                {
                    "file": path.name,
                    "bytes": nbytes,
                    "ocr_sec": round(ocr_sec, 3),
                    "ocr_chars": ocr_chars,
                    "embed_sec": round(per_item_sec, 3),
                    "batch_embed_sec": round(elapsed, 3),
                    "dim": len(vector),
                }
            )
        log.info(
            "bench batch index=%s size=%s embed_sec=%.3f per_image_sec=%.3f",
            batch_index // batch_size,
            len(batch),
            elapsed,
            per_item_sec,
        )

    total_sec = time.perf_counter() - wall_started
    rate = GPU_USD_PER_SEC.get(gpu)
    est_usd = None if rate is None else total_sec * rate
    est_jpy = None if est_usd is None else est_usd * JPY_PER_USD
    embed_secs = [row["embed_sec"] for row in per_image]
    ocr_secs = [row["ocr_sec"] for row in per_image]
    batch_secs = [row["embed_sec"] for row in per_batch]
    result = {
        "gpu": gpu,
        "torch_dtype": config.torch_dtype,
        "bnb_compute_dtype": config.bnb_4bit_compute_dtype,
        "quantization": config.quantization,
        "embedding_max_pixels": config.embedding_max_pixels,
        "embedding_batch_size": batch_size,
        "ocr_enabled": config.ocr_enabled,
        "image_count": len(images),
        "batch_count": len(per_batch),
        "ocr_load_sec": round(ocr_load_sec, 3),
        "embed_load_sec": round(embed_load_sec, 3),
        "load_sec": round(load_sec, 3),
        "ocr_total_sec": round(ocr_total_sec, 3),
        "ocr_mean_sec": round(sum(ocr_secs) / len(ocr_secs), 3),
        "embed_total_sec": round(embed_total_sec, 3),
        "embed_mean_sec_per_image": round(sum(embed_secs) / len(embed_secs), 3),
        "embed_mean_sec_per_batch": round(sum(batch_secs) / len(batch_secs), 3),
        "embed_min_sec_per_batch": round(min(batch_secs), 3),
        "embed_max_sec_per_batch": round(max(batch_secs), 3),
        "total_sec": round(total_sec, 3),
        "gpu_usd_per_sec": rate,
        "est_cost_usd": None if est_usd is None else round(est_usd, 4),
        "est_cost_jpy": None if est_jpy is None else round(est_jpy, 1),
        "note": "est_cost は公開単価×コンテナ内 wall 秒の概算。確定値は modal billing report で確認。",
        "per_batch": per_batch,
        "per_image": per_image,
    }
    log.info(
        "bench done %s",
        json.dumps({k: v for k, v in result.items() if k not in {"per_image", "per_batch"}}),
    )
    return result


@app.function(
    image=bench_image,
    gpu="A10",
    volumes={CACHE_VOLUME_DIR: cache_volume},
    env=_bench_worker_env(
        max_memory_cuda=_memory_for_gpu("A10"),
        torch_dtype=_dtype_for_gpu("A10"),
    ),
    timeout=60 * 60,
    scaledown_window=30,
)
def bench_a10() -> dict[str, Any]:
    return _run_benchmark("A10")


@app.function(
    image=bench_image,
    gpu="T4",
    volumes={CACHE_VOLUME_DIR: cache_volume},
    env=_bench_worker_env(
        max_memory_cuda=_memory_for_gpu("T4"),
        torch_dtype=_dtype_for_gpu("T4"),
    ),
    timeout=60 * 60,
    scaledown_window=30,
)
def bench_t4() -> dict[str, Any]:
    return _run_benchmark("T4")


def _parse_gpus(raw: str | None) -> list[str]:
    if not raw:
        return list(DEFAULT_GPUS)
    gpus = [part.strip().upper() for part in raw.split(",") if part.strip()]
    unknown = [g for g in gpus if g not in {"A10", "T4"}]
    if unknown:
        raise ValueError(f"unsupported BENCH_GPUS={unknown}; use A10 and/or T4")
    return gpus


@app.local_entrypoint()
def main(gpus: str = "") -> None:
    selected = _parse_gpus(gpus or os.environ.get("BENCH_GPUS"))
    image_files = list(FIXTURES_DIR.glob("*.png")) + list(FIXTURES_DIR.glob("*.jpg")) + list(FIXTURES_DIR.glob("*.webp"))
    if not image_files:
        raise SystemExit(
            f"fixtures not found in {FIXTURES_DIR}. Run: r_session=... mise run fetch-traq-samples"
        )

    runners = {
        "A10": bench_a10,
        "T4": bench_t4,
    }
    results: list[dict[str, Any]] = []
    for gpu in selected:
        print(f"=== bench gpu={gpu} ===")
        result = runners[gpu].remote()
        results.append(result)
        summary = {k: v for k, v in result.items() if k not in {"per_image", "per_batch"}}
        print(json.dumps(summary, ensure_ascii=False, indent=2))

    RESULTS_DIR.mkdir(parents=True, exist_ok=True)
    out = RESULTS_DIR / "modal-cost-bench-ocr-results.json"
    out.write_text(json.dumps(results, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"wrote {out}")
    print("Confirm actual spend with: modal billing report --for today --show-resources")
