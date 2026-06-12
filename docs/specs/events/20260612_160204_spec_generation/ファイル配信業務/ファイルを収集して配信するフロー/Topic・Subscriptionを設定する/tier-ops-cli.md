# Topic・Subscriptionを設定する - 運用 CLI 仕様

## 変更概要

`config validate` サブコマンドを提供し、単一 YAML 設定の構文・参照整合(Topic↔Subscription↔収集ソース↔認証情報参照)をデーモン起動前に検証する(SR-101)。検証は同一バイナリで共有する usecase / domain 層に委譲し(CLP-101)、CLI 専用のビジネスロジックは持たない。HTTP API は提供しない。

## コマンド仕様

### config validate

- **コマンド**: `file-pubsub config validate --config <path>`
- **対応 UC**: Topic・Subscriptionを設定する
- **認証**: なし(OS のファイル権限・実行ユーザに依存。CTP-007)

#### 引数

| パラメータ | 型 | 必須 | 説明 |
|-----------|---|------|------|
| --config | string (path) | Yes | 単一 YAML 設定ファイルのパス(CTP-003「単一 YAML 設定」) |

#### 出力(検証 OK)

| フィールド | 型 | 説明 |
|-----------|---|------|
| 設定要約 | テキスト(1〜数行) | Topic 数・Subscription 数・収集ソース数 |
| 終了コード | integer | 0 |

#### 出力(検証 NG)

エラーごとに「位置(YAML のキーパス)+ 原因 + 対処」を 1 件 1 ブロックで標準エラー出力へ表示する(ui-design.md)。

| 終了コード | 条件 | 出力 |
|----------:|------|-----------|
| 2 | YAML 構文エラー | 構文エラーの位置(行)+ 原因 + 対処 |
| 2 | 必須属性欠落(例: subscriptions[].directory 未定義) | `NG: topics[1].subscriptions[0].directory` 形式のキーパス + 原因 + 対処 |
| 2 | 参照不整合(Topic↔Subscription↔収集ソース↔認証情報参照) | 不整合の両端のキーパス + 原因 + 対処 |
| 2 | 不正な列挙値(source.type / original_file_handling) | キーパス + 許容値一覧 |
| 2 | --config 不正(ファイル不存在・読込不可) | パスと原因 + 対処 |

## 設定 YAML 構造(検証対象のデータモデル)

情報「設定」の属性をそのまま単一 YAML で表現する(CTP-003、ui-design.md「設定 YAML の構造ガイド」が正本)。

| キー | 型 | 説明 | 由来(RDRA) |
|--------|---|------|---------|
| polling_interval | integer(秒) | ポーリング間隔 | 情報「設定」 |
| archive_retention | integer(日) | Archive 保持期間(retention) | 情報「設定」/ 条件「Archive保持期間」 |
| retry_max_count | integer | リトライ上限回数 | 情報「設定」/ 条件「リトライ上限」 |
| metrics_port | integer | /metrics・/healthz の公開ポート | 情報「設定」 |
| topics[].name | string | Topic 名(orders / customers / invoices 等) | 情報「Topic」 |
| topics[].description | string | Topic の説明(任意) | 情報「Topic」 |
| topics[].source.type | enum(ftp / sftp / scp / local) | ソース種別 | 情報「収集ソース」/ バリエーション「収集ソース種別」 |
| topics[].source.host | string | 接続先ホスト(local の場合は不要) | 情報「収集ソース」 |
| topics[].source.directory | string | 対象ディレクトリパス | 情報「収集ソース」 |
| topics[].source.original_file_handling | enum(delete / copy) | 元ファイル処理方式(既定: delete) | バリエーション「元ファイル処理方式」 |
| topics[].source.stability_check | object | 安定待ち判定設定(サイズ・更新時刻の安定確認間隔) | 情報「収集ソース」/ 条件「書き込み完了判定」 |
| topics[].source.exclude_patterns | string[] | 除外パターン(任意) | 情報「収集ソース」 |
| topics[].source.auth.username | string | ユーザー名(`${ENV_VAR}` 参照可) | 情報「認証情報」 |
| topics[].source.auth.password | string | パスワード(`${ENV_VAR}` 参照可。key_file と排他) | 情報「認証情報」/ バリエーション「認証方式」 |
| topics[].source.auth.key_file | string | 鍵ファイルパス(password と排他) | 情報「認証情報」/ バリエーション「認証方式」 |
| topics[].subscriptions[].name | string | Subscription 名(current / next / test 等) | 情報「Subscription」/ バリエーション「Subscription種別」 |
| topics[].subscriptions[].directory | string | 配置先ディレクトリパス | 情報「Subscription」 |

## 検証項目(ビジネスルール)

- YAML 構文が正しいこと(パース可能であること)
- 必須キー(polling_interval / archive_retention / retry_max_count / metrics_port / topics / topics[].name / topics[].source / topics[].subscriptions / 各 directory)が存在すること
- `source.type` がバリエーション「収集ソース種別」の値(ftp / sftp / scp / local)であること。type が ftp / sftp / scp の場合は host と auth が必須、local の場合は host 不要
- `original_file_handling` がバリエーション「元ファイル処理方式」の値(delete / copy)であること
- Subscription は所属 Topic 配下に定義され、配置先ディレクトリパスを持つこと(参照整合: Topic↔Subscription)
- 認証情報参照(環境変数参照 `${ENV_VAR}` / 鍵ファイルパス)の記法が解釈可能であること。平文も許容する(CTP-002)
- Topic 名・同一 Topic 内の Subscription 名が重複しないこと(Manifest・Archive のパス解決が一意であるため)
- RDRA に無いサブコマンド・オプション(対話モード、`--json` 出力等)は追加しない

## ティア完了条件（BDD）

```gherkin
Feature: Topic・Subscriptionを設定する - 運用 CLI

  Scenario: 整合した設定の検証が成功する
    Given topic「orders」(source: sftp legacy-host01 /out/orders, auth: ${SFTP_USER}/${SFTP_PASSWORD}) と subscription「current」「next」を定義した config.yaml がある
    When 「file-pubsub config validate --config config.yaml」を実行する
    Then 終了コード 0 で、Topic 数 1・Subscription 数 2・収集ソース数 1 の要約が標準出力に表示される

  Scenario: 参照不整合をキーパス付きで報告する
    Given config.yaml の topics[0].subscriptions[1].directory が未定義である
    When 「file-pubsub config validate --config config.yaml」を実行する
    Then 終了コード 2 で、標準エラー出力に「NG: topics[0].subscriptions[1].directory」と原因・対処が表示される

  Scenario: local ソースでは host を要求しない
    Given topic「reports」の source が type=local, directory=/data/out/reports で host 未定義の config.yaml がある
    When 「file-pubsub config validate --config config.yaml」を実行する
    Then 終了コード 0 で検証 OK となる
```

## 共通コンポーネント参照

[common-components.md](../../../_cross-cutting/ux-ui/common-components.md) の以下を利用する。

- [C-14 ConfigLoader](../../../_cross-cutting/ux-ui/common-components.md#c-14-configloader) — config validate の検証本体(YAML 構文・必須キー・列挙値・参照整合・名前重複。違反はキーパス + 原因 + 対処で返却し終了コード 2 へ変換)
