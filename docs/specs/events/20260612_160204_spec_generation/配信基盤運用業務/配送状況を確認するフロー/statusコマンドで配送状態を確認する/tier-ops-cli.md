# statusコマンドで配送状態を確認する - 運用 CLI仕様

## 変更概要

`status` サブコマンドの本体仕様。Manifest(メッセージ別 JSON)を読み取り、message_id・topic・Subscription 別の配送状態(delivered / failed / dlq)を ui-design.md の列規約に従うテーブルで表示し、topic / Subscription 別の件数集計を併せて出力する(SP-101 / LP-401)。コマンド層は引数解析・出力整形のみを担い、Manifest アクセスは共有 usecase 層を経由する(CLP-101 / CLR-101)。HTTP API は提供しない。

## コマンド仕様

### status

- **コマンド**: `file-pubsub status`
- **対応 UC**: statusコマンドで配送状態を確認する
- **認証**: なし(OS のファイル権限・実行ユーザに依存。CTP-007)
- **データソース**: Manifest(配送状態の正は常に Manifest — CTR-003)

#### 引数

| 引数 | 型 | 必須 | 説明 |
|-----------|---|------|------|
| `--config <path>` | string | Yes | 単一 YAML 設定ファイルのパス(CTP-003)。Manifest 配置場所の解決に使う |
| `--topic <name>` | string | No | Topic 名で絞り込む |
| `--subscription <name>` | string | No | Subscription 名で絞り込む(current / next / test 等) |
| `--status <state>` | string | No | 配送状態で絞り込む。値域は delivered / failed / dlq のみ(Manifest の語彙) |

#### 出力(標準出力) — ui-design.md の列規約

明細テーブル(1 行 1 レコードの行指向。罫線装飾なし。日時は ISO 8601):

| 列 | 内容 | 由来(Manifest 属性) |
|----|------|--------------------|
| MESSAGE_ID | メッセージ ID(収集時刻 + Topic + 元ファイル名から採番) | message_id |
| TOPIC | Topic 名 | Topic名 |
| SUBSCRIPTION | Subscription 名 | Subscription別配送状態のキー |
| STATUS | 配送状態(delivered / failed / dlq。言い換えない) | Subscription別配送状態 |
| RETRY | リトライ回数 | リトライ回数 |
| DELIVERED_AT | 配送日時(未配送は `-`) | 配送日時 |
| REPLAY | 再送(Replay)による配送か(`replay` / `-`) | 再送(Replay)記録 |

表示例:

```text
MESSAGE_ID                                  TOPIC      SUBSCRIPTION  STATUS     RETRY  DELIVERED_AT          REPLAY
20260601T091500_orders_orders_20260601.csv  orders     current       delivered  0      2026-06-01T09:15:08   -
20260601T091500_orders_orders_20260601.csv  orders     next          failed     2      -                     -
```

明細の後に、topic / Subscription 別の件数集計(delivered / failed / dlq 別)を表示する(LP-401: 運用者が再送判断・DLQ 対処判断に使える集計ビュー)。

#### エラー・終了コード

ui-design.md「終了コード規約」に従う。

| 終了コード | 条件 | 出力 |
|----------------|------|-----------|
| 0 | 照会・表示完了(該当 0 件を含む) | 配送状態テーブル + 件数集計 |
| 1 | Manifest 読み取り失敗等の実行時エラー | 標準エラー出力に原因 + 対処 |
| 2 | `--config` 不正、設定に存在しない topic / subscription、不正な状態値(delivered / failed / dlq 以外) | 引数バリデーション結果(LR-401) |

## データモデル変更

RDB は使用しない。ローカルファイルシステム上の Manifest を読み取り専用で参照する。

### Manifest(読取のみ)

| 項目 | 型 | 説明 | 変更種別 |
|--------|---|------|---------|
| manifest/{message_id}.json | JSON ファイル | message_id、Topic名、Subscription別配送状態(delivered / failed / dlq)、リトライ回数、配送日時、再送(Replay)記録。配送イベント追記 + Subscription 別現在状態 | 変更なし(読取) |

## ビジネスルール

- 配送状態の正は常に Manifest とする(CTR-003)。status は Subscription ディレクトリの実ファイル有無等から状態を推定しない。
- 状態値は Manifest の語彙(delivered / failed / dlq)をそのまま表示し、独自の言い換えをしない(ui-design.md)。
- 出力は人間可読テーブルのみ。`--json` 等の機械可読出力オプションは RDRA に定義がないため発明しない。機械連携は終了コードと構造化ログで行う(CTR-002)。
- 状態の色分け等のカラー表現に依存しない(ux-design.md アクセシビリティ方針: カラー非依存・行指向)。
- コマンド層は Manifest へ直接アクセスせず、共有 usecase 層(status 照会)を経由する(CLR-101)。
- 本コマンドは参照のみで状態を変更しない。ログ・Manifest の保管は 90 日目安(CTP-001)であり、status はその範囲の履歴を照会する。

## ティア完了条件（BDD）

```gherkin
Feature: statusコマンドで配送状態を確認する - 運用 CLI

  Scenario: 列規約どおりのテーブルが表示される
    Given manifest/20260601T091500_orders_orders_20260601.csv.json に current=delivered(2026-06-01T09:15:08)・next=failed(リトライ 2) が記録されている
    When 「status --config config.yaml」を実行する
    Then ヘッダ行に MESSAGE_ID TOPIC SUBSCRIPTION STATUS RETRY DELIVERED_AT REPLAY が表示され明細 2 行が行指向で出力され終了コード 0 で終了する

  Scenario: 件数集計ビューが topic / Subscription 別に出力される
    Given topic=orders に delivered 10 件(current)・failed 2 件(next)、topic=invoices に dlq 1 件(current) の Manifest が存在する
    When 「status --config config.yaml」を実行する
    Then 明細の後に orders/current: delivered=10、orders/next: failed=2、invoices/current: dlq=1 の集計が表示される

  Scenario: grep で特定 message_id の行を抽出できる(行指向出力)
    Given manifest/ に複数メッセージの配送記録が存在する
    When 「status --config config.yaml」の出力を「grep 20260601T091500_orders_orders_20260601.csv」にパイプする
    Then 該当 message_id の行だけが抽出できる(1 行 1 レコードの規約)

  Scenario: 不正な状態値は引数エラーになる
    Given Manifest の語彙にない状態値「pending」を指定する
    When 「status --config config.yaml --status pending」を実行する
    Then 標準エラー出力に原因と対処が表示され終了コード 2 で終了する
```

## 共通コンポーネント参照

[common-components.md](../../../_cross-cutting/ux-ui/common-components.md) の以下を利用する(Manifest アクセスは共有 usecase 経由 — CLR-101)。

- [C-03 ManifestStore](../../../_cross-cutting/ux-ui/common-components.md#c-03-manifeststore) — message_id・topic・Subscription 別配送状態の読取(配送状態の正)
- [C-13 CLITableRenderer](../../../_cross-cutting/ux-ui/common-components.md#c-13-clitablerenderer) — 配送状態テーブル(MESSAGE_ID〜REPLAY の 7 列)+ topic / Subscription 別件数集計の行指向出力
- [C-14 ConfigLoader](../../../_cross-cutting/ux-ui/common-components.md#c-14-configloader) — `--config` の解決(Manifest 配置場所)と topic / subscription 存在検証
