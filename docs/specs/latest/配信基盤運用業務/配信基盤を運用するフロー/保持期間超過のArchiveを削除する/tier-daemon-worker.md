# 保持期間超過のArchiveを削除する - 常駐デーモン仕様

## 変更概要

ポーリングサイクル内の retention 削除ステップの仕様。設定 YAML の archive_retention(日数)に基づき、保持期限を超過した Archive ファイルだけを Topic 別に安全に削除する。期限内のファイル・Manifest の配送履歴には触れない。

## イベント処理仕様

### retention 処理(Archive 削除ハンドラ)

- **トリガー**: ポーリングサイクル(collect→archive→fanout→リトライ/DLQ→retention 削除)の retention 削除ステップ
- **入力チャネル**: config.yaml(archive_retention)、archive/{topic}/ のファイル一覧
- **出力チャネル**: archive/{topic}/(期限超過ファイルの削除)、構造化ログ

#### 処理フロー

1. 設定の archive_retention(日数)を参照する(情報「設定」)。
2. Topic 別に archive/{topic}/ 配下の Archive ファイル一覧と保存日時を取得する。
3. ドメイン層の保持期限判定(純粋ロジック)で、保存日時 + archive_retention(日)を現在時刻が超過しているファイルを削除対象と判定する。
4. 削除対象のファイルだけを削除する(SP-006 の「安全に削除」):
   - 期限内のファイルは削除しない(再送・監査に必要な期間のデータを確実に残す)。
   - 削除は Archive ファイル本体のみ。Manifest の配送履歴は削除せず、監査・追跡可能性を維持する(CTR-003。Manifest 保管は 90 日目安、CTP-001)。
5. 削除イベントを構造化ログ(topic / event_type=retention 削除)に出力する(Archiveファイル保持状態: 保持中 → 削除済)。

#### エラーハンドリング

| エラー種別 | リトライ | DLQ | 説明 |
|-----------|---------|-----|------|
| 個別ファイルの削除失敗(権限・I/O) | Yes(次サイクルで再試行) | No | 構造化ログに原因 + 対処を出力し、残りのファイルの処理を継続する。デーモンは停止しない |
| archive/ ディレクトリ走査失敗 | Yes(次サイクルで再試行) | No | 当該サイクルの retention 処理をスキップし、実行時エラーとして構造化ログに記録する |

## データモデル変更

### Archiveファイル(archive/{topic}/、削除)

| 項目 | 型 | 説明 | 変更種別 |
|------|---|------|---------|
| archive_path | string | 保存先パス(Topic 別)。削除対象の特定キー | 参照 |
| saved_at | datetime | 保存日時。保持期限判定の入力 | 参照 |
| retention_deadline | datetime | 保持期限(保存日時 + archive_retention) | 参照(判定) |
| ファイル本体 | file | 期限超過分のみ削除 | 削除 |

### 設定(config.yaml、参照のみ)

| 項目 | 型 | 説明 | 変更種別 |
|------|---|------|---------|
| archive_retention | integer | Archive 保持期間(日数)。保持目安 〜90 日・〜数十 GB(SP-006) | 参照 |

## ビジネスルール

- Archive保持期間: retention 処理では保持期間を超過した Archive ファイルだけを安全に削除する(条件「Archive保持期間」、SP-006)。
- 確実な保全: 期限内のデータは確実に残す。再送(Replay)・監査・障害復旧・差分比較の基盤を損なわない(情報「Archiveファイル」)。
- ディスク枯渇防止: 無限に溜めず、長期運用でのディスク枯渇を防ぐ(SP-006)。
- 履歴と実体の分離: 削除するのは Archive ファイル本体のみ。Manifest の配送履歴は保持し続ける(CTR-003)。
- 自己完結する正常動作: retention 削除はデーモン内で自己完結し、serve の終了コードに影響しない(ui-design.md 終了コード規約)。

## ティア完了条件（BDD）

```gherkin
Feature: 保持期間超過のArchiveを削除する - 常駐デーモン

  Scenario: 保持期限判定が期限超過ファイルのみを削除対象にする
    Given archive_retention=90(日)で、archive/orders/ に保存日時 2026-03-01 のファイル A と保存日時 2026-05-01 のファイル B がある
    When 2026-06-12 に retention 処理が実行される
    Then ファイル A(保持期限 2026-05-30 を超過)だけが削除対象と判定され削除される
    And ファイル B(保持期限 2026-07-30)は保持される

  Scenario: 削除後も Manifest の配送履歴は残る
    Given message_id 「20260301T090000_orders_sales_old.csv」 の Archive ファイルが retention 削除された
    When status コマンドで該当 message_id を照会する
    Then Manifest の配送履歴(delivered 記録)は参照できる

  Scenario: 削除失敗は構造化ログに記録され処理が継続する
    Given archive/orders/ の期限超過ファイルが permission denied で削除できない
    When retention 処理が実行される
    Then 構造化ログに topic=orders、event_type=retention 削除失敗、原因 + 対処が出力される
    And 他 Topic の retention 処理と収集・配信サイクルは継続する
```

## 共通コンポーネント参照

[common-components.md](../../../_cross-cutting/ux-ui/common-components.md) の以下を利用する。

- [C-09 StructuredLogger](../../../_cross-cutting/ux-ui/common-components.md#c-09-structuredlogger) — retention_deleted / 削除失敗イベントの出力
- [C-12 PollingScheduler](../../../_cross-cutting/ux-ui/common-components.md#c-12-pollingscheduler) — retention 削除ステップを含むサイクルの周期起動
- [C-14 ConfigLoader](../../../_cross-cutting/ux-ui/common-components.md#c-14-configloader) — archive_retention(保持日数)の参照
- [C-15 RetentionSweeper](../../../_cross-cutting/ux-ui/common-components.md#c-15-retentionsweeper) — 保持期限超過判定と期限超過 Archive の安全削除(本 UC の処理本体)
