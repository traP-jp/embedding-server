# 構成とモデル

[サービス Wiki](README.md) · [開発者向けガイド](development.md)

Go API がリクエストを受け付け、GPU 上の worker が推論します。テキストは常駐 worker、画像は Modal worker が担当します。どちらも同じ Go API からジョブを取得し、同じ埋め込みモデルを使います。

## 全体構成

接続関係は [Wiki の構成図](README.md#全体構成) を参照してください。

| 構成要素 | 実行場所・技術 | 役割 |
| --- | --- | --- |
| 公開経路 | Cloudflare Tunnel | 外部の HTTPS リクエストを Go API へ届ける |
| API | 部室サーバーの Docker Compose / Go・Echo | 認証、入力の検査、ジョブ登録、結果取得、Modal の起動、Webhook 通知 |
| ジョブ・結果・キャッシュ | 同じ Compose の PostgreSQL | 入力テキスト、状態、埋め込み結果、テキストキャッシュを保存 |
| 常駐 worker | 同じ Compose の Python / PyTorch・ROCm | 内部 API からテキストジョブを定期的に取得し、GPU で推論 |
| Modal worker | Modal の Python / PyTorch・CUDA | 画像ジョブを処理。起動中はテキストも取得し、テキストを優先 |
| 画像の一時保存 | Cloudflare R2 | API がアップロードし、Modal worker が読み取る |
| API の試用画面 | 同じ Compose の Swagger UI | OpenAPI の表示とリクエストの実行 |

PostgreSQL のジョブテーブルが待ち行列を兼ねます。API は自分ではモデル推論を実行せず、worker が内部 API で未処理ジョブを取得し、完了結果を返します。

コンテナの構成は [compose.yaml](../../compose.yaml)、各 worker の実装は [常駐 worker](../../worker/main.py) と [Modal worker](../../worker/modal_app.py) を参照してください。

## 使っているモデル

`Qwen/Qwen3-VL-Embedding-8B` は、文章と画像を同じベクトル空間へ変換する約80億パラメータのモデルです。テキストのクエリと画像の類似度を比較できます。モデルの詳細は [Qwen 公式モデルカード](https://huggingface.co/Qwen/Qwen3-VL-Embedding-8B) を参照してください。

| 項目 | このサービスの設定・動作 |
| --- | --- |
| モデル | 常駐・Modal とも `Qwen/Qwen3-VL-Embedding-8B` |
| モデルの重み | bitsandbytes の 4bit / NF4 量子化 |
| 計算の精度 | `float16` |
| ベクトルの出力 | 4096 次元・L2 正規化。API では JSON の数値配列として返す |
| 入力 | テキスト、画像、テキスト＋画像 |

モデル名は [embedding_engine.py](../../worker/embedding_engine.py) で固定しています。入力の整形とベクトルの正規化は [Qwen ラッパー](../../worker/scripts/qwen3_vl_embedding.py)、API の出力形式は [OpenAPI](../../openapi.yaml) を参照してください。

### ベクトルの性質

画像検索では、画像とテキストのクエリのベクトルを比較します。ベクトルの向きの近さを、意味の近さの指標として使います。

本サービスは L2 正規化したベクトルを返すため、内積はコサイン類似度とほぼ同じ値になります。

## テキストが処理される流れ

`/text` はキャッシュの有無にかかわらず同期でベクトルを返します。

1. 利用アプリが `POST /v1/embeddings/text` を送る。
2. API が同じテキストのキャッシュを調べ、あればその結果を返す。
3. キャッシュがなければ PostgreSQL にジョブを登録する。
4. 常駐 worker がジョブを取得し、Qwen で推論する。起動中の Modal が取得する場合もある。
5. worker が内部 API に結果を報告し、API が保存する。
6. 最初の HTTP リクエストへベクトルを返す。

テキストは1件から処理します。応答の待機上限は [テキスト API](README.md#テキスト--ベクトル) を参照してください。

## 画像が処理される流れ

`/images` と画像を含む `/multimodal` は、受付後に非同期で推論します。

1. 利用アプリが `/images` または `/multimodal` に画像を送る。
2. API が画像を R2 へ保存し、PostgreSQL にジョブを登録する。
3. API は `202` とジョブ ID を返す。画像ジョブの蓄積状況に応じて Modal の起動を検討する。
4. Modal worker が内部 API でジョブを取得し、R2 から画像を読む。
5. Modal worker が yomitoku で画像内の文字を読み取り、画像と認識テキストを Qwen に渡して推論する。
6. Modal worker が内部 API にベクトルを報告する。API は結果を保存し、R2 の画像の削除を試みる。
7. `webhook_url` を指定していれば、API が完了・失敗を通知する。

利用アプリは通知後に、公開 API キーを付けて GET で状態・結果を取得します。Webhook を使わない場合は、GET で定期的に確認します。

### 画像の起動条件と待ち時間

10件待ちの理由と待ち時間は [画像 worker の起動条件](README.md#画像-worker-の起動条件) を参照してください。

画像ジョブの受付時に、未処理の件数を `MODAL_BATCH_THRESHOLD` と比較します。次のいずれかに該当する場合は起動を見送ります。

- `processing` の画像ジョブがある。
- 前回の起動から `MODAL_MIN_INTERVAL` が経過していない。

起動後の Modal はキューが空になるまで処理します。

| 設定 | 件数の意味 |
| --- | --- |
| `MODAL_BATCH_THRESHOLD=10` | 起動に必要な未処理の画像ジョブの数 |
| `EMBEDDING_BATCH_SIZE=4` | 1回の推論にまとめるジョブの数 |

起動条件の実装は [modal_trigger.go](../../server/service/modal_trigger.go)、設定変更と手動実行は [開発者向けガイド](development.md#modal-の画像-worker) を参照してください。

## GPU と OCR の設定

以下は2026-10-05時点のローカル設定です。Modal の OCR 設定を反映するには、Secret を更新して再デプロイします。[デプロイ手順](../../deploy/modal/README.md#本番のデプロイと-go-からの-push-起動)を参照してください。

| 項目 | 常駐 worker | Modal worker |
| --- | --- | --- |
| GPU 向け構成 | AMD ROCm。Radeon PRO W6600 向けの設定 | CUDA、設定 GPU は T4 |
| ジョブ | テキストのみ | テキスト・画像 |
| 推論バッチ | 1ジョブ | 4ジョブ |
| 推論用画像のリサイズ上限 | 画像処理なし（設定値262,144画素） | 1,048,576画素 |
| OCR | 使用しない（テキストのみを処理） | yomitoku を有効化 |

Modal は `OCR_ENABLED=true`、`OCR_DEVICE=cuda` で yomitoku を使います。画像ごとの認識テキストは最大256文字に制限し、画像と一緒に Qwen へ渡します。API のレスポンスにはベクトルを返し、OCR の本文は含めません。

## データの保存

| データ | 保存先・期間 |
| --- | --- |
| 入力テキスト・ジョブ状態・結果 | PostgreSQL。作成から6時間を超えると清掃対象 |
| アップロード画像 | R2。完了・失敗時に削除を試み、古い画像は清掃対象 |
| テキストのキャッシュ | PostgreSQL。保存期限はなく、約3000件を目安に最終アクセスが古いものから削除 |

ジョブの清掃は API の起動時と、その後6時間ごとに実行します。作成から6時間を超えたジョブが削除対象です。実装は [cleanup.go](../../server/service/cleanup.go) を参照してください。
