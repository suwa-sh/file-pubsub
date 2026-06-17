# ファイルを収集する(Collect) - 常駐デーモン仕様

## 変更概要

収集(Collect)は二系統の収集モードを持つ。**pull 型**(local / FTP / SFTP / SCP)はポーリングサイクルの先頭工程として List → Fetch → Delete を行う。**push 受信モード(inbox)**は受信ディレクトリを fsnotify でイベント駆動監視し、低頻度フォールバックポーリングを併用するハイブリッドで取り込み契機を得る(LR-003)。収集コネクタは共通インターフェースで差し替え可能とし(LP-301、inbox コネクタは LP-302)、完了検知方式(安定判定 / rename / done マーカー。LR-204)・除外パターン・一時名書き込み・元ファイル処理(回収 / copy + 処理済み管理、push の done マーカー後始末 LR-305)・message_id 採番・Manifest への収集済記録を行う。後段(Archive / Fan-out / Manifest / Retry / Retention)は収集モードに依存せず既存処理をそのまま流用する。HTTP API・非同期メッセージングは関与しない。

## イベント処理仕様

### 収集サイクルハンドラ(collect / pull 型)

- **トリガー**: ポーリングスケジューラ(設定 `polling_interval` 秒ごと。前回サイクル完了を待ち多重起動しない — LR-001)
- **入力チャネル**: 収集ソース(FTP / SFTP / SCP / ローカルディレクトリ)の対象ディレクトリ
- **出力チャネル**: ローカルファイルシステム `work/collect/{topic}/`(収集済ファイル)、`manifest/{message_id}.json`(収集済記録)、`processed/{topic}.json`(copy 時)
- **AsyncAPI**: 該当なし(メッセージキューは使用しない)

#### 処理フロー

1. 設定の Topic ごとに収集ソースへ接続する(認証情報は環境変数参照 / 鍵ファイルパス / 平文を解決。CTP-002)
2. 対象ディレクトリのファイル一覧(名前・サイズ・更新時刻)を取得する
3. domain の判定ルールを適用する(ソース種別非依存 — LR-203):
   - 除外パターン該当 → 対象外
   - 完了検知方式に従い書き込み完了判定(既定=安定判定: サイズ・更新時刻が安定確認間隔で不一致なら書き込み中として次サイクルへ持ち越し)
   - copy 設定時、処理済み管理に収集元ファイル識別子が存在 → 再収集しない
4. 収集可能ファイルを一時名でダウンロードし、完了後に rename する(LR-303)
5. message_id を採番する(収集時刻 + Topic + 元ファイル名。LR-202)。同名再出力は別 message_id の新メッセージとする
6. Manifest に message_id・topic・収集時刻を AtomicWrite で記録し、メッセージ配送状態を「収集済」とする
7. 元ファイル処理判定:
   - 回収(既定): Archive 保存の成功を確認してから元ファイルを DELETE する(LR-303)
   - 残す(copy): 処理済み管理に収集元ファイル識別子・処理済み判定日時を記録する
8. Topic 別メトリクス(最終収集時刻・処理件数)を更新し、構造化ログ(event_type=収集)を出力する

### 受信ディレクトリ取り込みハンドラ(collect / push 受信モード = inbox)

- **トリガー**: fsnotify ウォッチャの create/rename/close イベント(イベント駆動)+ フォールバックポーリング(設定 `source.fallback_poll_interval` 秒ごと。省略時は `polling_interval` を流用 — LR-003)。inbox は trigger 設定を持たず常時ハイブリッド固定で動作し、同一ファイルを二重検知しても冪等に取り込む(LR-205)
- **入力チャネル**: 受信ディレクトリ(設定 `source.directory` を pull 型と共通流用)。共有 FS の実体(ローカルディスク / NFS / SMB)に依存しない
- **出力チャネル**: pull 型と同一(`work/collect/{topic}/`、`manifest/{message_id}.json`、`processed/{topic}.json`)
- **AsyncAPI**: 該当なし

#### 処理フロー

