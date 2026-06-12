# 再送(Replay)を実行する - 運用 CLI仕様

## 変更概要

`replay` サブコマンドを実装する。コマンド層(L-cli-command)は引数解析・バリデーション・結果サマリー表示のみを担い、再配置の実処理(Archive 読出・AtomicWrite 配置・Manifest 記録)は tier-daemon-worker と共有する usecase / domain / gateway 層に委譲する(CLP-101 / CLR-101)。HTTP API は提供しない。

## コマンド仕様

### replay

- **コマンド**: `file-pubsub replay`
- **対応 UC**: 再送(Replay)を実行する
- **認証**: なし(OS のファイル権限・実行ユーザに依存。CTP-007)
- **データソース**: Archive(読出元)、Subscription ディレクトリ(配置先)、Manifest(記録先。CTR-003)

#### 引数

| 引数 | 型 | 必須 | 説明 |
|-----------|---|------|------|
| `--config <path>` | string | Yes | 単一 YAML 設定ファイルのパス(CTP-003)。Archive / Subscription / Manifest 配置場所の解決に使う |
| `--topic <name>` | string | Yes | 再送対象の Topic 名(情報「Topic」) |
| `--message-id <id>` | string | 期間指定と排他で必須 | 再送対象のメッセージ指定(情報「メッセージ」の message_id) |
| `--from <date>` / `--to <date>` | string (ISO 8601 日付) | message_id 指定と排他で必須 | 再送対象の期間指定(message_id の収集時刻で絞り込む) |
| `--subscription <name>` | string | Yes | 宛先 Subscription(情報「Subscription」)。指定した Subscription にのみ再配置する(条件「Replay記録」) |

#### 出力(標準出力)

再配置の実行結果サマリーを表示する(ui-design.md「`replay` の出力」):

- 対象 topic、指定期間または message_id、宛先 Subscription、再配置件数
- 再送履歴は Manifest に記録され、`status` で確認できることの案内

#### エラー・終了コード

| 終了コード | 条件 | 出力 |
|----------------|------|-----------|
| 0 | 再配置完了(対象 0 件の場合も再配置件数 0 のサマリーで正常終了) | 再配置結果サマリー |
| 1 | Archive 読出失敗・宛先ディレクトリ書込失敗・Manifest 更新失敗等の実行時エラー | 標準エラー出力に原因 + 対処。一時名(.tmp)のまま残さず正式名への rename は行わない |
| 2 | 引数バリデーション NG(宛先 subscription 未指定、message_id と期間の両方指定/両方未指定、設定に存在しない topic / subscription、`--config` 不正) | 引数バリデーション結果(LR-401)。再配置は一切行わない |

## 非同期イベント

該当なし(メッセージキュー・非同期イベントは存在しない。Consumer へのファイル受け渡しは Subscription ディレクトリ経由で、後続 UC「Subscriptionディレクトリから再送ファイルを取得する」の外部 IF 仕様で定義する)。

## データモデル変更

RDB は使用しない。ローカルファイルシステム上の以下を操作する。

### Archive(読取のみ)

| 項目 | 型 | 説明 | 変更種別 |
|--------|---|------|---------|
| archive/{topic}/{message_id} | ファイル | 再送対象の読出元。保存先パス(Topic別)・message_id・元ファイル名・ファイル内容・保存日時(情報「Archiveファイル」) | 変更なし(読取) |

### Subscription ディレクトリ(書込)

| 項目 | 型 | 説明 | 変更種別 |
|--------|---|------|---------|
| {配置先ディレクトリパス}/{元ファイル名}.tmp | ファイル | 一時名での書込(AtomicWrite の前半) | 追加(一時) |
| {配置先ディレクトリパス}/{元ファイル名} | ファイル | 正式名へ rename(AtomicWrite の後半)。常に完全な内容 | 追加 |

### Manifest(追記)

| 項目 | 型 | 説明 | 変更種別 |
|--------|---|------|---------|
| manifest/{message_id}.json | JSON ファイル | 再送(Replay)記録の追記、Subscription別配送状態・配送日時の更新 | 更新(配送イベント追記) |

