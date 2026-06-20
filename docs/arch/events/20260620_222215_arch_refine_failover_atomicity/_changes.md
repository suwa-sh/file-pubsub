# 変更サマリ

- event_id: 20260620_222215_arch_refine_failover_atomicity
- trigger_event: spec:20260620_171535_add_redundant_failover(codex 3巡目レビュー・案Z)
- created_at: 2026-06-20T22:22:15
- mode: 差分更新(冗長構成 arch の codex 3巡目残指摘。CTP-010 / system 図 / E-011 のみ。CTP-006/CTR-004・他エンティティ・decisions・application_architecture は前イベント 20260620_213604 / 20260620_203907 を維持)

## 変更

- technology_context/cross_cutting_patterns/CTP-010(split-brain 被害の限定): name に「+ message_id ロック直列化」を追加。description を更新し、(3)メッセージ境界 lease 確認の永続化点に 原本 delete / 処理済み MarkProcessed を追加、(4)Manifest 更新を message_id 単位の更新ロック(O_CREATE|O_EXCL)で直列化する主機構 + ロック下の read-merge-write + 世代 CAS + merge precedence(delivered/dlq は決着で上書き不可・failed は delivered へ昇格可)を明記、(5)lease の heartbeat 所有者検証 + generation CAS による TOCTOU 検出を追記。**既知の制約**(NFS では完全な分散 CAS / 分散排他は不能・exactly-once 非保証・被害限定)を明記。reason を案Z(実務上の原子性 + 被害限定 + 既知制約明記)へ更新。source_model に 情報: Lock を追加
- technology_context/diagram_mermaid(system 図): ACT->STBY の単一ラベル「lease失効で昇格 / 外部クラスタfencing」を 2 経路へ分離 —「方式B: lease失効(renewed_at+ttl超過)を検知し奪取して昇格(STBY→ACT)」と「方式A: 外部クラスタが serve リソースを起動/停止(CLUSTER→ACT)」。CLUSTER->VIP の fencing/VIP 制御は維持。Manifest 配送記録のラベルを「message_idロック+read-merge-write」へ更新
- data_architecture/entities/E-011 Lock: `generation`(integer, nullable=false)属性を追加。取得・奪取で +1、heartbeat の generation CAS / 奪取の世代前進で TOCTOU を検出。renewed_at description に generation CAS を追記
- data_architecture/storage_mapping/E-011: reason に generation・heartbeat の所有者検証 + generation CAS・NFS 既知制約(exactly-once 非保証)を追記
- data_architecture ER 図(diagram_mermaid): LOCK ブロックに `integer generation` 行を追加

## 追加 / 削除

- 追加: E-011 の `generation` 属性 / system 図の方式A・方式B の 2 経路
- 削除: なし

## codex 4巡目対応(minor 表記整合)

- 4巡目 minor 表記整合(decision-011 title/列挙、CTP-006 generation): `arch-design.yaml` CTP-006 の lease record 説明に generation を追加(同ファイル E-011 で generation 定義済みとの欠落整合)。CTP-006 は本 arch event の差分対象外(前イベント維持)のため latest のみ修正。arch ドキュメントのみ。Go 実装は不変

## confidence: user の上書きについて

- CTP-010・E-011 は前イベントで confidence: user(REQ-016/REQ-018 のユーザー指定)。本変更は同一ユーザー判断(案Z)に基づく実装機構の精緻化(message_id ロック直列化・generation CAS・NFS 既知制約明記・図の経路分離)であり user → user の更新として扱う。RDRA の情報「Lock」「Manifest」に無い新規 RDRA 要素は追加していない(generation は情報「Lock」の属性、message_id ロックは情報「Manifest」更新の実装機構)。
