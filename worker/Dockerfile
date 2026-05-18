#syntax=docker/dockerfile:1
# Python 3.12 / PyTorch 2.10 / ROCm 7.2（Hub: rocm/pytorch、タグは再現性のため固定。更新時は Docker Hub の tags を確認）
FROM rocm/pytorch:rocm7.2_ubuntu24.04_py3.12_pytorch_release_2.10.0

WORKDIR /app

ENV PYTHONUNBUFFERED=1 \
	PYTORCH_CUDA_ALLOC_CONF=expandable_segments:True

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
	--mount=type=cache,target=/var/lib/apt,sharing=locked \
	rm -f /etc/apt/apt.conf.d/docker-clean \
	&& apt-get update \
	&& apt-get install -y --no-install-recommends libglib2.0-0 libgl1

COPY pyproject.toml ./

# ベースイメージの torch が ROCm 対応版であることを確認する
RUN python - <<'PY'
import torch
import torchvision

if not torch.version.hip:
    raise SystemExit(f"expected ROCm torch, got torch={torch.__version__}")
print(f"base ROCm torch={torch.__version__} torchvision={torchvision.__version__}")
PY

# pyproject.toml から依存パッケージを読み取り、先に install する
RUN --mount=type=cache,target=/root/.cache/pip,sharing=locked python - <<'PY'
import subprocess
import sys
import tomllib

with open("pyproject.toml", "rb") as f:
    project = tomllib.load(f)["project"]

dependencies = [
    *project.get("dependencies", []),
    *project.get("optional-dependencies", {}).get("runtime", []),
]

subprocess.check_call([
    sys.executable,
    "-m",
    "pip",
    "install",
    "--break-system-packages",
    "--timeout",
    "120",
    "--retries",
    "10",
    *dependencies,
])
PY

# ここでアプリケーションコードをコピーする
COPY . .

# 依存は前段で入れているため、ここでは worker 本体だけを editable install する
RUN python -m pip install --break-system-packages --no-cache-dir --no-build-isolation --no-deps -e .

CMD ["python", "main.py"]
