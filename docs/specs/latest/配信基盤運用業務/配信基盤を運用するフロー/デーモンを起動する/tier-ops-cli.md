# デーモンを起動する - 運用 CLI仕様

## 変更概要

`serve` サブコマンドの CLI 仕様。コマンド層は引数解析(--config)のみを担い、起動処理本体は同一バイナリ内で共有する tier-daemon-worker のランタイム層・ユースケース層に委譲する(CLP-101)。

## CLI 仕様

### serve サブコマンド

- **コマンド**: `file-pubsub serve --config <path>`
- **対応 UC**: デーモンを起動する(あわせて「デーモンをgraceful shutdownで停止する」「冪等に処理を再開する」の起点となる)
- **実行アクター**: 配信基盤運用者

#### 引数

| 引数 | 型 | 必須 | 説明 |
|------|---|------|------|
| --config <path> | string | Yes | 単一 YAML 設定ファイルのパス(CTP-003)。全サブコマンド共通フラグ |

#### 出力

| 出力 | 内容 |
|------|------|
| 起動時メッセージ(標準出力) | Lock 取得結果・設定要約(Topic 数等)・メトリクスポート(ui-design.md「serve の出力」) |
| 以降の出力 | JSON 構造化ログ(logged_at / event_type=起動 等。ui-design.md ログ規約)。スタックトレースを標準エラーへ垂れ流さない(CTR-002) |

#### 終了コード

| 終了コード | 条件 |
|----------:|------|
| 0 | graceful shutdown による正常終了(停止 UC 参照) |
| 1 | 実行時エラー(回復不能エラー) |
| 2 | 設定・引数エラー(--config 不正、設定検証 NG) |
| 3 | 二重起動(Lock 取得失敗。stale lock は安全に回復するため対象外) |

## ビジネスルール

- 同一バイナリのサブコマンド構成: serve は status / replay / config validate と同一バイナリで提供する(SR-102)。
- CLI 専用のビジネスロジックは持たない: Lock 取得・設定検証・サイクル制御はすべて共有レイヤー(tier-daemon-worker)の責務(CLP-101、CLR-101)。
- 起動前検証の推奨フロー: 設定編集後は `config validate` で検証してから serve する(ui-design.md 記述ルール「起動前検証」)。
- スクリプト親和性: 運用スクリプトは終了コードのみで成否を判定する(CTR-002)。

## ティア完了条件（BDD）

```gherkin
Feature: デーモンを起動する - 運用 CLI

  Scenario: serve が起動時メッセージを表示する
    Given 検証 OK の /etc/file-pubsub/config.yaml が存在する
    When 配信基盤運用者が file-pubsub serve --config /etc/file-pubsub/config.yaml を実行する
    Then 標準出力に Lock 取得結果・設定要約・メトリクスポートを含む起動時メッセージが表示される

  Scenario: --config 未指定はエラーになる
    Given 配信基盤運用者がシェルを開いている
    When file-pubsub serve を --config なしで実行する
    Then 原因と対処(--config <path> の指定)が表示され、終了コード 2 で終了する
```

## 共通コンポーネント参照

[common-components.md](../../../_cross-cutting/ux-ui/common-components.md) の以下を利用する(起動処理本体は tier-daemon-worker の共有レイヤーへ委譲 — CLP-101)。

- [C-08 LockManager](../../../_cross-cutting/ux-ui/common-components.md#c-08-lockmanager) — 起動時メッセージに表示する Lock 取得結果の取得元(取得処理は委譲)
- [C-09 StructuredLogger](../../../_cross-cutting/ux-ui/common-components.md#c-09-structuredlogger) — 起動後の JSON 構造化ログ出力(スタックトレースを垂れ流さない — CTR-002)
- [C-14 ConfigLoader](../../../_cross-cutting/ux-ui/common-components.md#c-14-configloader) — `--config` の読込・検証(NG は終了コード 2)
