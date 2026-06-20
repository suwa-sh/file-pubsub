# 変更内容 (event 20260620_171535_add_redundant_failover)

冗長構成(active/standby 自動フェイルオーバー)対応の差分仕様化(REQ-015〜018 / SPEC-015-01〜018-01)。
Lock を lease レコード化し、唯一性保証を方式B(lease 自動奪取)/ 方式A(外部クラスタ委譲)の二方式併用にする。
split-brain の被害を既存の冪等 I/O で「高々1メッセージの重複配信」に限定する。UC「デーモンを起動する」「冪等に処理を再開する」と、lock/設定/共通コンポーネント/メトリクスの cross-cutting が影響。正常系の採番形式・後段(Archive / Fan-out / Manifest)は不変。

## 変更したファイル

| ファイル | 変更内容 |
|---------|---------|
| `配信基盤運用業務/.../デーモンを起動する/spec.md` | Lock を lease 化(hostname+boot-id+acquired_at+renewed_at+ttl)。stale 判定を PID 生存確認から renewed_at+ttl 超過へ置換。バリエーション「唯一性保証方式」(方式A/方式B)・外部システム「外部クラスタ」を追加。デーモン稼働状態に standby待機・昇格/降格を追加。データフロー/処理フロー(sequence)に lease 取得・heartbeat・standby・奪取を反映。計算ルールに lease stale 判定・heartbeat 更新。BDD を lease/standby/方式A の具体値(ttl=30,hb=10,actimeo=60)で更新(SPEC-015-01/02/03) |
| `配信基盤運用業務/.../デーモンを起動する/_model-summary.yaml` | object_storage の lock を lease レコードへ、config.yaml の purpose に冗長化設定を追記 |
| `配信基盤運用業務/.../デーモンを起動する/tier-daemon-worker.md` | 起動シーケンスに lease 取得・standby 待機・奪取・heartbeat・降格を追記。データモデルを lease スキーマへ置換、設定に uniqueness_method/lease_ttl/heartbeat_interval を追加。エラーハンドリングに heartbeat 更新失敗を追加。ビジネスルール(唯一性保証方式・運用前提・split-brain 被害限定)を追記。BDD 更新。C-08 参照を lease 表記へ |
| `配信基盤運用業務/.../冪等に処理を再開する/spec.md` | フェイルオーバー=別ホストのクラッシュ再開と等価。split-brain 被害を AtomicWrite+at-least-once 冪等再開+fail-closed 照合で高々1メッセージ重複に限定(SPEC-016-01)。分岐条件に AtomicWrite配置・冪等照合の fail-closed を追加。関連 RDRA に AtomicWrite配置/message_id採番。BDD に昇格再開・split-brain の 2 Scenario 追加 |
| `配信基盤運用業務/.../冪等に処理を再開する/tier-daemon-worker.md` | 変更概要に split-brain 被害限定。ビジネスルールに「フェイルオーバー=クラッシュ再開と等価」「split-brain の被害限定」「復旧方式の冗長化前提への見直し(SPEC-018-01)」。BDD に split-brain Scenario 追加 |
| `_cross-cutting/datastore/object-storage-schema.yaml` | config.yaml の purpose に冗長化設定を追記。lock パターンを lease レコード(hostname/boot-id/acquired_at/renewed_at/ttl)+ renewed_at+ttl 超過 stale 判定 + 奪取回復へ更新。readers に「冪等に処理を再開する」追加 |
| `_cross-cutting/datastore/datastore-schema.md` | LOCK エンティティ ER を lease スキーマへ。config/lock の説明行を冗長化設定・lease へ更新 |
| `_cross-cutting/ux-ui/ui-design.md` | 設定 YAML 例に high_availability(uniqueness_method/lease_ttl/heartbeat_interval)。記述ルールに冗長化設定キーの検証規約。新節「active/standby 冗長化の運用前提(lease 方式)」(NFSv4/NTP/TTL>actimeo/方式A の VIP 同居/pull・push の前提/split-brain/観測) |
| `_cross-cutting/ux-ui/common-components.md` | C-08 LockManager を lease 取得・奪取・heartbeat・stale(renewed_at+ttl)回復・standby/昇格/降格・方式A/B へ更新。インターフェース案を Lease 構造体 + Heartbeat へ。サマリ表の利用 UC 数を 2→3 |
| `_cross-cutting/ux-ui/data-visualization.md` | 契約上の注意に「冗長化(lease)メトリクスは見送り」を明記(RDRA 情報「メトリクス」に無く、構造化ログ + /healthz で観測。RDRA に無いメトリクスを発明しない方針を維持) |
| `decisions/spec-decision-009.yaml` | (新規) 唯一性保証の二方式併用(lease 自動奪取 + 外部クラスタ委譲/fencing)、stale 判定を lease の renewed_at+ttl へ置換 |
| `decisions/spec-decision-010.yaml` | (新規) split-brain 被害を既存冪等 I/O(AtomicWrite + at-least-once 冪等再開 + fail-closed 照合)で高々1メッセージ重複に限定する設計判断 |

