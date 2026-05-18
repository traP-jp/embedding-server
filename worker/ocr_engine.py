from __future__ import annotations

import logging
import re
from typing import Any

from worker_config import Config

log: logging.Logger = logging.getLogger("worker")


class OcrEngine:
    def __init__(self, config: Config) -> None:
        self.config = config
        self._ocr: Any | None = None

        if self.config.ocr_enabled:
            from yomitoku import OCR

            log.info("loading yomitoku OCR device=%s", self.config.ocr_device)
            self._ocr = OCR(visualize=self.config.ocr_visualize, device=self.config.ocr_device)
            log.info("yomitoku OCR loaded")

    def read_image_text(self, image_path: str) -> str:
        if not self.config.ocr_enabled:
            return ""

        from PIL import Image
        import numpy as np

        assert self._ocr is not None

        with Image.open(image_path) as img:
            img = img.convert("RGB")
            if self.config.ocr_scale > 1:
                width, height = img.size
                img = img.resize(
                    (width * self.config.ocr_scale, height * self.config.ocr_scale),
                    Image.Resampling.BICUBIC,
                )
            arr = np.array(img)

        results, _ = self._ocr(arr)
        lines: list[str] = []
        for word_prediction in results.words:
            if (
                word_prediction.rec_score < self.config.ocr_rec_threshold
                or word_prediction.det_score < self.config.ocr_det_threshold
            ):
                continue

            text = str(word_prediction.content).strip()
            if not text:
                continue
            # 連続したスペースを半角スペースにまとめる
            lines.append(re.sub(r"\s+", " ", text))

        uniq: list[str] = []
        seen: set[str] = set()
        for line in lines:
            if line in seen:
                continue
            seen.add(line)
            uniq.append(line)

        ocr_text = "\n".join(uniq).strip()
        if len(ocr_text) > self.config.ocr_max_chars:
            ocr_text = ocr_text[: self.config.ocr_max_chars]
        return ocr_text
