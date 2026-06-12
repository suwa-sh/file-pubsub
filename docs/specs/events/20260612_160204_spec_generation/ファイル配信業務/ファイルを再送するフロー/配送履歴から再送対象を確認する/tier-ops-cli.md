# 配送履歴から再送対象を確認する - 運用 CLI仕様

## 変更概要

`status` サブコマンドに、再送対象特定のための Manifest 照会(配送状態テーブル表示 + 絞り込み + 件数集計)を実装する。コマンド層(L-cli-command)は引数解析・バリデーション・出力整形のみを担い、Manifest へのアクセスは tier-daemon-worker と共有する usecase 層を経由する(CLP-101 / CLR-101)。HTTP API は提供しない。

## コマンド仕様

### status

- **コマンド**: `file-pubsub status`
- **対応 UC**: 配送履歴から再送対象を確認する
- **認証**: なし(OS のファイル権限・実行ユーザに依存。CTP-007)
- **データソース**: Manifest(メッセージ別 JSON。配送状態の正は常に Manifest — CTR-003)

#### 引数

| 引数 | 型 | 必須 | 説明 |
|-----------|---|------|------|
| `--config <path>` | string | Yes | 単一 YAML 設定ファイルのパス(CTP-003)。Manifest 配置場所の解決に使う |
| `--topic <name>` | string | No | Topic 名で絞り込む(情報「Topic」) |
| `--subscription <name>` | string | No | Subscription 名で絞り込む(情報「Subscription」。current / next / test 等) |
| `--status <state>` | string | No | 配送状態で絞り込む。値域は Manifest の語彙 delivered / failed / dlq のみ |

#### 出力(標準出力)

ui-design.md「`status` のテーブル表示」の列規約に従う。1 行 1 レコードの行指向テーブル(罫線装飾なし、grep / awk で処理可能)。日時は ISO 8601。

| 列 | 内容 | 由来(Manifest 属性) |
|-----------|---|------|
| MESSAGE_ID | メッセージ ID | message_id |
| TOPIC | Topic 名 | Topic名 |
| SUBSCRIPTION | Subscription 名 | Subscription別配送状態のキー |
| STATUS | delivered / failed / dlq(言い換えない) | Subscription別配送状態 |
| RETRY | リトライ回数 | リトライ回数 |
| DELIVERED_AT | 配送日時(未配送は `-`) | 配送日時 |
| REPLAY | 再送(Replay)による配送か(`replay` / `-`) | 再送(Replay)記録 |

明細の後に topic / Subscription 別の件数集計(delivered / failed / dlq 別)を表示する(LP-401)。

#### エラー・終了コード

ui-design.md「終了コード規約」に従う。

| 終了コード | 条件 | 出力 |
|----------------|------|-----------|
| 0 | 照会・表示完了(該当 0 件を含む) | 配送状態テーブル + 件数集計 |
| 1 | Manifest 読み取り失敗等の実行時エラー | 標準エラー出力に原因 + 対処(ux-design.md エラーメッセージ設計原則) |
| 2 | `--config` 不正、設定に存在しない topic / subscription 指定、不正な状態値 | 引数バリデーション結果(LR-401) |

## データモデル変更

RDB は使用しない。ローカルファイルシステム上の以下を読み取り専用で参照する(レイアウトの正本は `_cross-cutting` のデータストアレイアウト)。

### Manifest(読取のみ)

| 項目 | 型 | 説明 | 変更種別 |
|--------|---|------|---------|
| manifest/{message_id}.json | JSON ファイル | message_id、Topic名、Subscription別配送状態(delivered / failed / dlq)、リトライ回数、配送日時、再送(Replay)記録 | 変更なし(SELECT 相当の読取) |
| dlq/{topic}/{message_id} | ファイル | 隔離メッセージの実体。dlq 状態の対象確認時に存在を参照(隔離理由・失敗回数・隔離日時は dlq/{topic}/{message_id}.meta.json) | 変更なし(読取) |

## ビジネスルール

- 配送状態の正は常に Manifest とする(CTR-003)。status は Manifest 以外から状態を推定しない。
- 状態値は Manifest の語彙(delivered / failed / dlq)をそのまま表示し、独自の言い換えをしない(ui-design.md)。
- コマンド層は Manifest へ直接アクセスせず、共有 usecase 層(status 照会)を経由する(CLR-101)。
- 不正な引数は実行前に終了コードで弾く(LR-401)。
- 出力は人間可読テーブルのみ。機械連携は終了コードと構造化ログで行い、`--json` 等の機械可読出力オプションは発明しない(ui-design.md)。
- 本コマンドは参照のみで、Manifest・Archive・Subscription ディレクトリを変更しない。

## ティア完了条件（BDD）

```gherkin
Feature: 配送履歴から再送対象を確認する - 運用 CLI

  Scenario: 絞り込み付き status が Manifest の該当行だけを表示する
    Given manifest/20260601T091500_orders_orders_20260601.csv.json に subscription=next が failed・リトライ回数 2 と記録されている
    When 「status --config config.yaml --topic orders --status failed」を実行する
    Then 出力に「20260601T091500_orders_orders_20260601.csv  orders  next  failed  2  -  -」の行が含まれ delivered の行は含まれず終了コード 0 で終了する

  Scenario: 件数集計が topic / Subscription 別に表示される
    Given topic=orders の Manifest が current に delivered 10 件・next に failed 2 件記録されている
    When 「status --config config.yaml --topic orders」を実行する
    Then 明細の後に orders/current: delivered=10、orders/next: failed=2 の件数集計が表示される

  Scenario: 存在しない topic の指定は引数エラーになる
    Given 設定 YAML config.yaml の topics に「unknown-topic」が定義されていない
    When 「status --config config.yaml --topic unknown-topic」を実行する
    Then 標準エラー出力に原因と対処が表示され終了コード 2 で終了する
```

## 共通コンポーネント参照

[common-components.md](../../../_cross-cutting/ux-ui/common-components.md) の以下を利用する(Manifest アクセスは共有 usecase 経由 — CLR-101)。

- [C-03 ManifestStore](../../../_cross-cutting/ux-ui/common-components.md#c-03-manifeststore) — topic / subscription / status 絞り込みによる配送状態の照会
- [C-13 CLITableRenderer](../../../_cross-cutting/ux-ui/common-components.md#c-13-clitablerenderer) — 配送状態テーブル(7 列)+ 件数集計の行指向出力
- [C-14 ConfigLoader](../../../_cross-cutting/ux-ui/common-components.md#c-14-configloader) — `--config` の解決(Manifest 配置場所)と topic / subscription 存在検証
