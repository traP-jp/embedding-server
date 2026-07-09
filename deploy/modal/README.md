# Modal worker デプロイ

既存の Docker Compose と ROCm 用 Dockerfile はそのまま残す。Modal では
`worker/modal_app.py`、CUDA 向けのイメージ、`embedding-worker` という名前の
Modal Secret を使う。

## 前提

- Go API と Postgres は Modal の外で起動している。
- API は Modal から到達できる公開 HTTPS URL を持っている。
- API と worker は同じ S3 互換バケットを参照している。

## 環境変数

Modal はローカルの `.env` を自動では読まないため、`deploy/modal/.env` を Modal Secret として登録する。
この Secret はモデル用ではなく、worker 実行環境に渡す環境変数一式。

`deploy/modal/.env.example` をコピーして `deploy/modal/.env` を作り、Modal から到達できる公開 URL を
`API_BASE_URL` に設定する。

```env
WORKER_API_MODE=url
API_BASE_URL=https://embedding-api.example.com
```

worker は `WORKER_API_MODE` で API の向き先を切り替える。

- Docker Compose: `compose.yaml` が `WORKER_API_MODE=host` を指定し、`API_HOST/API_PORT` を使う。
- Modal: Secret の `WORKER_API_MODE=url` により `API_BASE_URL` を使う。

Docker Compose 用の `.env` と Modal 用の `deploy/modal/.env` は分けて管理する。

Secret を作成または更新する。

```bash
mise run modal-secret
```

スクリプトは次のコマンドをラップしているだけ。

```bash
modal secret create --force embedding-worker --from-dotenv deploy/modal/.env
```

## 手動実行

```bash
mise run modal-run
```

## 定期ポーリングのデプロイ

```bash
mise run modal-deploy
```

デプロイされた `poll_queue` は 1 分に 1 回起動し、デフォルトでは最大 5 分間、
2 秒間隔で API を確認する。job がある場合だけモデルをロードし、最大 30 件の
claim 済みジョブを処理する。job がない poll ではモデルをロードしない。

## 実行時の調整項目

Modal 用のデプロイ設定は、意図的に `compose.yaml` から分離している。

- `MODAL_GPU=A10` はデプロイ時の Modal GPU 種別を指定する。
- `MODAL_MAX_CONTAINERS=1` は手動の `process_queue` 実行時に同時 worker
  コンテナ数を制限する。
- `MODAL_MAX_JOBS_PER_RUN=30` は 1 回の Modal 起動で処理する最大 job 数を指定する。
- `MODAL_POLL_MINUTES=1` は定期ポーリング間隔を指定する。
- `MODAL_WORKER_RUN_SECONDS=300` は 1 回の Modal 起動で待機・処理する最大秒数を指定する。
- `MODAL_IDLE_POLL_SECONDS=2` は job が無いときの claim 再試行間隔を指定する。
- Modal コンテナの warm 維持はデフォルト 30 秒にしている。
- `OCR_ENABLED`、`MODEL_MAX_MEMORY_CUDA`、`QUANTIZATION` などの worker 設定は、
  `deploy/modal/.env` 側で管理する。Modal で画像上限を上げる場合は `deploy/modal/.env` の
  `EMBEDDING_MAX_PIXELS` を大きめにする。
