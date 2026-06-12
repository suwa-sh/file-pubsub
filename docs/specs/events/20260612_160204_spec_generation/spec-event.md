# Spec Event Summary

## Overview

| 項目 | 内容 |
|------|------|
| Event ID | 20260612_160204_spec_generation |
| Created At | 2026-06-12T17:01:58+09:00 |
| Source | 初期構築 (trigger: rdra/nfr/arch 20260612) |
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
