import gc
import hashlib
import json
import logging
import math
import time
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
        started = time.perf_counter()
        try:
            import torch
            from scripts.qwen3_vl_embedding import Qwen3VLEmbedder

            self.torch = torch
            dtype = self._resolve_torch_dtype(self.config.torch_dtype)
            quantization_config = self._build_quantization_config(torch)
            log.info(
                "embedding model load started model=%s dtype=%s quantization=%s attn=%s cuda=%s cuda_devices=%s",
                QWEN_MODEL_NAME,
                dtype,
                self.config.quantization,
                self.config.attn_implementation or "default",
                torch.cuda.is_available(),
                torch.cuda.device_count() if torch.cuda.is_available() else 0,
            )

            if torch.cuda.is_available():
                current_device = torch.cuda.current_device()
                props = torch.cuda.get_device_properties(current_device)
                log.info(
                    "embedding model load gpu=%s device_index=%s total_memory_mib=%s",
                    torch.cuda.get_device_name(current_device),
                    current_device,
                    props.total_memory // (1024 * 1024),
                )

            model_kwargs: dict[str, Any] = {
                "model_name_or_path": QWEN_MODEL_NAME,
                "dtype": dtype,
                "low_cpu_mem_usage": True,
                "attn_implementation": self.config.attn_implementation,
                "max_pixels": self.config.embedding_max_pixels,
            }
            if quantization_config is not None:
                model_kwargs["quantization_config"] = quantization_config
                model_kwargs["device_map"] = self._resolve_device_map(self.config.model_device_map)
                max_memory = self._resolve_max_memory()
                if max_memory:
                    model_kwargs["max_memory"] = max_memory

            log.info("embedding model init started")
            self.embedder = Qwen3VLEmbedder(**model_kwargs)
            log.info("embedding model loaded elapsed_sec=%.3f", time.perf_counter() - started)
        except Exception:
            log.exception(
                "embedding model load failed elapsed_sec=%.3f",
                time.perf_counter() - started,
            )
            raise

    def _resolve_torch_dtype(self, name: str) -> Any:
        import torch

        match name.strip().lower():
            case "float16" | "fp16" | "half":
                return torch.float16
            case "bfloat16" | "bf16":
                return torch.bfloat16
            case "float32" | "fp32" | "float":
                return torch.float32
            case "auto":
                return "auto"
            case _:
                raise ValueError(f"unsupported TORCH_DTYPE: {name}")

    def _resolve_device_map(self, name: str) -> Any:
        match name.strip().lower():
            case "" | "auto":
                return "auto"
            case "cuda" | "cuda:0" | "gpu" | "single-gpu":
                return {"": 0}
            case "cpu":
                return {"": "cpu"}
            case _:
                raise ValueError(f"unsupported MODEL_DEVICE_MAP: {name}")

    def _resolve_max_memory(self) -> dict[Any, str] | None:
        max_memory: dict[Any, str] = {}
        if self.config.model_max_memory_cuda:
            max_memory[0] = self.config.model_max_memory_cuda
        if self.config.model_max_memory_cpu:
            max_memory["cpu"] = self.config.model_max_memory_cpu
        return max_memory or None

    def _build_quantization_config(self, torch: Any) -> Any | None:
        match self.config.quantization:
            case "" | "none" | "false" | "off":
                return None
            case "4bit" | "bnb-4bit":
                from transformers import BitsAndBytesConfig

                return BitsAndBytesConfig(
                    load_in_4bit=True,
                    bnb_4bit_quant_type=self.config.bnb_4bit_quant_type,
                    bnb_4bit_use_double_quant=self.config.bnb_4bit_use_double_quant,
                    bnb_4bit_compute_dtype=self._resolve_torch_dtype(self.config.bnb_4bit_compute_dtype),
                )
            case "8bit" | "bnb-8bit":
                from transformers import BitsAndBytesConfig

                return BitsAndBytesConfig(load_in_8bit=True)
            case _:
                raise ValueError(f"unsupported QUANTIZATION: {self.config.quantization}")

    def _embed_with_model(self, item: dict[str, Any]) -> list[float]:
        assert self.embedder is not None
        assert self.torch is not None

        started = time.perf_counter()
        log.info(
            "embedding inference started text_parts=%s image_count=%s",
            _count_text_parts(item),
            _count_images(item),
        )
        with self.torch.no_grad():
            embeddings = self.embedder.process([item])

        step_started = time.perf_counter()
        vector = embeddings.detach().to("cpu").float()
        log.info("embedding inference step=copy_to_cpu elapsed_sec=%.3f", time.perf_counter() - step_started)
        if len(vector) != 1: # バッチサイズは1だから1のはず
            raise ValueError(f"embedding job must produce exactly one vector, got {len(vector)}")

        step_started = time.perf_counter()
        gc.collect()
        if self.torch.cuda.is_available():
            self.torch.cuda.empty_cache()
        log.info("embedding inference step=cleanup elapsed_sec=%.3f", time.perf_counter() - step_started)
        log.info(
            "embedding inference completed dim=%s elapsed_sec=%.3f",
            len(vector[0]),
            time.perf_counter() - started,
        )
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


def _count_text_parts(item: dict[str, Any]) -> int:
    text = item.get("text")
    if isinstance(text, list):
        return len(text)
    if isinstance(text, str) and text:
        return 1
    return 0


def _count_images(item: dict[str, Any]) -> int:
    image = item.get("image")
    if isinstance(image, list):
        return len(image)
    if image:
        return 1
    return 0
