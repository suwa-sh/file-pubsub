# DLQ隔離メッセージを確認する - 運用 CLI仕様

## 変更概要

`status` サブコマンドの DLQ 確認ビューを実装する。状態 dlq での絞り込み時に、DLQ の属性(隔離メッセージ(message_id)・隔離理由・失敗回数・隔離日時)を表示するテーブルへ切り替える(ui-design.md「DLQ 確認」)。運用者はこの一覧から原因を把握し、再送(Replay)・破棄の対処を判断する。Manifest / DLQ へのアクセスは共有 usecase 層を経由する(CLP-101 / CLR-101)。HTTP API は提供しない。

## コマンド仕様

### status --status dlq(DLQ 確認ビュー)

- **コマンド**: `file-pubsub status --status dlq`
- **対応 UC**: DLQ隔離メッセージを確認する
- **認証**: なし(OS のファイル権限・実行ユーザに依存。CTP-007)
- **データソース**: Manifest の dlq 記録 + dlq/ ディレクトリの隔離情報(配送状態の正は Manifest — CTR-003)

#### 引数

| 引数 | 型 | 必須 | 説明 |
|-----------|---|------|------|
| `--config <path>` | string | Yes | 単一 YAML 設定ファイルのパス(CTP-003) |
| `--status dlq` | string | Yes(本ビューの起点) | dlq 状態での絞り込み。DLQ 属性テーブルに表示を切り替える |
| `--topic <name>` | string | No | Topic 名で絞り込む |

#### 出力(標準出力) — ui-design.md の DLQ 表示規約

| 列 | 内容 | 由来(DLQ / Manifest 属性) |
|----|------|--------------------------|
| MESSAGE_ID | 隔離メッセージの message_id | DLQ: 隔離メッセージ(message_id) |
| TOPIC | Topic 名 | メッセージ: Topic名 |
| ISOLATION_REASON | 隔離理由 | DLQ: 隔離理由 |
| FAILURES | 失敗回数(リトライ上限超過) | DLQ: 失敗回数 |
| ISOLATED_AT | 隔離日時(ISO 8601) | DLQ: 隔離日時 |

表示例:

```text
MESSAGE_ID                                TOPIC      ISOLATION_REASON              FAILURES  ISOLATED_AT
20260611T220500_invoices_inv_0042.csv     invoices   permission denied (write)     5         2026-06-11T22:31:10
```

topic 別の DLQ 件数集計も表示する(LP-401 の集計ビュー。/metrics の DLQ 件数と突き合わせ可能)。

#### エラー・終了コード

| 終了コード | 条件 | 出力 |
|----------------|------|-----------|
| 0 | 照会・表示完了(隔離 0 件を含む) | DLQ テーブル + topic 別件数 |
| 1 | Manifest / DLQ 読み取り失敗等の実行時エラー | 標準エラー出力に原因 + 対処 |
| 2 | `--config` 不正、設定に存在しない topic 指定 | 引数バリデーション結果(LR-401) |

## データモデル変更

RDB は使用しない。ローカルファイルシステム上の以下を読み取り専用で参照する。

### DLQ / Manifest(読取のみ)

| 項目 | 型 | 説明 | 変更種別 |
|--------|---|------|---------|
| dlq/{topic}/{message_id} | ファイル | リトライ上限超過で隔離されたメッセージの実体(UC「配信失敗をリトライしDLQへ隔離する」が作成) | 変更なし(読取) |
| dlq/{topic}/{message_id}.meta.json | JSON ファイル | 隔離情報(隔離理由・失敗回数・隔離日時) | 変更なし(読取) |
| manifest/{message_id}.json | JSON ファイル | Subscription別配送状態の dlq 記録・リトライ回数。DLQ 一覧との突き合わせ | 変更なし(読取) |

## ビジネスルール

- DLQ はリトライ規定回数(設定 YAML の `retry_max_count`)を超えた恒久的な失敗の隔離先であり、滞留させず運用者の対処判断(再送 / 破棄)に委ねる(SR-004、条件「リトライ上限」)。
- 本コマンドは確認のみで、DLQ からの再送・破棄は行わない。再送は `replay`(UC「再送(Replay)を実行する」)で実行する。破棄(隔離メッセージの削除)の自動化コマンドは RDRA に定義がないため発明しない。
- 隔離理由はエラーメッセージ設計原則(ux-design.md)に従い原因が特定できる粒度で表示する。構造化ログの `error_detail`(event_type=DLQ隔離)と message_id で突き合わせできる。
- コマンド層は DLQ / Manifest へ直接アクセスせず、共有 usecase 層を経由する(CLR-101)。

## ティア完了条件（BDD）

```gherkin
Feature: DLQ隔離メッセージを確認する - 運用 CLI

  Scenario: DLQ 属性テーブルが表示される
    Given dlq/invoices/20260611T220500_invoices_inv_0042.csv が存在し隔離メタ dlq/invoices/20260611T220500_invoices_inv_0042.csv.meta.json に隔離理由「permission denied (write)」失敗回数 5 隔離日時 2026-06-11T22:31:10 が記録されている
    When 「status --config config.yaml --status dlq」を実行する
    Then ヘッダ行に MESSAGE_ID TOPIC ISOLATION_REASON FAILURES ISOLATED_AT が表示され該当 1 行が行指向で出力され終了コード 0 で終了する

  Scenario: topic 絞り込みで特定 Topic の DLQ だけを確認する
    Given topic=invoices に 1 件・topic=orders に 2 件の DLQ 隔離メッセージが存在する
    When 「status --config config.yaml --status dlq --topic invoices」を実行する
    Then TOPIC=invoices の 1 行だけが表示され orders の行は表示されない

  Scenario: 隔離 0 件は正常終了する
    Given dlq/ に隔離メッセージが存在しない
    When 「status --config config.yaml --status dlq」を実行する
    Then 該当 0 件の表示で終了コード 0 で終了する
```

## 共通コンポーネント参照

[common-components.md](../../../_cross-cutting/ux-ui/common-components.md) の以下を利用する(Manifest / DLQ アクセスは共有 usecase 経由 — CLR-101)。

- [C-03 ManifestStore](../../../_cross-cutting/ux-ui/common-components.md#c-03-manifeststore) — dlq 記録の照会(dlq/ の隔離メタとの突き合わせ)
- [C-13 CLITableRenderer](../../../_cross-cutting/ux-ui/common-components.md#c-13-clitablerenderer) — DLQ 属性テーブル(MESSAGE_ID / TOPIC / ISOLATION_REASON / FAILURES / ISOLATED_AT)+ topic 別件数の出力
- [C-14 ConfigLoader](../../../_cross-cutting/ux-ui/common-components.md#c-14-configloader) — `--config` の解決と topic 存在検証