## 新規採番した ID

| ID | 種別 | 指すもの |
|----|------|---------|
| spec-decision-009 | Decision | 唯一性保証の二方式併用(方式B=lease 自動奪取 / 方式A=外部クラスタ委譲)・stale 判定の lease 化 |
| spec-decision-010 | Decision | split-brain 被害を冪等 I/O で高々1メッセージ重複に限定 |
| spec-decision-011 | Decision | (codex レビュー対応・案A)split-brain の重複上限「高々1メッセージ」(REQ-016)を、メッセージ境界 lease 確認 + Manifest の read-merge-write で実装上維持する |

(USDM/RDRA 側の REQ-015〜018 / SPEC-015-01〜018-01・バリエーション「唯一性保証方式」・外部システム「外部クラスタ」・状態 standby待機 等は requirements/rdra イベントで採番済み。spec はそれらを参照・反映)

## codex レビュー対応(#3-13、案A)

冗長構成 spec への codex レビュー指摘を、distillery 差分スタイルで取り込んだ(spec ドキュメントのみ。Go 実装コードは変更しない)。設計判断は案A(split-brain の重複上限 REQ-016「高々1メッセージ」を実装で維持: メッセージ境界 lease 確認 + Manifest の read-merge-write。重い分散合意は導入しない)。

