# 要件トレーサビリティマトリクス

RDRA モデル(`docs/rdra/latest/`)の全要素を分母とし、全 19 UC の spec.md(関連 RDRA モデル / バリエーション一覧 / 分岐条件一覧 / 状態遷移一覧)を分子として網羅率を算出する。

- 分母の算出: 情報の属性 = 情報.tsv の属性列をカンマ分割した個数 / 条件 = 条件.tsv の行数 / バリエーションの値 = バリエーション.tsv の値列をカンマ分割した個数 / 状態遷移パス = 状態.tsv で「遷移UC」が非空の行数 / 外部システム連携 = 外部システム.tsv の行数
- カバー判定: UC spec.md のトレーサビリティテーブルに行が存在し、`適用 tier` が具体的に記載されていれば covered

## 網羅率サマリー

| カテゴリ | 全要素数 | カバー済み | 未カバー | 網羅率 |
|---------|:-------:|:--------:|:------:|:-----:|
| 情報の属性 | 58 | 58 | 0 | 100% |
| 条件 | 13 | 13 | 0 | 100% |
| バリエーションの値 | 18 | 18 | 0 | 100% |
| 状態遷移パス | 23 | 23 | 0 | 100% |
| 外部システム連携 | 5 | 5 | 0 | 100% |
| **合計** | **117** | **117** | **0** | **100%** |

## 情報属性マトリクス

分母: 情報.tsv の `{情報名}.{属性名}` ごと(13 情報・58 属性)

