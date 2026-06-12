# Spec 一覧

file-pubsub の最新 Spec スナップショット。2 業務 / 5 BUC / 19 UC。

- 本システムは GUI を持たない Go 製常駐デーモン + 運用 CLI(tier-daemon-worker / tier-ops-cli)
- HTTP API は観測専用 2 エンドポイント(GET /healthz, GET /metrics)のみ。非同期イベント(AsyncAPI 対象)なし
- 永続化は RDB/KVS でなくファイルレイアウト + メッセージ別 JSON Manifest(設計判断は [decisions/](decisions/) 参照)

## UC 仕様

### ファイル配信業務

- **ファイルを収集して配信するフロー** — [buc-spec.md](<ファイル配信業務/ファイルを収集して配信するフロー/buc-spec.md>)
  - [Topic・Subscriptionを設定する](<ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>)
  - [ファイルを収集する(Collect)](<ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>)
  - [Archiveに保存する](<ファイル配信業務/ファイルを収集して配信するフロー/Archiveに保存する/spec.md>)
  - [Subscriptionへ複製配信する(Fan-out)](<ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionへ複製配信する(Fan-out)/spec.md>)
  - [配信失敗をリトライしDLQへ隔離する](<ファイル配信業務/ファイルを収集して配信するフロー/配信失敗をリトライしDLQへ隔離する/spec.md>)
  - [Subscriptionディレクトリからファイルを取得する](<ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionディレクトリからファイルを取得する/spec.md>)
- **ファイルを再送するフロー** — [buc-spec.md](<ファイル配信業務/ファイルを再送するフロー/buc-spec.md>)
  - [配送履歴から再送対象を確認する](<ファイル配信業務/ファイルを再送するフロー/配送履歴から再送対象を確認する/spec.md>)
  - [再送(Replay)を実行する](<ファイル配信業務/ファイルを再送するフロー/再送(Replay)を実行する/spec.md>)
  - [Subscriptionディレクトリから再送ファイルを取得する](<ファイル配信業務/ファイルを再送するフロー/Subscriptionディレクトリから再送ファイルを取得する/spec.md>)

### 配信基盤運用業務

- **配信基盤を運用するフロー** — [buc-spec.md](<配信基盤運用業務/配信基盤を運用するフロー/buc-spec.md>)
  - [シングルバイナリ/Dockerイメージを配置する](<配信基盤運用業務/配信基盤を運用するフロー/シングルバイナリ-Dockerイメージを配置する/spec.md>)
  - [デーモンを起動する](<配信基盤運用業務/配信基盤を運用するフロー/デーモンを起動する/spec.md>)
  - [冪等に処理を再開する](<配信基盤運用業務/配信基盤を運用するフロー/冪等に処理を再開する/spec.md>)
  - [デーモンをgraceful shutdownで停止する](<配信基盤運用業務/配信基盤を運用するフロー/デーモンをgraceful shutdownで停止する/spec.md>)
  - [保持期間超過のArchiveを削除する](<配信基盤運用業務/配信基盤を運用するフロー/保持期間超過のArchiveを削除する/spec.md>)
- **配信基盤を監視するフロー** — [buc-spec.md](<配信基盤運用業務/配信基盤を監視するフロー/buc-spec.md>)
  - [/healthzと/metricsをHTTPで公開する](<配信基盤運用業務/配信基盤を監視するフロー/-healthzと-metricsをHTTPで公開する/spec.md>) — API 2 本(GET /healthz, GET /metrics)
  - [外部監視基盤でTopic別メトリクスを観測する](<配信基盤運用業務/配信基盤を監視するフロー/外部監視基盤でTopic別メトリクスを観測する/spec.md>)
- **配送状況を確認するフロー** — [buc-spec.md](<配信基盤運用業務/配送状況を確認するフロー/buc-spec.md>)
  - [statusコマンドで配送状態を確認する](<配信基盤運用業務/配送状況を確認するフロー/statusコマンドで配送状態を確認する/spec.md>)
  - [DLQ隔離メッセージを確認する](<配信基盤運用業務/配送状況を確認するフロー/DLQ隔離メッセージを確認する/spec.md>)
  - [構造化ログを調査する](<配信基盤運用業務/配送状況を確認するフロー/構造化ログを調査する/spec.md>)

各 UC ディレクトリの構成: `spec.md`(UC 仕様 + BDD)+ `tier-daemon-worker.md` / `tier-ops-cli.md`(ティア別仕様)+ `_api-summary.yaml` + `_model-summary.yaml`

## 全体横断仕様

- [UX デザイン仕様(運用者向け CLI/設定/観測 UX)](_cross-cutting/ux-ui/ux-design.md)
- [UI デザイン仕様(CLI 出力・設定 YAML・構造化ログ規約)](_cross-cutting/ux-ui/ui-design.md)
- [データ可視化仕様(Prometheus メトリクス契約 + Grafana 設計ガイド)](_cross-cutting/ux-ui/data-visualization.md)
- [共通コンポーネント](_cross-cutting/ux-ui/common-components.md)
- [OpenAPI(観測専用 2 エンドポイント)](_cross-cutting/api/openapi.yaml)
- [データストアスキーマ(ファイルレイアウト + Manifest)](_cross-cutting/datastore/object-storage-schema.yaml) / [解説](_cross-cutting/datastore/datastore-schema.md)
- [トレーサビリティマトリクス](_cross-cutting/traceability-matrix.md)

※ asyncapi.yaml / rdb-schema.yaml / kvs-schema.yaml は対象(非同期イベント / RDB / KVS)が存在しないため未生成。

## 設計判断記録

- [spec-decision-001: API スタイル(観測専用 2 エンドポイントのみ、REST CRUD なし)](decisions/spec-decision-001.yaml)
- [spec-decision-002: 非同期境界(MQ 不採用、ディレクトリ + Manifest のファイルベース Pub/Sub)](decisions/spec-decision-002.yaml)
- [spec-decision-003: データ永続化(RDB 正規化なし、ファイルレイアウト + メッセージ別 JSON Manifest)](decisions/spec-decision-003.yaml)

## メタデータ

- Event ID: 20260612_160204_spec_generation
- 生成日時: 2026-06-12T17:01:58+09:00
- トリガー: 初期構築 (rdra:20260612_150425_initial_build, nfr:20260612_153353_nfr_initial_build, arch:20260612_154833_arch_initial_build)
- UC 総数: 19(業務 2 / BUC 5)
- API 総数: 2(非同期イベント: 0)
- 概要: [spec-event.md](spec-event.md) / [spec-event.yaml](spec-event.yaml)
