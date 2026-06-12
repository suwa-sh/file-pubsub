# Subscriptionディレクトリから再送ファイルを取得する - 常駐デーモン仕様（外部 IF 仕様）

## 変更概要

本 UC は Consumer 側の操作であり、tier-daemon-worker に新規の処理実装はない。代わりに、**Consumer から見た再送ファイル取得契約(Subscription ディレクトリ規約)** を外部インターフェース仕様として定義し、システム側(共有 gateway 層のファイルストア)が保証する配置不変条件を明文化する。本契約は ux-design.md「Subscription ディレクトリ規約(Consumer システム向け)」を再送ファイルへ適用したものである。

## 外部インターフェース仕様(再送ファイル受け渡し契約)

### 再送ファイル受け渡し

- **トリガー**: `replay` サブコマンドの実行による宛先 Subscription への再配置完了(前提 UC「再送(Replay)を実行する」)
- **入力チャネル**: なし(デーモンの新規入力はない)
- **出力チャネル**: Subscription ディレクトリ(設定 YAML の `subscriptions[].directory` = 配置先ディレクトリパス)
- **AsyncAPI**: 対象外(メッセージキューではなくファイル IF。チャネル契約は本書で定義)

#### Consumer 向け取得契約

| 項目 | 契約 | 根拠 |
|------|------|------|
| 取得手段 | Consumer は自システム向け Subscription ディレクトリから従来手段(FTP GET 等)でファイルを取得する。Consumer 無改修 | UC: Subscriptionディレクトリから再送ファイルを取得する |
| ファイルの完全性 | 正式名のファイルは常に完全な内容。一時名(`{元ファイル名}.tmp`)で書き込み後に正式名へ rename される。**一時名のファイルは取得対象にしないこと** | 条件: AtomicWrite配置(replay の配置規約) |
| 宛先限定 | 再送ファイルは replay で宛先指定された Subscription ディレクトリにのみ現れる。指定されていない Subscription には配置されない | 条件: Replay記録 |
| 配送の独立性 | Subscription ごとに配送は独立。自分の取得・削除・取り込みタイミングは他 Subscription に影響せず、影響も受けない。並行稼働中でも安全に遡及処理できる | 条件: 全Subscription同報配信(配送独立)、SP-002 |
| ファイル名 | 元ファイル名のまま配置される(内容は pass-through、変換・解釈なし) | CTP-004 |
| 取り込みタイミング | 即時取り込み / 夜間バッチ等、Consumer 側の任意のタイミングでよい | バリエーション: Consumer取り込みタイミング |
| 順序 | メッセージの順序保証はない。取り込み順序の制御は Consumer 側の責任 | 条件: Fan-out処理順序(スコープ境界) |
| 取得後の削除 | Consumer による取得・削除は従来運用のままでよい。削除してもシステムの配送記録(Manifest)は失われない | CTR-003(配送状態の正は Manifest) |
| 同名ファイルの再送 | 再送ファイルが Subscription ディレクトリに残存する同名ファイルと衝突した場合も、rename により完全な内容で上書き配置される。取り込み済み管理は Consumer 側の責任 | 条件: AtomicWrite配置 |

#### 処理フロー(Consumer 視点)

1. 自システム向け Subscription ディレクトリ(例: subscriptions/next/ に対応する配置先)を従来手段で一覧する
2. 一時名(`.tmp`)を除外し、正式名のファイルだけを取得対象とする
3. ファイルを GET し、従来運用に従い取得後削除する
4. 自分のタイミング(即時 / 夜間バッチ)で自システムへ再投入する。取り込み順序は自システム側で制御する

#### エラーハンドリング

| エラー種別 | リトライ | DLQ | 説明 |
|-----------|---------|-----|------|
| Consumer の取得失敗(接続断等) | Yes(Consumer 側) | No | ファイルは Subscription ディレクトリに残るため、Consumer は次回タイミングで再取得すればよい。システム側の対処は不要 |
| 再送ファイルが見つからない | No | No | replay の宛先指定が正しいかを運用者が `status` で確認する(再送履歴は Manifest に記録済み) |

## データモデル変更

なし(本 UC でデーモンが管理するデータに変更はない)。参照されるレイアウトは以下。

### Subscription ディレクトリ(Consumer が読取・削除)

| 項目 | 型 | 説明 | 変更種別 |
|--------|---|------|---------|
| {配置先ディレクトリパス}/{元ファイル名} | ファイル | replay により配置済みの再送ファイル。Consumer が GET・削除する | 変更なし(外部からの読取・削除) |

## ビジネスルール

- システム側の責務は「宛先 Subscription ディレクトリへの完全なファイルの配置保証」まで。取得・削除・再投入・順序制御は Consumer の責務とする(スコープ境界)。
- 再送は他 Subscription の配送に影響しないこと(条件「Replay記録」)。Current / Next 並行稼働の安全性の根拠。
- 正式名ファイルは常に完全な内容であること(条件「AtomicWrite配置」)。Consumer は安定待ち等の追加判定なしに取得してよい。
- Consumer の取得・削除によって Manifest の配送記録は変化しない(CTR-003)。

## ティア完了条件（BDD）

```gherkin
Feature: Subscriptionディレクトリから再送ファイルを取得する - 常駐デーモン(外部 IF)

  Scenario: 配置保証 - 宛先 Subscription にのみ完全なファイルが存在する
    Given replay --topic orders --message-id 20260601T091500_orders_orders_20260601.csv --subscription next が完了している
    When subscriptions の next と current の配置先ディレクトリを比較する
    Then next にのみ正式名 orders_20260601.csv が完全な内容で存在し一時名 .tmp ファイルは存在しない

  Scenario: 配送独立 - Consumer の削除が他 Subscription と Manifest に影響しない
    Given next の配置先ディレクトリに再送ファイル orders_20260601.csv が存在し manifest に Replay 記録がある
    When Consumerシステム(Next) がファイルを取得して削除する
    Then current の配置先ディレクトリと manifest/20260601T091500_orders_orders_20260601.csv.json の Replay 記録は変化しない
```

## 共通コンポーネント参照

[common-components.md](../../../_cross-cutting/ux-ui/common-components.md) の以下を利用する(本 UC は外部 IF 契約であり、実装は replay 側 UC が担う)。

- [C-02 AtomicWriter](../../../_cross-cutting/ux-ui/common-components.md#c-02-atomicwriter) — 再送ファイル受け渡し契約「正式名 = 常に完全な内容」の実装根拠
- [C-03 ManifestStore](../../../_cross-cutting/ux-ui/common-components.md#c-03-manifeststore) — 再送履歴(Replay 記録)参照と「Consumer の削除は配送記録に影響しない」契約の根拠
