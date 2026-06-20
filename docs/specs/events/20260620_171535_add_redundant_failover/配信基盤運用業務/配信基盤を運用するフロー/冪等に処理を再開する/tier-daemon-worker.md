# 冪等に処理を再開する - 常駐デーモン仕様

## 変更概要

再起動・処理中断後の再開を冪等にするデーモン仕様。専用の「再開モード」は持たず、通常の収集配信サイクルが Manifest と処理済み管理を参照することで、中断時点から二重配信・重複収集なく処理を継続する(SR-003、LP-101)。active/standby 冗長構成での standby → active 昇格(フェイルオーバー)は別ホストでのクラッシュ再開と等価で、同じ冪等再開で扱う。lease 方式の split-brain の窓では 2 つの serve が一時的に同一 data_dir を操作しうるが、被害は AtomicWrite + at-least-once 冪等再開 + fail-closed 照合(受動的被害限定=破損・喪失なし、spec-decision-010)と、メッセージ境界 lease 確認 + Manifest read-merge-write + 世代 CAS(能動的上限担保=高々1メッセージ、spec-decision-011)により高々 1 メッセージの重複配信に限定し、データ破損・喪失は起こさない(SPEC-016-01)。

## イベント処理仕様

### 再開サイクル(再起動後の収集配信サイクル)

- **トリガー**: 再起動後のポーリングスケジューラによる最初のサイクル起動(以降の通常サイクルと同一処理)
- **入力チャネル**: Manifest(message_id 別の配送状態)、処理済み管理、archive/{topic}/、収集ソース
- **出力チャネル**: Subscription 配置先ディレクトリ、Manifest(delivered / failed 記録)

#### 処理フロー

1. Manifest を読み、中断時点のメッセージ配送状態(収集済 / Archive保存済 / 配信中 / failed)を把握する。配送状態の正は常に Manifest とする(CTR-003)。
2. メッセージ単位に処理を進める(LP-101: メッセージ単位のトランザクション境界)。**各永続化点(収集 / Archive 保存 / Fan-out 配置 / Manifest 記録、加えて収集後の副作用=原本 delete(source remove)と処理済み管理への MarkProcessed)に入る前に lease 保持を再確認し(lock の hostname/boot-id が自分自身か、かつ ttl 以内か。UC「デーモンを起動する」LockManager)、lease を失っていれば「処理中のその 1 メッセージ」で停止して standby待機へ降格する(メッセージ境界 lease 確認、spec-decision-011。split-brain の窓で旧 active が複数メッセージを処理し続けず、重複を高々 1 メッセージに限定)**:
   - Archive 未保存のメッセージ → Archive 保存から再開する(LR-101: Archive 保存完了前の配信禁止)。
   - Archive保存済・配信中・failed のメッセージ → ドメイン層の二重配信防止判定で「delivered 記録がない Subscription」のみを配信対象として抽出する。