1. runtime の fsnotify ウォッチャが受信ディレクトリの変更イベントを受け取る。並行して fallback_poll_interval 周期のフォールバックポーリングが受信ディレクトリを走査する(イベント取りこぼし対策)。いずれも usecase の取り込み契機とする
2. 受信ディレクトリの候補(ファイル名・サイズ・更新時刻・done マーカー)を列挙する(LR-304)
3. domain の判定ルールを適用する(LR-204):
   - 除外パターン該当 → 対象外
   - 完了検知方式に従い書き込み完了判定:
     - 安定判定: サイズ・更新時刻の安定待ち(pull 型と同一ロジック)
     - rename: 正式名(一時拡張子でない)のみ取り込み対象。一時名(xxx.csv.tmp)は対象外
     - done マーカー: 対象 xxx に対応する xxx.done が存在すれば xxx を取り込み対象にする。マーカー自体(xxx.done)は配信対象に含めない
   - 処理済み管理に収集元ファイル識別子(ファイル名・収集元パス・done マーカー名等)が存在 → 再取り込みしない(二重検知・copy 両対応 — LR-205)
4. 取り込み対象を受信ディレクトリから読み取り、`work/collect/{topic}/` へ一時名で書き込み後 rename する(LR-304)
5. message_id を採番する(pull 型と同一。LR-202)。fsnotify とフォールバックポーリングが同一ファイルを二重検知しても、処理済み管理照合と合わせて二重取り込みしない
6. Manifest に収集済を AtomicWrite で記録し、メッセージ配送状態を「収集済」とする
7. 元ファイル処理判定:
   - 回収(既定): Archive 保存の成功を確認してから受信ディレクトリのファイルを削除する。done マーカー方式では xxx.done も削除する(マーカー後始末 — LR-305)
   - 残す(copy): 処理済み管理に収集元ファイル識別子(done マーカー方式ではマーカー名も)・処理済み判定日時を記録する
8. Topic 別メトリクス(最終収集時刻・処理件数)を更新し、構造化ログ(event_type=収集)を出力する

#### エラーハンドリング

| エラー種別 | リトライ | DLQ | 説明 |
|-----------|---------|-----|------|
| 収集ソース接続失敗(認証エラー・ホスト不達。pull 型) | Yes(次ポーリングサイクルで自動再試行) | No | 当該 Topic の収集をスキップし、topic を含む構造化ログ(原因 + 対処)を出力。デーモンは停止しない |
| 受信ディレクトリ未存在・アクセス不可(push 受信モード) | Yes(次フォールバックポーリングで自動再試行) | No | 当該 Topic の取り込みをスキップし、topic を含む構造化ログ(原因 + 対処)を出力。fsnotify ウォッチャ登録失敗もフォールバックポーリングで縮退継続。デーモンは停止しない |
| fsnotify イベント取りこぼし(NFS/SMB 等) | Yes(フォールバックポーリングで補完) | No | フォールバックポーリングが fallback_poll_interval 周期で受信ディレクトリを走査し、取りこぼしたファイルを取り込む |
| ダウンロード/取り込み中断(一時名のまま) | Yes(次契機で再取得) | No | 一時名ファイルは正式名に rename されないため後段に流れない。再取得で上書きする |
| 元ファイル削除失敗(pull の DELETE / push の受信ディレクトリ削除・マーカー削除) | Yes(次契機で再試行) | No | メッセージは収集済のまま進行。重複収集は message_id 採番・Manifest 照合・処理済み管理で防ぐ |
| 処理済み管理の書き込み失敗 | Yes(同サイクル内で AtomicWrite 再試行、失敗時は次契機) | No | 記録成功までは元ファイルを未処理扱いとし、安全側(再収集候補)に倒す |

## データモデル変更

RDB は使用しない。ローカルファイルシステムのレイアウト(収集モードに依存しない既存レイアウトを流用):

### work/collect/{topic}/{message_id}

