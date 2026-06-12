# ファイルを収集する(Collect) - 常駐デーモン仕様

## 変更概要

ポーリングサイクルの先頭工程として収集(Collect)を実装する。収集コネクタ(local / FTP / SFTP / SCP)は共通インターフェースで差し替え可能とし(LP-301)、安定待ち判定・除外パターン・一時名ダウンロード・元ファイル処理(回収 / copy + 処理済み管理)・message_id 採番・Manifest への収集済記録を行う。HTTP API・非同期メッセージングは関与しない。

## イベント処理仕様

### 収集サイクルハンドラ(collect)

- **トリガー**: ポーリングスケジューラ(設定 `polling_interval` 秒ごと。前回サイクル完了を待ち多重起動しない — LR-001)
- **入力チャネル**: 収集ソース(FTP / SFTP / SCP / ローカルディレクトリ)の対象ディレクトリ
- **出力チャネル**: ローカルファイルシステム `work/collect/{topic}/`(収集済ファイル)、`manifest/{message_id}.json`(収集済記録)、`processed/{topic}.json`(copy 時)
- **AsyncAPI**: 該当なし(メッセージキューは使用しない)

#### 処理フロー

1. 設定の Topic ごとに収集ソースへ接続する(認証情報は環境変数参照 / 鍵ファイルパス / 平文を解決。CTP-002)
2. 対象ディレクトリのファイル一覧(名前・サイズ・更新時刻)を取得する
3. domain の判定ルールを適用する(ソース種別非依存 — LR-203):
   - 除外パターン該当 → 対象外
   - サイズ・更新時刻が安定確認間隔で不一致 → 書き込み中として次サイクルへ持ち越し
   - copy 設定時、処理済み管理に収集元ファイル識別子が存在 → 再収集しない
4. 収集可能ファイルを一時名でダウンロードし、完了後に rename する(LR-303)
5. message_id を採番する(収集時刻 + Topic + 元ファイル名。LR-202)。同名再出力は別 message_id の新メッセージとする
6. Manifest に message_id・topic・収集時刻を AtomicWrite で記録し、メッセージ配送状態を「収集済」とする
7. 元ファイル処理判定:
   - 回収(既定): Archive 保存の成功を確認してから元ファイルを DELETE する(LR-303)
   - 残す(copy): 処理済み管理に収集元ファイル識別子・処理済み判定日時を記録する
8. Topic 別メトリクス(最終収集時刻・処理件数)を更新し、構造化ログ(event_type=収集)を出力する

#### エラーハンドリング

| エラー種別 | リトライ | DLQ | 説明 |
|-----------|---------|-----|------|
| 収集ソース接続失敗(認証エラー・ホスト不達) | Yes(次ポーリングサイクルで自動再試行) | No | 当該 Topic の収集をスキップし、topic を含む構造化ログ(原因 + 対処)を出力。デーモンは停止しない |
| ダウンロード中断(一時名のまま) | Yes(次サイクルで再取得) | No | 一時名ファイルは正式名に rename されないため後段に流れない。再取得で上書きする |
| 元ファイル DELETE 失敗 | Yes(次サイクルで再試行) | No | メッセージは収集済のまま進行。重複収集は message_id 採番と Manifest 照合で防ぐ |
| 処理済み管理の書き込み失敗 | Yes(同サイクル内で AtomicWrite 再試行、失敗時は次サイクル) | No | 記録成功までは元ファイルを未処理扱いとし、安全側(再収集候補)に倒す |

## データモデル変更

RDB は使用しない。ローカルファイルシステムのレイアウト(新規):

### work/collect/{topic}/{message_id}

| 項目 | 型 | 説明 | 変更種別 |
|--------|---|------|---------|
| ファイル実体 | binary(pass-through) | 収集済みファイル。一時名(`{message_id}.tmp`)で書き込み後 rename | 追加 |

### manifest/{message_id}.json(収集済の初期レコード)

| フィールド | 型 | 説明 | 変更種別 |
|--------|---|------|---------|
| message_id | string | 収集時刻 + Topic + 元ファイル名から採番 | 追加 |
| topic_name | string | Topic 名 | 追加 |
| original_file_name | string | 元ファイル名 | 追加 |
| collected_at | datetime(ISO 8601) | 収集時刻 | 追加 |
| status | string | メッセージ配送状態(この UC では「収集済」) | 追加 |
| retry_count | integer | リトライ回数(初期値 0。加算は別 UC「配信失敗をリトライしDLQへ隔離する」) | 追加 |

### processed/{topic}.json(copy 設定時のみ)