| 指摘 | 対応箇所 |
|------|---------|
| #3 [major] lock=lease の構造化スキーマ | `_cross-cutting/datastore/object-storage-schema.yaml` schemas.lock_lease を追加(hostname/boot_id/acquired_at/renewed_at/ttl の型・必須性・RFC3339・ttl 秒)。lock パターン purpose に schemas.lock_lease 参照を追記 |
| #4 [major] 方式A 起動モデルの一意化 | `デーモンを起動する/spec.md`(バリエーション一覧・処理フロー後の方式別 note・分岐条件「二重起動防止」)+ `tier-daemon-worker.md`(処理フロー 2 を方式B/方式A で分岐)。方式A は非 active で standby 常駐させず外部クラスタ起動時に常に active、lease は観測用、hostname/boot-id 不一致なら奪取、standby 判定はしない |
| #5 [major] data-directory 説明の整合 | `object-storage-schema.yaml` data-directory description を「ローカル FS、または HA 時は NFS(v4 推奨)共有 data_dir」へ |
| #6 [blocker] stale lease 奪取の原子性 | `デーモンを起動する/spec.md`(計算ルール「stale lease 奪取」追加・状態遷移 Lock状態 stale→取得済・分岐条件)+ `tier-daemon-worker.md`(処理フロー 2)。read→remove→O_CREATE\|O_EXCL で 1 人収束、敗者 standby 継続、heartbeat 競合も O_EXCL 収束 |
| #7 [major] heartbeat 失敗時の自発降格境界 | `デーモンを起動する/spec.md`(計算ルール「heartbeat 失敗時の自発降格」追加)+ `tier-daemon-worker.md`(処理フロー 3)。ttl 超過で scheduler 停止、メッセージ境界/各永続化前に lease 保持確認(案A 接続) |
| #8 [minor] boot-id の用途定義 | `デーモンを起動する/tier-daemon-worker.md`(boot_id データモデル行)+ `object-storage-schema.yaml` schemas.lock_lease。生成元(OS boot id 基本・不能時 UUID)・用途(起動世代識別/再起動と奪取の区別) |
| #9 [blocker] 案A の高々1メッセージ維持を BDD/分岐/計算へ | `冪等に処理を再開する/spec.md`(概要・分岐条件「メッセージ境界 lease 確認」・BDD 2 Scenario)+ `tier-daemon-worker.md`(処理フロー 2・ビジネスルール・BDD)。各メッセージ処理前の lease 保持確認で旧 active を高々1メッセージで停止 |
| #10 [major] Manifest の read-merge-write | `冪等に処理を再開する/spec.md`(データフロー表・処理フロー sequence・分岐条件「Manifest の read-merge-write」・BDD)+ `tier-daemon-worker.md`(処理フロー 3・Manifest データモデル・ビジネスルール・BDD・C-03 参照)。書込直前再読込+マージで lost update 回避、決着状態 retention 保護 |
| #11 [major,部分] LockManager/heartbeat/standby polling/旧 PID 移行の構造 | `デーモンを起動する/tier-daemon-worker.md`「構造と責務」節 + 「旧 PID lock からの移行・互換」節。`common-components.md` C-08 に HoldsLease 追加・奪取/標準キーを追記 |
| #12 [major] 設定キーを high_availability.* に統一 | `ui-design.md`(既設)・`tier-daemon-worker.md`(config 表を high_availability.* へ)・`common-components.md`(C-08 に正式キー追記)の 3 者を `high_availability.uniqueness_method`(lease/external_cluster)/`lease_ttl`/`heartbeat_interval` に統一 |
| #13 [major] 既定値と後方互換 | `tier-daemon-worker.md`「冗長化設定キーの既定値・検証(後方互換)」表(uniqueness_method=lease / lease_ttl=90 / heartbeat_interval=lease_ttl/3、省略時=単一インスタンス)+ `ui-design.md` 記述ルール行に既定値・後方互換を追記 |

不採用/保留: なし(arch CTP-006・NFR A.2.1.1 は後段 dist-architecture / dist-quality-attributes の責務のため触らない=指示どおりスコープ外。RDRA に無い要素の新規発明なし)。

## codex 再レビュー対応(2巡目: CAS 精緻化・Heartbeat 所有者検証・方式A状態遷移・cross-cutting契約・永続化点拡張)

冗長構成 spec への codex 再レビュー(2巡目)の残指摘を distillery 差分スタイルで取り込んだ(spec ドキュメントのみ。Go 実装は不変)。案A を「read-merge-write + 世代 CAS + 競合リトライ」へ精緻化(方針転換ではなく穴埋め)。Manifest は message_id 別ファイルのため競合は同一 message_id のみ。重い分散合意は導入しない。