| 項目 | 型 | 説明 | 変更種別 |
|--------|---|------|---------|
| ファイル実体 | binary(pass-through) | 収集済みファイル。一時名(`{message_id}.tmp`)で書き込み後 rename。pull 型・push 受信モードとも同一 | 変更なし(流用) |

### manifest/{message_id}.json(収集済の初期レコード)

| フィールド | 型 | 説明 | 変更種別 |
|--------|---|------|---------|
| message_id | string | 収集時刻 + Topic + 元ファイル名から採番 | 変更なし(流用) |
| topic_name | string | Topic 名 | 変更なし(流用) |
| original_file_name | string | 元ファイル名(push 受信モードでは受信ディレクトリ上のファイル名。done マーカー名は含めない) | 変更なし(流用) |
| collected_at | datetime(ISO 8601) | 収集時刻 | 変更なし(流用) |
| status | string | メッセージ配送状態(この UC では「収集済」) | 変更なし(流用) |
| retry_count | integer | リトライ回数(初期値 0。加算は別 UC「配信失敗をリトライしDLQへ隔離する」) | 変更なし(流用) |

### processed/{topic}.json(copy 設定時 / push の done マーカー残置時)

| フィールド | 型 | 説明 | 変更種別 |
|--------|---|------|---------|
| source_file_identifier | string | 収集元ファイル識別子(ファイル名・収集元パス・done マーカー名等)。push 受信モードの二重検知防止にも使用 | 変更(識別子に done マーカー名を追加) |
| processed_at | datetime(ISO 8601) | 処理済み判定日時 | 変更なし(流用) |

## ビジネスルール

- 書き込み完了判定: 書き込み中のファイルは収集しない。完了検知方式(安定判定 / rename / done マーカー)を収集ソース設定で選択し、domain 層の共通ルールとしソース種別・収集モードに依存させない(SP-003、LR-204)。既定は安定判定
- 取り込みトリガー(push 受信モード): fsnotify イベント駆動 + フォールバックポーリングのハイブリッド。ローカルディスクは即時、NFS/SMB はフォールバックで拾う。二重検知は処理済み管理・message_id 採番で冪等(LR-003、LR-205)
- 元ファイル処理判定: 回収が既定。pull 型は GET 後 DELETE、push 受信モードは受信ディレクトリから削除(done マーカー方式では xxx.done も削除 — LR-305)。copy 選択時は処理済み管理と照合し重複収集を防ぐ(SP-004)
- message_id 採番: 収集時刻 + Topic + 元ファイル名。同名再出力は別メッセージとして履歴を失わない(SR-002)
- 一時名書き込み: 収集中は一時名、完了後 rename。元ファイル削除は Archive 保存成功の確認後(LR-303 / LR-304)
- 収集コネクタは共通インターフェース経由で差し替え可能。pull 型(local/FTP/SFTP/SCP)と inbox コネクタは同 IF に従い、後段は収集モードに非依存(LP-301、LP-302、CLP-001)
- 収集イベント・エラーは message_id / topic を含む 1 行 JSON 構造化ログで出力する(CTP-001)

## ティア完了条件（BDD）