| 情報名 | 属性名 | 参照 UC | 参照 Spec ファイル | カバー状態 |
|--------|--------|--------|-----------------|:---------:|
| 設定 | ポーリング間隔 | [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) / [デーモンを起動する](<../配信基盤運用業務/配信基盤を運用するフロー/デーモンを起動する/spec.md>) | spec.md の関連 RDRA モデル + tier-ops-cli.md(設定 YAML) | covered |
| 設定 | Archive保持期間(retention) | [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) / [保持期間超過のArchiveを削除する](<../配信基盤運用業務/配信基盤を運用するフロー/保持期間超過のArchiveを削除する/spec.md>) | spec.md の関連 RDRA モデル(archive_retention の定義元) | covered |
| 設定 | リトライ上限回数 | [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) / [配信失敗をリトライしDLQへ隔離する](<../ファイル配信業務/ファイルを収集して配信するフロー/配信失敗をリトライしDLQへ隔離する/spec.md>) | spec.md の関連 RDRA モデル + 分岐条件一覧(リトライ上限) | covered |
| 設定 | メトリクスポート | [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) / [/healthzと/metricsをHTTPで公開する](<../配信基盤運用業務/配信基盤を監視するフロー/-healthzと-metricsをHTTPで公開する/spec.md>) | spec.md の関連 RDRA モデル(metrics_port の定義元) | covered |
| 設定 | Topic定義一覧 | [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) | spec.md の関連 RDRA モデル + tier-ops-cli.md(config validate) | covered |
| 設定 | Subscription定義一覧 | [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) | spec.md の関連 RDRA モデル + tier-ops-cli.md(config validate) | covered |
| 設定 | 収集ソース定義一覧 | [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) | spec.md の関連 RDRA モデル + tier-ops-cli.md(config validate) | covered |
| 設定 | 認証情報参照 | [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) | spec.md の関連 RDRA モデル + tier-ops-cli.md(`topics[].source.auth`) | covered |
| Topic | Topic名 | [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) / [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) | spec.md の関連 RDRA モデル | covered |
| Topic | 説明 | [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) | spec.md の関連 RDRA モデル | covered |
| Subscription | Subscription名 | [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) / [Subscriptionへ複製配信する(Fan-out)](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionへ複製配信する(Fan-out)/spec.md>) | spec.md の関連 RDRA モデル | covered |
| Subscription | 配置先ディレクトリパス | [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) / [Subscriptionディレクトリからファイルを取得する](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionディレクトリからファイルを取得する/spec.md>) | spec.md の関連 RDRA モデル | covered |
| Subscription | 所属Topic | [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) / [Subscriptionへ複製配信する(Fan-out)](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionへ複製配信する(Fan-out)/spec.md>) | spec.md の関連 RDRA モデル | covered |
| 収集ソース | ソース種別(FTP / SFTP / SCP / ローカルディレクトリ) | [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) / [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) | spec.md の関連 RDRA モデル + バリエーション一覧(収集ソース種別) | covered |
| 収集ソース | 接続先ホスト | [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) / [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) | spec.md の関連 RDRA モデル | covered |
| 収集ソース | 対象ディレクトリパス | [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) / [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) | spec.md の関連 RDRA モデル | covered |
| 収集ソース | 元ファイル処理方式(回収 / 残す) | [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) / [冪等に処理を再開する](<../配信基盤運用業務/配信基盤を運用するフロー/冪等に処理を再開する/spec.md>) | spec.md の関連 RDRA モデル + 分岐条件一覧(元ファイル処理判定) | covered |
| 収集ソース | 安定待ち判定設定 | [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) | spec.md の計算ルール一覧(安定判定) + 関連 RDRA モデル | covered |
| 収集ソース | 除外パターン | [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) | spec.md の計算ルール一覧(除外判定) + 関連 RDRA モデル | covered |
| 認証情報 | 記述方式(平文 / 環境変数参照 / 鍵ファイルパス) | [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) / [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) | spec.md のバリエーション一覧(認証方式) + 関連 RDRA モデル | covered |
| 認証情報 | ユーザー名 | [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) / [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) | spec.md の関連 RDRA モデル | covered |
| 認証情報 | パスワードまたは鍵ファイルパス | [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) / [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) | spec.md の関連 RDRA モデル | covered |
| メッセージ | message_id(収集時刻 + Topic + 元ファイル名から採番) | [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) | spec.md の計算ルール一覧(message_id 採番) + 分岐条件一覧(message_id採番) | covered |
| メッセージ | Topic名 | [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) / [statusコマンドで配送状態を確認する](<../配信基盤運用業務/配送状況を確認するフロー/statusコマンドで配送状態を確認する/spec.md>) | spec.md の関連 RDRA モデル | covered |
| メッセージ | 元ファイル名 | [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) / [statusコマンドで配送状態を確認する](<../配信基盤運用業務/配送状況を確認するフロー/statusコマンドで配送状態を確認する/spec.md>) | spec.md の関連 RDRA モデル | covered |
| メッセージ | 収集時刻 | [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) / [statusコマンドで配送状態を確認する](<../配信基盤運用業務/配送状況を確認するフロー/statusコマンドで配送状態を確認する/spec.md>) | spec.md の関連 RDRA モデル | covered |
| Manifest | message_id | [Subscriptionへ複製配信する(Fan-out)](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionへ複製配信する(Fan-out)/spec.md>) / [statusコマンドで配送状態を確認する](<../配信基盤運用業務/配送状況を確認するフロー/statusコマンドで配送状態を確認する/spec.md>) | spec.md の関連 RDRA モデル | covered |
| Manifest | Topic名 | [Subscriptionへ複製配信する(Fan-out)](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionへ複製配信する(Fan-out)/spec.md>) / [statusコマンドで配送状態を確認する](<../配信基盤運用業務/配送状況を確認するフロー/statusコマンドで配送状態を確認する/spec.md>) | spec.md の関連 RDRA モデル | covered |
| Manifest | Subscription別配送状態(delivered / failed / dlq) | [Subscriptionへ複製配信する(Fan-out)](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionへ複製配信する(Fan-out)/spec.md>) / [配送履歴から再送対象を確認する](<../ファイル配信業務/ファイルを再送するフロー/配送履歴から再送対象を確認する/spec.md>) | spec.md の関連 RDRA モデル + 状態遷移一覧 | covered |
| Manifest | リトライ回数 | [配信失敗をリトライしDLQへ隔離する](<../ファイル配信業務/ファイルを収集して配信するフロー/配信失敗をリトライしDLQへ隔離する/spec.md>) / [DLQ隔離メッセージを確認する](<../配信基盤運用業務/配送状況を確認するフロー/DLQ隔離メッセージを確認する/spec.md>) | spec.md の関連 RDRA モデル | covered |
| Manifest | 配送日時 | [Subscriptionへ複製配信する(Fan-out)](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionへ複製配信する(Fan-out)/spec.md>) / [statusコマンドで配送状態を確認する](<../配信基盤運用業務/配送状況を確認するフロー/statusコマンドで配送状態を確認する/spec.md>) | spec.md の関連 RDRA モデル + 状態遷移一覧(delivered 記録) | covered |
| Manifest | 再送(Replay)記録 | [再送(Replay)を実行する](<../ファイル配信業務/ファイルを再送するフロー/再送(Replay)を実行する/spec.md>) / [配送履歴から再送対象を確認する](<../ファイル配信業務/ファイルを再送するフロー/配送履歴から再送対象を確認する/spec.md>) | spec.md の関連 RDRA モデル + 分岐条件一覧(Replay記録) | covered |
| DLQ | 隔離メッセージ(message_id) | [配信失敗をリトライしDLQへ隔離する](<../ファイル配信業務/ファイルを収集して配信するフロー/配信失敗をリトライしDLQへ隔離する/spec.md>) / [DLQ隔離メッセージを確認する](<../配信基盤運用業務/配送状況を確認するフロー/DLQ隔離メッセージを確認する/spec.md>) | spec.md の関連 RDRA モデル | covered |
| DLQ | 隔離理由 | [配信失敗をリトライしDLQへ隔離する](<../ファイル配信業務/ファイルを収集して配信するフロー/配信失敗をリトライしDLQへ隔離する/spec.md>) / [DLQ隔離メッセージを確認する](<../配信基盤運用業務/配送状況を確認するフロー/DLQ隔離メッセージを確認する/spec.md>) | spec.md の関連 RDRA モデル | covered |
| DLQ | 失敗回数 | [配信失敗をリトライしDLQへ隔離する](<../ファイル配信業務/ファイルを収集して配信するフロー/配信失敗をリトライしDLQへ隔離する/spec.md>) / [DLQ隔離メッセージを確認する](<../配信基盤運用業務/配送状況を確認するフロー/DLQ隔離メッセージを確認する/spec.md>) | spec.md の関連 RDRA モデル + 分岐条件一覧(リトライ上限) | covered |
| DLQ | 隔離日時 | [配信失敗をリトライしDLQへ隔離する](<../ファイル配信業務/ファイルを収集して配信するフロー/配信失敗をリトライしDLQへ隔離する/spec.md>) / [DLQ隔離メッセージを確認する](<../配信基盤運用業務/配送状況を確認するフロー/DLQ隔離メッセージを確認する/spec.md>) | spec.md の関連 RDRA モデル | covered |
| 処理済み管理 | 収集元ファイル識別子(ファイル名・収集元パス等) | [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) / [冪等に処理を再開する](<../配信基盤運用業務/配信基盤を運用するフロー/冪等に処理を再開する/spec.md>) | spec.md の計算ルール一覧(処理済み照合) + 関連 RDRA モデル | covered |
| 処理済み管理 | 処理済み判定日時 | [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) | spec.md の関連 RDRA モデル + E2E 完了条件(copy 設定 Scenario) | covered |
| Archiveファイル | 保存先パス(Topic別) | [Archiveに保存する](<../ファイル配信業務/ファイルを収集して配信するフロー/Archiveに保存する/spec.md>) | spec.md の関連 RDRA モデル + 分岐条件一覧(Archive保存必須) | covered |
| Archiveファイル | Topic名 | [Archiveに保存する](<../ファイル配信業務/ファイルを収集して配信するフロー/Archiveに保存する/spec.md>) | spec.md の関連 RDRA モデル | covered |
| Archiveファイル | message_id | [Archiveに保存する](<../ファイル配信業務/ファイルを収集して配信するフロー/Archiveに保存する/spec.md>) / [再送(Replay)を実行する](<../ファイル配信業務/ファイルを再送するフロー/再送(Replay)を実行する/spec.md>) | spec.md の関連 RDRA モデル + 分岐条件一覧(message_id採番) | covered |
| Archiveファイル | 元ファイル名 | [Archiveに保存する](<../ファイル配信業務/ファイルを収集して配信するフロー/Archiveに保存する/spec.md>) | spec.md の関連 RDRA モデル | covered |
| Archiveファイル | ファイル内容 | [Archiveに保存する](<../ファイル配信業務/ファイルを収集して配信するフロー/Archiveに保存する/spec.md>) / [再送(Replay)を実行する](<../ファイル配信業務/ファイルを再送するフロー/再送(Replay)を実行する/spec.md>) | spec.md の関連 RDRA モデル(再送の読出元) | covered |
| Archiveファイル | 保存日時 | [Archiveに保存する](<../ファイル配信業務/ファイルを収集して配信するフロー/Archiveに保存する/spec.md>) / [保持期間超過のArchiveを削除する](<../配信基盤運用業務/配信基盤を運用するフロー/保持期間超過のArchiveを削除する/spec.md>) | spec.md の状態遷移一覧(保持期限 = 保存日時 + retention) | covered |
| Archiveファイル | 保持期限 | [Archiveに保存する](<../ファイル配信業務/ファイルを収集して配信するフロー/Archiveに保存する/spec.md>) / [保持期間超過のArchiveを削除する](<../配信基盤運用業務/配信基盤を運用するフロー/保持期間超過のArchiveを削除する/spec.md>) | spec.md の関連 RDRA モデル + 分岐条件一覧(Archive保持期間) | covered |
| Lock | ロック保持プロセス情報 | [デーモンを起動する](<../配信基盤運用業務/配信基盤を運用するフロー/デーモンを起動する/spec.md>) | spec.md の状態遷移一覧(lock ファイルに保持プロセス情報を記録) + 分岐条件一覧(二重起動防止) | covered |
| Lock | 取得日時(stale判定用) | [デーモンを起動する](<../配信基盤運用業務/配信基盤を運用するフロー/デーモンを起動する/spec.md>) / [デーモンをgraceful shutdownで停止する](<../配信基盤運用業務/配信基盤を運用するフロー/デーモンをgraceful shutdownで停止する/spec.md>) | spec.md の状態遷移一覧(Lock状態) + 関連 RDRA モデル | covered |
| メトリクス | Topic別最終収集時刻 | [/healthzと/metricsをHTTPで公開する](<../配信基盤運用業務/配信基盤を監視するフロー/-healthzと-metricsをHTTPで公開する/spec.md>) / [外部監視基盤でTopic別メトリクスを観測する](<../配信基盤運用業務/配信基盤を監視するフロー/外部監視基盤でTopic別メトリクスを観測する/spec.md>) | spec.md の関連 RDRA モデル + tier-daemon-worker.md(/metrics) | covered |
| メトリクス | 処理件数 | [/healthzと/metricsをHTTPで公開する](<../配信基盤運用業務/配信基盤を監視するフロー/-healthzと-metricsをHTTPで公開する/spec.md>) / [外部監視基盤でTopic別メトリクスを観測する](<../配信基盤運用業務/配信基盤を監視するフロー/外部監視基盤でTopic別メトリクスを観測する/spec.md>) | spec.md の関連 RDRA モデル + tier-daemon-worker.md(/metrics) | covered |
| メトリクス | 配信失敗数 | [/healthzと/metricsをHTTPで公開する](<../配信基盤運用業務/配信基盤を監視するフロー/-healthzと-metricsをHTTPで公開する/spec.md>) / [外部監視基盤でTopic別メトリクスを観測する](<../配信基盤運用業務/配信基盤を監視するフロー/外部監視基盤でTopic別メトリクスを観測する/spec.md>) | spec.md の関連 RDRA モデル + tier-daemon-worker.md(/metrics) | covered |
| メトリクス | DLQ件数 | [/healthzと/metricsをHTTPで公開する](<../配信基盤運用業務/配信基盤を監視するフロー/-healthzと-metricsをHTTPで公開する/spec.md>) / [外部監視基盤でTopic別メトリクスを観測する](<../配信基盤運用業務/配信基盤を監視するフロー/外部監視基盤でTopic別メトリクスを観測する/spec.md>) | spec.md の関連 RDRA モデル + tier-daemon-worker.md(/metrics) | covered |
| メトリクス | 滞留数 | [/healthzと/metricsをHTTPで公開する](<../配信基盤運用業務/配信基盤を監視するフロー/-healthzと-metricsをHTTPで公開する/spec.md>) / [外部監視基盤でTopic別メトリクスを観測する](<../配信基盤運用業務/配信基盤を監視するフロー/外部監視基盤でTopic別メトリクスを観測する/spec.md>) | spec.md の関連 RDRA モデル + tier-daemon-worker.md(/metrics) | covered |
| ログ | 出力日時 | [構造化ログを調査する](<../配信基盤運用業務/配送状況を確認するフロー/構造化ログを調査する/spec.md>) | spec.md の関連 RDRA モデル + tier-daemon-worker.md(ログ出力契約) | covered |
| ログ | message_id | [構造化ログを調査する](<../配信基盤運用業務/配送状況を確認するフロー/構造化ログを調査する/spec.md>) | spec.md の関連 RDRA モデル + 分岐条件一覧(CTP-001 必須フィールド) | covered |
| ログ | Topic名 | [構造化ログを調査する](<../配信基盤運用業務/配送状況を確認するフロー/構造化ログを調査する/spec.md>) | spec.md の関連 RDRA モデル + 分岐条件一覧(CTP-001 必須フィールド) | covered |
| ログ | Subscription名 | [構造化ログを調査する](<../配信基盤運用業務/配送状況を確認するフロー/構造化ログを調査する/spec.md>) | spec.md の関連 RDRA モデル + 分岐条件一覧(CTP-001 必須フィールド) | covered |
| ログ | イベント種別 | [構造化ログを調査する](<../配信基盤運用業務/配送状況を確認するフロー/構造化ログを調査する/spec.md>) | spec.md の状態遷移一覧(event_type が遷移に対応) | covered |
| ログ | エラー内容 | [構造化ログを調査する](<../配信基盤運用業務/配送状況を確認するフロー/構造化ログを調査する/spec.md>) | spec.md の関連 RDRA モデル + E2E 完了条件(配信失敗ログ Scenario) | covered |

