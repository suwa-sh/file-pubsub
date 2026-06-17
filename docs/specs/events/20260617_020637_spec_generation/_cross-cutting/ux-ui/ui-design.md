# UI デザイン仕様(CLI 出力・設定 YAML・構造化ログ規約)

> GUI を持たないため、テンプレートの「レイアウト・レスポンシブ・デザインシステム」は、_inference.md の方針に従い
> **CLI 出力フォーマット / 終了コード / 設定 YAML 構造 / 構造化ログ**の規約として翻案する。
> 機械可読出力オプション(`--json` 等)は RDRA に定義がないため発明しない。CLI 出力は人間可読テーブルのみとし、機械連携は終了コードと構造化ログで行う(CTR-002)。

## CLI 出力フォーマット規約

### 共通原則

- 出力先: 結果は標準出力、エラーメッセージは標準エラー出力。
- 1 行 1 レコードの行指向テーブル(`grep` / `awk` で処理可能)。罫線文字による囲み装飾はしない(Data-Ink Ratio: 装飾よりデータ)。
- 状態値は Manifest の語彙(`delivered` / `failed` / `dlq`)をそのまま表示し、独自の言い換えをしない。
- 日時は ISO 8601 形式で表示する。

### `status` のテーブル表示

Manifest に記録された message_id・topic・Subscription 別の配送状態を表示する(SP-101、LP-401「status の出力整形」)。

明細テーブルの列(情報「Manifest」の属性に対応):

| 列 | 内容 | 由来(Manifest 属性) |
|----|------|--------------------|
| MESSAGE_ID | メッセージ ID(収集時刻 + Topic + 元ファイル名から採番) | message_id |
| TOPIC | Topic 名 | Topic名 |
| SUBSCRIPTION | Subscription 名 | Subscription別配送状態のキー |
| STATUS | 配送状態(delivered / failed / dlq) | Subscription別配送状態 |
| RETRY | リトライ回数 | リトライ回数 |
| DELIVERED_AT | 配送日時(未配送は `-`) | 配送日時 |
| REPLAY | 再送(Replay)による配送か(`replay` / `-`) | 再送(Replay)記録、バリエーション「配信方式」 |

表示例:

```text
MESSAGE_ID                                TOPIC      SUBSCRIPTION  STATUS     RETRY  DELIVERED_AT          REPLAY
20260612T093001_orders_sales.csv          orders     current       delivered  0      2026-06-12T09:30:05   -
20260612T093001_orders_sales.csv          orders     next          failed     2      -                     -
20260611T220500_invoices_inv_0042.csv     invoices   current       dlq        5      -                     -
```

- 集計ビュー: 運用者が再送判断・DLQ 対処判断に使えるよう、topic / Subscription 別の件数集計(delivered / failed / dlq 別)も表示する(LP-401)。
- DLQ 確認(UC: DLQ隔離メッセージを確認する)では、DLQ の属性(隔離理由・失敗回数・隔離日時)を表示する:

```text
MESSAGE_ID                                TOPIC      ISOLATION_REASON              FAILURES  ISOLATED_AT
20260611T220500_invoices_inv_0042.csv     invoices   permission denied (write)     5         2026-06-11T22:31:10
```

### `config validate` の出力

- 検証 OK: 検証した設定の要約(Topic 数・Subscription 数・収集ソース数)を 1〜数行で表示し、終了コード 0。
- 検証 NG: エラーごとに「位置(YAML のキーパス)+ 原因 + 対処」を 1 件 1 ブロックで表示し、終了コード非 0(ux-design.md エラーメッセージ設計原則)。

```text
NG: topics[1].subscriptions[0].directory
原因: 配置先ディレクトリパスが未定義です
対処: subscriptions の各エントリに配置先ディレクトリパスを指定してください
```

### `replay` の出力

再配置の実行結果サマリー(対象 topic、指定期間または message_id、宛先 Subscription、再配置件数)を表示する。再送履歴は Manifest に記録され、`status` で確認できることを案内する(SP-102、CTR-003)。

### `serve` の出力

常駐デーモンのため、起動時メッセージ(Lock 取得結果・設定要約・メトリクスポート)以降は構造化ログ(後述)に出力する。スタックトレースを標準エラーへ垂れ流さない(CTR-002)。

## 終了コード規約

CTR-002「エラーは終了コードと構造化ログで表現」に基づき、0=正常 / 非 0=異常 を全サブコマンド共通とする。

| 終了コード | 分類 | 例 |
|----------:|------|----|
| 0 | 正常終了 | serve の graceful shutdown 完了、status 表示完了、config validate 検証 OK、replay 再配置完了 |
| 1 | 実行時エラー | 収集・配信・再配置中の回復不能エラー、Manifest 読み書き失敗 |
| 2 | 設定・引数エラー | config validate の検証 NG、replay の引数バリデーション NG(LR-401)、`--config` 不正 |
| 3 | 二重起動(Lock 取得失敗) | 同じ構成で 2 つ目の serve を起動した(SR-006。stale lock は安全に回復するため対象外) |

