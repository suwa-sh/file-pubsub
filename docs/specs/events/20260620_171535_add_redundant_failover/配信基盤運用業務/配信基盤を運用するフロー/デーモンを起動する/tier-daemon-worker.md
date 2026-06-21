# デーモンを起動する - 常駐デーモン仕様

## 変更概要

serve で起動される常駐デーモンの起動シーケンス仕様。ランタイム層が設定読込・検証、Lock(lease)取得(stale 回復・standby 待機・active 昇格を含む)、heartbeat 開始、組込 HTTP サーバ起動、ポーリングスケジューラ開始を行い、デーモン稼働状態を「起動中」から「稼働中(active)」または「standby待機」へ遷移させる。Lock は lease レコード化(hostname + boot-id + acquired_at + renewed_at + ttl)し、stale 判定は renewed_at + ttl 超過で行う(PID 生存確認に依存しない、SPEC-015-01)。唯一性保証は方式B(lease 自動奪取)/ 方式A(外部クラスタ委譲)を設定で切替・併用する(SPEC-015-02、spec-decision-009)。

## イベント処理仕様

### 起動シーケンス(デーモン起動ハンドラ)

- **トリガー**: serve サブコマンドによる起動指示(tier-ops-cli から)
- **入力チャネル**: なし(プロセス起動)
- **出力チャネル**: なし(以降の収集配信サイクルはポーリングスケジューラが起動)

#### 処理フロー

1. 設定 YAML(`--config` のパス)をゲートウェイ層で読込み、構文・参照整合(Topic↔Subscription↔収集ソース↔認証情報参照)と冗長化設定(唯一性保証方式 / lease TTL / heartbeat 間隔)を検証する。検証 NG は終了コード 2 で終了する(デーモンを稼働させない)。
2. Lock(lease)取得を試行する(LR-002、SPEC-015-01/02/03)。**唯一性保証方式で起動モデルを分岐する**:
   - **方式B(lease 自動奪取)**:
     - lock(lease)が存在しない → lease を作成し、hostname + boot-id + acquired_at + renewed_at + ttl を記録する(Lock状態: 取得済 → active へ)。
     - lease が有効(`現在時刻 - renewed_at <= ttl`)で他ホスト/他世代が保持 → 二重 serve を起こさず standby 待機に入る(デーモン稼働状態: 起動中 → standby待機)。standby は lease の ttl 失効を監視する。**同一ホスト・同一構成での 2 つ目の serve は standby が意味を持たないため起動を中断し終了コード 3 で終了する(従来の単一インスタンス二重起動防止、SR-006)。稼働中のインスタンスには影響しない**。
     - lease が stale(`現在時刻 - renewed_at > ttl`)→ 安全に回復して lease を奪取する(Lock状態: stale → 取得済 → active へ)。奪取は **read(現在の lease)→ remove(lock ファイル削除)→ O_CREATE|O_EXCL で再作成(boot-id を新世代へ更新)** の順で原子的に行う。複数 standby が同時に奪取を試みても O_EXCL により「最終的に1人だけ」が再作成に成功し、敗者(O_EXCL が EEXIST で失敗)は奪取を諦め standby 待機を継続する。旧 active が同時に heartbeat で renewed_at を書こうとしても O_EXCL で1人に収束し二重所有にならない。stale 判定は PID 生存確認ではなく renewed_at + ttl 超過で行う(マルチホストで他ホストの PID を判定できないため)。
   - **方式A(外部クラスタ委譲)**: 外部クラスタが serve リソースを起動した契機で**常に active として起動する**(非 active ノードで serve を常駐 standby させない)。起動した active は lease を稼働識別・観測用として必ず書く。稼働中 lease の hostname/boot-id が自分と異なれば(旧 active の残留 lease)、lease が有効・stale を問わず read→remove→O_CREATE|O_EXCL で自分の lease を書く(boot-id を更新。fencing で旧 active は既に停止済みのため安全)。**「lease 有効を見て standby に落ちる」判定は行わない**。TTL 失効による自動奪取も行わない(昇格契機は常に外部クラスタ)。
