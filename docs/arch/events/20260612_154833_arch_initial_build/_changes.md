# 変更サマリ

- event_id: 20260612_154833_arch_initial_build
- trigger_event: rdra:20260612_150425_initial_build, nfr:20260612_153353_nfr_initial_build
- モード: 初期構築(全要素を「追加」として記載)

## 追加

### technology_context
- languages: Go
- frameworks: Go 標準ライブラリ / Prometheus クライアントライブラリ / CLI フレームワーク(サブコマンド) / YAML パーサ
- constraints: シングルバイナリ配布 / 単一インスタンス前提(HA なし) / 外部 DB なし(ローカルファイルシステムのみ) / オンプレ単一サーバ + Docker / Producer・Consumer 無改修

### system_architecture
- tiers: tier-daemon-worker(常駐デーモン: SP-001〜SP-006, SR-001〜SR-007)
- tiers: tier-ops-cli(運用 CLI: SP-101〜SP-102, SR-101〜SR-102)
- cross_tier_policies: CTP-001〜CTP-009(構造化ログ / 認証情報の扱い / 単一 YAML 設定 / pass-through / シングルバイナリ + Docker 配布 / 単一インスタンス・非冗長 / セキュリティ統制最小実装 / 性能設計方針 / 運用体制)
- cross_tier_rules: CTR-001〜CTR-003(ベンダーニュートラル / 終了コード + 構造化ログ / 全配送操作の Manifest 記録)

### app_architecture
- tier_layers: tier-daemon-worker 4 層(L-daemon-runtime / L-daemon-usecase / L-daemon-domain / L-daemon-gateway。CLP-001, CLR-001)
- tier_layers: tier-ops-cli 1 層(L-cli-command + daemon ティアのレイヤー共有。CLP-101, CLR-101)

### data_architecture
- entities: E-001 設定 / E-002 Topic / E-003 Subscription / E-004 収集ソース / E-005 認証情報 / E-006 メッセージ / E-007 Manifest / E-008 DLQ / E-009 処理済み管理 / E-010 Archiveファイル / E-011 Lock / E-012 メトリクス / E-013 ログ(RDRA 情報.tsv の 13 情報すべて)
- storage_mapping: E-001〜E-011, E-013 = file、E-012 = cache(インメモリ)

### decisions
- arch-decision-001: ストレージ戦略(ローカルファイルシステムのみ・Manifest=JSON)
- arch-decision-002: 実行モデル(単一バイナリ 2 ティア)
- arch-decision-003: レイヤリング(4 層直接依存・IF は収集コネクタのみ)
- arch-decision-004: 観測戦略(Prometheus pull 型)

## 変更

- なし(初期構築)

## 削除

- なし(初期構築)
