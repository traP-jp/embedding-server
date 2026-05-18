import gc
import hashlib
import json
import logging
import math
from typing import Any

from worker_config import Config

log = logging.getLogger("worker")

QWEN_MODEL_NAME = "Qwen/Qwen3-VL-Embedding-8B"


class EmbeddingEngine:
    def __init__(self, config: Config) -> None:
        self.config = config
        self.embedder: Any | None = None
        self.torch: Any | None = None

        if config.fake_embeddings:
            log.warning("using fake deterministic embeddings")
            return

        self._load_model()

    def embed(self, item: dict[str, Any]) -> list[float]:
        if not item:
            raise ValueError("embedding input required")
        if self.config.fake_embeddings:
            return self._fake_embedding(item)
        return self._embed_with_model(item)

    def _load_model(self) -> None:
        import torch
        from scripts.qwen3_vl_embedding import Qwen3VLEmbedder

        self.torch = torch
        dtype = self._resolve_torch_dtype(self.config.torch_dtype)

        log.info(
            "loading embedding model=%s dtype=%s attn=%s cuda=%s",
            QWEN_MODEL_NAME,
            dtype,
            self.config.attn_implementation or "default",
            torch.cuda.is_available(),
        )
        if torch.cuda.is_available():
            log.info("gpu=%s", torch.cuda.get_device_name(0))

        self.embedder = Qwen3VLEmbedder(
            model_name_or_path=QWEN_MODEL_NAME,
            torch_dtype=dtype,
            low_cpu_mem_usage=True,
            attn_implementation=self.config.attn_implementation,
        )
        log.info("embedding model loaded")

    def _embed_with_model(self, item: dict[str, Any]) -> list[float]:
        assert self.embedder is not None
        assert self.torch is not None

        with self.torch.no_grad():
            embeddings = self.embedder.process([item])

        vector = embeddings.detach().to("cpu").float()
        if len(vector) != 1: # バッチサイズは1だから1のはず
            raise ValueError(f"embedding job must produce exactly one vector, got {len(vector)}")

        gc.collect()
        if self.torch.cuda.is_available():
            self.torch.cuda.empty_cache()
        return vector[0].tolist()

    def _fake_embedding(self, item: dict[str, Any]) -> list[float]:
        dim = self.config.fake_embedding_dim
        seed = json.dumps(item, sort_keys=True, default=str).encode("utf-8")
        digest = hashlib.sha256(seed).digest()
        values: list[float] = []
        while len(values) < dim:
            digest = hashlib.sha256(digest).digest()
            values.extend((byte / 127.5) - 1.0 for byte in digest)
        return _normalize(values[:dim])


def _normalize(vector: list[float]) -> list[float]:
    norm: int | float = math.sqrt(sum(value * value for value in vector))
    if norm == 0 or not math.isfinite(norm):
        raise ValueError("cannot normalize embedding vector")
    return [float(value / norm) for value in vector]