3. (active のみ)heartbeat を開始し、heartbeat 間隔ごとに lease の renewed_at を更新する。**heartbeat は更新前に lock を read して (a)hostname/boot-id が自分自身(self)と一致 かつ (b)generation が自分が最後に書いた値と一致(generation CAS)するか確認し、両方一致する場合のみ renewed_at を更新し generation を +1 する。不一致(他ノード/他世代が既に lease を奪取済み=generation を進めている=「lease lost」)なら更新せず ErrLeaseLost として失敗扱いにし、active を継続せず自発降格(稼働中 → standby待機)する。これにより旧 active が新 active の有効 lease を heartbeat の read→update の隙に上書きで奪い返す TOCTOU 経路を generation 不一致で検出して塞ぐ。**NFS 断等で更新できず `現在時刻 - 最後に成功した renewed_at > ttl` に至った場合も同様に、自身は active を継続せず scheduler を停止し standby待機へ降格して他ノードの昇格を妨げない。降格判定はメッセージ境界(各メッセージの 収集 / Archive 保存 / Fan-out 配置 / Manifest 記録、加えて収集後の副作用=原本 delete(source remove)と処理済み管理への MarkProcessed の前)と各永続化の前で lease 保持確認(lock ファイルの hostname/boot-id が自分自身か、かつ ttl 以内か)として行い、失っていれば「処理中のその1メッセージ」で停止して降格する。これにより split-brain の窓で重複させ得るのは高々 1 メッセージに限定される(メッセージ境界 lease 確認、spec-decision-011。被害限定の冪等 I/O は spec-decision-010)。NFS の原子性は実装依存のため generation CAS でも完全な排他は保証しない(既知の制約。exactly-once は保証しない)。
4. (active のみ)組込 HTTP サーバを metrics_port で起動し、/metrics・/healthz を公開する(SP-005。エンドポイント仕様は UC「/healthzと/metricsをHTTPで公開する」)。
5. (active のみ)ポーリングスケジューラを開始し、polling_interval ごとにユースケース層の収集配信サイクル(collect→archive→fanout→リトライ/DLQ→retention 削除)を起動する(LR-001)。サイクルの多重起動はしない(前回サイクル完了を待つ)。lease 保持者(active)だけが pull/archive する(standby は pull/archive しない、SPEC-017-01)。
6. デーモン稼働状態を「起動中 / standby待機」から「稼働中(active)」へ遷移させ、起動時メッセージ(Lock 取得結果・設定要約・メトリクスポート)を出力する。standby 待機・昇格・降格は構造化ログに出力する。以降の出力は構造化ログに行う。

#### エラーハンドリング

| エラー種別 | リトライ | DLQ | 説明 |
|-----------|---------|-----|------|
| 設定検証エラー | No | No | 起動前に検出し終了コード 2 で終了。エラー位置 + 原因 + 対処を 1 メッセージで提示する(唯一性保証方式 / lease TTL / heartbeat 間隔の不正を含む) |
| Lock 取得失敗(同一ホスト二重起動) | No | No | 終了コード 3 で終了。他ホストの有効 lease は standby 待機の契機であり対象外。stale lease は回復(奪取)対象のため対象外 |
| heartbeat 更新失敗(NFS 断等) | No | No | active を継続せず standby待機へ降格(ttl 超過で他ノードが奪取可能になる)。構造化ログに原因 + 対処(NFS 疎通・NTP 同期・lease TTL の見直し)を出力 |
| lease lost(heartbeat 所有者検証 / generation CAS で不一致) | No | No | lock の hostname/boot-id が他ノード/他世代、または generation が進んでいる(他 active が奪取済み)。renewed_at を更新せず ErrLeaseLost として active を継続せず standby待機へ自発降格(旧 active が read→update の隙に lease を奪い返さない)。構造化ログに lease lost を出力 |
| 組込 HTTP サーバ起動失敗(ポート使用中等) | No | No | 回復不能エラーとして終了コード 1。原因 + 対処(metrics_port の見直し)を構造化ログに出力 |

