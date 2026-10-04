# embedding-server

文章や画像を、Qwen3-VL-Embedding-8B でベクトルへ変換する API です。

主に traQ の画像検索に使います。添付画像を事前にベクトル化して保存し、検索時のテキストクエリと比較します。検索対象のメッセージ本文は基本的にベクトル化しません。

初めて利用する方は [サービス Wiki](docs/wiki/README.md) を参照してください。

| ページ | 内容 |
| --- | --- |
| [サービス Wiki](docs/wiki/README.md) | API の使い方、画像 worker の起動条件、Webhook、全体構成図 |
| [構成とモデル](docs/wiki/architecture.md) | 各要素の役割、モデル・GPU・OCR、内部処理 |
| [開発者向けガイド](docs/wiki/development.md) | 環境構築、テスト、デプロイ、設定変更 |
| [Modal デプロイ手順](deploy/modal/README.md) | Secret 設定、画像 worker の OCR 設定とデプロイ |
| [OpenAPI](openapi.yaml) | リクエストとレスポンスの仕様 |
| [Third Party Notices](THIRD_PARTY_NOTICES.md) | 同梱コード・モデルのライセンス情報 |
