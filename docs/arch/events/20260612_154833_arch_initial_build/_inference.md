# アーキテクチャ推論根拠サマリ

- event_id: 20260612_154833_arch_initial_build
- created_at: 2026-06-12T15:48:33

## RDRA/NFR モデル分析結果

### 分析した RDRA 要素

| モデル | 要素数 | 主な特徴 |
|--------|--------|---------|
| BUC | 5(2 業務) | ファイル配信業務(収集して配信 / 再送)と配信基盤運用業務(運用 / 配送状況確認 / 監視)。Collect→Archive→Fan-out→リトライ/DLQ の自動サイクルが中核 |
| アクター | 2 | 配信基盤運用者(提供者・兼務)と Consumerシステム担当者(受益者・従来手段で取得) |
| 外部システム | 5 | Producerシステム(無改修)、リモートファイル領域(FTP/SFTP/SCP)、Consumerシステム(Current/Next 並行稼働)、監視基盤(Prometheus/Grafana 等) |
| 情報 | 13 | 配信構成管理(設定/Topic/Subscription/収集ソース/認証情報)、メッセージ配送管理(メッセージ/Manifest/DLQ/処理済み管理)、Archive管理(Archiveファイル)、基盤運用管理(Lock/メトリクス/ログ) |
| 状態 | 5 | メッセージ配送状態、元ファイル収集状態、Archiveファイル保持状態、Lock状態、デーモン稼働状態 |
| 条件 | 13 | Archive保存必須、全Subscription同報配信、AtomicWrite配置、二重配信防止、リトライ上限、二重起動防止、graceful shutdown、Archive保持期間、Replay記録 等 |

### 参照した NFR グレード

| カテゴリ | 平均Lv | 主な影響 |
|---------|--------|---------|
| A. 可用性 | 約1.3(重要のみ。RPO Lv4 / 運用時間 Lv4 が突出) | 非冗長・単一インスタンス。データ損失なし(Archive 保存必須)と再起動 + Replay 復旧(RTO 数時間)を設計の柱に |
| B. 性能・拡張性 | 約1.1 | オンライン応答系なし。ポーリング逐次処理 + スケールアップで数千ファイル/日に対応 |
| C. 運用・保守性 | 約2.1(運用監視時間 Lv5 / 監視範囲 Lv3) | /healthz・/metrics による 24h 自動監視 + Topic 別アプリケーション監視。Archive 即時保存をバックアップとみなす |
| D. 移行性 | 約0.7 | 新規構築・移行なし。Consumer 切替は Subscription 並行稼働機能で実施 |
| E. セキュリティ | 約0.8 | 最小実装 + 導入先責務。認証情報は環境変数/鍵推奨・平文許容、保管時暗号化なし、監査は Manifest + 構造化ログ |
| F. 環境 | 約1.0 | Linux 主対象 + macOS、Web UI なし。Go シングルバイナリ + Docker で環境非依存 |

## 設計判断サマリ

### システムアーキテクチャ

| ティア | テクノロジー候補 | confidence | 根拠 |
|--------|----------------|-----------|------|
| tier-daemon-worker(常駐デーモン) | 常駐プロセス(daemon) / ポーリングスケジューラ / 組込 HTTP サーバ | user | ニアリアルタイム収集配信サイクルの自動実行 + 監視エンドポイント公開。ユーザー確定 |
| tier-ops-cli(運用 CLI) | CLI(サブコマンド) | user | serve / status / replay / 設定検証。同一バイナリで usecase 層を共有。ユーザー確定 |

### アプリケーションアーキテクチャ

#### tier-daemon-worker(4 層)

| レイヤー | 責務 | 依存先 | confidence |
|---------|------|--------|-----------|
| L-daemon-runtime | エントリポイント / シグナル / ポーリングスケジューラ / 組込 HTTP | L-daemon-usecase | user |
| L-daemon-usecase | 収集配信サイクル / リトライ・DLQ / retention / Replay / status。トランザクション境界 = メッセージ単位 + Manifest 更新 | L-daemon-domain, L-daemon-gateway | user |
| L-daemon-domain | モデル / 状態遷移 / 採番規則 / 安定判定 / 冪等判定 / リトライ上限判定(I/O なし) | なし | user |
| L-daemon-gateway | 収集コネクタ(共通 IF で差し替え可) / ファイルストア(AtomicWrite) / メトリクスエクスポータ | L-daemon-domain | user |

#### tier-ops-cli(1 層 + 共有)

| レイヤー | 責務 | 依存先 | confidence |
|---------|------|--------|-----------|
| L-cli-command | 引数解析 / バリデーション / 出力整形。処理本体は daemon ティアの usecase をパッケージ共有 | なし(同一バイナリのパッケージ共有は CLP-101 に明記) | user |

### データアーキテクチャ

| エンティティ | ストレージ | confidence | 根拠 |
|-------------|----------|-----------|------|
| E-001 設定 | file | user | 単一 YAML(外部 DB なし制約) |
| E-002 Topic / E-003 Subscription / E-004 収集ソース / E-005 認証情報 | file | user | 設定 YAML 内定義・参照 |
| E-006 メッセージ | file | user | Manifest と Archive のメタデータ |
| E-007 Manifest | file | user | メッセージ別 JSON(イベント追記 + 現在状態 = event_snapshot) |
| E-008 DLQ | file | user | dlq/ ディレクトリ + Manifest 記録 |
| E-009 処理済み管理 | file | user | 処理済み記録ファイル |
| E-010 Archiveファイル | file | user | archive/{topic}/ 配下 |
| E-011 Lock | file | user | lock ファイル(プロセス情報 + 取得日時) |
| E-012 メトリクス | cache | user | インメモリ。/metrics で公開、永続化しない |
| E-013 ログ | file | user | 構造化ログ(stdout/ファイル) |

## ユーザー確認による変更

| 対象 | 項目 | 推論値 | 確定値 | 変更理由 |
|------|------|--------|--------|---------|
| - | - | - | - | 本イベントはユーザー対話で確定済みの設計を初期構築として記録したもの(全項目 confidence: user で確定) |

## confidence 内訳

| セクション | high | medium | low | default | user | 合計 |
|-----------|:----:|:------:|:---:|:-------:|:----:|:----:|
| システムアーキテクチャ | 0 | 0 | 0 | 1 | 24 | 25 |
| アプリケーションアーキテクチャ | 0 | 0 | 0 | 2 | 14 | 16 |
| データアーキテクチャ | 0 | 0 | 0 | 0 | 13 | 13 |
| 合計 | 0 | 0 | 0 | 3 | 51 | 54 |
