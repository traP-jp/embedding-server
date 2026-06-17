from __future__ import annotations

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class Config(BaseSettings):
    model_config = SettingsConfigDict(
        case_sensitive=True,
        env_ignore_empty=True,
        extra="ignore",
        frozen=True,
    )

    api_host: str = Field(validation_alias="API_HOST")
    api_port: str = Field(validation_alias="API_PORT")
    poll_interval_seconds: float = Field(validation_alias="POLL_INTERVAL_SECONDS")
    model_device_map: str = Field(validation_alias="MODEL_DEVICE_MAP")
    model_max_memory_cuda: str = Field(validation_alias="MODEL_MAX_MEMORY_CUDA")
    model_max_memory_cpu: str = Field(validation_alias="MODEL_MAX_MEMORY_CPU")
    embedding_max_pixels: int = Field(validation_alias="EMBEDDING_MAX_PIXELS")
    torch_dtype: str = Field(validation_alias="TORCH_DTYPE")
    quantization: str = Field(validation_alias="QUANTIZATION")
    bnb_4bit_quant_type: str = Field(validation_alias="BNB_4BIT_QUANT_TYPE")
    bnb_4bit_use_double_quant: bool = Field(validation_alias="BNB_4BIT_USE_DOUBLE_QUANT")
    bnb_4bit_compute_dtype: str = Field(validation_alias="BNB_4BIT_COMPUTE_DTYPE")
    attn_implementation: str = Field(validation_alias="ATTN_IMPLEMENTATION")
    # 開発環境
    fake_embeddings: bool = Field(validation_alias="EMBEDDING_WORKER_FAKE")
    fake_embedding_dim: int = Field(validation_alias="FAKE_EMBEDDING_DIM")
    # ocr
    ocr_enabled: bool = Field(validation_alias="OCR_ENABLED")
    ocr_device: str = Field(validation_alias="OCR_DEVICE")
    ocr_scale: int = Field(validation_alias="OCR_SCALE")
    ocr_rec_threshold: float = Field(validation_alias="OCR_REC_THRESHOLD")
    ocr_det_threshold: float = Field(validation_alias="OCR_DET_THRESHOLD")
    ocr_max_chars: int = Field(validation_alias="OCR_MAX_CHARS")
    ocr_visualize: bool = Field(validation_alias="OCR_VISUALIZE")
    # s3
    s3_endpoint_url: str = Field(validation_alias="S3_ENDPOINT_URL")
    s3_bucket: str = Field(validation_alias="S3_BUCKET")
    s3_region: str = Field(validation_alias="S3_REGION")
    s3_access_key_id: str = Field(validation_alias="S3_ACCESS_KEY_ID")
    s3_secret_access_key: str = Field(validation_alias="S3_SECRET_ACCESS_KEY")

    @property
    def api_base_url(self) -> str:
        return f"http://{self.api_host}:{self.api_port}"
