#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ENV_FILE="${ENV_FILE:-${ROOT_DIR}/deploy/modal/.env}"

if [[ ! -f "${ENV_FILE}" ]]; then
	echo "Modal 用 .env が見つかりません: ${ENV_FILE}" >&2
	exit 1
fi

modal secret create --force embedding-worker --from-dotenv "${ENV_FILE}"
