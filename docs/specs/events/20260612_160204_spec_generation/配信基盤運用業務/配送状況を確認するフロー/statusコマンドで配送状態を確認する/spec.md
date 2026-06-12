# statusコマンドで配送状態を確認する

## 概要

配信基盤運用者が `status` サブコマンドで、Manifest に記録された message_id・topic・Subscription 別の配送状態(delivered / failed / dlq)を確認する。並行稼働中の障害調査・再送判断・監査に使う。出力は ui-design.md の列規約(MESSAGE_ID / TOPIC / SUBSCRIPTION / STATUS / RETRY / DELIVERED_AT / REPLAY)に従う人間可読テーブルと、topic / Subscription 別の件数集計である。

> GUI は存在しない。RDRA の画面「配送状況照会画面」は運用 CLI(`status` サブコマンド)の出力として実現する(_inference.md / ux-design.md)。

## データフロー

```mermaid
graph LR
  OPS["配信基盤運用者"]
  subgraph CLI["tier-ops-cli"]
    CMD["L-cli-command<br/>status 引数(絞り込み: topic / subscription / 状態)"]
    VIEW["L-cli-command<br/>配送状態テーブル + 件数集計ビュー"]
  end
  subgraph SHARED["共有レイヤー(tier-daemon-worker のパッケージ)"]
    UCQ["L-daemon-usecase<br/>StatusQuery(配送状態照会)"]
    DM["L-daemon-domain<br/>Manifest モデル(delivered / failed / dlq)"]
    GW["L-daemon-gateway<br/>ファイルストア(Manifest 読取)"]
  end
  subgraph FS["ローカルファイルシステム"]
    MAN[("manifest/{message_id}.json")]
  end
  OPS --> CMD
  CMD --> UCQ
  UCQ --> GW
  GW -->|"Manifest JSON 読取"| MAN
  MAN --> GW
  GW --> DM
  DM --> UCQ
  UCQ --> VIEW
  VIEW --> OPS
```

| レイヤー | データモデル | 変換内容 |
|---------|------------|---------|
| CLI L-cli-command | status 引数(`--config` 必須、絞り込み: topic / subscription / 状態) | 引数バリデーション(LR-401)+ StatusQuery への変換 |
| 共有 L-daemon-usecase | StatusQuery(topic, subscription, status) | Manifest 照会のフロー制御(CLR-101: CLI からのデータアクセスは共有 usecase 経由) |
| 共有 L-daemon-domain | Manifest モデル(message_id、Topic名、Subscription別配送状態、リトライ回数、配送日時、再送(Replay)記録) | 絞り込み・topic / Subscription 別の件数集計(LP-401) |
| 共有 L-daemon-gateway | Manifest レコード(メッセージ別 JSON ファイル) | manifest/ 配下の JSON 読取 → ドメインモデル変換 |
| 出力 | 配送状態テーブル(MESSAGE_ID / TOPIC / SUBSCRIPTION / STATUS / RETRY / DELIVERED_AT / REPLAY)+ topic / Subscription 別件数集計 | ui-design.md の列規約。1 行 1 レコードの行指向・日時 ISO 8601・状態は Manifest の語彙のまま |

## 処理フロー

```mermaid
sequenceDiagram
  actor OPS as 配信基盤運用者

  box rgb(255,245,230) tier-ops-cli
    participant CMD as L-cli-command
  end

  box rgb(240,255,240) 共有レイヤー(tier-daemon-worker)
    participant UC as L-daemon-usecase
    participant DM as L-daemon-domain
    participant GW as L-daemon-gateway
  end

  participant FS as ローカルファイルシステム

  OPS->>CMD: status --config config.yaml
  CMD->>CMD: 引数バリデーション(LR-401)
  CMD->>UC: StatusQuery(絞り込みなし)
  UC->>GW: Manifest 読取
  GW->>FS: manifest/ 配下のメッセージ別 JSON を読取
  FS-->>GW: Manifest レコード群
  GW-->>UC: Manifest モデル
  UC->>DM: topic / Subscription 別の状態集計
  DM-->>UC: 明細行 + 件数集計(delivered / failed / dlq 別)
  UC-->>CMD: 照会結果
  CMD-->>OPS: 配送状態テーブル + 集計ビュー表示・終了コード 0
```

## バリエーション一覧

| バリエーション名 | 値 | 処理内容 | 適用 tier | 適用箇所 |
|----------------|---|---------|----------|---------|
| 配信方式 | 通常配信(Fan-out)、再送(Replay) | REPLAY 列の表示切替(再送(Replay)による配送は `replay`、通常配信は `-`)。Manifest の再送(Replay)記録から判定 | tier-ops-cli | status のテーブル表示 |
| Subscription種別 | current、next、test | SUBSCRIPTION 列・subscription 絞り込みの値域。並行稼働中の Current / Next 別の配送確認に使う | tier-ops-cli | status の絞り込み・表示 |

## 分岐条件一覧

