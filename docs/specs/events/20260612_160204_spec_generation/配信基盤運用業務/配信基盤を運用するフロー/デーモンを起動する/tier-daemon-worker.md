# デーモンを起動する - 常駐デーモン仕様

## 変更概要

serve で起動される常駐デーモンの起動シーケンス仕様。ランタイム層が設定読込・検証、Lock 取得(stale 回復含む)、組込 HTTP サーバ起動、ポーリングスケジューラ開始を行い、デーモン稼働状態を「起動中」から「稼働中」へ遷移させる。

## イベント処理仕様

### 起動シーケンス(デーモン起動ハンドラ)

- **トリガー**: serve サブコマンドによる起動指示(tier-ops-cli から)
- **入力チャネル**: なし(プロセス起動)
- **出力チャネル**: なし(以降の収集配信サイクルはポーリングスケジューラが起動)

#### 処理フロー

1. 設定 YAML(`--config` のパス)をゲートウェイ層で読込み、構文・参照整合(Topic↔Subscription↔収集ソース↔認証情報参照)を検証する。検証 NG は終了コード 2 で終了する(デーモンを稼働させない)。
2. Lock 取得を試行する(LR-002):
   - lock ファイルが存在しない → lock を作成し、ロック保持プロセス情報と取得日時を記録する(Lock状態: 取得済)。
   - lock が存在し保持プロセスが生存 → 二重起動と判定し、起動を中断して終了コード 3 で終了する(デーモン稼働状態: 起動中 → 停止済)。稼働中のインスタンスには影響しない(SR-006)。
   - lock が存在し保持プロセスが死亡 → stale lock と判定し、安全に回復して Lock を再取得する(Lock状態: stale → 取得済)。
3. 組込 HTTP サーバを metrics_port で起動し、/metrics・/healthz を公開する(SP-005。エンドポイント仕様は UC「/healthzと/metricsをHTTPで公開する」)。
4. ポーリングスケジューラを開始し、polling_interval ごとにユースケース層の収集配信サイクル(collect→archive→fanout→リトライ/DLQ→retention 削除)を起動する(LR-001)。サイクルの多重起動はしない(前回サイクル完了を待つ)。
5. デーモン稼働状態を「起動中」から「稼働中」へ遷移させ、起動時メッセージ(Lock 取得結果・設定要約・メトリクスポート)を出力する。以降の出力は構造化ログに行う。

#### エラーハンドリング

| エラー種別 | リトライ | DLQ | 説明 |
|-----------|---------|-----|------|
| 設定検証エラー | No | No | 起動前に検出し終了コード 2 で終了。エラー位置 + 原因 + 対処を 1 メッセージで提示する |
| Lock 取得失敗(二重起動) | No | No | 終了コード 3 で終了。stale lock は回復対象のため対象外 |
| 組込 HTTP サーバ起動失敗(ポート使用中等) | No | No | 回復不能エラーとして終了コード 1。原因 + 対処(metrics_port の見直し)を構造化ログに出力 |

## データモデル変更

### Lock(lock ファイル)

| 項目 | 型 | 説明 | 変更種別 |
|------|---|------|---------|
| lock_holder_process_info | string | ロック保持プロセス情報(プロセス生存確認に使用) | 追加(起動時に記録) |
| acquired_at | datetime | 取得日時(stale 判定用) | 追加(起動時に記録) |

### 設定(config.yaml、読込のみ)

| 項目 | 型 | 説明 | 変更種別 |
|------|---|------|---------|
| polling_interval | integer | ポーリング間隔(秒)。スケジューラの周期 | 参照 |
| metrics_port | integer | /metrics・/healthz の公開ポート | 参照 |
| topic_definitions / subscription_definitions / source_definitions / credential_refs | text | 参照整合の検証対象 | 参照 |

## ビジネスルール

- 二重起動防止: デーモンは起動時に Lock を取得し、同じ構成で 2 つ目のデーモンは起動せず終了する。異常終了で残った stale lock からはプロセス生存確認で安全に回復する(条件「二重起動防止」、SR-006、LR-002)。
- 起動前検証: 設定ミスはデーモン起動前に検出する(SR-101 のフィードフォワード)。検証 NG のままデーモンを稼働させない。
- ポーリングスケジューラ: polling_interval ごとにサイクルを起動し、多重起動しない(LR-001)。
- エラー表現: 起動エラーは終了コード(2=設定、3=二重起動、1=実行時)と構造化ログで表現する(CTR-002、ui-design.md 終了コード規約)。

## ティア完了条件（BDD）

```gherkin
Feature: デーモンを起動する - 常駐デーモン

  Scenario: Lock を取得して稼働中へ遷移する
    Given 検証 OK の config.yaml(polling_interval=60、metrics_port=9090)があり lock ファイルが存在しない
    When デーモンの起動シーケンスが実行される
    Then lock ファイルに pid=12345 と取得日時が記録される
    And 組込 HTTP サーバがポート 9090 で起動し、ポーリングスケジューラが 60 秒間隔で開始される

  Scenario: stale lock を回復して起動する
    Given lock ファイルに死亡済みプロセスの pid=99999 が記録されている
    When デーモンの起動シーケンスが実行される
    Then プロセス生存確認で stale と判定され、lock が pid=12346(新インスタンス)で再取得される

  Scenario: 二重起動は終了コード 3 で中断する
    Given pid=12345 の生存プロセスが lock を保持している
    When 2 つ目のデーモンの起動シーケンスが実行される
    Then Lock 取得に失敗し終了コード 3 で終了する
    And 既存デーモンの lock ファイルは変更されない
```

## 共通コンポーネント参照

[common-components.md](../../../_cross-cutting/ux-ui/common-components.md) の以下を利用する。

- [C-08 LockManager](../../../_cross-cutting/ux-ui/common-components.md#c-08-lockmanager) — 起動時の Lock 取得・stale 回復・二重起動検出(終了コード 3)
- [C-09 StructuredLogger](../../../_cross-cutting/ux-ui/common-components.md#c-09-structuredlogger) — daemon_started / config_error イベントの出力
- [C-11 HTTPEndpoint](../../../_cross-cutting/ux-ui/common-components.md#c-11-httpendpoint) — /metrics・/healthz 組込 HTTP サーバの起動
- [C-12 PollingScheduler](../../../_cross-cutting/ux-ui/common-components.md#c-12-pollingscheduler) — polling_interval ごとの収集配信サイクル開始
- [C-14 ConfigLoader](../../../_cross-cutting/ux-ui/common-components.md#c-14-configloader) — 起動時の設定読込・検証(NG は終了コード 2)
- [C-15 RetentionSweeper](../../../_cross-cutting/ux-ui/common-components.md#c-15-retentionsweeper) — サイクル内 retention 削除ステップとしての組み込み