```gherkin
Feature: ファイルを収集する(Collect) - 常駐デーモン

  Scenario: 安定したファイルだけを一時名ダウンロードで収集する(pull 型)
    Given topic「orders」の SFTP 収集ソース /out/orders に安定済みの「orders_20260612.csv」と書き込み中の「orders_20260613.csv」がある
    When 収集サイクルが実行される
    Then 「orders_20260612.csv」のみが一時名でダウンロードされ rename され、manifest に status=収集済 で記録される
    And 「orders_20260613.csv」は収集されない

  Scenario: push 受信モードでイベント駆動とフォールバックの両方で取りこぼさず冪等に取り込む
    Given topic「invoices」の収集ソースが type=inbox, directory=/inbox/invoices, completion=stability で設定されている
    When Producer が受信ディレクトリへ「invoices_0042.csv」を put する
    Then fsnotify イベント駆動(ローカルディスク)またはフォールバックポーリング(NFS/SMB)で取り込まれ、manifest に status=収集済 で 1 件だけ記録される
    And 同一ファイルを二重検知しても処理済み管理・message_id 採番により重複メッセージは発生しない

  Scenario: done マーカー方式でマーカーを契機に取り込みマーカーを後始末する
    Given topic「invoices」の push 受信モードが completion=marker, original_file_handling=delete で設定されている
    When Producer が「invoices_0046.csv」を put した後に「invoices_0046.csv.done」を put する
    Then 「invoices_0046.csv」が取り込まれ「invoices_0046.csv.done」は配信対象とならない
    And Archive 保存成功後に「invoices_0046.csv」と「invoices_0046.csv.done」の双方が受信ディレクトリから削除される

  Scenario: copy 設定の処理済み照合で重複収集を防ぐ
    Given topic「customers」(original_file_handling=copy) の processed/customers.json に「customers_20260612.csv」が記録済みである
    When 収集サイクルが実行される
    Then 「customers_20260612.csv」は収集対象外となり新しいメッセージは発生しない

  Scenario: 接続失敗時もデーモンが停止しない(pull 型)
    Given topic「orders」の収集ソース legacy-host01 への接続が認証エラーで失敗する
    When 収集サイクルが実行される
    Then topic=orders, event_type=収集失敗 を含む構造化ログが出力され、他 Topic の収集処理は継続する
```

## 共通コンポーネント参照

[common-components.md](../../../_cross-cutting/ux-ui/common-components.md) の以下を利用する。

- [C-01 SourceConnector](../../../_cross-cutting/ux-ui/common-components.md#c-01-sourceconnector) — 収集ソース接続・ファイル一覧取得・一時名ダウンロード・元ファイル DELETE。push 受信モードでは inbox 実装が受信ディレクトリ列挙・取り込み・受信ディレクトリ削除(done マーカー後始末含む)を同 IF で提供(LP-302、LR-304、LR-305)
- [C-02 AtomicWriter](../../../_cross-cutting/ux-ui/common-components.md#c-02-atomicwriter) — Manifest・処理済み管理の AtomicWrite 記録
- [C-03 ManifestStore](../../../_cross-cutting/ux-ui/common-components.md#c-03-manifeststore) — 収集済の初期 Manifest レコード作成
- [C-04 MessageIDGenerator](../../../_cross-cutting/ux-ui/common-components.md#c-04-messageidgenerator) — 収集時刻 + Topic + 元ファイル名からの message_id 採番
- [C-05 DeliveryStateMachine](../../../_cross-cutting/ux-ui/common-components.md#c-05-deliverystatemachine) — メッセージ配送状態「収集済」への初期遷移
- [C-06 StabilityChecker](../../../_cross-cutting/ux-ui/common-components.md#c-06-stabilitychecker) — 完了検知方式(安定待ち / rename / done マーカー)・除外パターンによる収集可否判定
- [C-07 ProcessedStore](../../../_cross-cutting/ux-ui/common-components.md#c-07-processedstore) — copy 設定時・push 二重検知時の処理済み照合・処理済み記録
- [C-09 StructuredLogger](../../../_cross-cutting/ux-ui/common-components.md#c-09-structuredlogger) — collected / 収集失敗イベントの 1 行 JSON 出力
- [C-10 MetricsRegistry](../../../_cross-cutting/ux-ui/common-components.md#c-10-metricsregistry) — Topic 別最終収集時刻・処理件数の更新(収集モード非依存)
- [C-12 PollingScheduler](../../../_cross-cutting/ux-ui/common-components.md#c-12-pollingscheduler) — pull 型収集サイクルの周期トリガー(多重起動なし)。push 受信モードでは fsnotify ウォッチャ + fallback_poll_interval 周期のフォールバックポーリングで補完(LR-003)
- [C-14 ConfigLoader](../../../_cross-cutting/ux-ui/common-components.md#c-14-configloader) — Topic / 収集ソース定義(収集モード・受信ディレクトリ・取り込みトリガー方式・fallback_poll_interval・完了検知方式)・認証情報・安定待ち設定の参照
