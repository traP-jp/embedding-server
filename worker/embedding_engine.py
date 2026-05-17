from __future__ import annotations

import hashlib
import importlib.util
import json
import logging
import sys
from pathlib import Path
from typing import Any

from vector_math import mean_vectors, normalize
from worker_config import Config, env_int

log = logging.getLogger("worker")


class EmbeddingEngine:
    def __init__(self, config: Config) -> None:
        self.config = config
        self._fake_dim = env_int("FAKE_EMBEDDING_DIM")

        if config.fake_embeddings:
            log.warning("using fake deterministic embeddings")
            self.embedder = None
            self.torch = None
            return

        import torch

        self.torch = torch
        qwen_embedder = _load_qwen_embedder(config.model_script)
        dtype = self._resolve_torch_dtype(config.torch_dtype)

        kwargs: dict[str, Any] = {
            "torch_dtype": dtype,
            "low_cpu_mem_usage": True,
        }
        if config.attn_implementation:
            kwargs["attn_implementation"] = config.attn_implementation
        if config.max_pixels is not None:
            kwargs["max_pixels"] = config.max_pixels

        log.info(
            "loading embedding model=%s dtype=%s attn=%s cuda=%s",
            config.model_name,
            dtype,
            config.attn_implementation or "default",
            torch.cuda.is_available(),
        )
        if torch.cuda.is_available():
            log.info("gpu=%s", torch.cuda.get_device_name(0))

        self.embedder = qwen_embedder(model_name_or_path=config.model_name, **kwargs)
        log.info("embedding model loaded")

    def embed(self, items: list[dict[str, Any]]) -> list[float]:
        if not items:
            raise ValueError("embedding input required")
        if self.config.fake_embeddings:
            vectors = [self._fake_embedding(item) for item in items]
            return normalize(mean_vectors(vectors))

        assert self.embedder is not None
        assert self.torch is not None

        vectors: list[list[float]] = []
        for offset in range(0, len(items), self.config.batch_size):
            batch = items[offset : offset + self.config.batch_size]
            with self.torch.no_grad():
                embeddings = self.embedder.process(batch)
            if isinstance(embeddings, self.torch.Tensor):
                embeddings = embeddings.detach().to("cpu").float()
                batch_vectors = embeddings.tolist()
            else:
                batch_vectors = embeddings

            if not isinstance(batch_vectors, list):
                raise TypeError(f"unexpected embedding output: {type(batch_vectors)}")
            vectors.extend(batch_vectors)

            if self.torch.cuda.is_available():
                self.torch.cuda.empty_cache()

        return normalize(mean_vectors(vectors))

    def _resolve_torch_dtype(self, dtype_name: str) -> Any:
        assert self.torch is not None
        torch = self.torch
        if dtype_name == "auto":
            if torch.cuda.is_available() and getattr(torch.cuda, "is_bf16_supported", lambda: False)():
                return torch.bfloat16
            if torch.cuda.is_available():
                return torch.float16
            return torch.float32

        mapping = {
            "float16": torch.float16,
            "fp16": torch.float16,
            "bfloat16": torch.bfloat16,
            "bf16": torch.bfloat16,
            "float32": torch.float32,
            "fp32": torch.float32,
        }
        try:
            return mapping[dtype_name]
        except KeyError as e:
            raise ValueError(f"unsupported TORCH_DTYPE={dtype_name}") from e

    def _fake_embedding(self, item: dict[str, Any]) -> list[float]:
        seed = json.dumps(item, sort_keys=True, default=str).encode("utf-8")
        digest = hashlib.sha256(seed).digest()
        values: list[float] = []
        while len(values) < self._fake_dim:
            digest = hashlib.sha256(digest).digest()
            values.extend((byte / 127.5) - 1.0 for byte in digest)
        return normalize(values[: self._fake_dim])


def _load_qwen_embedder(script_path: str) -> Any:
    try:
        from qwen3_vl_embedding import Qwen3VLEmbedder

        return Qwen3VLEmbedder
    except ImportError:
        pass

    path = Path(script_path)
    if not path.exists():
        raise FileNotFoundError(
            f"Qwen3 embedding script not found: {path}. "
            "Set QWEN_EMBEDDING_SCRIPT or build the worker image."
        )

    spec = importlib.util.spec_from_file_location("qwen3_vl_embedding", path)
    if spec is None or spec.loader is None:
        raise ImportError(f"failed to load Qwen3 embedding script: {path}")

    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module.Qwen3VLEmbedder
