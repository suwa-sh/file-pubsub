# 構造化ログを調査する - 常駐デーモン仕様（ログ出力契約）

## 変更概要

tier-daemon-worker(および同一バイナリの CLI)が出力する構造化ログの**出力契約**を定義する。どのメッセージのどの Subscription 配信が失敗したかを特定できる粒度(CTP-001)を満たす 1 イベント 1 行の JSON とし、運用者が grep / jq と `status` の突き合わせだけで障害調査を完結できることを保証する。エラーは usecase でリトライ可否を分類し(LR-102)、runtime / CLI で終了コードと構造化ログに変換する(CLR-001 / CTR-002)。

## ログ出力契約

### 出力先・形式

| 項目 | 契約 | 根拠 |
|------|------|------|
| 出力先 | stdout(またはログファイル)。導入先のログ転送・ローテーションの標準運用に委ねる | storage_mapping E-013、CTP-007 |
| 形式 | 1 イベント 1 行の JSON(行指向。jq / grep で処理可能) | ui-design.md「構造化ログのフィールド規約」 |
| 文字どおりの構造維持 | スタックトレースを利用者向けの結果にしない。必要な場合も `error_detail` 内に収め、行構造を壊さない | CTR-002 |
| 保管期間 | ログ・Manifest の保管は 90 日目安 | CTP-001 |

### フィールド定義(情報「ログ」の属性そのまま)

| フィールド | 型 | 必須 | 内容 |
|-----------|----|:---:|------|
| `logged_at` | string (ISO 8601) | 必須 | 出力日時 |
| `message_id` | string | 配送系イベントで必須 | 対象メッセージの message_id(Manifest・Archive・DLQ と共通キー) |
| `topic` | string | 配送系イベントで必須 | Topic 名 |
| `subscription` | string | Subscription 配信イベントで必須 | Subscription 名 |
| `event_type` | string | 必須 | イベント種別(下表) |
| `error_detail` | string | エラー時のみ | エラー内容。原因 + 対処を 1 メッセージで記述(ux-design.md エラーメッセージ設計原則)。リトライ可否(一時的 / 恒久的)を文言で区別する |

### event_type の値域(状態モデルの遷移に対応)

| event_type | 対応する遷移・処理 | message_id | topic | subscription |
|-----------|------------------|:---:|:---:|:---:|
| collected | メッセージ配送状態: → 収集済(Collect) | 必須 | 必須 | - |
| archived | メッセージ配送状態: 収集済 → Archive保存済 | 必須 | 必須 | - |
| delivered | メッセージ配送状態: 配信中 → 配信済(delivered) | 必須 | 必須 | 必須 |
| delivery_failed | メッセージ配送状態: 配信中 → 配信失敗(failed) | 必須 | 必須 | 必須 |
| retry | メッセージ配送状態: 配信失敗(failed) → リトライ中 → 配信中 | 必須 | 必須 | 必須 |
| dlq_isolated | メッセージ配送状態: リトライ中 → DLQ隔離(dlq) | 必須 | 必須 | 必須 |
| replayed | 再送(Replay)による再配置(配信済/dlq → 配信中) | 必須 | 必須 | 必須 |
| retention_deleted | Archiveファイル保持状態: 保持中 → 削除済 | 必須 | 必須 | - |
| daemon_started / daemon_stopped | デーモン稼働状態: 起動中 → 稼働中 / 停止処理中 → 停止済 | - | - | - |
| config_error 等の非配送系エラー | 設定・起動時のエラー(配送に紐づかない) | -(代替コンテキストを error_detail に含める) | - | - |

出力例:

```json
{"logged_at":"2026-06-01T09:15:12+09:00","message_id":"20260601T091500_orders_orders_20260601.csv","topic":"orders","subscription":"next","event_type":"delivery_failed","error_detail":"配置先ディレクトリへの書き込みに失敗 (permission denied)。配置先ディレクトリの権限と実行ユーザを確認してください"}
```

### 出力契約の不変条件

1. **Subscription 単位の配信イベントには必ず `message_id` + `topic` + `subscription` の 3 点を含める**(CTP-001)。これが「どのメッセージのどの Subscription 配信が失敗したか」の特定粒度の根拠。
2. `message_id` は Manifest・Archive・DLQ と共通のキーであり、`status` の表示と相互参照できる(CTR-003)。
3. `error_detail` は原因 + 対処を 1 メッセージで伝える。原因だけ・対処だけのメッセージは禁止(ux-design.md)。
4. 配送に紐づかないエラー(設定エラー等)は message_id / topic / subscription を省略してよいが、特定に必要な代替コンテキスト(設定ファイルのキー位置等)を `error_detail` に含める。
5. エラーの分類(一時的 = リトライ対象 / 恒久的 = DLQ 隔離)は usecase 層で行い(LR-102)、runtime / CLI が終了コードと構造化ログに変換する(CLR-001)。