## 条件マトリクス

分母: 条件.tsv の各条件(13 件)

| 条件名 | ルール | 適用 UC | 適用 Spec ファイル | カバー状態 |
|--------|-------|--------|-----------------|:---------:|
| 書き込み完了判定 | サイズ・更新時刻が安定するまで収集しない。除外パターン該当は対象外。リモート GET は一時名 DL 後 rename | [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) | spec.md の分岐条件一覧 | covered |
| 元ファイル処理判定 | GET 後 DELETE(回収)が既定。copy 選択時は処理済み管理と照合し再収集しない | [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) / [冪等に処理を再開する](<../配信基盤運用業務/配信基盤を運用するフロー/冪等に処理を再開する/spec.md>) | spec.md の分岐条件一覧 | covered |
| Archive保存必須 | 配信(Fan-out)前に必ず archive/ 配下へ Topic 別に保存。保存完了まで配信を開始しない | [Archiveに保存する](<../ファイル配信業務/ファイルを収集して配信するフロー/Archiveに保存する/spec.md>) | spec.md の分岐条件一覧 | covered |
| message_id採番 | 同名ファイル再出力は新メッセージ。収集時刻 + Topic + 元ファイル名から採番し履歴を失わない | [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) / [Archiveに保存する](<../ファイル配信業務/ファイルを収集して配信するフロー/Archiveに保存する/spec.md>) | spec.md の分岐条件一覧 | covered |
| 全Subscription同報配信 | Topic の全 Subscription ディレクトリへ同一内容を複製。配送は Subscription ごとに独立 | [Subscriptionへ複製配信する(Fan-out)](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionへ複製配信する(Fan-out)/spec.md>) / [Subscriptionディレクトリからファイルを取得する](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionディレクトリからファイルを取得する/spec.md>) | spec.md の分岐条件一覧 | covered |
| Fan-out処理順序 | 順序保証はせずファイル名昇順で処理。取り込み順序の制御は Consumer の責任 | [Subscriptionへ複製配信する(Fan-out)](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionへ複製配信する(Fan-out)/spec.md>) | spec.md の分岐条件一覧 | covered |
| AtomicWrite配置 | 一時名で書き込み後 rename。正式名ファイルは常に完全な内容を保証 | [Subscriptionへ複製配信する(Fan-out)](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionへ複製配信する(Fan-out)/spec.md>) / [Subscriptionディレクトリからファイルを取得する](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionディレクトリからファイルを取得する/spec.md>) / [再送(Replay)を実行する](<../ファイル配信業務/ファイルを再送するフロー/再送(Replay)を実行する/spec.md>) | spec.md の分岐条件一覧 | covered |
| 二重配信防止 | 再起動・中断後の再開は Manifest の配送状態を参照し未配信 Subscription にのみ配信 | [Subscriptionへ複製配信する(Fan-out)](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionへ複製配信する(Fan-out)/spec.md>) / [冪等に処理を再開する](<../配信基盤運用業務/配信基盤を運用するフロー/冪等に処理を再開する/spec.md>) | spec.md の分岐条件一覧 | covered |
| リトライ上限 | 規定回数以内のリトライで delivered、超過は DLQ へ隔離し Manifest に dlq 記録 | [配信失敗をリトライしDLQへ隔離する](<../ファイル配信業務/ファイルを収集して配信するフロー/配信失敗をリトライしDLQへ隔離する/spec.md>) / [DLQ隔離メッセージを確認する](<../配信基盤運用業務/配送状況を確認するフロー/DLQ隔離メッセージを確認する/spec.md>) | spec.md の分岐条件一覧 | covered |
| 二重起動防止 | 起動時に Lock を取得し 2 つ目のデーモンは起動せず終了。stale lock からは安全に回復 | [デーモンを起動する](<../配信基盤運用業務/配信基盤を運用するフロー/デーモンを起動する/spec.md>) / [冪等に処理を再開する](<../配信基盤運用業務/配信基盤を運用するフロー/冪等に処理を再開する/spec.md>) | spec.md の分岐条件一覧 | covered |
| graceful shutdown | 停止シグナル受信後、処理中メッセージを完了してから停止。中途半端な状態を残さない | [デーモンをgraceful shutdownで停止する](<../配信基盤運用業務/配信基盤を運用するフロー/デーモンをgraceful shutdownで停止する/spec.md>) | spec.md の分岐条件一覧 | covered |
| Archive保持期間 | retention 処理は保持期間を超過した Archive ファイルだけを安全に削除 | [保持期間超過のArchiveを削除する](<../配信基盤運用業務/配信基盤を運用するフロー/保持期間超過のArchiveを削除する/spec.md>) | spec.md の分岐条件一覧 | covered |
| Replay記録 | 再送は Topic・期間(またはメッセージ指定)・宛先 Subscription を指定し、指定先にのみ再配置。履歴は Manifest に記録 | [再送(Replay)を実行する](<../ファイル配信業務/ファイルを再送するフロー/再送(Replay)を実行する/spec.md>) / [Subscriptionディレクトリから再送ファイルを取得する](<../ファイル配信業務/ファイルを再送するフロー/Subscriptionディレクトリから再送ファイルを取得する/spec.md>) | spec.md の分岐条件一覧 | covered |

