# Archiveに保存する - 常駐デーモン仕様

## 変更概要

ポーリングサイクルの collect→archive→fanout 順序固定のうち archive 工程を実装する。収集済みファイルを `archive/{topic}/{message_id}` へ AtomicWrite で保存し、保持期限(保存日時 + retention)を付与し、Manifest の配送状態を「Archive保存済」へ更新する。Archive 保存の完了確認までは Fan-out を開始しない(SP-001、LR-101)。Archive への収集時即時保存を実質バックアップとする(RPO: データ損失なし)。

## イベント処理仕様

### Archive 保存ハンドラ(archive)

- **トリガー**: ポーリングサイクル内の collect 工程完了後(配送状態が「収集済」のメッセージごと)
- **入力チャネル**: ローカルファイルシステム `work/collect/{topic}/{message_id}`(収集済ファイル)
- **出力チャネル**: `archive/{topic}/{message_id}`(Archive ファイル)、`manifest/{message_id}.json`(状態更新)
- **AsyncAPI**: 該当なし(メッセージキューは使用しない)

#### 処理フロー

1. 配送状態が「収集済」のメッセージを Manifest から解決する
2. domain で保存先パス(`archive/{topic}/{message_id}`)と保持期限(保存日時 + 設定 `archive_retention` 日)を算出する
3. 収集済みファイルを一時名で archive/ へ書き込み、完了後に正式名へ rename する(AtomicWrite — LR-301)
4. Manifest を AtomicWrite で更新する(status=Archive保存済、保存日時、保持期限)
5. メッセージ配送状態を「収集済」→「Archive保存済」、Archiveファイル保持状態を「保持中」とする
6. Manifest 更新の完了後、`work/collect/{topic}/{message_id}` の収集済みファイルを削除する(以降は Archive が正。work 領域に滞留させない)
7. 構造化ログ(event_type=Archive保存、message_id、topic)を出力し、Topic 別メトリクス(処理件数)を更新する
8. 保存完了の確認をもって当該メッセージの Fan-out 工程を許可する

#### エラーハンドリング

| エラー種別 | リトライ | DLQ | 説明 |
|-----------|---------|-----|------|
| Archive 書き込み失敗(ディスク不足・権限エラー) | Yes(次ポーリングサイクルで再試行) | No | 配送状態を「収集済」のまま保持し Fan-out を開始しない(Archive保存必須)。原因 + 対処を構造化ログに出力 |
| 書き込み中の異常終了 | Yes(再起動後の冪等再開) | No | 一時名ファイルのみ残るため正式名の不完全ファイルは生じない。再実行で AtomicWrite をやり直す |
| Manifest 更新失敗 | Yes(同サイクル内再試行、失敗時は次サイクル) | No | Archive 実体が存在しても Manifest が「収集済」なら再実行で同一パスへ冪等に上書き保存する |

## データモデル変更

RDB は使用しない。ローカルファイルシステムのレイアウト(新規/更新):

### archive/{topic}/{message_id}(新規)

| 項目 | 型 | 説明 | 変更種別 |
|--------|---|------|---------|
| ファイル実体 | binary(pass-through) | 収集ファイルの完全な複製。message_id をファイル名に用い同名再出力でも上書きしない | 追加 |

### manifest/{message_id}.json(更新)

| フィールド | 型 | 説明 | 変更種別 |
|--------|---|------|---------|
| status | string | 「収集済」→「Archive保存済」 | 変更 |
| archive_path | string | 保存先パス(Topic別)。archive/{topic}/{message_id} | 追加 |
| saved_at | datetime(ISO 8601) | 保存日時 | 追加 |
| retention_deadline | datetime(ISO 8601) | 保持期限(保存日時 + retention 日数) | 追加 |

### work/collect/{topic}/{message_id}(削除)

| 項目 | 型 | 説明 | 変更種別 |
|--------|---|------|---------|
| ファイル実体 | binary(pass-through) | Archive 保存 + Manifest 更新の完了後に削除する(以降の配信元は Archive) | 削除 |

## ビジネスルール

- Archive 保存必須: 配信(Fan-out)前に必ず Topic 別へ保存し、保存完了まで配信を開始しない(SP-001、LR-101)
- message_id 採番: 保存パスに message_id を含め、同名ファイルの再出力を上書きしない(SR-002)
- AtomicWrite: 一時名で書き込み後 rename し、正式名のファイルは常に完全な内容とする(LR-301)
- 保持期限: 保存日時 + retention(設定 `archive_retention`)。期限超過分の削除は別 UC「保持期間超過のArchiveを削除する」の責務(SP-006)
- 処理単位はメッセージ単位とし、中断しても冪等に再開できる(LP-101)

## ティア完了条件（BDD）

```gherkin
Feature: Archiveに保存する - 常駐デーモン

  Scenario: AtomicWrite で Topic 別に保存し Manifest を更新する
    Given message_id「20260612T093001_orders_orders_20260612.csv」が work/collect/orders/ に存在し配送状態が「収集済」である
    And 設定の archive_retention が 90 日である
    When archive 工程が実行される
    Then archive/orders/20260612T093001_orders_orders_20260612.csv が正式名で存在する
    And manifest/20260612T093001_orders_orders_20260612.csv.json の status が「Archive保存済」、retention_deadline が保存日時 + 90 日になる

  Scenario: 保存失敗時は Fan-out を抑止する
    Given archive/ への書き込みが権限エラーで失敗する
    When archive 工程が実行される
    Then 配送状態は「収集済」のまま変化せず、当該メッセージの Fan-out は開始されない
    And event_type=Archive保存失敗 の構造化ログに message_id と原因・対処が含まれる

  Scenario: 再起動後に冪等に保存をやり直す
    Given archive/orders/ に一時名「20260612T093001_orders_orders_20260612.csv.tmp」だけが残っている(前回異常終了)
    When デーモン再起動後の archive 工程が実行される
    Then 正式名ファイルが AtomicWrite で作成され、一時名ファイルに由来する不完全な正式名ファイルは存在しない
```

## 共通コンポーネント参照

[common-components.md](../../../_cross-cutting/ux-ui/common-components.md) の以下を利用する。

- [C-02 AtomicWriter](../../../_cross-cutting/ux-ui/common-components.md#c-02-atomicwriter) — archive/ への保存と Manifest 更新の AtomicWrite
- [C-03 ManifestStore](../../../_cross-cutting/ux-ui/common-components.md#c-03-manifeststore) — Archive保存済への状態更新・保存日時・保持期限の記録
- [C-05 DeliveryStateMachine](../../../_cross-cutting/ux-ui/common-components.md#c-05-deliverystatemachine) — 収集済→Archive保存済の状態遷移
- [C-09 StructuredLogger](../../../_cross-cutting/ux-ui/common-components.md#c-09-structuredlogger) — archived / Archive保存失敗イベントの出力
- [C-10 MetricsRegistry](../../../_cross-cutting/ux-ui/common-components.md#c-10-metricsregistry) — Topic 別処理件数の更新
- [C-14 ConfigLoader](../../../_cross-cutting/ux-ui/common-components.md#c-14-configloader) — archive_retention(保持期間)の参照
- [C-15 RetentionSweeper](../../../_cross-cutting/ux-ui/common-components.md#c-15-retentionsweeper) — 保持期限(retention_deadline)算出の domain 純粋関数を共有