3. 抽出した未配信 Subscription のみへ AtomicWrite(一時名 → rename)で配置し、成功した Subscription を Manifest に delivered として **message_id 単位の更新ロック + read-merge-write + 世代 CAS + 競合リトライ** で記録する(SR-001、SR-003)。Manifest 更新は全体上書きでなく次の手順で行う: (0) **同一 message_id の更新ロック(manifest/{message_id}.json.lock を O_CREATE|O_EXCL で取得)を取り、同一 message_id への 2 active 同時更新を直列化する(主機構)。取得失敗(他者保持)はリトライ/バックオフし、上限超過は fail-closed(配信保留)。以降 (a)〜(d) はロック保持下で行い、完了後にロックを解放(削除)する**、(a) 対象 message_id の Manifest を再読込し、このとき世代(mtime または Manifest レコードの revision)を観測する、(b) Subscription 別配送状態(delivered / failed / dlq)を **merge precedence(決着状態 delivered/dlq は保持・上書き不可、中間状態 failed は再配信成功時に delivered へ上書き可)** に従って自分の更新内容とマージする、(c) 一時名で書き、**rename の直前に Manifest の世代を再確認(世代 CAS をロック下の二重チェックとして併用)**する、(d) 世代が読込時と一致すれば rename を確定し、不一致(NFS でロックの原子性が破れ他 active が更新)なら一時ファイルを破棄して (a) から再試行する(競合リトライ。有限回・バックオフ。Manifest は message_id 別ファイルのため競合対象は同一 message_id のみ)。これにより 2 active が同一 message_id を相前後して更新しても先に記録された delivered/dlq の決着状態を取りこぼさず、read→rename 間の窓でも lost update を起こさない(spec-decision-011)。決着状態は retention で保護され後勝ち上書きで未配信へ戻らない。リトライ上限超過は fail-closed(配信保留・上書き回避)で安全側に倒す。**既知の制約**: NFS では O_CREATE|O_EXCL・read/write の原子性が実装依存で完全な分散排他は保証できないため、本機構は『実務上の原子性 + 被害限定』であり exactly-once は保証しない(破損なし・被害は重複配信に限定)。
4. delivered 記録済みの Subscription へは重複配置しない(冪等)。全 Subscription が delivered のメッセージはスキップする。
5. 収集処理では、copy 設定の収集ソースについて処理済み管理と照合し、処理済みの元ファイルは再収集しない(SP-004)。回収(GET 後 DELETE)設定では元ファイルが収集ソースに存在しないため照合不要。
6. 再開時の配信イベントも通常どおり構造化ログ(message_id / topic / subscription / event_type)に出力する(CTP-001)。

#### エラーハンドリング

| エラー種別 | リトライ | DLQ | 説明 |
|-----------|---------|-----|------|
| 再開後の配置失敗(一時的) | Yes | No | Manifest に failed を記録し、リトライ処理(UC「配信失敗をリトライしDLQへ隔離する」)へ。リトライ回数は Manifest に記録 |
| リトライ上限超過 | No | Yes | DLQ へ隔離し Manifest に dlq を記録(同 UC の責務) |
| Manifest 読み書き失敗 | No | No | 実行時エラー。配送状態が確認できないメッセージへの配信は保留し、原因 + 対処を構造化ログに出力(二重配信より保留を優先) |

## データモデル変更

### Manifest(message_id 別 JSON、参照 + 更新)

| 項目 | 型 | 説明 | 変更種別 |
|------|---|------|---------|
| message_id | string | 冪等処理の単位キー | 参照 |
| subscription_delivery_status | text | Subscription 別配送状態(delivered / failed / dlq)。再開時の配信対象判定の根拠。更新は message_id 更新ロック + read-merge-write + 世代 CAS(message_id ロックで同一 message_id 更新を直列化し、ロック下で書込直前に再読込・merge precedence でマージ、rename 直前に世代=mtime/revision を再確認し変化していたら再読込してリトライ。2 active 同時更新でも決着状態の lost update を回避) | 参照 + 更新(delivered 記録) |
| revision | integer | 更新世代カウンタ(任意)。世代 CAS の基準。持たない場合は mtime を世代に用いる | 参照 + 更新 |
| retry_count | integer | リトライ回数 | 参照 |
| delivered_at | datetime | 配送日時 | 更新 |

### 処理済み管理(参照)

| 項目 | 型 | 説明 | 変更種別 |
|------|---|------|---------|
| source_file_identifier | string | 収集元ファイル識別子(ファイル名・収集元パス等)。copy 設定時の再収集判定キー | 参照 |
| processed_at | datetime | 処理済み判定日時 | 参照 |

## ビジネスルール