## バリエーションマトリクス

分母: バリエーション.tsv の `{バリエーション名}.{値}` ごと(7 種・18 値)

| バリエーション名 | 値 | 適用 UC | 適用 Spec ファイル | カバー状態 |
|----------------|---|--------|-----------------|:---------:|
| 収集ソース種別 | FTP | [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) / [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) | spec.md のバリエーション一覧 | covered |
| 収集ソース種別 | SFTP | [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) / [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) | spec.md のバリエーション一覧 + E2E 完了条件(SFTP Scenario) | covered |
| 収集ソース種別 | SCP | [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) / [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) | spec.md のバリエーション一覧 | covered |
| 収集ソース種別 | ローカルディレクトリ | [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) / [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) | spec.md のバリエーション一覧 + E2E 完了条件(ローカル収集 Scenario) | covered |
| 元ファイル処理方式 | 回収(GET後DELETE) | [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) / [冪等に処理を再開する](<../配信基盤運用業務/配信基盤を運用するフロー/冪等に処理を再開する/spec.md>) | spec.md のバリエーション一覧 | covered |
| 元ファイル処理方式 | 残す(copy) | [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) / [冪等に処理を再開する](<../配信基盤運用業務/配信基盤を運用するフロー/冪等に処理を再開する/spec.md>) | spec.md のバリエーション一覧 | covered |
| Subscription種別 | current | [Subscriptionへ複製配信する(Fan-out)](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionへ複製配信する(Fan-out)/spec.md>) / [Subscriptionディレクトリからファイルを取得する](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionディレクトリからファイルを取得する/spec.md>) | spec.md のバリエーション一覧 | covered |
| Subscription種別 | next | [Subscriptionへ複製配信する(Fan-out)](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionへ複製配信する(Fan-out)/spec.md>) / [再送(Replay)を実行する](<../ファイル配信業務/ファイルを再送するフロー/再送(Replay)を実行する/spec.md>) | spec.md のバリエーション一覧 | covered |
| Subscription種別 | test | [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) / [Subscriptionへ複製配信する(Fan-out)](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionへ複製配信する(Fan-out)/spec.md>) | spec.md のバリエーション一覧 | covered |
| Consumer取り込みタイミング | 即時取り込み | [Subscriptionディレクトリからファイルを取得する](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionディレクトリからファイルを取得する/spec.md>) / [Subscriptionへ複製配信する(Fan-out)](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionへ複製配信する(Fan-out)/spec.md>) | spec.md のバリエーション一覧 | covered |
| Consumer取り込みタイミング | 夜間バッチ | [Subscriptionディレクトリからファイルを取得する](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionディレクトリからファイルを取得する/spec.md>) / [Subscriptionへ複製配信する(Fan-out)](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionへ複製配信する(Fan-out)/spec.md>) | spec.md のバリエーション一覧 | covered |
| 配信方式 | 通常配信(Fan-out) | [Subscriptionへ複製配信する(Fan-out)](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionへ複製配信する(Fan-out)/spec.md>) / [冪等に処理を再開する](<../配信基盤運用業務/配信基盤を運用するフロー/冪等に処理を再開する/spec.md>) | spec.md のバリエーション一覧 | covered |
| 配信方式 | 再送(Replay) | [再送(Replay)を実行する](<../ファイル配信業務/ファイルを再送するフロー/再送(Replay)を実行する/spec.md>) / [配送履歴から再送対象を確認する](<../ファイル配信業務/ファイルを再送するフロー/配送履歴から再送対象を確認する/spec.md>) | spec.md のバリエーション一覧 | covered |
| 認証方式 | YAML平文記述 | [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) / [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) | spec.md のバリエーション一覧 | covered |
| 認証方式 | 環境変数参照(${ENV_VAR}) | [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) / [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) | spec.md のバリエーション一覧 | covered |
| 認証方式 | 鍵ファイルパス指定 | [Topic・Subscriptionを設定する](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) / [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) | spec.md のバリエーション一覧 | covered |
| 配布形態 | シングルバイナリ | [シングルバイナリ/Dockerイメージを配置する](<../配信基盤運用業務/配信基盤を運用するフロー/シングルバイナリ-Dockerイメージを配置する/spec.md>) | spec.md のバリエーション一覧 | covered |
| 配布形態 | Dockerコンテナイメージ | [シングルバイナリ/Dockerイメージを配置する](<../配信基盤運用業務/配信基盤を運用するフロー/シングルバイナリ-Dockerイメージを配置する/spec.md>) | spec.md のバリエーション一覧 | covered |

