#syntax=docker/dockerfile:1
# Python 3.12 / PyTorch 2.10 / ROCm 7.2（Hub: rocm/pytorch、タグは再現性のため固定。更新時は Docker Hub の tags を確認）
FROM rocm/pytorch:rocm7.2_ubuntu24.04_py3.12_pytorch_release_2.10.0

ARG BNB_BUILD_FROM_SOURCE=0
ARG BNB_GIT_REF=0.49.2
ARG BNB_ROCM_ARCH=gfx1030

WORKDIR /app

ENV PYTHONUNBUFFERED=1 \
	XDG_CACHE_HOME=/cache \
	PYTORCH_CUDA_ALLOC_CONF=expandable_segments:True

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
	--mount=type=cache,target=/var/lib/apt,sharing=locked \
	rm -f /etc/apt/apt.conf.d/docker-clean \
	&& apt-get update \
	&& apt-get install -y --no-install-recommends build-essential git libglib2.0-0 libgl1

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

RUN --mount=type=cache,target=/root/.cache/pip,sharing=locked \
	if [ "$BNB_BUILD_FROM_SOURCE" = "1" ]; then \
		python -m pip uninstall --break-system-packages -y bitsandbytes; \
		python -m pip install --break-system-packages --no-cache-dir "cmake>=3.31.6" "scikit-build-core>=0.11"; \
		rm -rf /opt/bitsandbytes-src; \
		git clone --depth 1 --branch "$BNB_GIT_REF" https://github.com/bitsandbytes-foundation/bitsandbytes.git /opt/bitsandbytes-src; \
		cmake -DCOMPUTE_BACKEND=hip -DBNB_ROCM_ARCH="$BNB_ROCM_ARCH" -S /opt/bitsandbytes-src -B /opt/bitsandbytes-src/build; \
		cmake --build /opt/bitsandbytes-src/build -j"$(nproc)"; \
		python -m pip install --break-system-packages --no-build-isolation --no-deps -e /opt/bitsandbytes-src; \
		python -c 'import importlib.metadata; import bitsandbytes as bnb; print("source bitsandbytes", importlib.metadata.version("bitsandbytes"), bnb.__file__)'; \
	fi

# ここでアプリケーションコードをコピーする
COPY . .

# 依存は前段で入れているため、ここでは worker 本体だけを editable install する
RUN python -m pip install --break-system-packages --no-cache-dir --no-build-isolation --no-deps -e .

CMD ["python", "main.py"]
