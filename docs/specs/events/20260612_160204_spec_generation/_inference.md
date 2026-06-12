# Spec 生成 分析根拠 (Step1)

- event_id: 20260612_160204_spec_generation
- trigger_event: rdra:20260612_150425_initial_build, nfr:20260612_153353_nfr_initial_build, arch:20260612_154833_arch_initial_build
- 初期構築モード: BUC.tsv の全 UC (19) を対象に Spec を生成する

## UC ツリーと UC-ティアマッピング

arch-design.yaml の tiers は `tier-daemon-worker`(常駐デーモン)と `tier-ops-cli`(運用 CLI)の 2 つ。
本システムに Presentation 系ティア(GUI)は存在しない。design-event.yaml は不在(デザインシステム対象外)。

### ファイル配信業務
- **ファイルを収集して配信するフロー** (6 UC)
  | UC | 対象ティア | 備考 |
  |---|---|---|
  | Topic・Subscriptionを設定する | tier-ops-cli | 設定 YAML 編集 + 検証(config validate) |
  | ファイルを収集する(Collect) | tier-daemon-worker | 収集コネクタ(local/FTP/SFTP/SCP) |
  | Archiveに保存する | tier-daemon-worker | 配信前必須保存 |
  | Subscriptionへ複製配信する(Fan-out) | tier-daemon-worker | AtomicWrite・配送独立 |
  | 配信失敗をリトライしDLQへ隔離する | tier-daemon-worker | リトライ上限・DLQ |
  | Subscriptionディレクトリからファイルを取得する | (外部 IF 仕様) | Consumer 側操作。システム境界仕様として spec.md のみ厚く、tier は配置保証の観点で daemon |
- **ファイルを再送するフロー** (3 UC)
  | UC | 対象ティア |
  |---|---|
  | 配送履歴から再送対象を確認する | tier-ops-cli |
  | 再送(Replay)を実行する | tier-ops-cli (実処理は共有 usecase) |
  | Subscriptionディレクトリから再送ファイルを取得する | (外部 IF 仕様) |

### 配信基盤運用業務
- **配信基盤を運用するフロー** (5 UC)
  | UC | 対象ティア |
  |---|---|
  | シングルバイナリ/Dockerイメージを配置する | (導入手順仕様) tier-ops-cli |
  | デーモンを起動する | tier-daemon-worker + tier-ops-cli(serve コマンド) |
  | 冪等に処理を再開する | tier-daemon-worker |
  | デーモンをgraceful shutdownで停止する | tier-daemon-worker |
  | 保持期間超過のArchiveを削除する | tier-daemon-worker |
- **配信基盤を監視するフロー** (2 UC)
  | UC | 対象ティア |
  |---|---|
  | /healthzと/metricsをHTTPで公開する | tier-daemon-worker |
  | 外部監視基盤でTopic別メトリクスを観測する | (外部 IF 仕様。メトリクス契約を定義) |
- **配送状況を確認するフロー** (3 UC)
  | UC | 対象ティア |
  |---|---|
  | statusコマンドで配送状態を確認する | tier-ops-cli |
  | DLQ隔離メッセージを確認する | tier-ops-cli |
  | 構造化ログを調査する | (ログ契約仕様) tier-daemon-worker のログ出力仕様 |

## 全体横断設計方針 (Step2 の翻案)

GUI が無いため、_cross-cutting/ux-ui は **運用者向け CLI/設定/観測の UX** として生成する:
- `ux-design.md`: 運用者ジャーニー(導入→設定→起動→監視→障害調査→再送)、コマンド体系(serve/status/replay/config validate)、エラーメッセージ原則
- `ui-design.md`: CLI 出力フォーマット規約(status のテーブル、終了コード)、設定 YAML 構造、構造化ログのフィールド規約
- `data-visualization.md`: Prometheus メトリクス契約と Grafana ダッシュボード設計ガイド(topic 別パネル・アラートルール例)

## API / データストア方針
- HTTP API は /metrics と /healthz の 2 エンドポイントのみ(GET)。openapi.yaml は OpenAPI 3.1 で生成。AsyncAPI 対象イベントなし(生成しない)
- データストアは RDB/KVS を使わない。全永続化はローカルファイルシステム。`_model-summary.yaml` の tables は使わず、file レイアウト(ディレクトリ構造 + Manifest JSON スキーマ)を object_storage 相当のパス設計として `datastore/file-layout.yaml`(rdb-schema.yaml の代替)に集約する
- RDRA 整合性: RDRA に無い要素は追加しない(必要なら docs/todo.md へ記録)
