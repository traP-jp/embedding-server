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


def env_int(name: str) -> int:
    return int(required_env(name))


def env_float(name: str) -> float:
    return float(required_env(name))


@dataclass(frozen=True)
class Config:
    api_base_url: str
    poll_interval_seconds: float
    torch_dtype: str
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

    @classmethod
    def from_env(cls) -> "Config":
        api_host = required_env("API_HOST")
        api_port = required_env("API_PORT")
        api_base_url = f"http://{api_host}:{api_port}"

        return cls(
            api_base_url=api_base_url,
            poll_interval_seconds=env_float("POLL_INTERVAL_SECONDS"), # ジョブが無いときの待機秒数
            torch_dtype=required_env("TORCH_DTYPE").lower(),
            attn_implementation=required_env("ATTN_IMPLEMENTATION"),
            fake_embeddings=env_bool("EMBEDDING_WORKER_FAKE"), # 埋め込みを偽のものにするか
            fake_embedding_dim=env_int("FAKE_EMBEDDING_DIM"),
            ocr_enabled=env_bool("OCR_ENABLED"),
            ocr_device=required_env("OCR_DEVICE"),
            ocr_scale=env_int("OCR_SCALE"),
            ocr_rec_threshold=env_float("OCR_REC_THRESHOLD"),# OCRの認識しきい値
            ocr_det_threshold=env_float("OCR_DET_THRESHOLD"),#OCRの検出しきい値
            ocr_max_chars=env_int("OCR_MAX_CHARS"),
            ocr_visualize=env_bool("OCR_VISUALIZE"),
        )
