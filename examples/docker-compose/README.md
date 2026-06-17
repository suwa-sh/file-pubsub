# docker compose 動作確認環境

file-pubsub と収集元サーバ (SFTP / FTP) を docker compose で一括起動し、収集 (Collect) → Archive 保存 → Fan-out 配信の一連の動作を事前確認できる環境です。Windows 開発 PC (Docker Desktop) での動作確認を想定していますが、macOS / Linux でも同じ手順で動きます。

## 構成

| サービス | 役割 |
|---|---|
| `file-pubsub` | 本体。`config.yaml` の 4 topic (orders=SFTP / customers=FTP / invoices=local / receipts=inbox(push 受信)) を扱う |
| `sftp-server` | 収集元 SFTP サーバ (atmoz/sftp)。ホストの `sources/sftp/` が producer の `/out` に見える |
| `ftp-server` | 収集元 FTP サーバ (delfer/alpine-ftp-server、パッシブモード)。ホストの `sources/ftp/` が `/ftp/producer` に見える |

producer 役はホストの `sources/` 配下にファイルを置くだけです (pull 型 3 topic はポーリング、`receipts` は push 受信モードで `sources/inbox/` へ置いたファイルを fsnotify イベント駆動 + フォールバックポーリングで取り込みます)。配信結果 (Archive / Manifest / Subscription 配下の複製) はホストの `data/` 配下で確認できます。

## 動作確認手順 (Windows / Docker Desktop)

前提: Docker Desktop がインストール済みで起動していること。コマンドは PowerShell で実行します (macOS / Linux はターミナルで同じコマンド)。

### 1. 起動

```powershell
cd examples\docker-compose
docker compose up -d --build
```

初回はイメージのビルドと取得が走ります。起動確認:

```powershell
docker compose ps
curl.exe http://localhost:19090/healthz   # → ok
```

### 2. サンプルファイル投入

各収集元にファイルを置きます (producer 役)。

```powershell
echo "id,qty" > sources\sftp\orders_20260612.csv
echo "id,name" > sources\ftp\customers_20260612.csv
echo "id,amount" > sources\local\invoices_20260612.csv
```

push 受信モード (`receipts`、完了検知=done マーカー) は、本体を置いた後に完了マーカー (`.done`) を置くと取り込まれます (マーカーを置くまでは取り込まれません):

```powershell
echo "id,amount" > sources\inbox\receipts_0001.csv
echo "" > sources\inbox\receipts_0001.csv.done   # このマーカー出現で receipts_0001.csv が取り込まれる
```

### 3. 配信結果の確認

安定待ち (2 秒) + ポーリング (5 秒) があるため、15 秒ほど待ってから確認します。

```powershell
dir data\subscriptions\orders\current     # orders_20260612.csv が複製されている
dir data\subscriptions\orders\next        # 同じファイルが独立に複製されている
dir data\subscriptions\customers\current
dir data\subscriptions\invoices\current
dir data\subscriptions\receipts\current   # receipts_0001.csv が複製されている (マーカーは配信されない)
```

- `current` / `next` の両方に同じファイルが複製されていれば Fan-out 成功です。一方を削除しても他方には影響しません (並行稼働の確認)。
- 収集元 (`sources\sftp` 等) のファイルは GET 後 DELETE (既定) で回収され、消えています。`receipts` (push 受信) は本体 `receipts_0001.csv` とマーカー `receipts_0001.csv.done` の双方が回収されます (マーカー自体は配信対象外)。
- Archive と Manifest も確認できます: `dir data\archive\orders`、`type data\manifest\<message_id>.json`

### 4. status / メトリクスの確認

```powershell
docker compose exec file-pubsub file-pubsub status --config /etc/file-pubsub/config.yaml
curl.exe http://localhost:19090/metrics | findstr file_pubsub
```

status に各メッセージの Subscription 別配送状態 (delivered) が表示されれば確認完了です。

### 5. 停止

```powershell
docker compose down
```

生成データを消すときは `data\` 配下 (archive / manifest / subscriptions 等) を削除してください。

## トラブルシュート

- 収集されない: `docker compose logs file-pubsub` で構造化ログ (1 行 JSON) を確認します。`collect_failed` イベントに原因と対処が出ます。
- `*.tmp` / `*.part` は除外パターンで収集対象外です (書き込み途中ファイルの保護)。
- Windows のメモ帳等で `sources\` 配下のファイルを直接編集すると、保存のたびに新しいメッセージとして収集されます (同名再出力は別 message_id)。