## ビジネスルール

- 再送は Topic・期間(またはメッセージ指定)・宛先 Subscription を指定して実行し、指定した Subscription にのみ再配置する。他 Subscription の配送に影響しない(条件「Replay記録」、SP-102)。
- Replay の配送履歴も Manifest に記録する。配送状態の正は常に Manifest(CTR-003)。
- 配置は一時名で書き込んでから正式名へ rename する AtomicWrite とする(条件「AtomicWrite配置」、LR-301)。
- 不正な指定は実行前に終了コードで弾く(LR-401)。誤った状態を作ってから気づかせない(ux-design.md フィードフォワード)。
- 再実行しても Manifest に基づき追跡できる冪等な操作とし、運用者が「もう一度実行してよいか」を悩まなくてよい設計とする(ux-design.md 冪等性による安心感)。
- コマンド層は Archive / Manifest へ直接アクセスせず、共有 usecase 層(Replay)を経由する(CLR-101)。

## ティア完了条件（BDD）

```gherkin
Feature: 再送(Replay)を実行する - 運用 CLI

  Scenario: message_id 指定の replay が宛先にのみ AtomicWrite で配置し Manifest に記録する
    Given archive/orders/20260601T091500_orders_orders_20260601.csv が存在し config.yaml に topic=orders の subscription=current と next が定義されている
    When 「replay --config config.yaml --topic orders --message-id 20260601T091500_orders_orders_20260601.csv --subscription next」を実行する
    Then next の配置先ディレクトリに orders_20260601.csv が配置され一時名 .tmp ファイルが残存せず current の配置先ディレクトリは変化せず manifest/20260601T091500_orders_orders_20260601.csv.json に Replay 記録が追記され終了コード 0 で終了する

  Scenario: 期間指定の replay が再配置件数をサマリー表示する
    Given archive/orders/ に 2026-05 の message_id を持つファイルが 20 件存在する
    When 「replay --config config.yaml --topic orders --from 2026-05-01 --to 2026-05-31 --subscription next」を実行する
    Then 標準出力に対象 topic=orders・期間 2026-05-01..2026-05-31・宛先 next・再配置件数 20 のサマリーと status で確認できる旨の案内が表示される

  Scenario: message_id と期間の両方指定は引数エラーになる
    Given archive/orders/ に再送可能なファイルが存在する
    When 「replay --config config.yaml --topic orders --message-id 20260601T091500_orders_orders_20260601.csv --from 2026-05-01 --to 2026-05-31 --subscription next」を実行する
    Then 原因(message_id と期間は排他)と対処が表示され終了コード 2 で終了する

  Scenario: 対象 0 件は再配置件数 0 の正常終了となる
    Given archive/orders/ に 2026-04 の message_id を持つファイルが存在しない
    When 「replay --config config.yaml --topic orders --from 2026-04-01 --to 2026-04-30 --subscription next」を実行する
    Then 再配置件数 0 のサマリーが表示され終了コード 0 で終了する
```

## 共通コンポーネント参照

[common-components.md](../../../_cross-cutting/ux-ui/common-components.md) の以下を利用する(再配置の実処理は共有 usecase 経由 — CLR-101)。

- [C-02 AtomicWriter](../../../_cross-cutting/ux-ui/common-components.md#c-02-atomicwriter) — 宛先 Subscription への一時名→rename 再配置
- [C-03 ManifestStore](../../../_cross-cutting/ux-ui/common-components.md#c-03-manifeststore) — Replay 記録の追記・Subscription 別配送状態の更新
- [C-05 DeliveryStateMachine](../../../_cross-cutting/ux-ui/common-components.md#c-05-deliverystatemachine) — 再送による再配置(replayed)の状態記録
- [C-13 CLITableRenderer](../../../_cross-cutting/ux-ui/common-components.md#c-13-clitablerenderer) — 再配置結果サマリーの出力整形
- [C-14 ConfigLoader](../../../_cross-cutting/ux-ui/common-components.md#c-14-configloader) — `--config` の解決(Archive / Subscription / Manifest 配置場所)と topic / subscription 存在検証