- 運用スクリプトは終了コードのみで成否を判定し、出力メッセージのパースに依存しないこと。
- 配信失敗のリトライ・DLQ 隔離はデーモン内で自己完結する正常な動作であり、serve の終了コードには影響しない(異常は構造化ログとメトリクスで観測する)。

## 設定 YAML の構造ガイド

CTP-003「単一 YAML 設定」: topics / 収集ソース / subscriptions / ポーリング間隔 / retention / リトライ / メトリクスポート / 認証情報参照 を単一 YAML で定義する(情報「設定」の属性そのまま)。Producer を変更せずに配信構成を増減・変更できる。

### 階層構造

```yaml
# 全体設定(情報「設定」の属性)
polling_interval: 60        # ポーリング間隔(秒)
archive_retention: 90       # Archive 保持期間 retention(日)— 条件「Archive保持期間」
retry_max_count: 5          # リトライ上限回数 — 条件「リトライ上限」
metrics_port: 9090          # /metrics・/healthz の公開ポート

# Topic 定義一覧(情報「Topic」)
topics:
  - name: orders            # Topic 名(論理的なファイル種別)
    description: "受注ファイル"
    # 収集ソース定義(情報「収集ソース」)— Topic ごとに切り替え可能
    # pull 型(FTP/SFTP/SCP/local): file-pubsub が List → Fetch → Delete で取りに行く
    source:
      type: sftp            # ソース種別: ftp / sftp / scp / local / inbox(push 受信モードと排他)
      host: legacy-host01   # 接続先ホスト(local / inbox の場合は不要)
      directory: /out/orders        # 対象ディレクトリパス(pull 型)
      original_file_handling: delete  # 元ファイル処理方式: delete(回収・既定) / copy(残す)
      stability_check:               # 安定待ち判定設定 — 条件「書き込み完了判定」
        interval: 10                 # サイズ・更新時刻の安定確認間隔(秒)
      exclude_patterns:              # 除外パターン
        - "*.tmp"
      # 認証情報(情報「認証情報」)— 環境変数参照 ${ENV_VAR} / 鍵ファイルパス推奨、平文許容(CTP-002)
      auth:
        username: ${SFTP_USER}
        password: ${SFTP_PASSWORD}  # または key_file: /etc/file-pubsub/keys/orders_rsa
    # Subscription 定義一覧(情報「Subscription」)— Topic 配下に複数定義
    subscriptions:
      - name: current       # Subscription 名(current / next / test 等)
        directory: /pub/orders/current   # 配置先ディレクトリパス
      - name: next
        directory: /pub/orders/next

  - name: invoices          # push 受信モード(inbox)の Topic 例
    description: "請求ファイル"
    # push 受信モード(inbox): Producer が受信ディレクトリへ直接 put し、file-pubsub が取り込む
    source:
      type: inbox           # ソース種別: push(put)受信モード(pull 型と排他)
      directory: /inbox/invoices  # 受信ディレクトリパス(pull 型と共通の directory キーを流用。host/auth は使わない)
      original_file_handling: delete  # delete(回収=受信ディレクトリから削除・既定) / copy(残す)
      completion: stability # 完了検知方式: stability(既定) / rename / marker — バリエーション「完了検知方式」
      # 取り込みトリガーは trigger キーを設けず常時ハイブリッド固定(fsnotify イベント駆動 + フォールバックポーリング)
      fallback_poll_interval: 30  # フォールバックポーリング間隔(秒)。省略時は polling_interval を流用 — REQ-013
      # completion: marker のとき、完了マーカーは「<元ファイル名>.done」固定(xxx.done。可変サフィックスは設定項目化しない)
      exclude_patterns:
        - "*.tmp"
    subscriptions:
      - name: current
        directory: /pub/invoices/current
```

### 記述ルール

