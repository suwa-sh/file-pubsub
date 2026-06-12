# USDM 要求仕様書

- システム名: file-pubsub
- イベントID: 20260612_150425_initial_build
- 作成日時: 2026-06-12T15:04:25
- ソース: 初期要望.txt (file-pubsub 初期要望 改訂版。構想メモを壁打ちで見直したもの)

| ID | 要求 | 仕様 | 理由（背景） | 説明 |
|----|------|------|------------|------|
| REQ-001 | FTP GET/DELETE 型のレガシーファイル IF を、Producer を変更せずに複数 Consumer へ同報配信できる Pub/Sub 風の配信モデルへ変換したい | | Consumer 側システムの更改で Current/Next の並行稼働が必要だが、FTP GET+DELETE 方式は先に取得した Consumer がファイルを削除するため並行稼働できない。他のファイル IF はファイル転送基盤で同報配信できており、FTP GET 型だけが例外。Producer は変更したくない | |
| SPEC-001-01 | | Topic(Producer から出力される論理的なファイル種別。例: orders / customers / invoices)と Subscription(Consumer ごとの配送先。例: current / next / test)の 2 概念で配信構成を単一 YAML 設定で定義できる | | 1. Given 設定 YAML に Topic と複数の Subscription が定義されている When 構成を読み込む Then Topic ごとに Subscription の一覧が解決される<br>2. Given Subscription を追加する設定変更 When 構成を再読み込みする Then Producer 側の変更なしに新しい配送先が増える |
| SPEC-001-02 | | Archive に保存したファイルを Subscription ごとのローカルディレクトリ(subscriptions/current, subscriptions/next 等)へ複製する(Fan-out)。配信先は file-pubsub 稼働サーバ上のローカルディレクトリのみで、Consumer が従来手段(FTP GET 等)で取りに来る | | 1. Given Topic に新しいファイルが収集された When Fan-out が実行される Then 全 Subscription のディレクトリに同一内容のファイルが配置される<br>2. Given Current と Next の 2 つの Subscription When 一方がファイルを取り込んで削除する Then 他方の Subscription のファイルには影響しない |
| SPEC-001-03 | | Subscription ごとに配送が独立しており、Consumer の取り込みタイミングの差(即時取り込み / 夜間バッチ等)を吸収できる。メッセージの順序保証はせず、Fan-out 配置はファイル名昇順で処理する(取り込み順序は Consumer の責任) | | 1. Given Current が即時取り込み・DWH が夜間取り込み When それぞれが自分のペースでファイルを取得・削除する Then 互いの配送に影響しない |
| REQ-002 | Producer が出力するファイルを、リモートサーバ(FTP / SFTP / SCP)およびローカルディレクトリから収集(Collect)したい | | 実際のレガシー IF は Producer が FTP 領域へ出力する方式であり、リモート収集が本来の収集要件。プロトコルは現場により FTP / SFTP / SCP が混在する。収集と後段(Archive / Fan-out / Manifest)を分離し、後段をソース種別に依存させないことで構成の自由度を保つ | |
| SPEC-002-01 | | 収集ソースを Topic ごとに設定で切り替えられる(FTP / SFTP / SCP / ローカルディレクトリ)。後段(Archive / Fan-out / Manifest)はソース種別に依存しない | | 1. Given Topic の収集ソースが FTP 領域に設定されている When Collect が実行される Then ファイルが取得され Archive へ保存される<br>2. Given 収集ソース種別を ftp から sftp へ設定変更する When Collect を実行する Then 後段(Archive / Fan-out / Manifest)は変更なしに動作する |
| SPEC-002-02 | | 収集後の元ファイルは GET 後 DELETE(回収)が既定。Topic 設定で「残す(copy)」も選択でき、copy 時は処理済み管理で同じファイルの重複収集を防ぐ | | 1. Given 既定設定の Topic When Collect が完了する Then 収集元からファイルが削除されている<br>2. Given copy 設定の Topic When 同じファイルが残ったまま次の Collect が実行される Then 処理済みのファイルは再収集されない |
| SPEC-002-03 | | Producer が書き込み中のファイルを収集しない(サイズ/更新時刻の安定待ち、除外パターン)。リモート GET 中も一時名でダウンロードし完了後 rename して、Archive に途中状態を残さない | | 1. Given Producer がファイルを書き込み中 When Collect が実行される Then 該当ファイルは収集されず次回以降に持ち越される<br>2. Given リモート GET が中断された When Archive を参照する Then 途中状態のファイルは存在しない |
| REQ-003 | ポーリング間隔を設定できる常駐デーモンとして安全に動作させたい | | Producer はニアリアルタイムでファイルを出力し続けるため、定期的な収集・配信の自動実行が必要。単一インスタンス前提のため二重起動の防止と、停止時のデータ保全が運用上不可欠 | |
| SPEC-003-01 | | ポーリング間隔を設定できる常駐プロセスとして動作する | | 1. Given ポーリング間隔が設定されている When デーモンを起動する Then 設定間隔で収集・配信サイクルが繰り返される |
| SPEC-003-02 | | 二重起動を Lock で防止し、異常終了後の stale lock からは安全に回復する | | 1. Given デーモンが実行中 When 同じ構成で 2 つ目のデーモンを起動する Then 2 つ目は起動せず終了する<br>2. Given 異常終了で残った stale lock When デーモンを起動する Then 安全に回復して処理を開始できる |
| SPEC-003-03 | | graceful shutdown(停止指示を受けたら処理中メッセージを完了してから停止する) | | 1. Given メッセージ処理中 When 停止シグナルを受ける Then 処理中のメッセージを完了してから停止し、中途半端な状態を残さない |
| REQ-004 | 収集したファイルを必ず Archive に保存したい | | 再送・監査・障害復旧・差分比較の基盤になる。GET/DELETE 方式ではファイルが消えてしまい、これらが一切できない | |
| SPEC-004-01 | | 収集したファイルを配信前に必ず archive/ 配下へ Topic 別に保存する | | 1. Given ファイルが収集された When Fan-out の前後いずれの時点でも Then archive/ 配下に元ファイルが Topic 別に残っている<br>2. Given Fan-out が失敗した When 処理を確認する Then Archive には保存済みで、再実行で配信を回復できる |
| SPEC-004-02 | | Archive の保持期間を設定でき、超過分を安全に削除する(無限に溜めない) | | 1. Given 保持期間が設定されている When retention 処理が実行される Then 期間を超過した Archive ファイルだけが削除される |
| REQ-005 | Consumer が書き込み途中のファイルを読まないようにしたい | | Consumer は任意のタイミングでファイルを取得するため、途中状態のファイルを読むと取り込みデータが破損する | |
| SPEC-005-01 | | Subscription ディレクトリへの配置は一時名(file.csv.tmp)で書き込んでから正式名(file.csv)へ rename する Atomic Write 方式とする | | 1. Given Fan-out で配置中のファイル When Consumer が Subscription ディレクトリを参照する Then 正式名のファイルは常に完全な内容である |
| REQ-006 | どのファイルをどの Subscription へ配送したかの履歴(Manifest)を管理し、配送状況を確認できるようにしたい | | 配送の成否を Subscription 単位で追跡できないと、並行稼働中の障害調査・再送判断・監査ができない | |
| SPEC-006-01 | | メッセージ(収集ファイル)ごとに message_id / topic / 各 Subscription の配送状態(delivered / failed / dlq)を Manifest として記録する | | 1. Given ファイルが収集・配信された When Manifest を参照する Then message_id・topic・各 Subscription の配送状態が確認できる<br>2. Given 一部の Subscription への配信が失敗した When Manifest を参照する Then 失敗した Subscription が特定できる |
| SPEC-006-02 | | 配送状況を確認する status コマンドを提供する | | 1. Given 配送履歴が存在する When status コマンドを実行する Then topic / Subscription 別の配送状態が確認できる |
| REQ-007 | 再起動・再実行しても二重配信しない冪等な処理にしたい | | 常駐デーモンは障害・メンテナンスでの再起動が前提。再開のたびに二重配信や履歴消失が起きると並行稼働の信頼性が成り立たない | |
| SPEC-007-01 | | 同名ファイルの再出力は新しいメッセージとして扱う(上書きで履歴を失わない)。message_id は収集時刻 + Topic + 元ファイル名から採番する | | 1. Given 同名ファイルが再度出力された When Collect が実行される Then 別の message_id を持つ新しいメッセージとして Archive・Manifest に記録される |
| SPEC-007-02 | | デーモンの再起動・処理中断後の再開で二重配信しない | | 1. Given 配信途中で中断したメッセージがある When デーモンを再起動する Then 未配信の Subscription にのみ配信され、配信済みへ重複配置されない |
| REQ-008 | Archive から過去のファイルを再送(Replay)したい | | 「先月分を再投入したい」のような障害復旧・遡及処理の要望に応えるため。Archive があることで GET/DELETE 方式では不可能だった再送が可能になる | |
| SPEC-008-01 | | Archive に保存済みのファイルを、Topic・期間(またはメッセージ指定)・宛先 Subscription を指定して再送できる。Replay も Manifest に記録する | | 1. Given Archive に先月分のファイルが保存されている When 対象範囲と宛先 Subscription を指定して Replay を実行する Then 指定した Subscription にのみ該当ファイルが再配置される<br>2. Given Replay を実行した When Manifest を参照する Then 再送の配送履歴が記録されている |
| REQ-009 | 配信に失敗し続けたメッセージをリトライし、規定回数を超えたら DLQ へ隔離したい | | 一時的な障害は自動回復させ、恒久的な失敗は滞留させずに隔離して運用者が判断できるようにするため。将来の Messaging 基盤(Kafka / RabbitMQ / Google Pub/Sub)移行時にも DLQ の概念が一致し橋渡しになる | |
| SPEC-009-01 | | 配信失敗はリトライし、規定回数を超えたら DLQ へ隔離して Manifest に記録する | | 1. Given ある Subscription への配信が一時的に失敗する When リトライが実行される Then 規定回数以内に成功すれば delivered になる<br>2. Given 配信が規定回数失敗した When 処理を継続する Then 該当メッセージが DLQ へ隔離され Manifest に dlq として記録される |
| REQ-010 | 死活監視と Topic ごとの異常検知に使えるメトリクスを公開したい | | 常駐デーモンとして運用するため、止まっていないか(死活)と、Topic 単位で収集・配信が滞っていないか(異常検知)を外部監視基盤から観測できる必要がある | |
| SPEC-010-01 | | デーモンが HTTP で Prometheus 形式の /metrics と死活監視用の /healthz を公開する | | 1. Given デーモンが稼働中 When /healthz にアクセスする Then 正常応答が返る<br>2. Given デーモンが稼働中 When /metrics にアクセスする Then Prometheus 形式のメトリクスが返る |
| SPEC-010-02 | | Topic ごとの異常検知に使える粒度(topic 別の最終収集時刻 / 処理件数 / 配信失敗数 / DLQ 件数 / 滞留数 等)のメトリクスを出す。しきい値判定・アラート発報は外部監視基盤(Prometheus / Grafana 等)の責務とする | | 1. Given ある Topic の収集が止まっている When /metrics を参照する Then topic 別の最終収集時刻から異常を判定できる<br>2. Given ある Topic で配信失敗・DLQ が発生している When /metrics を参照する Then topic 別の失敗数・DLQ 件数が確認できる |
| REQ-011 | レガシー現場のサーバへ容易に導入・運用できる配布形態と設定にしたい | | OSS として現場提案・導入する際の障壁を最小にするため。レガシー現場はランタイム追加が難しいことが多い | |
| SPEC-011-01 | | Go 実装のシングルバイナリとして配布する(Linux 主対象 + macOS 動作)。MIT ライセンスの OSS とする | | 1. Given Linux サーバ When バイナリ 1 個を配置して設定 YAML を与える Then 追加ランタイムなしで動作する |
| SPEC-011-02 | | 設定は単一 YAML(topics / 収集ソース / subscriptions / ポーリング間隔 / retention / リトライ / メトリクスポート)。認証情報は YAML への平文記述も許容しつつ、環境変数参照(${ENV_VAR})と鍵ファイルパス指定を推奨として README で案内する | | 1. Given 認証情報を ${ENV_VAR} で参照する設定 When デーモンを起動する Then 環境変数の値で認証される<br>2. Given 鍵ファイルパスを指定した SFTP/SCP 設定 When Collect を実行する Then 鍵認証で収集できる |
| SPEC-011-03 | | Docker コンテナイメージを提供し、Windows の開発 PC(Docker Desktop)でも動作確認できる。docker compose で一式(file-pubsub + 収集元 FTP/SFTP サーバの例)を起動できる動作確認環境を同梱する | | 1. Given Docker Desktop が動く Windows 開発 PC When docker compose で動作確認環境を起動する Then file-pubsub と収集元サーバの例が起動し、収集から配信までを確認できる |
| SPEC-011-04 | | 構造化ログを出力する(運用者が障害調査できる粒度) | | 1. Given 配信失敗が発生した When ログを参照する Then どのメッセージのどの Subscription 配信が失敗したか特定できる |
