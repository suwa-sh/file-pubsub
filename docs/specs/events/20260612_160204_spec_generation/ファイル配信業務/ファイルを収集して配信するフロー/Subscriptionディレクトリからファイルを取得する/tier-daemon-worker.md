# Subscriptionディレクトリからファイルを取得する - 常駐デーモン仕様

## 変更概要

この UC の操作主体は Consumer システムであり、本ティアに新規の処理実装はない。本ティアの責務は、Consumer から見た **Subscription ディレクトリ契約の配置保証**(正式名ファイルは常に完全 / 配送独立 / Consumer の取得・削除に非干渉)を、Fan-out / Replay の配置実装(別 UC)を通じて維持することに限定される。

## イベント処理仕様

### 配置保証(取得側ハンドラなし)

- **トリガー**: なし(Consumer の取得は本システムのイベントではない。本システムは Subscription ディレクトリを監視・走査しない)
- **入力チャネル**: なし
- **出力チャネル**: 各 Subscription の配置先ディレクトリ(Fan-out / Replay の配置は別 UC の責務)
- **AsyncAPI**: 該当なし(メッセージキューは使用しない)

#### 処理フロー(保証事項の整理)

1. 配置は必ず AtomicWrite(一時名 `*.tmp` → 正式名 rename)で行う(SR-001、LR-301)。Consumer が正式名で観測するファイルは常に完全な内容である
2. Subscription ディレクトリ内のファイルに対して、本システムは配置(PUT)以外の操作をしない。Consumer による取得・削除を妨げず、削除を検知して配送状態を変えることもしない(配送状態の正は Manifest — CTR-003)
3. Subscription ごとに配送は独立しており、ある Subscription での取得・削除・滞留が他 Subscription の配置に影響しない(SP-002)
4. 再送(Replay)による再配置も同じ契約(AtomicWrite・宛先 Subscription 限定)で行われる(SP-102。実装は別 UC)

#### エラーハンドリング

| エラー種別 | リトライ | DLQ | 説明 |
|-----------|---------|-----|------|
| Consumer の取得失敗(本システム外) | -(Consumer 責任) | No | 配置済みファイルは Consumer が再取得するまで残る。本システムは関与しない |
| Consumer が一時名ファイルを誤取得 | -(契約違反) | No | 契約上、一時名(`*.tmp`)は取得対象外。README / 契約文書で案内する |

## データモデル変更

RDB は使用しない。この UC による新規レイアウトはない(参照のみ)。

### {subscription.directory}/{original_file_name}(既存・Consumer から見た外部 IF)

| 項目 | 型 | 説明 | 変更種別 |
|--------|---|------|---------|
| ファイル実体 | binary(pass-through) | Consumer が従来手段で GET / DELETE する配信ファイル。正式名は常に完全な内容 | 変更なし(契約定義) |

## ビジネスルール

- 正式名のファイルは常に完全な内容(AtomicWrite 配置。一時名は取得対象外)(条件「AtomicWrite配置」)
- Subscription ごとに配送は独立し、一方の取得・削除は他方に影響しない(条件「全Subscription同報配信」)
- 取り込みタイミング(即時 / 夜間バッチ)・取り込み順序の制御は Consumer 側の責任(バリエーション「Consumer取り込みタイミング」、条件「Fan-out処理順序」)
- ファイル内容は pass-through(変換・解釈なし)。ファイル名は元ファイル名のまま(CTP-004)
- Consumer の取得・削除は Manifest の配送状態に影響しない(配送状態の正は Manifest — CTR-003)

## ティア完了条件（BDD）

```gherkin
Feature: Subscriptionディレクトリからファイルを取得する - 常駐デーモン(配置保証)

  Scenario: 正式名ファイルは常に完全な内容である
    Given Fan-out が /pub/orders/current へ「orders_20260612.csv」を AtomicWrite で配置した
    When 配置完了後の任意の時点で正式名「orders_20260612.csv」を読み取る
    Then ファイル内容は Archive の同 message_id のファイルと完全に一致する

  Scenario: Consumer の削除が他 Subscription と配送状態に影響しない
    Given /pub/orders/current と /pub/orders/next に「orders_20260612.csv」が配置済みで Manifest は current=delivered, next=delivered である
    When /pub/orders/current の「orders_20260612.csv」が Consumer により削除される
    Then /pub/orders/next の「orders_20260612.csv」は変化しない
    And Manifest の配送状態は current=delivered のまま変わらない

  Scenario: 書き込み中は一時名のみが存在する
    Given Fan-out が /pub/orders/current へ配置を開始した直後である
    When 配置完了(rename)前に /pub/orders/current のファイル一覧を取得する
    Then 「orders_20260613.csv.tmp」のみが存在し、正式名「orders_20260613.csv」は存在しない
```

## 共通コンポーネント参照

[common-components.md](../../../_cross-cutting/ux-ui/common-components.md) の以下を利用する(本 UC は契約定義であり、実装は配置側 UC が担う)。

- [C-02 AtomicWriter](../../../_cross-cutting/ux-ui/common-components.md#c-02-atomicwriter) — 「正式名ファイルは常に完全な内容」という配置保証契約の実装根拠
- [C-03 ManifestStore](../../../_cross-cutting/ux-ui/common-components.md#c-03-manifeststore) — 「Consumer の取得・削除は配送状態に影響しない」契約の根拠(配送状態の正は Manifest)