### 構造と責務(LockManager / heartbeat ループ / standby polling ループ)

冗長化の lease 制御は次の構造に責務分割する(実装タスクの細分化までは規定せず、構造と責務のみ。C-08 LockManager 参照)。

| 構成要素 | 配置レイヤー | 責務 |
|---------|------------|------|
| LockManager | ゲートウェイ層(`L-daemon-gateway`) | lock ファイル(lease レコード)の I/O を担う。lease 取得(`Acquire`: lock 無しなら O_CREATE\|O_EXCL で作成。stale なら read→remove→O_CREATE\|O_EXCL で奪取し boot-id 更新)、heartbeat(`Heartbeat`: 更新前に lock を read して hostname/boot-id が self 一致 かつ generation が自分の最終書込値と一致(generation CAS)するか確認し、両方一致時のみ renewed_at を更新し generation を +1。不一致なら ErrLeaseLost を返す=所有者検証 + TOCTOU 検出)、stale 判定(`現在時刻 - renewed_at > ttl`、PID 生存確認に依存しない)、lease 保持確認(lock の hostname/boot-id が自分か、かつ ttl 以内か)、解放(`Release`: graceful shutdown 時に削除)を提供する。複数 standby 同時奪取は O_EXCL で勝者1人に収束する |
| heartbeat ループ | ランタイム層(`L-daemon-runtime`、active のみ) | heartbeat_interval ごとに LockManager.Heartbeat を呼び renewed_at を更新する。Heartbeat が ErrLeaseLost(他ノードが奪取済み)を返す、または更新失敗が継続して ttl 超過に至ったら scheduler を停止し standby待機へ降格する(自発降格の境界判定。lease lost / ttl 超過の双方が降格契機) |
| standby polling ループ | ランタイム層(`L-daemon-runtime`、方式B の standby のみ) | heartbeat_interval 程度の間隔で lease の stale を監視し、stale を検知したら LockManager.Acquire で奪取を試行する(勝者なら active へ昇格、敗者なら standby 継続)。方式A では外部クラスタが起動契機を持つためこのループは動かさない(serve 自体が外部クラスタ起動時にのみ存在) |
| メッセージ境界 lease 確認 | ユースケース層(active のみ) | 各メッセージの 収集 / Archive 保存 / Fan-out 配置 / Manifest 記録 に加え、収集後の副作用=原本 delete(source remove)と処理済み管理への MarkProcessed の前に LockManager の lease 保持確認を行い、失っていれば「処理中のその1メッセージ」で停止して降格する(spec-decision-011) |

#### 旧 PID lock からの移行・互換

- 旧スキーマ(lock_holder_process_info=PID + 取得日時)は lease レコード(hostname + boot-id + acquired_at + renewed_at + ttl)へ置換する(SPEC-015-01)。
- `high_availability` を省略した単一インスタンス運用では、lease 化せず**現行の PID 相当の二重起動防止**(同一構成の 2 つ目を終了コード 3 で弾く)を維持する(後方互換)。lock ファイルの存在で二重起動を防ぐ点は従来どおりで、運用者の既存手順を壊さない。
- `high_availability` を設定すると LockManager は lease レコードで動作する。旧形式の lock ファイルが残っていた場合は、renewed_at/ttl を持たないため stale 相当として扱い(あるいは起動時に検出して)安全に奪取・再作成して lease 形式へ移行する。

## データモデル変更

### Lock(lock ファイル = lease レコード)

