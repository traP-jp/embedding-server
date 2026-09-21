# embedding-server

## Swagger UI

Swagger UIは専用コンテナで起動する。

```text
http://localhost:8081
https://api-embeddings.mumumu6.net
```

Swagger UIからAPIを実行する場合も、公開APIの `API_KEY` が必要になる。

## Cloudflare Tunnel で公開する

本番は Cloudflare Dashboard で remotely-managed tunnel を作成し、Published application を次のように設定する。

- Public hostname: `embeddings.mumumu6.net`
- Service URL: `http://api:8080`
- Public hostname: `api-embeddings.mumumu6.net`
- Service URL: `http://swagger:8080`

Dashboard が発行した tunnel token をルート `.env` の `CLOUDFLARE_TUNNEL_TOKEN` に設定し、`tunnel` profile を有効にして起動する。
`API_KEY` と `INTERNAL_API_KEY` には、それぞれ別の十分に長い乱数を設定する。

```sh
openssl rand -hex 32
openssl rand -hex 32
```

```sh
docker compose --profile tunnel up -d --build
```

ローカル開発では Cloudflare Tunnel を起動せず、通常どおり次のコマンドで起動できる。

```sh
docker compose up -d --build
```

`cloudflared` はCompose内のサービスへ接続するため、Service URLのhostは
`localhost`ではなく`api`または`swagger`にする。APIとSwaggerのhost公開ポートはloopbackのみに制限している。

Modal の `API_BASE_URL` には上記の公開 HTTPS URL を設定する。

### 疎通確認

```sh
# API キーなし: 401
curl -i \
  https://embedding-api.example.com/v1/embeddings/jobs/00000000-0000-0000-0000-000000000000

# 外部 API キーあり: 404 なら Tunnel・認証・Go API まで到達済み
curl -i \
  -H "Authorization: Bearer ${API_KEY}" \
  https://embedding-api.example.com/v1/embeddings/jobs/00000000-0000-0000-0000-000000000000

# worker まで含む正常系: 200
curl -sS -o /dev/null -w '%{http_code}\n' \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{"text":"connectivity check"}' \
  https://embedding-api.example.com/v1/embeddings/text
```

認証を無効にする `AUTH_DISABLED=true` はローカル用であり、`APP_ENV=production` では起動を拒否する。

## Webhook の受信

Webhook は公開 IP に到達する HTTPS URL のみ利用できる。ループバック・プライベート IP・
リンクローカルなどへの接続とリダイレクトは拒否する。DNS の検証は接続時にも行う。

Webhook は `id` と `status`（`completed` または `failed`）だけを POST する。
結果のベクトル・エラー本文・API キー・署名は送らない。専用の秘密値の設定も不要。

```json
{"id":"00000000-0000-0000-0000-000000000000","status":"completed"}
```

受信側では通知を結果取得の合図として扱い、自分が作成した job id であることを確認してから、
設定済みの API サーバーの `GET /v1/embeddings/jobs/{id}` を `Authorization: Bearer <API_KEY>` 付きで呼ぶ。
状態と結果は GET の応答を正として扱い、通知だけで完了・失敗を確定しない。
重複通知に備えて job id 単位で処理を冪等にする。通知は一度だけ送るため、未着時は GET で確認できる。
旧方式で通知先に送信した `API_KEY` はローテーションする。

画像は 1 枚 20 MiB、1 リクエスト 4 枚まで。worker は展開前に 1 枚 2,000 万画素を上限として検査する。
HTTP ボディは画像系 81 MiB、その他 1 MiB で制限する（multipart の付加情報を含む）。

検証コマンド:

```sh
(cd server && go test -race ./...)
# リポジトリルートから（worker の依存関係をインストール済みの場合）
PYTHONPATH=worker worker/.venv/bin/python -m unittest discover -s worker/tests -v
```
