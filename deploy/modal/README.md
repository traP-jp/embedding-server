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

## Go からの push 起動（推奨）

`modal deploy worker/modal_app.py`（`MODAL_ENABLE_SCHEDULE` なし）すると
`run_batch` という HTTP endpoint が公開される。

1. `deploy/modal/.env` に `MODAL_TRIGGER_TOKEN` と `MODAL_GPU=T4` を入れる
2. `mise run modal-secret` で Secret 更新
3. `mise run modal-deploy-push` でデプロイ（定期ポーリングなし）
4. 出た `run_batch` の URL を Go の `MODAL_TRIGGER_URL` に入れる。
   同じ `MODAL_TRIGGER_TOKEN` を Go 側にも設定する（Go は `?token=` で付与する）。
5. Go 側で `MODAL_ENABLE=true` にする（`false` なら URL/Token があっても起動しない）。

Go は pending 画像ジョブが `MODAL_BATCH_THRESHOLD`（既定 10）以上で
この URL に POST する（トークンは query `?token=`）。

キューが空なら Modal worker は待たずに終了する。

## 定期ポーリングのデプロイ（任意・非推奨）

空振り課金しやすいので、通常は push 起動を使う。

```bash
mise run modal-deploy
```

## 実行時の調整項目

Modal 用のデプロイ設定は、意図的に `compose.yaml` から分離している。

- `MODAL_GPU=T4` はデプロイ時の Modal GPU 種別を指定する（既定 T4）。
- `MODAL_MAX_CONTAINERS=1` は同時 worker コンテナ数を制限する。
- `MODAL_MAX_JOBS_PER_RUN=0` は 1 回の起動で処理する最大 job 数（**0 = キューが空になるまで**）。
- `MODAL_WORKER_RUN_SECONDS=0` はソフトな時間上限（**0 = なし**。Modal の function timeout までドレイン）。
- `MODAL_FUNCTION_TIMEOUT_SECONDS=10800` は `process_queue` の function timeout（既定 3h）。**deploy 時**に反映される。
- `MODAL_SCALEDOWN_WINDOW_SECONDS=30` は GPU worker の warm 維持秒数（deploy 時）。
- 起動ラッシュ時は `MODAL_MAX_CONTAINERS` を 2〜4 に上げると並列 claim できる。
  閾値は「起こす条件」だけで、起こしたあとは溜まっている分をまとめて消化する。
- Go は `processing` の画像ジョブがあるあいだは再 trigger しない（二重 spawn 防止）。
- `MODAL_RECLAIM_TTL=30m` で古い `processing` を `pending` に戻し、残件があれば再起動する。
- `MODAL_TRIGGER_TOKEN` は `run_batch` 起動口の Bearer トークン。
- function timeout を超えても残る場合は stale reclaim または閾値で起こす。
- `OCR_ENABLED`、`MODEL_MAX_MEMORY_CUDA`、`QUANTIZATION` などの worker 設定は、
  `deploy/modal/.env` 側で管理する。Modal で画像上限を上げる場合は `deploy/modal/.env` の
  `EMBEDDING_MAX_PIXELS` を大きめにする。
- `EMBEDDING_BATCH_SIZE` はモデル推論のバッチサイズ（未設定時は 1）。Modal の画像処理では
  4〜8 を目安に Secret へ入れる。部室のテキスト向け worker は 1 のままでよい。
