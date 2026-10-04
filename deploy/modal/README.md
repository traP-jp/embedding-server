# Modal worker デプロイ

[開発者向けガイド](../../docs/wiki/development.md) · [利用者向けサービス Wiki](../../docs/wiki/README.md)

本番では Go API が Modal worker の起動を依頼します。デプロイには `mise run modal-deploy-push` を使います。

## 前提

- Go API と PostgreSQL が部室サーバーで起動している。
- Modal から API の公開 HTTPS URL に接続できる。
- API と worker が同じ R2 バケットを使う。

## 環境変数

worker の環境変数は `deploy/modal/.env` に設定し、Modal Secret に登録します。

`deploy/modal/.env` がない場合だけ [.env.example](.env.example) をコピーしてください。

- `API_BASE_URL`：API の公開 URL を設定します。
- R2 のバケット・認証情報：Go API と同じ値を設定します。
- `INTERNAL_API_KEY`：Go API と同じ値を設定します。

```env
WORKER_API_MODE=url
API_BASE_URL=https://embeddings.mumumu6.net
```

worker は `WORKER_API_MODE` で API の接続先を切り替えます。

- Docker Compose: [compose.yaml](../../compose.yaml) が `WORKER_API_MODE=host` を指定し、`API_HOST/API_PORT` を使う。
- Modal: Secret の `WORKER_API_MODE=url` により `API_BASE_URL` を使う。

Docker Compose 用の `.env` と Modal 用の `deploy/modal/.env` は分けて管理してください。

次のコマンドで Secret を登録・更新します。

```bash
mise run modal-secret
```

## 手動実行

```bash
mise run modal-run
```

## 本番のデプロイと Go からの push 起動

`mise run modal-deploy-push` は、Go API が起動依頼を送る HTTP エンドポイント `run_batch` を公開します。

1. `deploy/modal/.env` に Go API と同じ `INTERNAL_API_KEY`、`MODAL_GPU=T4`、`OCR_ENABLED=true` を設定します。
2. `mise run modal-secret` で Secret を更新します。
3. `mise run modal-deploy-push` でデプロイします。
4. 表示された `run_batch` の URL を Go API の `MODAL_TRIGGER_URL` に設定します。
5. Go API 側で `MODAL_ENABLE=true`、`MODAL_BATCH_THRESHOLD=10`、Modal と同じ `INTERNAL_API_KEY` を設定します。
6. `mise run deploy-api` で Go API 側の設定を反映します。

Go API は未処理の画像ジョブが `MODAL_BATCH_THRESHOLD`（既定10）以上になると、`run_batch` に POST します。認証ヘッダーは `Authorization: Bearer <INTERNAL_API_KEY>` です。

件数はサービス全体の未処理の画像ジョブで数えます。複数画像を送った1回の POST も1ジョブです。

10件待ちの理由は [画像 worker の起動条件](../../docs/wiki/README.md#画像-worker-の起動条件)、通知と結果取得は [Webhook の流れ](../../docs/wiki/README.md#画像の受付--webhook--get) を参照してください。

認証キーは用途ごとに分けます。

- `API_KEY`: クライアント → Go（公開 API）
- `INTERNAL_API_KEY`: worker/Modal → Go（`/internal/...`）、Go → Modal `run_batch`

Modal worker はキューが空になると終了します。

## 実行時の調整項目

Modal worker の設定は `deploy/modal/.env` で管理します。

| 変数・設定例 | 動作 |
| --- | --- |
| `MODAL_GPU=T4` | GPU の種類。既定は T4 |
| `MODAL_MAX_CONTAINERS=1` | 同時に起動する worker コンテナの上限 |
| `MODAL_MAX_JOBS_PER_RUN=0` | 1回の起動で処理するジョブ数の上限。0はキューが空になるまで処理 |
| `MODAL_WORKER_RUN_SECONDS=0` | worker が次のジョブを取得し続ける時間の上限。0はこの制限なし |
| `MODAL_FUNCTION_TIMEOUT_SECONDS=10800` | `process_queue` の実行時間の上限。既定は3時間 |
| `MODAL_SCALEDOWN_WINDOW_SECONDS=30` | 処理後に GPU コンテナを保持する秒数 |
| `EMBEDDING_BATCH_SIZE=4` | 1回の推論にまとめるジョブ数。コードの既定は1、常駐 worker も1 |
| `EMBEDDING_MAX_PIXELS` | 推論用画像のリサイズ上限 |
| `OCR_ENABLED=true` | yomitoku による文字認識を有効化 |
| `MODEL_MAX_MEMORY_CUDA` / `QUANTIZATION` | モデルの GPU メモリ上限・量子化方式 |

`MODAL_WORKER_RUN_SECONDS=0` でも、関数の実行時間には `MODAL_FUNCTION_TIMEOUT_SECONDS` の上限が適用されます。推論バッチや画像のリサイズ上限を増やす場合は、VRAM 使用量を実測してください。

GPU の種類・関数タイムアウト・コンテナ上限などは再デプロイで反映します。`mise run modal-deploy-push` はローカルの `deploy/modal/.env` を読み込みます。OCR などの worker 設定は Secret も更新してください。

### 処理が止まったジョブの回収

Go API は `processing` の画像ジョブがある間、Modal の追加起動を見送ります。

`MODAL_RECLAIM_TTL=30m` は Go API 側の設定です。最終更新から30分を超えた `processing` のジョブを `pending` に戻します。

Modal がタイムアウトして処理中のジョブが残った場合も、回収の対象です。回収後に画像ジョブ数と起動間隔を確認し、起動条件を満たした場合に依頼します。

起動後の worker は、起動条件の10件を下回ってもキューが空になるまで処理します。復旧時のログ確認と手動実行は [開発者向けガイド](../../docs/wiki/development.md#modal-の画像-worker) を参照してください。
