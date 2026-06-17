from __future__ import annotations

import os
from dataclasses import dataclass


def required_env(name: str) -> str:
    raw = os.environ.get(name)
    if raw is None or raw.strip() == "":
        raise ValueError(f"missing required environment variable: {name}")
    return raw.strip()


def env_bool(name: str) -> bool:
    raw = required_env(name)
    return raw.strip().lower() in {"1", "true", "yes", "on"}


def optional_env_bool(name: str, fallback: bool) -> bool:
    raw = os.environ.get(name)
    if raw is None or raw.strip() == "":
        return fallback
    return raw.strip().lower() in {"1", "true", "yes", "on"}


def env_int(name: str) -> int:
    return int(required_env(name))


def optional_env_int(name: str, fallback: int) -> int:
    raw = os.environ.get(name)
    if raw is None or raw.strip() == "":
        return fallback
    return int(raw)


def env_float(name: str) -> float:
    return float(required_env(name))


@dataclass(frozen=True)
class Config:
    api_base_url: str
    poll_interval_seconds: float
    model_device_map: str
    model_max_memory_cuda: str
    model_max_memory_cpu: str
    embedding_max_pixels: int
    torch_dtype: str
    quantization: str
    bnb_4bit_quant_type: str
    bnb_4bit_use_double_quant: bool
    bnb_4bit_compute_dtype: str
    attn_implementation: str
    fake_embeddings: bool
    fake_embedding_dim: int
    ocr_enabled: bool
    ocr_device: str
    ocr_scale: int
    ocr_rec_threshold: float
    ocr_det_threshold: float
    ocr_max_chars: int
    ocr_visualize: bool
    s3_endpoint_url: str
    s3_bucket: str
    s3_region: str
    s3_access_key_id: str
    s3_secret_access_key: str

    @classmethod
    def from_env(cls) -> "Config":
        api_host = required_env("API_HOST")
        api_port = required_env("API_PORT")
        api_base_url = f"http://{api_host}:{api_port}"

        return cls(
            api_base_url=api_base_url,
            poll_interval_seconds=env_float("POLL_INTERVAL_SECONDS"), # ジョブが無いときの待機秒数
            model_device_map=os.environ.get("MODEL_DEVICE_MAP", "auto").strip().lower(),
            model_max_memory_cuda=os.environ.get("MODEL_MAX_MEMORY_CUDA", "").strip(),
            model_max_memory_cpu=os.environ.get("MODEL_MAX_MEMORY_CPU", "").strip(),
            embedding_max_pixels=optional_env_int("EMBEDDING_MAX_PIXELS", 256 * 256),
            torch_dtype=required_env("TORCH_DTYPE").lower(),
            quantization=os.environ.get("QUANTIZATION", "none").strip().lower(),
            bnb_4bit_quant_type=os.environ.get("BNB_4BIT_QUANT_TYPE", "nf4").strip().lower(),
            bnb_4bit_use_double_quant=optional_env_bool("BNB_4BIT_USE_DOUBLE_QUANT", True),
            bnb_4bit_compute_dtype=os.environ.get("BNB_4BIT_COMPUTE_DTYPE", "bfloat16").strip().lower(),
            attn_implementation=required_env("ATTN_IMPLEMENTATION"),
            fake_embeddings=optional_env_bool("EMBEDDING_WORKER_FAKE", False), # 埋め込みを偽のものにするか
            fake_embedding_dim=optional_env_int("FAKE_EMBEDDING_DIM", 1024),
            ocr_enabled=env_bool("OCR_ENABLED"),
            ocr_device=required_env("OCR_DEVICE"),
            ocr_scale=env_int("OCR_SCALE"),
            ocr_rec_threshold=env_float("OCR_REC_THRESHOLD"),# OCRの認識しきい値
            ocr_det_threshold=env_float("OCR_DET_THRESHOLD"),#OCRの検出しきい値
            ocr_max_chars=env_int("OCR_MAX_CHARS"),
            ocr_visualize=env_bool("OCR_VISUALIZE"),
            s3_endpoint_url=os.environ.get("S3_ENDPOINT_URL", "").strip(),
            s3_bucket=os.environ.get("S3_BUCKET", "").strip(),
            s3_region=os.environ.get("S3_REGION", "").strip(),
            s3_access_key_id=os.environ.get("S3_ACCESS_KEY_ID", "").strip(),
            s3_secret_access_key=os.environ.get("S3_SECRET_ACCESS_KEY", "").strip(),
        )