| 条件名 | 判定ルール | 適用 tier | 適用箇所 | BDD Scenario |
|--------|----------|----------|---------|-------------|
| (条件.tsv 該当なし) | この UC に適用される条件.tsv の条件はない(参照のみの UC)。配送状態の値域は Manifest の語彙(delivered / failed / dlq)に従う | tier-ops-cli | status 照会 | - |

## 計算ルール一覧

| 計算名 | 入力情報 | 計算式/ロジック | 出力情報 | 適用 tier |
|--------|---------|---------------|---------|----------|
| 配送状態件数集計 | Manifest の Subscription別配送状態(delivered / failed / dlq) | topic / Subscription 別に状態ごとの件数を合計する(LP-401「status の出力整形」: 運用者が再送判断・DLQ 対処判断に使えるテーブル) | topic / Subscription 別の delivered / failed / dlq 件数 | tier-ops-cli |

## 状態遷移一覧

| 状態モデル | 遷移元 | 遷移先 | トリガー | 事前条件 | 事後処理 | 適用 tier |
|-----------|--------|--------|---------|---------|---------|----------|
| メッセージ配送状態 | (遷移なし・参照のみ) | - | - | - | 配信済(delivered) / 配信失敗(failed) / DLQ隔離(dlq) の現在状態を読み取り、障害調査・再送判断・監査の入力にする | tier-ops-cli |

## 関連 RDRA モデル

| モデル種別 | 要素名 | 関連 |
|-----------|--------|------|
| 業務 | 配信基盤運用業務 | このUCが属する業務 |
| BUC | 配送状況を確認するフロー | このUCを含むBUC |
| アクティビティ | 配送状況を確認する | このUCを含むアクティビティ |
| アクター | 配信基盤運用者 | status で配送状況を確認する(価値受益) |
| 画面 | 配送状況照会画面 | CLI `status` サブコマンドの出力として実現 |
| 情報 | Manifest | 参照(message_id、Topic名、Subscription別配送状態(delivered / failed / dlq)、リトライ回数、配送日時、再送(Replay)記録) |
| 情報 | メッセージ | 参照(message_id(収集時刻 + Topic + 元ファイル名から採番)、Topic名、元ファイル名、収集時刻) |
| 情報 | Topic | 参照(Topic名)。絞り込み・集計キー |
| 情報 | Subscription | 参照(Subscription名、所属Topic)。絞り込み・集計キー |
| 状態 | メッセージ配送状態 | delivered / failed / dlq の現在状態を参照(遷移はしない) |
| バリエーション | 配信方式 | REPLAY 列の表示判定 |
| バリエーション | Subscription種別 | SUBSCRIPTION 列の値域 |

## E2E 完了条件（BDD）

### 正常系

```gherkin
Feature: statusコマンドで配送状態を確認する

  Scenario: 全 topic の配送状態テーブルと件数集計を表示する
    Given manifest/ に topic=orders の message_id=20260601T091500_orders_orders_20260601.csv が subscription=current は delivered・subscription=next は failed(リトライ 2 回) と記録されている
    When 配信基盤運用者が「status --config config.yaml」を実行する
    Then MESSAGE_ID / TOPIC / SUBSCRIPTION / STATUS / RETRY / DELIVERED_AT / REPLAY の列で current=delivered と next=failed の 2 行が表示され topic / Subscription 別の件数集計が続いて表示され終了コード 0 で終了する

  Scenario: subscription で絞り込んで並行稼働中の Next だけを確認する
    Given manifest/ に topic=orders の配送記録が subscription=current と next の両方に存在する
    When 配信基盤運用者が「status --config config.yaml --subscription next」を実行する
    Then SUBSCRIPTION=next の行だけが表示され Current 側の配送に関する行は表示されない

  Scenario: 再送(Replay)による配送が REPLAY 列で識別できる
    Given message_id=20260601T091500_orders_orders_20260601.csv が subscription=next へ replay 済みで manifest に Replay 記録がある
    When 配信基盤運用者が「status --config config.yaml --topic orders」を実行する
    Then 該当行の REPLAY 列に replay が表示され通常配信の行は - と表示される
```

### 異常系

```gherkin
  Scenario: Manifest 読み取り失敗は実行時エラーとなる
    Given manifest/ ディレクトリに実行ユーザの読み取り権限がない
    When 配信基盤運用者が「status --config config.yaml」を実行する
    Then 標準エラー出力に原因(Manifest 読み取り失敗)と対処(権限と実行ユーザの確認)が表示され終了コード 1 で終了する

  Scenario: 不正な状態値の指定は実行前に弾かれる
    Given Manifest の語彙にない状態値「pending」を指定する
    When 配信基盤運用者が「status --config config.yaml --status pending」を実行する
    Then 原因(状態値は delivered / failed / dlq のみ)と対処が表示され終了コード 2 で終了する
```

## ティア別仕様

- [運用 CLI](tier-ops-cli.md)

### 統合 API Spec

- [OpenAPI Spec](../../../_cross-cutting/api/openapi.yaml)（全 UC 統合。この UC に HTTP API はない）
- AsyncAPI Spec: 対象イベントなし(生成しない)
