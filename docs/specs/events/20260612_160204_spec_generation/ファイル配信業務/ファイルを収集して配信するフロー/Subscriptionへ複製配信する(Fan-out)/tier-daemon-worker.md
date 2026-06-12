# Subscriptionへ複製配信する(Fan-out) - 常駐デーモン仕様

## 変更概要

ポーリングサイクルの fanout 工程を実装する。Archive のファイルを Topic 配下の全 Subscription ディレクトリへ AtomicWrite(一時名→rename)で複製配置し、配送結果(delivered / failed)を message_id・topic・Subscription 単位で Manifest に記録する。配置はファイル名昇順で処理し、Subscription ごとに配送を独立させ、Manifest 参照の冪等判定で二重配信を防ぐ。

## イベント処理仕様

### Fan-out 配信ハンドラ(fanout)

- **トリガー**: ポーリングサイクル内の archive 工程完了後(配送状態が「Archive保存済」または未配信 Subscription が残るメッセージごと)
- **入力チャネル**: ローカルファイルシステム `archive/{topic}/{message_id}`(Archive ファイル)、`manifest/{message_id}.json`(配送状態)
- **出力チャネル**: 各 Subscription の配置先ディレクトリ(設定 `topics[].subscriptions[].directory`)、`manifest/{message_id}.json`(配送結果)
- **AsyncAPI**: 該当なし(メッセージキューは使用しない)

#### 処理フロー

1. 配信対象メッセージ(status=Archive保存済、または failed 等で未配信 Subscription が残るもの)をファイル名昇順で解決する(SR-005)
2. domain の二重配信防止判定で、Manifest の Subscription 別配送状態から delivered 済みを除外し未配信 Subscription 一覧を得る(SR-003)
3. メッセージ配送状態を「配信中」とし Manifest を AtomicWrite で更新する
4. 未配信 Subscription ごとに独立して配置を実行する(SP-002。一方の失敗は他方に影響しない):
   - Archive ファイルを配置先ディレクトリへ一時名(`{元ファイル名}.tmp`)で書き込む
   - 完了後に正式名(`{元ファイル名}`)へ rename する(SR-001)
   - 成功: Manifest に delivered + 配送日時を記録し、状態を「配信済(delivered)」とする
   - 失敗: Manifest に failed を記録し、状態を「配信失敗(failed)」とする(リトライ・DLQ は別 UC「配信失敗をリトライしDLQへ隔離する」)
5. 構造化ログ(event_type=配信、message_id、topic、subscription)を出力し、Topic 別メトリクス(処理件数・配信失敗数)を更新する

#### エラーハンドリング

| エラー種別 | リトライ | DLQ | 説明 |
|-----------|---------|-----|------|
| Subscription ディレクトリへの配置失敗(権限エラー・ディスク不足・パス不存在) | Yes(別 UC のリトライ処理に委譲) | Yes(リトライ上限超過時、別 UC で隔離) | Manifest に failed を記録し、message_id + topic + subscription を含む構造化ログを出力。他 Subscription への配送は継続 |
| 配置中の異常終了(一時名残留) | Yes(再起動後の冪等再開) | No | 正式名の不完全ファイルは生じない(AtomicWrite)。再開時に Manifest 参照で未配信分のみ再配置 |
| Manifest 更新失敗 | Yes(同サイクル内再試行、失敗時は次サイクル) | No | 記録の正は Manifest(CTR-003)。更新成功まで当該 Subscription は未配信扱いとし、再配置は同一内容の上書きで冪等 |

## データモデル変更

RDB は使用しない。ローカルファイルシステムのレイアウト(新規/更新):

### {subscription.directory}/{元ファイル名}(新規)

| 項目 | 型 | 説明 | 変更種別 |
|--------|---|------|---------|
| ファイル実体 | binary(pass-through) | Archive ファイルの同一内容の複製。一時名(`.tmp` 付与)で書き込み後 rename。正式名は常に完全な内容 | 追加 |

