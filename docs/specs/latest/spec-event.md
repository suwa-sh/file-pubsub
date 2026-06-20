# Spec Event Summary

## Overview

| 項目 | 内容 |
|------|------|
| Event ID | 20260620_171535_add_redundant_failover |
| Created At | 2026-06-20T17:15:35+09:00 |
| Source | 初期構築 20260612_160204 → push 受信モード追加 20260617_020637 → completion ネスト化 20260617_081425 → 冪等性ハードニング(message_id 同一秒衝突の連番回避 SPEC-007-01・marker+copy 残存マーカー SPEC-014-02)20260617_121332 → 冗長構成(active/standby 自動フェイルオーバー)対応(Lock の lease レコード化 SPEC-015-01・唯一性保証の方式A/方式B 併用 SPEC-015-02/03・split-brain 被害の高々1メッセージ重複限定 SPEC-016-01・NFS/NTP/TTL 制約 SPEC-017・単一インスタンス前提の見直し SPEC-018-01)20260620_171535 (trigger: rdra 20260620_171535_add_redundant_failover) + codex レビュー対応(案A: split-brain 重複上限 REQ-016「高々1メッセージ」をメッセージ境界 lease 確認 + Manifest read-merge-write で実装維持 spec-decision-011・方式A 起動モデルの一意化・stale lease 奪取の原子性 read→remove→O_CREATE\|O_EXCL・heartbeat 自発降格境界・boot-id 用途・lock=lease 構造化スキーマ・high_availability.* キー統一と既定値/後方互換 #3-13) + codex 再レビュー対応(2巡目: 案A を read-merge-write + 世代 CAS + 競合リトライ へ精緻化 B1・decisions 役割分担 010=受動的被害限定/011=能動的上限担保 #9・方式A を standby 状態遷移から除去 #4・Heartbeat 所有者検証で旧 active の lease 奪い返し阻止・cross-cutting コンポーネント契約 ConfigLoader HighAvailability/ManifestStore PutMerged #12/#13・メッセージ境界 lease 確認の永続化点を原本 delete/MarkProcessed へ拡張・ui-design split-brain 参照 #7・冪等再開 UC で C-08 使用 #8) + codex 3巡目対応(案Z: Manifest 更新を message_id 単位の更新ロック O_CREATE\|O_EXCL で直列化[主機構]・lease に generation 追加し heartbeat を generation CAS で TOCTOU 検出・NFS で完全な分散排他は不能という既知制約を spec/decision/schema に明記[exactly-once 非保証・被害限定]・方式A 起動 sequence と arch 図 ACT->STBY を方式B/方式A の 2 経路に分離・起動 UC の降格境界永続化点に原本 delete/MarkProcessed 追加・decision-011 に failed merge precedence=delivered/dlq 決着上書き不可 を定義)の累積スナップショット(spec は差分スタイル) |
| UC 総数 | 19 |
| API 総数 | 2 |
| 非同期イベント総数 | 0 |
| 業務数 | 2 |
| BUC 数 | 5 |

## UC 一覧

| 業務 | BUC | UC | API数 | 非同期 | インフラ |
|------|-----|-----|:-----:|:-----:|:-------:|
| ファイル配信業務 | ファイルを収集して配信するフロー | Topic・Subscriptionを設定する | 0 | - | - |
| ファイル配信業務 | ファイルを収集して配信するフロー | ファイルを収集する(Collect) | 0 | - | - |
| ファイル配信業務 | ファイルを収集して配信するフロー | Archiveに保存する | 0 | - | - |
| ファイル配信業務 | ファイルを収集して配信するフロー | Subscriptionへ複製配信する(Fan-out) | 0 | - | - |
| ファイル配信業務 | ファイルを収集して配信するフロー | 配信失敗をリトライしDLQへ隔離する | 0 | - | - |
| ファイル配信業務 | ファイルを収集して配信するフロー | Subscriptionディレクトリからファイルを取得する | 0 | - | - |
| ファイル配信業務 | ファイルを再送するフロー | 配送履歴から再送対象を確認する | 0 | - | - |
| ファイル配信業務 | ファイルを再送するフロー | 再送(Replay)を実行する | 0 | - | - |
| ファイル配信業務 | ファイルを再送するフロー | Subscriptionディレクトリから再送ファイルを取得する | 0 | - | - |
| 配信基盤運用業務 | 配信基盤を運用するフロー | シングルバイナリ-Dockerイメージを配置する | 0 | - | - |
| 配信基盤運用業務 | 配信基盤を運用するフロー | デーモンを起動する | 0 | - | - |
| 配信基盤運用業務 | 配信基盤を運用するフロー | 冪等に処理を再開する | 0 | - | - |
| 配信基盤運用業務 | 配信基盤を運用するフロー | デーモンをgraceful shutdownで停止する | 0 | - | - |
| 配信基盤運用業務 | 配信基盤を運用するフロー | 保持期間超過のArchiveを削除する | 0 | - | - |
| 配信基盤運用業務 | 配信基盤を監視するフロー | -healthzと-metricsをHTTPで公開する | 2 | - | - |
| 配信基盤運用業務 | 配信基盤を監視するフロー | 外部監視基盤でTopic別メトリクスを観測する | 0 | - | - |
| 配信基盤運用業務 | 配送状況を確認するフロー | statusコマンドで配送状態を確認する | 0 | - | - |
| 配信基盤運用業務 | 配送状況を確認するフロー | DLQ隔離メッセージを確認する | 0 | - | - |
| 配信基盤運用業務 | 配送状況を確認するフロー | 構造化ログを調査する | 0 | - | - |

## UC ファイル構成

### ファイル配信業務

#### ファイルを収集して配信するフロー

- **Topic・Subscriptionを設定する**: spec.md, tier-ops-cli.md
- **ファイルを収集する(Collect)**: spec.md, tier-daemon-worker.md
- **Archiveに保存する**: spec.md, tier-daemon-worker.md
- **Subscriptionへ複製配信する(Fan-out)**: spec.md, tier-daemon-worker.md
- **配信失敗をリトライしDLQへ隔離する**: spec.md, tier-daemon-worker.md
- **Subscriptionディレクトリからファイルを取得する**: spec.md, tier-daemon-worker.md

#### ファイルを再送するフロー

- **配送履歴から再送対象を確認する**: spec.md, tier-ops-cli.md
- **再送(Replay)を実行する**: spec.md, tier-ops-cli.md
- **Subscriptionディレクトリから再送ファイルを取得する**: spec.md, tier-daemon-worker.md

### 配信基盤運用業務

#### 配信基盤を運用するフロー

- **シングルバイナリ-Dockerイメージを配置する**: spec.md, tier-ops-cli.md
- **デーモンを起動する**: spec.md, tier-daemon-worker.md, tier-ops-cli.md
- **冪等に処理を再開する**: spec.md, tier-daemon-worker.md
- **デーモンをgraceful shutdownで停止する**: spec.md, tier-daemon-worker.md
- **保持期間超過のArchiveを削除する**: spec.md, tier-daemon-worker.md

#### 配信基盤を監視するフロー

- **-healthzと-metricsをHTTPで公開する**: spec.md, tier-daemon-worker.md
- **外部監視基盤でTopic別メトリクスを観測する**: spec.md, tier-daemon-worker.md

#### 配送状況を確認するフロー

- **statusコマンドで配送状態を確認する**: spec.md, tier-ops-cli.md
- **DLQ隔離メッセージを確認する**: spec.md, tier-ops-cli.md
- **構造化ログを調査する**: spec.md, tier-daemon-worker.md

## 全体横断仕様

### UX Design

- User Flows: 3
- IA Pages: 4
- Psychology Principles: 5

### UI Design

- Layout Patterns: 4
- Responsive Breakpoints: 0
- Component Guidelines: 4

### Data Visualization

- Target Screens: 1
- Chart Types: 5
