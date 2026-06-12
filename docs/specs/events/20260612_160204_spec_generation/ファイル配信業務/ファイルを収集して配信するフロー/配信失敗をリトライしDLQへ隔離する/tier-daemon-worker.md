# 配信失敗をリトライしDLQへ隔離する - 常駐デーモン仕様

## 変更概要

ポーリングサイクルのリトライ / DLQ 工程を実装する。Manifest に failed と記録された配送(message_id × Subscription)を対象に、リトライ上限(設定 `retry_max_count`)以内は Archive から再配置し、上限超過は `dlq/` へ隔離して Manifest に dlq を記録する。エラーのリトライ可否分類は usecase に集約する(LR-102)。恒久的な失敗を滞留させず、運用者の対処判断(再送 / 破棄)に委ねる(SR-004)。

## イベント処理仕様

### リトライ / DLQ ハンドラ(retry-dlq)

- **トリガー**: ポーリングサイクル内の fanout 工程後(Manifest に failed が記録された配送ごと)
- **入力チャネル**: ローカルファイルシステム `manifest/{message_id}.json`(failed 検出・リトライ回数)、`archive/{topic}/{message_id}`(再配置の配信元)
- **出力チャネル**: 各 Subscription の配置先ディレクトリ(再配置)、`dlq/{topic}/{message_id}`(隔離)、`manifest/{message_id}.json`(記録)
- **AsyncAPI**: 該当なし(メッセージキューは使用しない)

#### 処理フロー

1. Manifest から Subscription 別配送状態が failed の配送(message_id × Subscription)を検出する
2. usecase でエラーをリトライ可否分類する(LR-102):一時的(配置先の一時障害等)はリトライ対象、恒久的はリトライ上限の枠組みで DLQ へ収束させる
3. domain のリトライ上限判定: Manifest のリトライ回数と設定 `retry_max_count` を比較する
4. 規定回数以内の場合(状態: 配信失敗→リトライ中→配信中):
   - Archive ファイルを該当 Subscription ディレクトリへ AtomicWrite で再配置する
   - 成功: Manifest に delivered + 配送日時を記録(状態: 配信済)
   - 失敗: Manifest のリトライ回数を +1 し failed を維持(次サイクルで再試行)
5. 規定回数を超過した場合(状態: リトライ中→DLQ隔離):
   - `dlq/{topic}/{message_id}` へ隔離し、隔離理由・失敗回数・隔離日時を記録する
   - Manifest の該当 Subscription を dlq として記録する
   - 以降の自動再試行対象から除外する(滞留させない)
6. 構造化ログ(event_type=リトライ / DLQ隔離、message_id、topic、subscription、error_detail=原因 + 対処)を出力し、Topic 別メトリクス(配信失敗数・DLQ 件数)を更新する

#### エラーハンドリング

| エラー種別 | リトライ | DLQ | 説明 |
|-----------|---------|-----|------|
| 再配置失敗(一時的: 配置先の一時障害) | Yes(リトライ回数 +1、次サイクルで再試行) | Yes(上限超過時) | リトライ規定回数以内は自動回復を試みる |
| 再配置失敗(恒久的: 権限エラー等が継続) | Yes(上限まで) | Yes | 上限超過で dlq/ へ隔離し Manifest に dlq 記録。運用者判断へ |
| dlq/ への隔離書き込み失敗 | Yes(次サイクルで隔離を再試行) | - | Manifest が dlq 未記録のため再試行される。冪等(同一パス上書き)で二重隔離しない |
| Manifest 更新失敗 | Yes(同サイクル内再試行、失敗時は次サイクル) | No | 配送状態の正は Manifest(CTR-003)。記録成功まで処理は完了扱いにしない |

## データモデル変更

RDB は使用しない。ローカルファイルシステムのレイアウト(新規/更新):

### dlq/{topic}/{message_id}(新規)

| 項目 | 型 | 説明 | 変更種別 |
|--------|---|------|---------|
| ファイル実体 | binary(pass-through) | リトライ上限超過メッセージの隔離ファイル(Archive からの複製) | 追加 |

### dlq/{topic}/{message_id}.meta.json(新規)

