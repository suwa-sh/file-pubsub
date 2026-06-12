# シングルバイナリ/Dockerイメージを配置する - 運用 CLI仕様

## 変更概要

file-pubsub の導入手順仕様。tier-ops-cli は serve / status / replay / config validate を提供する単一バイナリであり(SR-102)、この UC ではそのバイナリ(または Docker イメージ)を導入先へ配置し、docker compose の動作確認環境で収集から配信までを事前確認できる状態を整える。GUI・HTTP API・追加コンポーネントは持たない(CTP-005)。

## 導入手順仕様

### 配布物

| 配布形態 | 対象環境 | 内容 | 根拠 |
|---------|---------|------|------|
| シングルバイナリ | レガシー現場の Linux サーバ(主対象)+ macOS | Go 実装の単一実行ファイル。追加ランタイム不要 | バリエーション「配布形態」、CTP-005、技術制約「シングルバイナリ配布」 |
| Dockerコンテナイメージ | Windows 開発 PC 等 | コンテナイメージ + docker compose 一式(動作確認環境) | バリエーション「配布形態」、CTP-005、NFR C.4.1.1(テスト環境 Lv2 docker compose) |

### 手順 1: シングルバイナリ配置

1. 配布物(file-pubsub バイナリ)を導入先サーバの配置先(例: `/opt/file-pubsub/file-pubsub`)へ転送する。
2. 実行権限を付与する(`chmod +x /opt/file-pubsub/file-pubsub`)。
3. 実行ユーザを決め、データディレクトリ(archive/ / subscriptions/ / manifest 等の親ディレクトリ)への読み書き権限を OS のファイル権限で整える(CTP-007: アクセス制御は OS のファイル権限・実行ユーザに依存する)。
4. バイナリ単体で実行できること(追加ランタイム不要)を確認する。serve / status / replay / config validate が同一バイナリのサブコマンドとして利用できる(SR-102)。

### 手順 2: Docker イメージ導入と docker compose 動作確認環境

1. Docker イメージを取得する(`docker pull`)。
2. 同梱の docker compose 一式を展開し、`docker compose up -d` で動作確認環境を起動する。
3. 動作確認環境では、収集ソース(ローカルディレクトリ)へファイルを置くと Collect → Archive 保存 → Fan-out の一連の動作を事前確認できる(BUC 説明「docker compose の動作確認環境で収集から配信までの動作を事前確認できる」)。
4. 確認後は `docker compose down` で停止する。

### 手順 3: 設定 YAML の準備(後続 UC への引き継ぎ)

- 単一 YAML 設定ファイル(config.yaml)を配置し、`--config` で参照させる(CTP-003「単一 YAML 設定」)。
- 設定の編集と検証(`config validate`)は UC「Topic・Subscriptionを設定する」、デーモンの起動は UC「デーモンを起動する」で行う。
- 認証情報は環境変数参照(`${ENV_VAR}`)と鍵ファイルパス指定を推奨し、README で案内する(CTP-002)。

## ビジネスルール

- 追加ランタイム不要: シングルバイナリは Go ランタイムや追加ミドルウェアのインストールなしで実行できる(技術制約「シングルバイナリ配布(追加ランタイム不要)」)。
- 単一バイナリのサブコマンド構成: serve / status / replay / config validate を単一バイナリで提供し、運用機能を追加コンポーネントなしに提供する(SR-102)。
- Web UI は持たない: 配置するのはバイナリ(またはイメージ)と設定 YAML のみで、ブラウザ要件はない(CTP-005)。
- 環境非依存: 特定クラウドベンダーのサービスに依存せず、OS とローカルファイルシステム、標準プロトコルのみを前提とする(CTR-001)。
- データ移行なし: 新規構築のため導入時のデータ移行はない(CTP-005)。

## ティア完了条件（BDD）

```gherkin
Feature: シングルバイナリ/Dockerイメージを配置する - 運用 CLI

  Scenario: 配置したシングルバイナリが追加ランタイムなしで実行できる
    Given Go ランタイム未導入の Linux サーバの /opt/file-pubsub/file-pubsub にバイナリが配置され実行権限が付与されている
    When 配信基盤運用者が file-pubsub を実行する
    Then バイナリが追加ランタイムなしで起動し、serve / status / replay / config validate のサブコマンドが利用できる

  Scenario: docker compose 動作確認環境が起動する
    Given Windows 開発 PC に Docker と docker compose がインストールされ、動作確認環境一式が展開されている
    When 配信基盤運用者が docker compose up -d を実行する
    Then file-pubsub コンテナが起動し、収集から配信までの動作確認に使える状態になる
```

## 共通コンポーネント参照

該当なし。本 UC は導入手順仕様であり、実装コンポーネントを直接利用しない([common-components.md](../../../_cross-cutting/ux-ui/common-components.md) の全コンポーネントを内包した単一バイナリ / Docker イメージの配布形態を定める)。