## 運用者の調査手順

1. **対象の特定**: `status --config config.yaml --status failed`(または `--status dlq`)で失敗メッセージの message_id を特定する。
2. **イベント抽出**: ログを `grep <message_id>` で抽出し、該当メッセージの全イベント行(collected → archived → delivered / delivery_failed → retry → dlq_isolated)を時系列で確認する。
3. **原因の絞り込み**: `jq 'select(.event_type=="delivery_failed")'` 等で失敗イベントの `error_detail` を抽出し、原因 + 対処を読む。subscription フィールドでどの Subscription 配信の失敗かを確定する。
4. **対処**: error_detail の対処(権限修正等)を実施し、必要なら `replay` で再送する。リトライで自動回復済みかは event_type=retry → delivered の並びで判断できる。

## エラーハンドリング(ログ出力自体)

| エラー種別 | リトライ | DLQ | 説明 |
|-----------|---------|-----|------|
| ログ出力先への書き込み失敗 | No | No | 配送処理は継続する(ログ出力失敗で配送を止めない)。stdout 出力の場合は導入先のログ転送の責務 |

## データモデル変更

RDB は使用しない。本契約で出力されるログのレイアウトは以下。

### 構造化ログ(追記)

| 項目 | 型 | 説明 | 変更種別 |
|--------|---|------|---------|
| stdout / ログファイル | 1 行 JSON の追記ストリーム | logged_at、message_id、topic、subscription、event_type、error_detail(情報「ログ」の属性) | 追加(イベントごとに 1 行追記) |

## ビジネスルール

- 構造化ログは「運用者が外部の助けなしに障害調査を完結できる粒度」を満たすこと(情報「ログ」の要求粒度、CTP-001)。
- ログは観測用であり、配送状態の正は常に Manifest とする(CTR-003)。ログと Manifest が食い違う場合は Manifest を正とする。
- フィールド名・値域は本契約から変更しない(外部監視基盤・調査スクリプトとの互換性維持)。
- 機械可読の調査インターフェースはこのログと終了コードのみ(CTR-002)。`--json` 等の CLI 出力オプションは発明しない。

## ティア完了条件（BDD）

```gherkin
Feature: 構造化ログを調査する - 常駐デーモン(ログ出力契約)

  Scenario: Subscription 配信失敗イベントが 3 点キー付きの 1 行 JSON で出力される
    Given topic=orders の message_id=20260601T091500_orders_orders_20260601.csv の subscription=next への配信が permission denied で失敗する
    When デーモンが delivery_failed イベントを記録する
    Then ログに logged_at・message_id=20260601T091500_orders_orders_20260601.csv・topic=orders・subscription=next・event_type=delivery_failed・error_detail(原因 + 対処) を含む 1 行 JSON が追記される

  Scenario: メッセージのライフサイクルが event_type で追跡できる
    Given message_id=20260601T091500_orders_orders_20260601.csv が収集から current への配信成功まで処理される
    When ログを該当 message_id で grep する
    Then event_type が collected → archived → delivered の順で出力されており subscription=current の delivered 行が確認できる

  Scenario: 非配送系エラーは代替コンテキストを含む
    Given 設定 YAML の topics[0].source.host が不正で接続できない
    When デーモンが設定起因のエラーを記録する
    Then ログ行に message_id は含まれないが error_detail に設定キー位置(topics[0].source.host)と原因 + 対処が含まれ行構造(1 行 JSON)が維持されている

  Scenario: ログ出力失敗でも配送処理は継続する
    Given ログファイルへの書き込みが一時的に失敗する
    When デーモンが配信サイクルを実行する
    Then 配送処理(Collect → Archive → Fan-out)は停止せず継続する
```

## 共通コンポーネント参照

[common-components.md](../../../_cross-cutting/ux-ui/common-components.md) の以下を利用する。

- [C-09 StructuredLogger](../../../_cross-cutting/ux-ui/common-components.md#c-09-structuredlogger) — 本 UC のログ出力契約(フィールド規約・event_type 値域・3 点キー不変条件)を実装する出力コンポーネント。本書がその契約の正本