## 状態遷移マトリクス

分母: 状態.tsv の `{状態モデル}: {遷移元} → {遷移先}` のうち「遷移UC」が非空の行(5 モデル・23 パス)。遷移UCが空の行(終了状態 4 行、Producer 出力による「(初期)→書き込み中」、異常終了による「取得済→stale」)はシステムの UC が遷移させないため分母に含めない。

| 状態モデル | 遷移元 | 遷移先 | 適用 UC | 適用 Spec ファイル | カバー状態 |
|-----------|--------|--------|--------|-----------------|:---------:|
| メッセージ配送状態 | (初期) | 収集済 | [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) | spec.md の状態遷移一覧 | covered |
| メッセージ配送状態 | 収集済 | Archive保存済 | [Archiveに保存する](<../ファイル配信業務/ファイルを収集して配信するフロー/Archiveに保存する/spec.md>) / [冪等に処理を再開する](<../配信基盤運用業務/配信基盤を運用するフロー/冪等に処理を再開する/spec.md>) | spec.md の状態遷移一覧 | covered |
| メッセージ配送状態 | Archive保存済 | 配信中 | [Subscriptionへ複製配信する(Fan-out)](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionへ複製配信する(Fan-out)/spec.md>) / [冪等に処理を再開する](<../配信基盤運用業務/配信基盤を運用するフロー/冪等に処理を再開する/spec.md>) | spec.md の状態遷移一覧 | covered |
| メッセージ配送状態 | 配信中 | 配信済(delivered) | [Subscriptionへ複製配信する(Fan-out)](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionへ複製配信する(Fan-out)/spec.md>) / [デーモンをgraceful shutdownで停止する](<../配信基盤運用業務/配信基盤を運用するフロー/デーモンをgraceful shutdownで停止する/spec.md>) | spec.md の状態遷移一覧 | covered |
| メッセージ配送状態 | 配信中 | 配信失敗(failed) | [Subscriptionへ複製配信する(Fan-out)](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionへ複製配信する(Fan-out)/spec.md>) / [デーモンをgraceful shutdownで停止する](<../配信基盤運用業務/配信基盤を運用するフロー/デーモンをgraceful shutdownで停止する/spec.md>) | spec.md の状態遷移一覧 | covered |
| メッセージ配送状態 | 配信失敗(failed) | リトライ中 | [配信失敗をリトライしDLQへ隔離する](<../ファイル配信業務/ファイルを収集して配信するフロー/配信失敗をリトライしDLQへ隔離する/spec.md>) | spec.md の状態遷移一覧 | covered |
| メッセージ配送状態 | リトライ中 | 配信中 | [配信失敗をリトライしDLQへ隔離する](<../ファイル配信業務/ファイルを収集して配信するフロー/配信失敗をリトライしDLQへ隔離する/spec.md>) | spec.md の状態遷移一覧 | covered |
| メッセージ配送状態 | リトライ中 | DLQ隔離(dlq) | [配信失敗をリトライしDLQへ隔離する](<../ファイル配信業務/ファイルを収集して配信するフロー/配信失敗をリトライしDLQへ隔離する/spec.md>) | spec.md の状態遷移一覧 | covered |
| メッセージ配送状態 | DLQ隔離(dlq) | 配信中 | [再送(Replay)を実行する](<../ファイル配信業務/ファイルを再送するフロー/再送(Replay)を実行する/spec.md>) | spec.md の状態遷移一覧 | covered |
| メッセージ配送状態 | 配信済(delivered) | 配信中 | [再送(Replay)を実行する](<../ファイル配信業務/ファイルを再送するフロー/再送(Replay)を実行する/spec.md>) | spec.md の状態遷移一覧 | covered |
| デーモン稼働状態 | (初期) | 起動中 | [デーモンを起動する](<../配信基盤運用業務/配信基盤を運用するフロー/デーモンを起動する/spec.md>) | spec.md の状態遷移一覧 | covered |
| デーモン稼働状態 | 起動中 | 稼働中 | [デーモンを起動する](<../配信基盤運用業務/配信基盤を運用するフロー/デーモンを起動する/spec.md>) | spec.md の状態遷移一覧 | covered |
| デーモン稼働状態 | 起動中 | 停止済 | [デーモンを起動する](<../配信基盤運用業務/配信基盤を運用するフロー/デーモンを起動する/spec.md>) | spec.md の状態遷移一覧 | covered |
| デーモン稼働状態 | 稼働中 | 停止処理中 | [デーモンをgraceful shutdownで停止する](<../配信基盤運用業務/配信基盤を運用するフロー/デーモンをgraceful shutdownで停止する/spec.md>) | spec.md の状態遷移一覧 | covered |
| デーモン稼働状態 | 停止処理中 | 停止済 | [デーモンをgraceful shutdownで停止する](<../配信基盤運用業務/配信基盤を運用するフロー/デーモンをgraceful shutdownで停止する/spec.md>) | spec.md の状態遷移一覧 | covered |
| Archiveファイル保持状態 | (初期) | 保持中 | [Archiveに保存する](<../ファイル配信業務/ファイルを収集して配信するフロー/Archiveに保存する/spec.md>) | spec.md の状態遷移一覧 | covered |
| Archiveファイル保持状態 | 保持中 | 削除済 | [保持期間超過のArchiveを削除する](<../配信基盤運用業務/配信基盤を運用するフロー/保持期間超過のArchiveを削除する/spec.md>) | spec.md の状態遷移一覧 | covered |
| 元ファイル収集状態 | 書き込み中 | 収集可能 | [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) | spec.md の状態遷移一覧 | covered |
| 元ファイル収集状態 | 収集可能 | 回収済 | [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) | spec.md の状態遷移一覧 | covered |
| 元ファイル収集状態 | 収集可能 | 残置済 | [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) | spec.md の状態遷移一覧 | covered |
| Lock状態 | (初期) | 取得済 | [デーモンを起動する](<../配信基盤運用業務/配信基盤を運用するフロー/デーモンを起動する/spec.md>) | spec.md の状態遷移一覧 | covered |
| Lock状態 | 取得済 | 解放済 | [デーモンをgraceful shutdownで停止する](<../配信基盤運用業務/配信基盤を運用するフロー/デーモンをgraceful shutdownで停止する/spec.md>) | spec.md の状態遷移一覧 | covered |
| Lock状態 | stale | 取得済 | [デーモンを起動する](<../配信基盤運用業務/配信基盤を運用するフロー/デーモンを起動する/spec.md>) | spec.md の状態遷移一覧 | covered |

