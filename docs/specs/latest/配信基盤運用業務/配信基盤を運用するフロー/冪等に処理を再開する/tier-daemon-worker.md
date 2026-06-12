# 冪等に処理を再開する - 常駐デーモン仕様

## 変更概要

再起動・処理中断後の再開を冪等にするデーモン仕様。専用の「再開モード」は持たず、通常の収集配信サイクルが Manifest と処理済み管理を参照することで、中断時点から二重配信・重複収集なく処理を継続する(SR-003、LP-101)。

## イベント処理仕様

### 再開サイクル(再起動後の収集配信サイクル)

- **トリガー**: 再起動後のポーリングスケジューラによる最初のサイクル起動(以降の通常サイクルと同一処理)
- **入力チャネル**: Manifest(message_id 別の配送状態)、処理済み管理、archive/{topic}/、収集ソース
- **出力チャネル**: Subscription 配置先ディレクトリ、Manifest(delivered / failed 記録)

#### 処理フロー

1. Manifest を読み、中断時点のメッセージ配送状態(収集済 / Archive保存済 / 配信中 / failed)を把握する。配送状態の正は常に Manifest とする(CTR-003)。
2. メッセージ単位に処理を進める(LP-101: メッセージ単位のトランザクション境界):
   - Archive 未保存のメッセージ → Archive 保存から再開する(LR-101: Archive 保存完了前の配信禁止)。
   - Archive保存済・配信中・failed のメッセージ → ドメイン層の二重配信防止判定で「delivered 記録がない Subscription」のみを配信対象として抽出する。
3. 抽出した未配信 Subscription のみへ AtomicWrite(一時名 → rename)で配置し、成功した Subscription を Manifest に delivered として記録する(SR-001、SR-003)。
4. delivered 記録済みの Subscription へは重複配置しない(冪等)。全 Subscription が delivered のメッセージはスキップする。
5. 収集処理では、copy 設定の収集ソースについて処理済み管理と照合し、処理済みの元ファイルは再収集しない(SP-004)。回収(GET 後 DELETE)設定では元ファイルが収集ソースに存在しないため照合不要。
6. 再開時の配信イベントも通常どおり構造化ログ(message_id / topic / subscription / event_type)に出力する(CTP-001)。

#### エラーハンドリング

| エラー種別 | リトライ | DLQ | 説明 |
|-----------|---------|-----|------|
| 再開後の配置失敗(一時的) | Yes | No | Manifest に failed を記録し、リトライ処理(UC「配信失敗をリトライしDLQへ隔離する」)へ。リトライ回数は Manifest に記録 |
| リトライ上限超過 | No | Yes | DLQ へ隔離し Manifest に dlq を記録(同 UC の責務) |
| Manifest 読み書き失敗 | No | No | 実行時エラー。配送状態が確認できないメッセージへの配信は保留し、原因 + 対処を構造化ログに出力(二重配信より保留を優先) |

## データモデル変更

### Manifest(message_id 別 JSON、参照 + 更新)

| 項目 | 型 | 説明 | 変更種別 |
|------|---|------|---------|
| message_id | string | 冪等処理の単位キー | 参照 |
| subscription_delivery_status | text | Subscription 別配送状態(delivered / failed / dlq)。再開時の配信対象判定の根拠 | 参照 + 更新(delivered 記録) |
| retry_count | integer | リトライ回数 | 参照 |
| delivered_at | datetime | 配送日時 | 更新 |

### 処理済み管理(参照)

| 項目 | 型 | 説明 | 変更種別 |
|------|---|------|---------|
| source_file_identifier | string | 収集元ファイル識別子(ファイル名・収集元パス等)。copy 設定時の再収集判定キー | 参照 |
| processed_at | datetime | 処理済み判定日時 | 参照 |

## ビジネスルール

- 二重配信防止: 再開では Manifest の配送状態を参照し、未配信の Subscription にのみ配信する。配信済みの Subscription へは重複配置しない(条件「二重配信防止」、SR-003)。
- メッセージ単位の冪等性: 1 メッセージの処理進行と Manifest 更新を単位とし、どこで中断してもメッセージ単位で冪等に再開できる(LP-101)。
- Archive 保存必須の順序維持: 再開時も collect→archive→fanout の順序を崩さない(LR-101)。
- 重複収集防止: copy 設定では処理済み管理と照合し、処理済みファイルを再収集しない(条件「元ファイル処理判定」)。
- 履歴の維持: 再開によって Manifest の既存履歴を消さない。配送記録は追記され監査・追跡に使える(CTR-003、SR-002)。
- 復旧方式: 単一インスタンス・非冗長の前提で、障害復旧は「再起動による冪等再開 + Archive からの Replay」で行う(CTP-006、RTO は再起動 + 追いつき配信で数時間以内)。

## ティア完了条件（BDD）

```gherkin
Feature: 冪等に処理を再開する - 常駐デーモン

  Scenario: delivered 済み Subscription を除外して配信する
    Given Manifest に message_id 「20260612T093001_orders_sales.csv」 の current=delivered、next=failed が記録されている
    When 再開サイクルが二重配信防止判定を実行する
    Then 配信対象は 「next」 のみと判定される
    And 「next」 への AtomicWrite 成功後に Manifest の next が delivered に更新される

  Scenario: Archive 未保存のメッセージは Archive 保存から再開する
    Given message_id 「20260612T094500_orders_stock.csv」 が収集済のまま Archive 未保存で中断している
    When 再開サイクルが実行される
    Then archive/orders/ への保存が完了してから Fan-out が開始される

  Scenario: 全 Subscription 配信済みのメッセージはスキップする
    Given Manifest に message_id 「20260612T093001_orders_sales.csv」 の current=delivered、next=delivered が記録されている
    When 再開サイクルが実行される
    Then どの Subscription へも再配置されず、Manifest も変更されない
```

## 共通コンポーネント参照

[common-components.md](../../../_cross-cutting/ux-ui/common-components.md) の以下を利用する(専用再開モードはなく通常サイクルの共通部品で冪等性を実現する)。

- [C-01 SourceConnector](../../../_cross-cutting/ux-ui/common-components.md#c-01-sourceconnector) — 再開後の通常収集サイクルでの収集
- [C-02 AtomicWriter](../../../_cross-cutting/ux-ui/common-components.md#c-02-atomicwriter) — 未配信 Subscription への再配置(一時名残留からの安全なやり直し)
- [C-03 ManifestStore](../../../_cross-cutting/ux-ui/common-components.md#c-03-manifeststore) — 中断時点の配送状態の参照と delivered 記録(配送状態の正)
- [C-05 DeliveryStateMachine](../../../_cross-cutting/ux-ui/common-components.md#c-05-deliverystatemachine) — delivered 済み Subscription を除外する二重配信防止(冪等)判定
- [C-07 ProcessedStore](../../../_cross-cutting/ux-ui/common-components.md#c-07-processedstore) — copy 設定時の処理済み照合(重複収集防止)
- [C-09 StructuredLogger](../../../_cross-cutting/ux-ui/common-components.md#c-09-structuredlogger) — 再開時の配信イベント出力
- [C-12 PollingScheduler](../../../_cross-cutting/ux-ui/common-components.md#c-12-pollingscheduler) — 再起動後の最初のサイクル起動