- 二重配信防止: 再開では Manifest の配送状態を参照し、未配信の Subscription にのみ配信する。配信済みの Subscription へは重複配置しない(条件「二重配信防止」、SR-003)。
- メッセージ単位の冪等性: 1 メッセージの処理進行と Manifest 更新を単位とし、どこで中断してもメッセージ単位で冪等に再開できる(LP-101)。
- Archive 保存必須の順序維持: 再開時も collect→archive→fanout の順序を崩さない(LR-101)。
- 重複収集防止: copy 設定では処理済み管理と照合し、処理済みファイルを再収集しない(条件「元ファイル処理判定」)。
- 履歴の維持: 再開によって Manifest の既存履歴を消さない。配送記録は追記され監査・追跡に使える(CTR-003、SR-002)。
- フェイルオーバー = クラッシュ再開と等価: standby → active 昇格後の最初のサイクルも通常の冪等再開で扱う。別ホストで再開しても data_dir は NFS 共有のため、Manifest/処理済み管理の参照で中断時点から継続できる(SPEC-015-03)。
- split-brain の被害限定: 旧 active と新 active が一時的に共存しても、AtomicWrite(正式名は常に完全)+ at-least-once 冪等再開(resumeArchiving は原本後始末をしない設計)+ Manifest 存在確認・処理済み照合の fail-closed により、被害は高々 1 メッセージの重複配信に限定され破損・喪失はない(SPEC-016-01、spec-decision-010)。照合 I/O 失敗時は配信保留・再収集回避で安全側に倒す。
- 「高々1メッセージ」の上限維持(案Z): REQ-016 の上限は実装上、(a)メッセージ境界 lease 確認(各永続化点=収集 / Archive 保存 / Fan-out 配置 / Manifest 記録 / 原本 delete / 処理済み MarkProcessed の前に lease 保持を再確認し、失っていればその 1 メッセージで停止・降格して複数メッセージを処理し続けない)と(b)Manifest の message_id 単位の更新ロック + read-merge-write + 世代 CAS + 競合リトライ(同一 message_id 更新前に O_CREATE|O_EXCL でロックを取り直列化し、ロック下で書込直前に再読込し merge precedence=決着 delivered/dlq は上書き不可・failed は delivered へ昇格可 でマージ、rename 直前に世代=mtime/revision を再確認し変化していたら再読込してリトライ。2 active 同時更新の lost update を回避。決着状態 delivered/dlq は retention で保護)で維持する。重い分散合意機構(専用ロックサービス等)は導入しない(spec-decision-011)。spec-decision-010 が受動的被害限定(破損・喪失なし)、spec-decision-011 が能動的上限担保(高々1メッセージ)を担い、010 の上限主張は 011 の能動機構に依存する。**既知の制約**: NFS では O_CREATE|O_EXCL・read/write の原子性が実装依存で完全な分散排他は保証できないため、本機構は『実務上の原子性 + 被害限定』であり exactly-once は保証しない(破損なし・被害は重複配信に限定)。
- 復旧方式: active/standby 冗長化(方式B=lease 自動奪取 / 方式A=外部クラスタ委譲)を前提とし、障害時は standby の自動昇格による冪等再開で配信を継続する。冗長構成を持たない環境では従来どおり「再起動による冪等再開 + Archive からの Replay」で復旧する(SPEC-018-01。RTO は自動フェイルオーバーで短縮、非冗長時は再起動 + 追いつき配信で数時間以内)。

## ティア完了条件（BDD）