## 外部システム連携マトリクス

分母: 外部システム.tsv の各外部システム(5 件)

| 外部システム名 | 役割 | 連携 UC | 連携 Spec ファイル | カバー状態 |
|-------------|------|--------|-----------------|:---------:|
| Producerシステム | レガシーファイル IF のファイルを出力し続ける連携元(無改修前提) | [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) | spec.md の関連 RDRA モデル(イベント「出力ファイル受け渡し」) + tier-daemon-worker.md | covered |
| リモートファイル領域 | Producer 出力ファイルが置かれる FTP/SFTP/SCP サーバ上の収集元 | [ファイルを収集する(Collect)](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) | spec.md の関連 RDRA モデル(イベント「ファイル取得」) + tier-daemon-worker.md(収集コネクタ) | covered |
| Consumerシステム(Current) | subscriptions/current から従来手段でファイルを取得する現行システム | [Subscriptionディレクトリからファイルを取得する](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionディレクトリからファイルを取得する/spec.md>) / [Subscriptionディレクトリから再送ファイルを取得する](<../ファイル配信業務/ファイルを再送するフロー/Subscriptionディレクトリから再送ファイルを取得する/spec.md>) | spec.md の関連 RDRA モデル(イベント「配信ファイル受け渡し」「再送ファイル受け渡し」) | covered |
| Consumerシステム(Next) | subscriptions/next から取得し Current と並行稼働する更改後システム | [Subscriptionディレクトリからファイルを取得する](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionディレクトリからファイルを取得する/spec.md>) / [Subscriptionディレクトリから再送ファイルを取得する](<../ファイル配信業務/ファイルを再送するフロー/Subscriptionディレクトリから再送ファイルを取得する/spec.md>) | spec.md の関連 RDRA モデル(イベント「配信ファイル受け渡し」「再送ファイル受け渡し」) | covered |
| 監視基盤 | /healthz 死活監視・/metrics しきい値判定・アラート発報(Prometheus/Grafana 等) | [/healthzと/metricsをHTTPで公開する](<../配信基盤運用業務/配信基盤を監視するフロー/-healthzと-metricsをHTTPで公開する/spec.md>) / [外部監視基盤でTopic別メトリクスを観測する](<../配信基盤運用業務/配信基盤を監視するフロー/外部監視基盤でTopic別メトリクスを観測する/spec.md>) | spec.md の関連 RDRA モデル(イベント「監視データ提供」「アラート通知」) + tier-daemon-worker.md | covered |

