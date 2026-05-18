#syntax=docker/dockerfile:1
# Python 3.12 / PyTorch 2.10 / ROCm 7.2（Hub: rocm/pytorch、タグは再現性のため固定。更新時は Docker Hub の tags を確認）
FROM rocm/pytorch:rocm7.2_ubuntu24.04_py3.12_pytorch_release_2.10.0

WORKDIR /app

ENV PIP_NO_CACHE_DIR=1 \
	PYTHONUNBUFFERED=1 \
	PYTORCH_CUDA_ALLOC_CONF=expandable_segments:True

COPY . .

RUN apt-get update \
	&& apt-get install -y --no-install-recommends libglib2.0-0 libgl1 \
	&& rm -rf /var/lib/apt/lists/*

# The base image already contains the ROCm-enabled torch build. Install Python
# application dependencies into that interpreter instead of creating an isolated
# venv that would hide the ROCm torch package.
RUN python - <<'PY'
import torch
import torchvision

if not torch.version.hip:
    raise SystemExit(f"expected ROCm torch, got torch={torch.__version__}")
print(f"base ROCm torch={torch.__version__} torchvision={torchvision.__version__}")
PY

# pyprojectをinstall
RUN python -m pip install --break-system-packages --no-cache-dir -e ".[runtime]" \
	&& python - <<'PY'
import torch
import torchvision

if not torch.version.hip:
    raise SystemExit(f"expected ROCm torch, got torch={torch.__version__}")
print(f"using ROCm torch={torch.__version__} torchvision={torchvision.__version__}")
PY

CMD ["python", "main.py"]