| フィールド | 型 | 説明 | 変更種別 |
|--------|---|------|---------|
| message_id | string | 隔離メッセージ(message_id) | 追加 |
| isolation_reason | string | 隔離理由(例: permission denied (write)) | 追加 |
| failure_count | integer | 失敗回数 | 追加 |
| isolated_at | datetime(ISO 8601) | 隔離日時 | 追加 |

### manifest/{message_id}.json(更新)

| フィールド | 型 | 説明 | 変更種別 |
|--------|---|------|---------|
| subscription_delivery_status | object | 該当 Subscription を failed → delivered(回復時)/ dlq(上限超過時)へ更新 | 変更 |
| retry_count | integer | リトライ回数(再試行失敗のたびに +1) | 変更 |
| status | string | 「配信失敗(failed)」→「リトライ中」→「配信中」/「DLQ隔離(dlq)」 | 変更 |

## ビジネスルール

- リトライ上限: 規定回数(設定 `retry_max_count`)以内に成功すれば delivered。超過は DLQ へ隔離し Manifest に dlq 記録(SR-004)
- エラーのリトライ可否分類は usecase に集約する(LR-102)。判断は usecase、終了コード・ログ表現は runtime(CLR-001)
- 再配置も AtomicWrite(一時名→rename)で行い、Consumer が不完全ファイルを取得しない(SR-001)
- DLQ 隔離は滞留させないための仕組みであり、デーモンは停止しない。serve の終了コードにも影響しない(ui-design.md 終了コード規約)
- DLQ 隔離メッセージの確認は status コマンド(別 UC「DLQ隔離メッセージを確認する」)、再配信は再送 Replay(別 UC「再送(Replay)を実行する」)の責務
- リトライ・隔離イベントは message_id + topic + subscription を含む 1 行 JSON 構造化ログで出力する(CTP-001)

## ティア完了条件（BDD）

```gherkin
Feature: 配信失敗をリトライしDLQへ隔離する - 常駐デーモン

  Scenario: failed な配送を Archive から再配置して回復する
    Given manifest/20260612T093001_orders_orders_20260612.csv.json の next が failed(retry_count=1)である
    And 配置先 /pub/orders/next が書き込み可能に回復している
    When retry-dlq 工程が実行される
    Then archive/orders/ のファイルが /pub/orders/next へ AtomicWrite で再配置され、Manifest の next が delivered になる

  Scenario: retry_max_count 超過で dlq/ へ隔離する
    Given retry_max_count が 5 で、manifest/20260611T220500_invoices_inv_0042.csv.json の current が failed(retry_count=5)である
    When retry-dlq 工程が実行される
    Then dlq/invoices/20260611T220500_invoices_inv_0042.csv と隔離メタ(隔離理由・失敗回数 5・隔離日時)が作成される
    And Manifest の current が dlq になり、以降の自動再試行対象から除外される

  Scenario: DLQ 件数メトリクスを更新する
    Given topic「invoices」で 1 件のメッセージが DLQ 隔離された
    When /metrics が取得される
    Then topic=invoices の DLQ 件数が 1 増加している
```

## 共通コンポーネント参照

[common-components.md](../../../_cross-cutting/ux-ui/common-components.md) の以下を利用する。

- [C-02 AtomicWriter](../../../_cross-cutting/ux-ui/common-components.md#c-02-atomicwriter) — Archive からの再配置・dlq/ への隔離書き込み
- [C-03 ManifestStore](../../../_cross-cutting/ux-ui/common-components.md#c-03-manifeststore) — failed 検出・retry_count 加算・delivered / dlq 記録
- [C-05 DeliveryStateMachine](../../../_cross-cutting/ux-ui/common-components.md#c-05-deliverystatemachine) — リトライ上限判定と failed→リトライ中→配信中 / dlq の遷移
- [C-09 StructuredLogger](../../../_cross-cutting/ux-ui/common-components.md#c-09-structuredlogger) — retry / dlq_isolated イベント(原因 + 対処)の出力
- [C-10 MetricsRegistry](../../../_cross-cutting/ux-ui/common-components.md#c-10-metricsregistry) — Topic 別配信失敗数・DLQ 件数の更新
- [C-14 ConfigLoader](../../../_cross-cutting/ux-ui/common-components.md#c-14-configloader) — retry_max_count(リトライ上限)の参照