| 項目 | 型 | 説明 | 変更種別 |
|------|---|------|---------|
| hostname | string | active な serve が稼働するホスト名(どのホストが active かの識別) | 追加(起動時に記録) |
| boot_id | string | active 起動世代の識別子。生成元は OS の boot id(例 `/proc/sys/kernel/random/boot_id`)を基本とし、取得不能なプラットフォームでは起動ごとに採番する UUID で代替する。用途は active 起動世代の識別で、(a)同一ホストの再起動(boot id が変わる/新 UUID)と(b)別ホストによる奪取(hostname が変わる)を区別し、lease 保持確認(自分自身の世代か)とフェイルオーバ後の旧 active 残留 lease の識別に用いる | 追加(起動・奪取時に記録) |
| acquired_at | datetime | lease を取得した日時 | 追加(取得時に記録) |
| renewed_at | datetime | heartbeat で更新する最終更新日時(stale 判定の基準) | 追加(heartbeat ごとに更新) |
| ttl | integer | lease の有効期間(秒)。`現在時刻 - renewed_at > ttl` で stale | 追加(取得時に記録) |
| generation | integer | lease 世代カウンタ。取得・奪取で +1。heartbeat は所有者検証 + generation CAS(self 一致かつ generation 一致時のみ更新し +1)で TOCTOU を検出し、旧 active の lease 奪い返しを防ぐ(spec-decision-009) | 追加(取得・奪取・heartbeat 時に更新) |

> 旧スキーマの lock_holder_process_info(PID) + 取得日時による PID 生存確認方式は、マルチホストで他ホストの PID を判定できず破綻するため lease レコードへ置換する(SPEC-015-01)。

### 設定(config.yaml、読込のみ)

| 項目 | 型 | 説明 | 変更種別 |
|------|---|------|---------|
| polling_interval | integer | ポーリング間隔(秒)。スケジューラの周期 | 参照 |
| metrics_port | integer | /metrics・/healthz の公開ポート | 参照 |
| high_availability.uniqueness_method | enum | 唯一性保証方式: `lease`(方式B=lease 自動奪取)/ `external_cluster`(方式A=外部クラスタ委譲)。`high_availability` ブロックを省略すると単一インスタンス運用 | 参照(追加) |
| high_availability.lease_ttl | integer | lease TTL(秒)。NFS 属性キャッシュ(actimeo 既定60s)より十分大きく。省略時の既定も同様(SPEC-017-01) | 参照(追加) |
| high_availability.heartbeat_interval | integer | heartbeat 間隔(秒)。active が renewed_at を更新する周期(lease_ttl より十分小さく) | 参照(追加) |
| topic_definitions / subscription_definitions / source_definitions / credential_refs | text | 参照整合の検証対象 | 参照 |

#### 冗長化設定キー `high_availability.*` の既定値・検証(後方互換)

設定キーは `high_availability.*` に統一する(ui-design.md・common-components.md と同一)。`high_availability` ブロックを省略すると**従来どおり単一インスタンス運用**で、lease 化せず現行の PID 相当の二重起動防止(同一構成の 2 つ目を終了コード 3 で弾く)で動作する(後方互換)。present 時の既定値・検証は方式ごとに次のとおり。

| キー | 型 | 既定値(present 時) | 検証 | 適用方式 |
|------|---|----------------------|------|---------|
| `uniqueness_method` | enum | `lease` | `lease` / `external_cluster` のいずれか。それ以外は終了コード 2 | 共通 |
| `lease_ttl` | integer(秒) | `90`(NFS の actimeo 既定 60s より十分大きい値) | 正の整数。`heartbeat_interval` より十分大きいこと。`actimeo`(既定 60s)より十分大きいこと(満たさない場合は警告) | 方式B/方式A 共通(方式A でも lease は観測用に書くため有効) |
| `heartbeat_interval` | integer(秒) | `lease_ttl / 3`(既定 lease_ttl=90 のとき 30) | 正の整数。`lease_ttl` より十分小さいこと | 方式B(active が renewed_at を更新)。方式A では観測用 lease の更新周期 |
| (ブロック省略時) | — | 単一インスタンス運用 | lease 化しない。同一構成 2 つ目は終了コード 3(従来の二重起動防止) | 単一インスタンス |