| 指摘 | 対応箇所 |
|------|---------|
| B1/観点2 [blocker] Manifest lost update を 世代 CAS で精緻化 | `冪等に処理を再開する/spec.md`(概要・データフロー表・処理フロー sequence の rename 直前 CAS 分岐・分岐条件「Manifest の read-merge-write + 世代 CAS」・計算ルール「Manifest 世代 CAS」追加・BDD)+ `tier-daemon-worker.md`(処理フロー 3 を read→世代観測→merge→rename 直前 CAS→不一致リトライへ・Manifest データモデル・ビジネスルール・C-03 参照・BDD)+ `spec-decision-011`(decision に世代 CAS/リトライ追記・alternatives に「CAS なし read-merge-write のみ」追加) |
| #9 [major] decisions 役割分担 | `spec-decision-010`(title・context末尾に「受動的被害限定」役割と 011 依存を明記)+ `spec-decision-011`(decision末尾に 010=受動的被害限定 / 011=能動的上限担保 の役割分担)。両 spec.md/tier の本文にも役割分担を反映 |
| #4/観点3 [major] 方式A を standby 状態遷移から除去 | `デーモンを起動する/spec.md` 状態遷移の「起動中→standby待機」「standby待機→稼働中」を方式B 限定に、状態サマリ行を方式A=(初期)→起動中→稼働中 / 方式B のみ standby に修正 |
| Heartbeat 所有者検証 [major] | `common-components.md` C-08(Heartbeat 署名に self 追加・ErrLeaseLost・所有者検証の責務記述)+ `デーモンを起動する/tier-daemon-worker.md`(処理フロー 3・構造表 LockManager/heartbeat ループ・heartbeat ビジネスルール・エラーハンドリング lease lost 行・C-08 参照・BDD)+ `spec.md`(計算ルール heartbeat 更新/自発降格・状態遷移 稼働中→standby待機・BDD)。一致時のみ renewed_at 更新、不一致は自発降格(旧 active が奪い返さない) |
| #12/#13 [major] cross-cutting コンポーネント契約 | `common-components.md` C-14 ConfigLoader(Config に HighAvailability 型追加・Load の既定値補完・Validate の HA 検証・責務記述)+ C-03 ManifestStore(PutMerged=read-merge-write+世代 CAS+競合リトライ を追加・Put との役割分担・責務記述) |
| 観点1 [major] 永続化点の拡張 | `冪等に処理を再開する/spec.md`(分岐条件・BDD)+ `tier-daemon-worker.md`(処理フロー 2・ビジネスルール・BDD)+ `デーモンを起動する/spec.md` 計算ルール「自発降格」。メッセージ境界 lease 確認対象に 原本 delete(source remove)と 処理済み MarkProcessed を追加 |
| #7 [minor] ui-design.md split-brain 参照 | `ui-design.md` 運用前提節イントロ・split-brain 行に spec-decision-011 とメッセージ境界 lease 確認 / read-merge-write+世代 CAS を参照追記 |
| #8 [minor] 冪等再開 UC で C-08 を使用 | `common-components.md` UC×コンポーネントマトリクスの「冪等に処理を再開する」行 C-08 を - → ○。C-03 schema(object-storage-schema.yaml manifest_json)に revision フィールド + read-merge-write+CAS purpose を追記 |