### manifest/{message_id}.json(更新)

| フィールド | 型 | 説明 | 変更種別 |
|--------|---|------|---------|
| status | string | 「Archive保存済」→「配信中」→「配信済(delivered)」/「配信失敗(failed)」 | 変更 |
| subscription_delivery_status | object(Subscription 名 → delivered / failed / dlq) | Subscription 別配送状態。配送イベント追記 + Subscription 別現在状態 | 追加/変更 |
| delivered_at | datetime(ISO 8601) | 配送日時(Subscription 別) | 追加 |

## ビジネスルール

- 全 Subscription 同報配信: Topic の全 Subscription へ同一内容を複製。配送は Subscription ごとに独立(SP-002)
- AtomicWrite 配置: 一時名→rename。正式名ファイルは常に完全(SR-001、LR-301)
- 二重配信防止: Manifest の配送状態を参照し未配信 Subscription にのみ配信(SR-003、LP-101)
- Fan-out 処理順序: ファイル名昇順。順序保証はせず、取り込み順序の制御は Consumer 責任(SR-005)
- 全配送操作は message_id・topic・Subscription 単位で Manifest に記録。配送状態の正は常に Manifest(CTR-003)
- 配信失敗時の自動リトライ・DLQ 隔離は別 UC「配信失敗をリトライしDLQへ隔離する」の責務(failed 記録までが本 UC)

## ティア完了条件（BDD）

```gherkin
Feature: Subscriptionへ複製配信する(Fan-out) - 常駐デーモン

  Scenario: AtomicWrite で全 Subscription に配置し Manifest に記録する
    Given archive/orders/20260612T093001_orders_orders_20260612.csv が存在し Manifest の status が「Archive保存済」である
    And topic「orders」に subscription「current」「next」が定義されている
    When fanout 工程が実行される
    Then 両 Subscription ディレクトリに正式名「orders_20260612.csv」が配置され、一時名「orders_20260612.csv.tmp」は残らない
    And Manifest の subscription_delivery_status が current=delivered, next=delivered になる

  Scenario: Manifest 参照で未配信 Subscription のみに配信する
    Given Manifest の subscription_delivery_status が current=delivered, next=failed である
    When fanout 工程(再開)が実行される
    Then next にのみ配置が実行され current のファイルは再配置されない

  Scenario: 配置失敗を failed として記録し他 Subscription へ継続する
    Given subscription「next」の配置先 /pub/orders/next への書き込みが権限エラーになる
    When fanout 工程が実行される
    Then Manifest に next=failed が記録され、current への配置と delivered 記録は正常に完了する
    And 構造化ログに message_id・topic=orders・subscription=next・原因 + 対処が出力される
```

## 共通コンポーネント参照

[common-components.md](../../../_cross-cutting/ux-ui/common-components.md) の以下を利用する。

- [C-02 AtomicWriter](../../../_cross-cutting/ux-ui/common-components.md#c-02-atomicwriter) — Subscription ディレクトリへの一時名→rename 配置
- [C-03 ManifestStore](../../../_cross-cutting/ux-ui/common-components.md#c-03-manifeststore) — Subscription 別 delivered / failed の配送結果記録
- [C-05 DeliveryStateMachine](../../../_cross-cutting/ux-ui/common-components.md#c-05-deliverystatemachine) — 未配信 Subscription 抽出(二重配信防止)と配信中→delivered / failed 遷移
- [C-09 StructuredLogger](../../../_cross-cutting/ux-ui/common-components.md#c-09-structuredlogger) — delivered / delivery_failed イベント(message_id + topic + subscription)の出力
- [C-10 MetricsRegistry](../../../_cross-cutting/ux-ui/common-components.md#c-10-metricsregistry) — Topic 別処理件数・配信失敗数の更新
- [C-14 ConfigLoader](../../../_cross-cutting/ux-ui/common-components.md#c-14-configloader) — Subscription 配置先ディレクトリ定義の参照
