# デーモンをgraceful shutdownで停止する - 常駐デーモン仕様

## 変更概要

停止シグナル受信から終了までの graceful shutdown 仕様。ランタイム層のシグナルハンドリング(LP-001)が新規サイクル起動を止め、処理中メッセージの完了を待ち、Lock を解放して終了コード 0 で終了する。中途半端な状態(一時名ファイルの残留、Manifest 未記録)を残さない。

## イベント処理仕様

### 停止シグナルハンドラ

- **トリガー**: 停止シグナル受信(SIGTERM 等のプロセス停止シグナル)
- **入力チャネル**: OS シグナル
- **出力チャネル**: Manifest(処理中メッセージの配送結果記録)、lock ファイル(解放)

#### 処理フロー

1. 停止シグナルを受信したら、デーモン稼働状態を「稼働中」から「停止処理中」へ遷移させる。
2. 新規ポーリングサイクルの起動を停止する(実行中のサイクルへの割込みはしない)。
3. 処理中のメッセージの完了を待つ(SR-007):
   - 配置中のファイルは AtomicWrite(一時名 → rename)を完了させる。
   - 配送結果(delivered / failed)を Manifest に記録する。配置に失敗した場合も failed を記録してから停止し、未記録状態を残さない。
4. 組込 HTTP サーバ(/metrics・/healthz)を停止する(以降、監視基盤の死活監視は DOWN を検知する)。
5. Lock を解放し(lock ファイル削除。Lock状態: 取得済 → 解放済)、デーモン稼働状態を「停止済」へ遷移させて終了コード 0 で終了する。
6. 停止イベントを構造化ログ(event_type=停止)に出力する。

#### エラーハンドリング

| エラー種別 | リトライ | DLQ | 説明 |
|-----------|---------|-----|------|
| 停止処理中の配置失敗 | No(停止を優先) | No | Manifest に failed を記録してから停止する。リトライは再起動後のサイクル(UC「配信失敗をリトライしDLQへ隔離する」)に委ねる |
| Lock 解放失敗 | No | No | 実行時エラーとして構造化ログに原因 + 対処を出力。残留 lock は次回起動の stale 回復で安全に処理される(SR-006) |
| SIGKILL 等による強制終了 | - | - | graceful shutdown は実行されず stale lock が残る。次回起動の stale 回復 + 冪等再開で復旧する(本ハンドラの対象外) |

## データモデル変更

### Lock(lock ファイル)

| 項目 | 型 | 説明 | 変更種別 |
|------|---|------|---------|
| lock ファイル自体 | file | graceful shutdown 完了時に削除(解放)し、次回起動が正常に行えるようにする | 削除 |

### Manifest(処理中メッセージの記録)

| 項目 | 型 | 説明 | 変更種別 |
|------|---|------|---------|
| subscription_delivery_status | text | 停止前に処理中メッセージの配送結果(delivered / failed)を記録 | 更新 |
| delivered_at | datetime | 配置成功時の配送日時 | 更新 |

## ビジネスルール

- graceful shutdown: 停止シグナルを受けたら新規処理を止め、処理中のメッセージを完了してから停止する。中途半端な状態を残さない(条件「graceful shutdown」、SR-007)。
- 計画停止の許容: graceful shutdown により計画停止ありの 24 時間運用を支える(NFR A.1.1.1 Lv4、A.1.1.3 Lv1)。
- Lock 解放と稼働状態の連動: Lock の解放はデーモン稼働状態の遷移(停止処理中→停止済)と連動する(情報「Lock」)。
- 正常終了の表現: graceful shutdown の完了は終了コード 0。配信失敗のリトライ・DLQ 隔離はデーモン内で自己完結する正常動作であり終了コードに影響しない(ui-design.md 終了コード規約)。

## ティア完了条件（BDD）

```gherkin
Feature: デーモンをgraceful shutdownで停止する - 常駐デーモン

  Scenario: SIGTERM で処理中メッセージ完了後に停止する
    Given デーモン(pid=12345)が message_id 「20260612T093001_orders_sales.csv」 を Subscription 「next」 へ配置中である
    When SIGTERM を受信する
    Then 配置の AtomicWrite が完了し Manifest に next=delivered が記録される
    And lock ファイルが削除され、終了コード 0 で終了する

  Scenario: 停止処理中は新規サイクルを起動しない
    Given デーモンが停止処理中(デーモン稼働状態: 停止処理中)である
    When ポーリング間隔 60 秒が経過する
    Then 新規の収集配信サイクルは起動されない

  Scenario: HTTP サーバ停止により監視基盤が DOWN を検知できる
    Given デーモンが /healthz をポート 9090 で公開して稼働している
    When SIGTERM を受信して graceful shutdown が完了する
    Then GET /healthz への接続が失敗するようになる(外部監視基盤が死活 DOWN を検知する前提を満たす)
```

## 共通コンポーネント参照

[common-components.md](../../../_cross-cutting/ux-ui/common-components.md) の以下を利用する。

- [C-02 AtomicWriter](../../../_cross-cutting/ux-ui/common-components.md#c-02-atomicwriter) — 処理中配置の AtomicWrite 完了(一時名残留を残さない)
- [C-03 ManifestStore](../../../_cross-cutting/ux-ui/common-components.md#c-03-manifeststore) — 停止前の処理中メッセージの配送結果(delivered / failed)記録
- [C-05 DeliveryStateMachine](../../../_cross-cutting/ux-ui/common-components.md#c-05-deliverystatemachine) — 停止前の delivered / failed の確定遷移
- [C-08 LockManager](../../../_cross-cutting/ux-ui/common-components.md#c-08-lockmanager) — Lock の解放(lock ファイル削除)
- [C-09 StructuredLogger](../../../_cross-cutting/ux-ui/common-components.md#c-09-structuredlogger) — daemon_stopped イベントの出力
- [C-11 HTTPEndpoint](../../../_cross-cutting/ux-ui/common-components.md#c-11-httpendpoint) — 組込 HTTP サーバの停止(監視基盤の DOWN 検知前提)
- [C-12 PollingScheduler](../../../_cross-cutting/ux-ui/common-components.md#c-12-pollingscheduler) — 新規サイクル起動の停止・実行中サイクルの完了待ち
