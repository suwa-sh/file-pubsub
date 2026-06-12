# /healthzと/metricsをHTTPで公開する - 常駐デーモン仕様

## 変更概要

常駐デーモンの組込 HTTP サーバ(ランタイム層)とメトリクスエクスポータ(ゲートウェイ層、LR-302)の仕様。公開するのは GET /healthz と GET /metrics の 2 エンドポイントのみで、オンライン応答系・Web UI は持たない(CTP-008、CTP-007)。メトリクス契約の詳細は [data-visualization.md](../../../_cross-cutting/ux-ui/data-visualization.md) を正とする。

## API 仕様

### 死活監視エンドポイント

- **メソッド**: GET
- **パス**: /healthz
- **認証**: なし(アクセス制御は OS・ネットワークの責務。CTP-007)
- **OpenAPI**: [openapi.yaml](../../../_cross-cutting/api/openapi.yaml) の `paths./healthz.get` を参照

#### リクエスト

| パラメータ | 型 | 必須 | 説明 |
|-----------|---|------|------|
| (なし) | - | - | リクエストパラメータは持たない |

#### レスポンス

| フィールド | 型 | 説明 |
|-----------|---|------|
| (ボディ) | text | HTTP 200(デーモン稼働中)。死活はステータスコードで判定し、ボディの内容に契約はない |

#### エラーレスポンス

| ステータスコード | 条件 | レスポンス |
|----------------|------|-----------|
| (接続失敗) | デーモン停止中(プロセスなし) | connection refused(外部監視基盤が DOWN と判定する) |

### Prometheus メトリクスエンドポイント

- **メソッド**: GET
- **パス**: /metrics
- **認証**: なし(同上)
- **OpenAPI**: [openapi.yaml](../../../_cross-cutting/api/openapi.yaml) の `paths./metrics.get` を参照

#### リクエスト

| パラメータ | 型 | 必須 | 説明 |
|-----------|---|------|------|
| (なし) | - | - | リクエストパラメータは持たない |

#### レスポンス(Prometheus テキスト形式)

情報「メトリクス」の属性をそのまま公開する。これ以外のメトリクスは RDRA に無いため契約に含めない(data-visualization.md)。メトリクス名は候補であり実装時に確定し、確定後は後方互換を維持する。

| メトリクス名候補 | 型 | ラベル | 説明 |
|----------------|---|-------|------|
| file_pubsub_last_collect_timestamp_seconds | gauge | topic | Topic 別最終収集時刻(Unix 秒)。収集成功のたびに更新 |
| file_pubsub_processed_total | counter | topic | 処理件数(収集〜配信サイクルで処理したメッセージ累計) |
| file_pubsub_delivery_failure_total | counter | topic | 配信失敗数(Subscription 配信の失敗累計。リトライによる再失敗を含む) |
| file_pubsub_dlq_count | gauge | topic | DLQ 件数(現在 DLQ に隔離されているメッセージ数) |
| file_pubsub_backlog_count | gauge | topic | 滞留数(収集済みでまだ全 Subscription へ配信完了していないメッセージ数) |

#### エラーレスポンス

| ステータスコード | 条件 | レスポンス |
|----------------|------|-----------|
| 404 | /healthz・/metrics 以外のパス | 公開エンドポイントは 2 つのみ |
| (接続失敗) | デーモン停止中 | connection refused |

## メトリクスエクスポータ仕様(ゲートウェイ層)

- Topic 別の最終収集時刻・処理件数・配信失敗数・DLQ 件数・滞留数を**インメモリで集計**し、Prometheus 形式で公開する(LR-302)。
- 永続化しない: デーモン再起動で counter はリセットされる。時系列の蓄積は外部監視基盤側の責務であり、監視側は rate() / increase() ベースで扱う(storage_mapping E-012: cache)。
- 集計イベントの発生源はユースケース層の収集配信サイクル(収集成功・処理完了・配信失敗・DLQ 隔離・滞留変化)。
- ラベルは `topic` のみ。異常検知の単位は Topic とする(情報「メトリクス」の関連情報が Topic のため)。

## ビジネスルール

- 公開ポートは設定 YAML の metrics_port(情報「設定」)。組込 HTTP サーバはデーモン起動時に開始、graceful shutdown 時に停止する。
- しきい値判定・アラート発報は外部監視基盤の責務。本体はしきい値を持たない(SP-005)。
- HTTP アクセスは監視ポーリングのみを想定する(NFR B.1.1.1 Lv1: 同時アクセスは監視ポーリングのみ)。スクレイプ間隔はポーリング間隔と独立に設定してよい。
- 攻撃面の最小化: Web UI なし・HTTP は監視エンドポイントのみ(CTP-007)。
- メトリクス契約の安定提供: メトリクス名は実装時に確定し、確定後は後方互換を維持する(data-visualization.md)。

## ティア完了条件（BDD）

```gherkin
Feature: /healthzと/metricsをHTTPで公開する - 常駐デーモン

  Scenario: /healthz が稼働中に 200 を返す
    Given デーモンが metrics_port=9090 で稼働中である
    When GET /healthz を実行する
    Then HTTP 200 が返る

  Scenario: /metrics が topic ラベル付きの 5 メトリクスを返す
    Given Topic 「orders」 と 「invoices」 が定義され、orders で 12 件処理・invoices で 3 件処理済みである
    When GET /metrics を実行する
    Then Prometheus テキスト形式で file_pubsub_last_collect_timestamp_seconds / file_pubsub_processed_total / file_pubsub_delivery_failure_total / file_pubsub_dlq_count / file_pubsub_backlog_count が topic="orders" と topic="invoices" のラベル付きで返る

  Scenario: メトリクスはインメモリ集計で再起動時にリセットされる
    Given file_pubsub_processed_total{topic="orders"} が 12 である
    When デーモンを再起動した直後に GET /metrics を実行する
    Then file_pubsub_processed_total{topic="orders"} は 0 から再開している
```

## 共通コンポーネント参照

[common-components.md](../../../_cross-cutting/ux-ui/common-components.md) の以下を利用する。

- [C-10 MetricsRegistry](../../../_cross-cutting/ux-ui/common-components.md#c-10-metricsregistry) — Topic 別 5 メトリクスのインメモリ集計と Prometheus 形式公開(エクスポータ本体)
- [C-11 HTTPEndpoint](../../../_cross-cutting/ux-ui/common-components.md#c-11-httpendpoint) — /healthz・/metrics の 2 エンドポイントのみを公開する組込 HTTP サーバ
- [C-14 ConfigLoader](../../../_cross-cutting/ux-ui/common-components.md#c-14-configloader) — metrics_port(公開ポート)の参照