2巡目の不採用/保留: なし。arch 側(#9 設定の high_availability.* ネスト化・#10 E-011 旧 PID フィールド)は arch 差分イベントで対応(下記)。

## codex 3巡目対応(案Z: message_id ロック直列化・lease generation CAS・NFS 限界明記・方式A sequence/arch図・起動UC永続化点・failed merge precedence)

冗長構成 spec への codex 3巡目レビューの残指摘を distillery 差分スタイルで取り込んだ(spec/arch ドキュメントのみ。Go 実装は不変)。設計方針は案Z(NFS 共有 FS 上で完全な分散 CAS は原理的に困難という前提を受け入れ、(1)実務上の原子性を message_id 単位の更新ロックで担保し、(2)残る理論限界は『既知の制約』として正直に明記。exactly-once は約束せず、稀な競合は被害限定で吸収し決着状態は retention 保護。重い分散合意機構は導入しない)。

| 指摘 | 対応箇所 |
|------|---------|
| [blocker] Manifest 世代 CAS の原子性(message_id 単位の更新ロックで直列化) | `_cross-cutting/datastore/object-storage-schema.yaml`(manifest pattern purpose・新規 pattern `manifest/{message_id}.json.lock`・revision field・data-directory description に NFS 既知制約)+ `spec-decision-011`(decision に message_id ロックを主機構として追記・context に NFS 前提・consequences に NFS 限界・alternatives に「世代 CAS のみ」追加)+ `冪等に処理を再開する/spec.md`(概要・データフロー・処理フロー sequence に lock acquire/release・分岐条件・計算ルール・関連RDRA・BDD)+ `冪等に処理を再開する/tier-daemon-worker.md`(処理フロー step3 (0) lock・データモデル・ビジネスルール・C-03 参照・BDD)+ `common-components.md` C-03 PutMerged。NFS 限界(O_CREATE\|O_EXCL の原子性は実装依存・exactly-once 非保証)を各所に明記 |
| [major] Heartbeat の TOCTOU(lease generation CAS) | `object-storage-schema.yaml` schemas.lock_lease に `generation` field 追加 + lock pattern purpose + 説明 + arch 差分(E-011 に generation)。`spec-decision-009`(decision に generation CAS 手順・NFS 既知制約・consequences)+ `デーモンを起動する/spec.md`(データフロー FS_Lock・sequence heartbeat note・計算ルール heartbeat 更新)+ `デーモンを起動する/tier-daemon-worker.md`(処理フロー3・構造表 Heartbeat・Lease データモデルに generation・ビジネスルール・BDD・C-08 参照・エラーハンドリング lease lost)+ `common-components.md` C-08(責務・Lease 構造体 Generation・Heartbeat 署名)。NFS 限界を明記 |
| [major整合] 方式A sequence 分離 | `デーモンを起動する/spec.md` 処理フロー sequence の lease 分岐を方式B 限定 note 付きに分離(「lease あり+有効」を方式B のみの standby に。方式A は処理フロー後 note 参照) |
| [major整合] arch 図の ACT->STBY 2 経路分離 | arch 差分イベント(下記)の diagram_mermaid を「方式B: lease 失効で standby→active」「方式A: 外部クラスタが serve リソースを起動/停止」の 2 経路に分離 |
| [major整合] 起動 UC 永続化点に 原本 delete + MarkProcessed | `デーモンを起動する/tier-daemon-worker.md`(処理フロー3 の降格判定永続化点列挙・構造表「メッセージ境界 lease 確認」行に 原本 delete(source remove)と MarkProcessed を追加。冪等再開 UC と一致) |
| [major整合] decision-011 failed merge precedence | `spec-decision-011`(decision に merge precedence=delivered/dlq は決着で上書き不可・failed は中間で delivered へ昇格可 を定義)+ `冪等に処理を再開する/spec.md`(分岐条件・計算ルール「merge precedence」追加・BDD)+ `冪等に処理を再開する/tier-daemon-worker.md`(処理フロー3 (b)・BDD)+ `common-components.md` C-03。merge 対象(delivered/failed/dlq)と保護する決着状態(delivered/dlq)の不整合を解消 |
| [minor] config.yaml purpose の HA キーをネスト表記へ | `object-storage-schema.yaml` config.yaml pattern purpose を `high_availability.uniqueness_method/lease_ttl/heartbeat_interval` ネスト表記へ統一 |

3巡目の不採用/保留: なし。RDRA に無い要素の新規発明なし(generation/manifest lock は情報「Lock」「Manifest」の属性・実装機構であり新規 RDRA 要素ではない)。

## codex 4巡目対応(minor 表記整合)

4巡目 minor 表記整合(decision-011 title/列挙、CTP-006 generation): `spec-decision-011` の title を旧称「案A」から案Z の実体(message_id ロック + 世代 CAS + read-merge-write + メッセージ境界 lease 確認)へ修正し、メッセージ境界 lease 確認の永続化点列挙に 原本 delete(source remove)・処理済み管理への MarkProcessed を追加(冪等再開 spec.md と一致)。arch 側は `arch-design.yaml` CTP-006 の lease record 説明に generation を追加(E-011 と整合)。spec ドキュメントのみ。Go 実装は不変。

## メトリクス契約の判断

lease 状態・active 昇格回数等の専用メトリクスは**追加見送り**。RDRA 情報「メトリクス」の属性(最終収集時刻 / 処理件数 / 配信失敗数 / DLQ 件数 / 滞留数)に無く、昇格・standby・降格・lease 奪失は構造化ログ(event_type)で観測でき、`/healthz`(active のみ 200)で稼働判定できる。RDRA に無いメトリクスを発明しない既存方針(data-visualization.md)を維持。data-visualization.md に判断を明記。

## トレーサビリティへの影響

RDRA 側で追加済みの要素(バリエーション「唯一性保証方式」、外部システム「外部クラスタ」、状態「standby待機」、各 condition の更新、BUC「デーモンを起動する」「冪等に処理を再開する」の更新)を spec の関連 RDRA モデル表へ反映。spec 側で RDRA に無いアクター/情報/BUC/エンティティは発明していない。