## ビジネスルール

- 二重起動防止(single-writer): デーモンは起動時に Lock(lease)を取得し、active な serve を常に 1 つに保つ。有効な lease を他ホストが保持していれば standby 待機に入り、stale lease(renewed_at + ttl 超過)からは奪取で安全に回復する。同一ホスト・同一構成の 2 つ目は終了コード 3 で中断する(条件「二重起動防止」、SR-006、LR-002、SPEC-015-01)。
- 唯一性保証方式: 方式B(lease 自動奪取)では file-pubsub 単体で standby が ttl 失効を検知し昇格する。方式A(外部クラスタ委譲)では Pacemaker/keepalived 等の fencing に唯一性を委ね、file-pubsub は TTL 失効による自動奪取を行わない。設定で切替・併用でき同一バイナリでカバーする(SPEC-015-02、spec-decision-009)。
- heartbeat と降格(所有者検証 + generation CAS): active は heartbeat 間隔で、lock を read して hostname/boot-id が自分自身 かつ generation が自分の最終書込値と一致することを確認してから renewed_at を更新し generation を +1 する。所有者または generation が他ノード/他世代(他 active が奪取して generation を進めた=lease lost)なら更新せず自発降格する(旧 active が新 active の lease を read→update の隙に奪い返す TOCTOU を generation 不一致で検出する)。更新できず ttl 超過した場合も active を継続せず降格し、他ノードの昇格を妨げない(SPEC-015-03)。NFS の原子性は実装依存のため generation CAS でも完全な排他は保証しない(既知の制約)。
- split-brain の被害限定: lease 方式の split-brain の窓では 2 つの serve が一時的に同一 data_dir を操作しうるが、被害は AtomicWrite + at-least-once 冪等再開 + fail-closed 照合(受動的被害限定、spec-decision-010)と、メッセージ境界 lease 確認 + Manifest の message_id 更新ロック + read-merge-write + 世代 CAS(能動的上限担保、spec-decision-011)により高々 1 メッセージの重複配信に限定され、データ破損・喪失は起こさない(UC「冪等に処理を再開する」)。lease の generation CAS で heartbeat の TOCTOU を検出し旧 active の奪い返しも防ぐ。NFS の原子性は実装依存のため完全な分散排他は保証せず、本機構は『実務上の原子性 + 被害限定』で exactly-once は保証しない(既知の制約)。
- 運用前提: lease/TTL 判定は時刻と NFS 属性キャッシュに依存する。NFSv4(O_CREATE|O_EXCL の原子性)推奨・NTP 時刻同期前提・lease TTL を actimeo(既定60s)より十分大きく設定する(SPEC-017-01)。pull 型は active(lease 保持者)だけが引く。push 受信は fsnotify が NFS で効かないためフォールバックポーリング前提(対応済み、SPEC-017-02)。
- 起動前検証: 設定ミスはデーモン起動前に検出する(SR-101 のフィードフォワード)。検証 NG のままデーモンを稼働させない。
- ポーリングスケジューラ: active のみ polling_interval ごとにサイクルを起動し、多重起動しない(LR-001)。
- エラー表現: 起動エラーは終了コード(2=設定、3=同一ホスト二重起動、1=実行時)と構造化ログで表現する(CTR-002、ui-design.md 終了コード規約)。

## ティア完了条件（BDD）