| ルール | 内容 | 根拠 |
|-------|------|------|
| 単一ファイル | 配信構成はこの 1 ファイルが唯一の起点。分割設定・環境別 include は持たない | CTP-003、情報「設定」 |
| Topic 追加 = 設定追加のみ | Topic / Subscription の増減は YAML 編集だけで行い、Producer・Consumer を変更しない | CTP-003 |
| 認証情報の推奨記法 | 環境変数参照 `${ENV_VAR}` と鍵ファイルパス指定を推奨。YAML 平文も許容するが README で注意喚起する。push 受信モード(inbox)はローカル/共有 FS のため `auth` は不要(アクセス制御は OS・マウントの責務) | CTP-002、バリエーション「認証方式」 |
| 起動前検証 | 編集後は必ず `config validate` で構文・参照整合(Topic↔Subscription↔収集ソース↔認証情報参照)を検証してから serve する。push 受信モードでは `directory` 必須・`completion` は stability/rename/marker のいずれか(省略時 stability)・`fallback_poll_interval`(省略可。省略時 polling_interval)は正の整数 を検証する | SR-101 |
| ソース種別非依存 | source の type を切り替えても(pull 型 ↔ push 受信モード inbox を含む)subscriptions 以下や後段(Archive/Fan-out/Manifest)の定義は変わらない | LP-301、LP-302、情報「収集ソース」 |
| push 受信モードの設定キー | `type: inbox` のとき `directory`(受信ディレクトリ。pull 型と共通キーを流用。host/auth は使わない)・`completion`(stability/rename/marker、既定 stability。marker は完了マーカー `<元ファイル名>.done` 固定)・`fallback_poll_interval`(秒。省略時 polling_interval を流用)を指定する。トリガーは常時ハイブリッド固定のため `trigger` キーは設けない。pull 型固有のキー(host/auth)は使わない | REQ-012/013/014、バリエーション「収集ソース種別」「取り込みトリガー方式」「完了検知方式」 |

### push 受信モード(inbox)のメトリクス契約への影響

メトリクスは Topic ラベル付きで収集モードに依存しない。push 受信モードでも既存の 5 系列(`file_pubsub_last_collect_timestamp_seconds` 最終収集時刻 gauge / `file_pubsub_processed_total` 処理件数 counter / `file_pubsub_delivery_failure_total` 配信失敗数 counter / `file_pubsub_dlq_count` DLQ 件数 gauge / 滞留数 gauge)をそのまま `topic` ラベルで出す(SP-005、UC「外部監視基盤でTopic別メトリクスを観測する」)。push 受信モードでファイルを取り込んだ Topic も `file_pubsub_last_collect_timestamp_seconds{topic}` が更新され、収集停滞検知(`time() - file_pubsub_last_collect_timestamp_seconds`)・処理件数の観測がそのまま機能する。**RDRA に無いメトリクス(inbox 専用系列・トリガー別内訳等)は契約に含めず発明しない**。fsnotify ウォッチャ登録失敗やフォールバック縮退は構造化ログ(event_type=収集失敗)で観測する。

## 構造化ログのフィールド規約

CTP-001「構造化ログ」: message_id・Topic・Subscription・イベント種別を含む JSON 構造化ログ。どのメッセージのどの Subscription 配信が失敗したかを特定できる粒度とする。フィールドは情報.tsv「ログ」の属性そのまま。

### フィールド定義

| フィールド | 型 | 必須 | 内容(情報「ログ」の属性) |
|-----------|----|:---:|------------------------|
| `logged_at` | string (ISO 8601) | 必須 | 出力日時 |
| `message_id` | string | 配送系イベントで必須 | message_id |
| `topic` | string | 配送系イベントで必須 | Topic 名 |
| `subscription` | string | Subscription 配信イベントで必須 | Subscription 名 |
| `event_type` | string | 必須 | イベント種別(収集 / Archive 保存 / 配信 / リトライ / DLQ 隔離 / retention 削除 / 起動 / 停止 等、メッセージ配送状態・デーモン稼働状態の遷移に対応) |
| `error_detail` | string | エラー時のみ | エラー内容(原因 + 対処。ux-design.md エラーメッセージ設計原則に従う) |

出力例:

```json
{"logged_at":"2026-06-12T09:30:12+09:00","message_id":"20260612T093001_orders_sales.csv","topic":"orders","subscription":"next","event_type":"delivery_failed","error_detail":"配置先ディレクトリへの書き込みに失敗 (permission denied)。配置先ディレクトリの権限と実行ユーザを確認してください"}
```

### 運用ルール

| ルール | 内容 | 根拠 |
|-------|------|------|
| 1 イベント 1 行 JSON | jq / grep で `message_id` や `topic` をキーに追跡できる行指向 JSON とする | 情報「ログ」、UC: 構造化ログを調査する |
| 配信失敗の特定粒度 | Subscription 単位の配信イベントには必ず `message_id` + `topic` + `subscription` の 3 点を含める | CTP-001 |
| status との突き合わせ | `message_id` は Manifest・Archive・DLQ と共通のキーであり、`status` の表示と相互参照できる | CTR-003 |
| 保管期間 | ログ・Manifest の保管は 90 日目安 | CTP-001 |
| スタックトレース | 利用者向けの結果にはしない。必要な場合も `error_detail` 内に収め、構造を壊さない | CTR-002 |
| push 受信モードのイベント | push 受信モード(inbox)の取り込み・取り込み失敗も既存の `event_type`(収集 / 収集失敗)で表現し、新規 event_type を増やさない。受信ディレクトリ未存在・fsnotify 登録失敗・フォールバック縮退は `topic` + `error_detail`(原因 + 対処)で記録する | CTP-001、REQ-012/013 |