| フィールド | 型 | 説明 | 変更種別 |
|--------|---|------|---------|
| source_file_identifier | string | 収集元ファイル識別子(ファイル名・収集元パス等) | 追加 |
| processed_at | datetime(ISO 8601) | 処理済み判定日時 | 追加 |

## ビジネスルール

- 書き込み完了判定: 書き込み中のファイルは収集しない。安定待ち(サイズ・更新時刻)と除外パターンは domain 層の共通ルールとしソース種別に依存させない(SP-003、LR-203)
- 元ファイル処理判定: GET 後 DELETE が既定。copy 選択時は処理済み管理と照合し重複収集を防ぐ(SP-004)
- message_id 採番: 収集時刻 + Topic + 元ファイル名。同名再出力は別メッセージとして履歴を失わない(SR-002)
- 一時名ダウンロード: GET 中は一時名、完了後 rename。GET 後 DELETE は Archive 保存成功の確認後(LR-303)
- 収集コネクタは共通インターフェース経由で差し替え可能。IF 導入はここのみ(LP-301、CLP-001)
- 収集イベント・エラーは message_id / topic を含む 1 行 JSON 構造化ログで出力する(CTP-001)

## ティア完了条件（BDD）

```gherkin
Feature: ファイルを収集する(Collect) - 常駐デーモン

  Scenario: 安定したファイルだけを一時名ダウンロードで収集する
    Given topic「orders」の SFTP 収集ソース /out/orders に安定済みの「orders_20260612.csv」と書き込み中の「orders_20260613.csv」がある
    When 収集サイクルが実行される
    Then 「orders_20260612.csv」のみが一時名でダウンロードされ rename され、manifest に status=収集済 で記録される
    And 「orders_20260613.csv」は収集されない

  Scenario: copy 設定の処理済み照合で重複収集を防ぐ
    Given topic「customers」(original_file_handling=copy) の processed/customers.json に「customers_20260612.csv」が記録済みである
    When 収集サイクルが実行される
    Then 「customers_20260612.csv」は収集対象外となり新しいメッセージは発生しない

  Scenario: 接続失敗時もデーモンが停止しない
    Given topic「orders」の収集ソース legacy-host01 への接続が認証エラーで失敗する
    When 収集サイクルが実行される
    Then topic=orders, event_type=収集失敗 を含む構造化ログが出力され、他 Topic の収集処理は継続する
```

## 共通コンポーネント参照

[common-components.md](../../../_cross-cutting/ux-ui/common-components.md) の以下を利用する。

- [C-01 SourceConnector](../../../_cross-cutting/ux-ui/common-components.md#c-01-sourceconnector) — 収集ソース接続・ファイル一覧取得・一時名ダウンロード・元ファイル DELETE
- [C-02 AtomicWriter](../../../_cross-cutting/ux-ui/common-components.md#c-02-atomicwriter) — Manifest・処理済み管理の AtomicWrite 記録
- [C-03 ManifestStore](../../../_cross-cutting/ux-ui/common-components.md#c-03-manifeststore) — 収集済の初期 Manifest レコード作成
- [C-04 MessageIDGenerator](../../../_cross-cutting/ux-ui/common-components.md#c-04-messageidgenerator) — 収集時刻 + Topic + 元ファイル名からの message_id 採番
- [C-05 DeliveryStateMachine](../../../_cross-cutting/ux-ui/common-components.md#c-05-deliverystatemachine) — メッセージ配送状態「収集済」への初期遷移
- [C-06 StabilityChecker](../../../_cross-cutting/ux-ui/common-components.md#c-06-stabilitychecker) — 安定待ち判定・除外パターンによる収集可否判定
- [C-07 ProcessedStore](../../../_cross-cutting/ux-ui/common-components.md#c-07-processedstore) — copy 設定時の処理済み照合・処理済み記録
- [C-09 StructuredLogger](../../../_cross-cutting/ux-ui/common-components.md#c-09-structuredlogger) — collected / 収集失敗イベントの 1 行 JSON 出力
- [C-10 MetricsRegistry](../../../_cross-cutting/ux-ui/common-components.md#c-10-metricsregistry) — Topic 別最終収集時刻・処理件数の更新
- [C-12 PollingScheduler](../../../_cross-cutting/ux-ui/common-components.md#c-12-pollingscheduler) — 収集サイクルの周期トリガー(多重起動なし)
- [C-14 ConfigLoader](../../../_cross-cutting/ux-ui/common-components.md#c-14-configloader) — Topic / 収集ソース定義・認証情報・安定待ち設定の参照