```gherkin
Feature: デーモンを起動する - 常駐デーモン

  Scenario: lease を取得して active で稼働中へ遷移する
    Given 検証 OK の config.yaml(polling_interval=60、metrics_port=9090、唯一性保証方式=方式B、lease TTL=30、heartbeat 間隔=10)があり lock(lease)ファイルが存在しない
    When デーモンの起動シーケンスが host-a で実行される
    Then lease に hostname=host-a、boot-id、acquired_at、renewed_at、ttl=30 が記録される
    And heartbeat が 10 秒間隔で renewed_at を更新する
    And 組込 HTTP サーバがポート 9090 で起動し、ポーリングスケジューラが 60 秒間隔で開始される

  Scenario: stale lease を奪取して active へ昇格する
    Given lease に host-b の renewed_at が 60 秒前(ttl=30 超過)で記録されている
    When host-a でデーモンの起動シーケンスが実行される
    Then renewed_at + ttl 超過で stale と判定され(PID 生存確認に依存しない)、lease が hostname=host-a・新 boot-id で奪取・再取得される

  Scenario: 他ホストの有効 lease では standby 待機に入る(方式B)
    Given host-a の有効な lease(renewed_at が ttl=30 以内)が記録されている
    When host-b でデーモンの起動シーケンスが方式B で実行される
    Then host-b は lease を奪取せず standby 待機に入り(二重 serve を起こさない)、host-a の lease 失効(ttl 超過)を監視する

  Scenario: heartbeat 所有者検証 + generation CAS で旧 active が lease を奪い返さず降格する
    Given host-a が active で稼働していたが host-b が lease を奪取し lock の hostname=host-b・新 boot-id・generation を進めた状態になっている
    When host-a の heartbeat ループが lock を read して renewed_at を更新しようとする
    Then 更新前の所有者検証 + generation CAS で hostname/boot-id が host-a でない(または generation が host-a の最終書込値と不一致)ため ErrLeaseLost となり renewed_at は更新されない
    And host-a は scheduler を停止し稼働中から standby待機へ降格する(host-b の有効 lease を read→update の隙に上書きで奪い返さない)

  Scenario: 同一ホストの二重起動は終了コード 3 で中断する
    Given host-a で稼働中のプロセスが有効な lease(renewed_at が ttl 以内)を保持している
    When 同じ host-a・同じ構成で 2 つ目のデーモンの起動シーケンスが実行される
    Then 有効 lease を奪取できず終了コード 3 で終了する
    And 既存デーモンの lease ファイルは変更されない
```

## 共通コンポーネント参照

[common-components.md](../../../_cross-cutting/ux-ui/common-components.md) の以下を利用する。

- [C-08 LockManager](../../../_cross-cutting/ux-ui/common-components.md#c-08-lockmanager) — 起動時の Lock(lease)取得・heartbeat 更新(所有者検証 + generation CAS=hostname/boot-id 一致かつ generation 一致時のみ renewed_at 更新し +1、不一致は ErrLeaseLost で自発降格)・stale(renewed_at + ttl 超過)回復・奪取(generation を進める)/standby 待機・active 昇格/降格・同一ホスト二重起動検出(終了コード 3)。唯一性保証方式(方式A/方式B)で昇格契機を切替
- [C-09 StructuredLogger](../../../_cross-cutting/ux-ui/common-components.md#c-09-structuredlogger) — daemon_started / config_error イベントの出力
- [C-11 HTTPEndpoint](../../../_cross-cutting/ux-ui/common-components.md#c-11-httpendpoint) — /metrics・/healthz 組込 HTTP サーバの起動
- [C-12 PollingScheduler](../../../_cross-cutting/ux-ui/common-components.md#c-12-pollingscheduler) — polling_interval ごとの収集配信サイクル開始
- [C-14 ConfigLoader](../../../_cross-cutting/ux-ui/common-components.md#c-14-configloader) — 起動時の設定読込・検証(NG は終了コード 2)
- [C-15 RetentionSweeper](../../../_cross-cutting/ux-ui/common-components.md#c-15-retentionsweeper) — サイクル内 retention 削除ステップとしての組み込み
