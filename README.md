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
