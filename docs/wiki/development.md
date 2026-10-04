# 開発者向けガイド

[サービス Wiki](README.md) · [構成とモデル](architecture.md)

API の使い方は [サービス Wiki](README.md#api-一覧) を参照してください。

開発・運用には [mise タスク](../../mise.toml) を使います。コマンドはリポジトリルートで実行してください。

| 操作 | コマンド |
| --- | --- |
| 本番の起動・更新 | `mise run deploy-up` |
| 本番の停止 | `mise run deploy-down` |
| API の設定反映 | `mise run deploy-api` |
| コンテナ状態 | `mise run deploy-ps` |
| ログ | `mise run deploy-logs` |
| GPU なしの開発環境の起動・停止 | `mise run dev-up` / `mise run dev-down` |
| Modal Secret の登録・更新 | `mise run modal-secret` |
| Modal の本番デプロイ | `mise run modal-deploy-push` |
| Modal の手動実行 | `mise run modal-run` |
| テスト・静的検査 | `mise run test` |

## セットアップ

### 共通の前提

Docker Compose と、R2 のエンドポイント・バケット・認証情報を準備してください。PostgreSQL は Compose で起動します。

テキストだけを使う場合も、API と worker の起動には R2 の接続設定が必要です。

常駐 worker は AMD ROCm 用の Dockerfile と `/dev/kfd`・`/dev/dri` を使います。GPU のないマシンでは [開発用 Compose](#gpu-なしの開発用-compose) を使ってください。

`.env` がない場合だけ、設定例をコピーしてください。既存の `.env` は上書きしないでください。

```bash
cp .env.example .env
```

`API_KEY` は公開 API 用、`INTERNAL_API_KEY` は内部通信用です。次のコマンドで異なるキーを2つ生成し、`.env` に設定してください。DB と R2 の設定例も実環境の値に置き換えてください。

```bash
openssl rand -hex 32
openssl rand -hex 32
```

`AUTH_DISABLED=true` はローカル開発用です。`APP_ENV=production` では認証の無効化を拒否します。

### 主要な環境変数

コードの既定値と設定例は次のとおりです。配備時には `.env` と Modal Secret の値を使います。

| 変数 | 設定・意味 |
| --- | --- |
| `APP_ENV` / `API_PORT` | API の必須設定。例は `production` / `8080` |
| `AUTH_DISABLED` | コード既定 `false`。認証無効はローカル用 |
| `API_KEY` / `INTERNAL_API_KEY` | 公開用・内部用に異なる秘密値 |
| `POSTGRES_HOST/PORT/USER/PASSWORD/DB/SSLMODE` | API の必須 DB 接続設定。DB テーブルは起動時に AutoMigrate |
| `S3_ENDPOINT_URL/BUCKET/REGION/ACCESS_KEY_ID/SECRET_ACCESS_KEY/PREFIX` | R2 の接続設定。prefix の例は `jobs` |
| `WORKER_API_MODE` | Docker では `host`、Modal では `url` |
| `API_HOST` / `API_PORT` | `host` モードの接続先。Compose 内は `api:8080` |
| `API_BASE_URL` | `url` モードの接続先。Modal から到達できる API URL |
| `EMBEDDING_WORKER_FAKE` / `FAKE_EMBEDDING_DIM` | ダミー推論の有効化と次元。実モデルの代用として意味検索の評価には使わない |
| `MODEL_DEVICE_MAP` / `MODEL_MAX_MEMORY_CUDA/CPU` | モデル配置・メモリ設定 |
| `TORCH_DTYPE` / `QUANTIZATION` | 精度・量子化。例は `float16` / `4bit` |
| `ATTN_IMPLEMENTATION` | 注意機構。設定例は `sdpa` |
| `EMBEDDING_BATCH_SIZE` | コード既定1。Modal 設定例は4 |
| `EMBEDDING_MAX_PIXELS` | 推論用画像リサイズ上限。ルート例262144、Modal 例1048576 |
| `OCR_ENABLED` / `OCR_*` | Modal は `true` で yomitoku を使用。認識テキストは画像ごと最大256文字。その他はデバイス・拡大倍率・信頼度などの設定 |
| `MODAL_ENABLE` | **コード既定true、ルート `.env.example` はfalse**。push 起動には URL と内部キーも必要 |
| `MODAL_TRIGGER_URL` | Modal デプロイ時の `run_batch` URL |
| `MODAL_BATCH_THRESHOLD` / `MODAL_MIN_INTERVAL` | コード既定10ジョブ / 30秒 |
| `MODAL_TRIGGER_TIMEOUT` | コード既定15秒 |
| `MODAL_RECLAIM_TTL/EVERY` | コード既定30分 / 1分 |
| `CLOUDFLARE_TUNNEL_TOKEN` | `tunnel` profile で公開する場合に設定 |

設定例は [.env.example](../../.env.example) と [Modal の .env.example](../../deploy/modal/.env.example) を参照してください。読み込みと検証の実装は [Go 設定](../../server/config/config.go) と [Python 設定](../../worker/worker_config.py) にあります。

### 通常の Compose

R2・DB・認証キー・GPU の設定後、実行してください。

```bash
mise run up
```

API・PostgreSQL・Swagger UI・テキスト専用 worker が起動します。停止とコンテナの削除には `mise run down` を使います。API と Swagger UI のホスト側の公開先は `127.0.0.1` です。

画像も処理する場合は [Modal のセットアップ](../../deploy/modal/README.md) を行ってください。

### GPU なしの開発用 Compose

開発用 worker は4096要素のダミーベクトルを返します。[development/compose.yaml](../../development/compose.yaml) で `FAKE_EMBEDDING_DIM="4096"` を設定しています。

```bash
mise run dev-up
```

停止とコンテナの削除には `mise run dev-down` を使います。開発用 Compose は Swagger UI を含みません。

開発用 worker もテキスト専用です。画像の受付から結果取得まで試すには、Modal などの画像 worker が必要です。

開発用 API のポートには `127.0.0.1` の指定がありません。ローカル限定で使う場合は、ポートの公開先を変更してください。

### Modal の画像 worker

初回セットアップは [Modal デプロイ手順](../../deploy/modal/README.md) を参照してください。OCR の設定も、同じ手順で Secret を更新して再デプロイします。

`MODAL_BATCH_THRESHOLD=10` は起動に必要な未処理の画像ジョブの数です。`EMBEDDING_BATCH_SIZE=4` は1回の推論にまとめる件数です。

起動条件や接続 URL など、Go API 側の設定を変更したら次のコマンドで API コンテナを再作成します。

```bash
mise run deploy-api
```

少数の画像で確認する場合は、`mise run modal-run` で手動実行します。10件待ちをせずに GPU worker を起動するため、費用が発生します。

本番では `mise run modal-deploy-push` でデプロイし、Go API から起動します。

起動に失敗した場合や起動間隔の制限で見送られた場合、`pending` のジョブが自動で再処理されるとは限りません。10件以上あっても処理が始まらない場合は API・Modal のログを確認し、必要に応じて手動実行してください。実装は [modal_trigger.go](../../server/service/modal_trigger.go) を参照してください。

### Cloudflare Tunnel で公開

Cloudflare Dashboard で remotely-managed tunnel を作り、Published application を設定してください。

| Public hostname | Service URL |
| --- | --- |
| `embeddings.mumumu6.net` | `http://api:8080` |
| `api-embeddings.mumumu6.net` | `http://swagger:8080` |

発行されたトークンを `CLOUDFLARE_TUNNEL_TOKEN` に入れ、次のコマンドを実行してください。

```bash
mise run deploy-up
```

Service URL のホストには Compose 内の `api` / `swagger` を指定してください。`localhost` は cloudflared 自身を指します。

API のポートや公開ホストを変更した場合は、Tunnel と Modal の接続先、CORS の許可元も更新してください。

### 公開 API の疎通確認

以下は Bash の例です。公開 API キーを `API_KEY` 環境変数に設定してから実行してください。

```bash
export BASE_URL='https://embeddings.mumumu6.net'

# API キーなし: 401
curl -i \
  "${BASE_URL}/v1/embeddings/jobs/00000000-0000-0000-0000-000000000000"

# 公開 API キーあり: 存在しないジョブなので 404
curl -i \
  -H "Authorization: Bearer ${API_KEY}" \
  "${BASE_URL}/v1/embeddings/jobs/00000000-0000-0000-0000-000000000000"

# worker まで含む確認: 200 と4096要素のベクトル
curl --fail-with-body -sS \
  -H "Authorization: Bearer ${API_KEY}" \
  -H 'Content-Type: application/json' \
  -d "{\"text\":\"connectivity check $(date +%s%N)\"}" \
  "${BASE_URL}/v1/embeddings/text"
```

最後の例は入力に時刻を付け、キャッシュにないテキストで worker の推論を確認します。

画像は [Webhook の流れ](README.md#画像の受付--webhook--get) に沿って、受付・Webhook・公開 API キー付きの GET まで確認します。1件だけで試す場合は [Modal の手動実行](../../deploy/modal/README.md#手動実行) を使ってください。

## 内部 API

worker がジョブの取得と結果報告に使います。認証はすべて `Authorization: Bearer <INTERNAL_API_KEY>` です。

| メソッド・パス | 用途 | 成功時 |
| --- | --- | --- |
| `POST /internal/worker/jobs/claim` | pending ジョブを1件取得し processing にする | `200` + `id` / `payload`。取得できなければ `204` |
| `POST /internal/worker/jobs/{id}/complete` | `{"result":{"vector":[...]}}` で完了報告（4096要素） | `204` |
| `POST /internal/worker/jobs/{id}/fail` | 本文なしで失敗報告 | `204` |

claim の対象は `{"kinds":["text"]}` または `{"kinds":["image"]}` で指定できます。両方を対象にするとテキスト優先、同種のジョブは作成時刻順に取得します。

payload には入力に応じて `text` と `image_objects` を含みます。`image_objects` は `[{"key":"jobs/<UUID>/0"}]` 形式で、worker が R2 から画像を読むためのオブジェクトキーです。

complete / fail は `processing` のジョブだけを更新します。ジョブが存在しない場合や、現在の状態で更新できない場合は `404` を返します。完了・失敗時に画像の削除を試み、削除に失敗した画像は定期清掃の対象にします。

仕様は [OpenAPI](../../openapi.yaml) を参照してください。実装は [内部 API のハンドラー](../../server/router/worker.go)、[ジョブの DB 処理](../../server/repository/gormrepo/embedding_job.go)、[画像保存・削除](../../server/service/job_file.go) に分かれています。

## 運用中の確認

公開経路・認証・テキストの推論は、[公開 API の疎通確認](#公開-api-の疎通確認) の curl で確認してください。画像は受付後に GET し、`completed` になることまで確認します。

## 開発時の検証

```bash
mise run test
```

Go の race テスト・`go vet` と Python worker の unittest を実行します。Python のテストには、worker の依存関係をインストール済みの `worker/.venv` を使います。