```gherkin
Feature: 冪等に処理を再開する - 常駐デーモン

  Scenario: delivered 済み Subscription を除外して配信する
    Given Manifest に message_id 「20260612T093001_orders_sales.csv」 の current=delivered、next=failed が記録されている
    When 再開サイクルが二重配信防止判定を実行する
    Then 配信対象は 「next」 のみと判定される
    And 「next」 への AtomicWrite 成功後に Manifest の next が delivered に更新される

  Scenario: Archive 未保存のメッセージは Archive 保存から再開する
    Given message_id 「20260612T094500_orders_stock.csv」 が収集済のまま Archive 未保存で中断している
    When 再開サイクルが実行される
    Then archive/orders/ への保存が完了してから Fan-out が開始される

  Scenario: 全 Subscription 配信済みのメッセージはスキップする
    Given Manifest に message_id 「20260612T093001_orders_sales.csv」 の current=delivered、next=delivered が記録されている
    When 再開サイクルが実行される
    Then どの Subscription へも再配置されず、Manifest も変更されない

  Scenario: split-brain でも被害は高々1メッセージの重複配信に限定される
    Given 旧 active(host-a)と 新 active(host-b)が split-brain の窓で同一 data_dir を操作している
    When 両者が同じ message_id を未配信 Subscription 「next」 へ配置する
    Then AtomicWrite により正式名は常に完全な内容で、Manifest/処理済み照合の fail-closed 冪等により重複は高々 1 メッセージに留まりデータ破損・喪失はない

  Scenario: メッセージ境界 lease 確認で旧 active が高々1メッセージで停止する
    Given host-a(旧 active)が lease を失ったが split-brain の窓で 1 メッセージを処理中である
    When host-a が次の永続化点(収集 / Archive 保存 / Fan-out 配置 / Manifest 記録 / 原本 delete / 処理済み MarkProcessed)に入る前に lease 保持を確認する
    Then lock の hostname/boot-id が自分でない(または ttl 超過)ため lease 喪失を検知し、その 1 メッセージで停止して standby待機へ降格する

  Scenario: message_id 更新ロックで同一 message_id の Manifest 更新を直列化する
    Given host-a と host-b が同一 message_id の Manifest を同時に更新しようとしている
    When 両者が manifest/{message_id}.json.lock を O_CREATE|O_EXCL で取得しようとする
    Then 最終的に1人だけがロックを取得して read-merge-write を直列実行し、敗者はリトライ/バックオフ(上限超過は配信保留)する
    And 勝者の更新完了・ロック解放後に敗者が再取得して最新の Manifest を更新する

  Scenario: Manifest の read-merge-write + 世代 CAS で 2 active 同時更新でも決着状態を取りこぼさない
    Given NFS でロックの原子性が破れ host-b が Manifest を読込み世代を観測した直後に、host-a が current=delivered を書いて Manifest の世代が変化した
    When host-b が next=delivered をマージして rename する直前に Manifest の世代を再確認する
    Then 世代が読込時から変化したため host-b は一時ファイルを破棄して再読込・再マージしてリトライし、merge precedence で既存 current=delivered を保持しつつ next=delivered をマージして、リトライ後の Manifest に current=delivered と next=delivered の双方が保持される(lost update なし)
```

## 共通コンポーネント参照

[common-components.md](../../../_cross-cutting/ux-ui/common-components.md) の以下を利用する(専用再開モードはなく通常サイクルの共通部品で冪等性を実現する)。

- [C-01 SourceConnector](../../../_cross-cutting/ux-ui/common-components.md#c-01-sourceconnector) — 再開後の通常収集サイクルでの収集
- [C-02 AtomicWriter](../../../_cross-cutting/ux-ui/common-components.md#c-02-atomicwriter) — 未配信 Subscription への再配置(一時名残留からの安全なやり直し)
- [C-03 ManifestStore](../../../_cross-cutting/ux-ui/common-components.md#c-03-manifeststore) — 中断時点の配送状態の参照と delivered 記録(配送状態の正)。更新は PutMerged=message_id 更新ロック + read-merge-write + 世代 CAS(message_id ロックで直列化し、ロック下で書込直前に再読込・merge precedence でマージ、rename 直前に世代を再確認し変化していたら再読込してリトライ。2 active 同時更新の lost update を回避、spec-decision-011)
- [C-08 LockManager](../../../_cross-cutting/ux-ui/common-components.md#c-08-lockmanager) — メッセージ境界 lease 確認(各メッセージ処理前の lease 保持再確認。失っていればその 1 メッセージで停止・降格、spec-decision-011)
- [C-05 DeliveryStateMachine](../../../_cross-cutting/ux-ui/common-components.md#c-05-deliverystatemachine) — delivered 済み Subscription を除外する二重配信防止(冪等)判定
- [C-07 ProcessedStore](../../../_cross-cutting/ux-ui/common-components.md#c-07-processedstore) — copy 設定時の処理済み照合(重複収集防止)
- [C-09 StructuredLogger](../../../_cross-cutting/ux-ui/common-components.md#c-09-structuredlogger) — 再開時の配信イベント出力
- [C-12 PollingScheduler](../../../_cross-cutting/ux-ui/common-components.md#c-12-pollingscheduler) — 再起動後の最初のサイクル起動
