#syntax=docker/dockerfile:1
# Python 3.12 / PyTorch 2.10 / ROCm 7.2（Hub: rocm/pytorch、タグは再現性のため固定。更新時は Docker Hub の tags を確認）
FROM rocm/pytorch:rocm7.2_ubuntu24.04_py3.12_pytorch_release_2.10.0

COPY --from=ghcr.io/astral-sh/uv:0.11 /uv /usr/local/bin/uv

WORKDIR /app

ENV UV_COMPILE_BYTECODE=1 \
	UV_LINK_MODE=copy \
	PIP_NO_CACHE_DIR=1 \
	PYTHONUNBUFFERED=1

COPY pyproject.toml uv.lock main.py ./
RUN uv sync --frozen

CMD ["uv", "run", "python", "main.py"]