## 未カバー要素一覧

全カテゴリで網羅率 100%(117/117)のため、未カバー要素はない。RDRA 側の見直しが必要な要素も検出されなかった(rdra-feedback.md は作成しない)。

| カテゴリ | 要素 | 想定される理由 | 対応方針 |
|---------|------|-------------|---------|
| (なし) | - | - | - |

## BUC ↔ UC 対応表

分母: BUC.tsv の 5 BUC × 19 UC(関係 1:N)

| 業務 | BUC | UC | BUC Spec | UC Spec |
|------|-----|----|----------|---------|
| ファイル配信業務 | ファイルを収集して配信するフロー | Topic・Subscriptionを設定する | [buc-spec.md](<../ファイル配信業務/ファイルを収集して配信するフロー/buc-spec.md>) | [spec.md](<../ファイル配信業務/ファイルを収集して配信するフロー/Topic・Subscriptionを設定する/spec.md>) |
| ファイル配信業務 | ファイルを収集して配信するフロー | ファイルを収集する(Collect) | [buc-spec.md](<../ファイル配信業務/ファイルを収集して配信するフロー/buc-spec.md>) | [spec.md](<../ファイル配信業務/ファイルを収集して配信するフロー/ファイルを収集する(Collect)/spec.md>) |
| ファイル配信業務 | ファイルを収集して配信するフロー | Archiveに保存する | [buc-spec.md](<../ファイル配信業務/ファイルを収集して配信するフロー/buc-spec.md>) | [spec.md](<../ファイル配信業務/ファイルを収集して配信するフロー/Archiveに保存する/spec.md>) |
| ファイル配信業務 | ファイルを収集して配信するフロー | Subscriptionへ複製配信する(Fan-out) | [buc-spec.md](<../ファイル配信業務/ファイルを収集して配信するフロー/buc-spec.md>) | [spec.md](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionへ複製配信する(Fan-out)/spec.md>) |
| ファイル配信業務 | ファイルを収集して配信するフロー | 配信失敗をリトライしDLQへ隔離する | [buc-spec.md](<../ファイル配信業務/ファイルを収集して配信するフロー/buc-spec.md>) | [spec.md](<../ファイル配信業務/ファイルを収集して配信するフロー/配信失敗をリトライしDLQへ隔離する/spec.md>) |
| ファイル配信業務 | ファイルを収集して配信するフロー | Subscriptionディレクトリからファイルを取得する | [buc-spec.md](<../ファイル配信業務/ファイルを収集して配信するフロー/buc-spec.md>) | [spec.md](<../ファイル配信業務/ファイルを収集して配信するフロー/Subscriptionディレクトリからファイルを取得する/spec.md>) |
| ファイル配信業務 | ファイルを再送するフロー | 配送履歴から再送対象を確認する | [buc-spec.md](<../ファイル配信業務/ファイルを再送するフロー/buc-spec.md>) | [spec.md](<../ファイル配信業務/ファイルを再送するフロー/配送履歴から再送対象を確認する/spec.md>) |
| ファイル配信業務 | ファイルを再送するフロー | 再送(Replay)を実行する | [buc-spec.md](<../ファイル配信業務/ファイルを再送するフロー/buc-spec.md>) | [spec.md](<../ファイル配信業務/ファイルを再送するフロー/再送(Replay)を実行する/spec.md>) |
| ファイル配信業務 | ファイルを再送するフロー | Subscriptionディレクトリから再送ファイルを取得する | [buc-spec.md](<../ファイル配信業務/ファイルを再送するフロー/buc-spec.md>) | [spec.md](<../ファイル配信業務/ファイルを再送するフロー/Subscriptionディレクトリから再送ファイルを取得する/spec.md>) |
| 配信基盤運用業務 | 配信基盤を運用するフロー | シングルバイナリ/Dockerイメージを配置する | [buc-spec.md](<../配信基盤運用業務/配信基盤を運用するフロー/buc-spec.md>) | [spec.md](<../配信基盤運用業務/配信基盤を運用するフロー/シングルバイナリ-Dockerイメージを配置する/spec.md>) |
| 配信基盤運用業務 | 配信基盤を運用するフロー | デーモンを起動する | [buc-spec.md](<../配信基盤運用業務/配信基盤を運用するフロー/buc-spec.md>) | [spec.md](<../配信基盤運用業務/配信基盤を運用するフロー/デーモンを起動する/spec.md>) |
| 配信基盤運用業務 | 配信基盤を運用するフロー | デーモンをgraceful shutdownで停止する | [buc-spec.md](<../配信基盤運用業務/配信基盤を運用するフロー/buc-spec.md>) | [spec.md](<../配信基盤運用業務/配信基盤を運用するフロー/デーモンをgraceful shutdownで停止する/spec.md>) |
| 配信基盤運用業務 | 配信基盤を運用するフロー | 保持期間超過のArchiveを削除する | [buc-spec.md](<../配信基盤運用業務/配信基盤を運用するフロー/buc-spec.md>) | [spec.md](<../配信基盤運用業務/配信基盤を運用するフロー/保持期間超過のArchiveを削除する/spec.md>) |
| 配信基盤運用業務 | 配信基盤を運用するフロー | 冪等に処理を再開する | [buc-spec.md](<../配信基盤運用業務/配信基盤を運用するフロー/buc-spec.md>) | [spec.md](<../配信基盤運用業務/配信基盤を運用するフロー/冪等に処理を再開する/spec.md>) |
| 配信基盤運用業務 | 配送状況を確認するフロー | statusコマンドで配送状態を確認する | [buc-spec.md](<../配信基盤運用業務/配送状況を確認するフロー/buc-spec.md>) | [spec.md](<../配信基盤運用業務/配送状況を確認するフロー/statusコマンドで配送状態を確認する/spec.md>) |
| 配信基盤運用業務 | 配送状況を確認するフロー | DLQ隔離メッセージを確認する | [buc-spec.md](<../配信基盤運用業務/配送状況を確認するフロー/buc-spec.md>) | [spec.md](<../配信基盤運用業務/配送状況を確認するフロー/DLQ隔離メッセージを確認する/spec.md>) |
| 配信基盤運用業務 | 配送状況を確認するフロー | 構造化ログを調査する | [buc-spec.md](<../配信基盤運用業務/配送状況を確認するフロー/buc-spec.md>) | [spec.md](<../配信基盤運用業務/配送状況を確認するフロー/構造化ログを調査する/spec.md>) |
| 配信基盤運用業務 | 配信基盤を監視するフロー | /healthzと/metricsをHTTPで公開する | [buc-spec.md](<../配信基盤運用業務/配信基盤を監視するフロー/buc-spec.md>) | [spec.md](<../配信基盤運用業務/配信基盤を監視するフロー/-healthzと-metricsをHTTPで公開する/spec.md>) |
| 配信基盤運用業務 | 配信基盤を監視するフロー | 外部監視基盤でTopic別メトリクスを観測する | [buc-spec.md](<../配信基盤運用業務/配信基盤を監視するフロー/buc-spec.md>) | [spec.md](<../配信基盤運用業務/配信基盤を監視するフロー/外部監視基盤でTopic別メトリクスを観測する/spec.md>) |
