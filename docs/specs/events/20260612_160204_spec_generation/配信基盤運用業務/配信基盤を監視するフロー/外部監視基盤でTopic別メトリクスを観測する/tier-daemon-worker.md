# 外部監視基盤でTopic別メトリクスを観測する - 常駐デーモン仕様(外部 IF 仕様)

## 変更概要

外部監視基盤(Prometheus / Grafana 等)から見た file-pubsub のメトリクス契約を定義する。実装上の新規変更はなく(エンドポイント実装は UC「/healthzと/metricsをHTTPで公開する」の責務)、この UC は**監視基盤が依存してよい契約**と**推奨アラートルール例**、**責務境界**を定める外部 IF 仕様である。正本は [data-visualization.md](../../../_cross-cutting/ux-ui/data-visualization.md)。

## 外部 IF 仕様

### 監視基盤が依存してよい契約

| 項目 | 契約 | 根拠 |
|------|------|------|
| エンドポイント | GET /healthz(死活)・GET /metrics(Prometheus テキスト形式)のみ。ポートは設定 YAML の metrics_port | SP-005、情報「設定」 |
| メトリクス系列 | topic ラベル付きの 5 系列: file_pubsub_last_collect_timestamp_seconds(gauge) / file_pubsub_processed_total(counter) / file_pubsub_delivery_failure_total(counter) / file_pubsub_dlq_count(gauge) / file_pubsub_backlog_count(gauge)。名称は候補で実装時に確定し、確定後は後方互換を維持する | 情報「メトリクス」の属性そのまま |
| ラベル | `topic` のみ。Subscription・message_id 単位のメトリクスは提供しない(その粒度は status コマンドと構造化ログの責務) | 情報「メトリクス」(関連情報: Topic) |
| 集計特性 | インメモリ集計・永続化なし。デーモン再起動で counter リセット。監視側は rate()/increase() ベースで扱い、生値の単調増加を前提にしない | storage_mapping E-012(cache)、LR-302 |
| 死活判定 | /healthz の 200 応答 = 稼働中。停止中は接続失敗(監視基盤側で up=0 相当として扱う) | UC「/healthzと/metricsをHTTPで公開する」 |
| スクレイプ間隔 | デーモンのポーリング間隔と独立に設定してよい(HTTP アクセスは監視ポーリングのみの想定) | CTP-008 |

### 推奨アラートルール例(外部監視基盤側で定義)

しきい値の決定・判定・発報はすべて外部監視基盤の責務であり、file-pubsub 本体はしきい値を持たない(SP-005)。しきい値は Topic のポーリング間隔・業務上の許容遅延に合わせて導入先が調整する。

| アラート | PromQL 例 | 検知意図 → 運用者のアクション |
|---------|----------|---------------------------|
| 死活異常 | up{job="file-pubsub"} == 0(または /healthz probe 失敗) | デーモン停止 → 再起動(冪等再開) |
| 最終収集時刻の停滞 | time() - file_pubsub_last_collect_timestamp_seconds > 1800 等 | 収集停止 Topic の検知 → Producer・収集ソース疎通の調査 |
| DLQ 滞留 | file_pubsub_dlq_count > 0 | 恒久的な配信失敗 → status で隔離理由確認、再送(replay)/破棄の判断 |
| 配信失敗の増加 | increase(file_pubsub_delivery_failure_total[15m]) > 10 等 | 一時障害の多発 → 構造化ログ調査 |
| 滞留の増加 | file_pubsub_backlog_count > 100 等 | 配信が処理量に追いつかない → 設定チューニング・リソース増強(CTP-008 スケールアップ) |

### 責務境界

| 責務 | file-pubsub 本体 | 外部監視基盤 | 配信基盤運用者 |
|------|:---------------:|:-----------:|:-------------:|
| メトリクスの算出・公開(/metrics)・死活(/healthz) | ○ | - | - |
| 時系列蓄積・しきい値判定・アラート発報・ダッシュボード表示 | - | ○(24 時間自動監視) | - |
| 検知後の障害調査・再送判断・再起動 | - | - | ○(営業時間内・status / 構造化ログ / replay) |

## データモデル変更

変更なし。この UC は観測のみであり、file-pubsub 側のデータストア(Manifest / Archive / Lock 等)に書き込みを行わない。メトリクスの時系列データは外部監視基盤側に蓄積される(file-pubsub の管理外)。

## ビジネスルール

- 24 時間自動監視 + 営業時間内人対応: 監視は外部監視基盤による 24 時間自動監視、人による対応は営業時間内・兼務の配信基盤運用者が行う(CTP-009)。
- 異常検知の単位は Topic: どの Topic のどの段階(収集 / 配信 / DLQ)が異常かまでを監視基盤が示し、message_id 単位の特定は status コマンドと構造化ログへ誘導する(data-visualization.md)。
- 契約の安定性: 監視基盤が依存するのは上表の契約のみ。契約外の内部実装(集計タイミング等)に依存させない。
- 障害検知方式: 監視ツール検知(NFR C.3.1.1 Lv2)。アラートから障害対応・再送判断につなげる。

## ティア完了条件（BDD）

```gherkin
Feature: 外部監視基盤でTopic別メトリクスを観測する - 常駐デーモン(外部 IF)

  Scenario: 契約どおりの 5 系列が topic ラベル付きで観測できる
    Given Prometheus が /metrics を 30 秒間隔でスクレイプしている
    When Topic 「orders」 で収集・配信・DLQ 隔離が発生する
    Then file_pubsub_last_collect_timestamp_seconds / file_pubsub_processed_total / file_pubsub_delivery_failure_total / file_pubsub_dlq_count / file_pubsub_backlog_count の 5 系列が topic="orders" ラベル付きで取得できる
    And 契約外のメトリクス系列に依存せずダッシュボード・アラートを構成できる

  Scenario: 再起動リセットが rate ベース監視で吸収される
    Given file_pubsub_processed_total{topic="orders"} が 120 まで増加している
    When 計画停止(graceful shutdown)後に再起動して counter が 0 になる
    Then increase() ベースの監視パネルは負の値を示さず観測を継続できる
```

## 共通コンポーネント参照

[common-components.md](../../../_cross-cutting/ux-ui/common-components.md) の以下を利用する(本 UC は外部 IF 契約であり、実装は公開側 UC が担う)。

- [C-10 MetricsRegistry](../../../_cross-cutting/ux-ui/common-components.md#c-10-metricsregistry) — 監視基盤が依存する topic ラベル付き 5 系列契約の提供元(インメモリ集計・再起動リセット特性を含む)
- [C-11 HTTPEndpoint](../../../_cross-cutting/ux-ui/common-components.md#c-11-httpendpoint) — 監視基盤がスクレイプする /healthz・/metrics エンドポイント契約の提供元
