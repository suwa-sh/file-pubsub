# アーキテクチャ推論根拠サマリ(差分更新)

- event_id: 20260620_222215_arch_refine_failover_atomicity
- created_at: 2026-06-20T22:22:15
- trigger_event: spec:20260620_171535_add_redundant_failover(codex 3巡目レビュー・案Z)
- mode: 差分更新(CTP-010 / system 図 / E-011 の精緻化のみ。規模・ティア構成・レイヤリング・他 CTP/CTR・decisions・application_architecture は不変)

## トリガー

spec イベント 20260620_171535_add_redundant_failover への codex 3巡目レビューで、Manifest 更新の原子性(blocker)・Heartbeat の TOCTOU(major)・arch 図の経路混在(major)が arch 側にも波及:

| 指摘 | 内容 | 対応 |
|------|------|------|
| [blocker] | Manifest 世代 CAS だけでは read→rename 間の lost update が残り、2 active 同時更新で決着状態を取りこぼしうる | CTP-010 に message_id 単位の更新ロック(O_CREATE\|O_EXCL)による直列化を主機構として追記。世代 CAS はロック下の二重チェックに位置づけ。NFS で完全な分散排他は不能という既知制約(exactly-once 非保証・被害限定)を明記 |
| [major] | heartbeat の所有者検証は check→update の隙に旧 active が新 active の lease を奪い返せる TOCTOU が残る | E-011 Lock に generation(世代カウンタ)属性を追加し、heartbeat を generation CAS で直列化(CTP-010 にも記載) |
| [major] | system 図の ACT->STBY が「lease失効で昇格 / 外部クラスタfencing」を同一ラベルにして方式A/方式B が混在 | 図を「方式B: lease失効で standby→active」「方式A: 外部クラスタが serve リソースを起動/停止」の 2 経路に分離 |

## 推論判断(案Z)

- NFS 共有 FS 上で完全な分散 CAS / 分散排他は原理的に困難という前提を受け入れる。実務上の原子性を message_id 単位の更新ロックで担保し、残る理論限界は CTP-010 description / E-011 generation / storage_mapping reason に「既知の制約」として正直に明記する(exactly-once は約束しない。稀な競合は被害限定 = AtomicWrite で破損なし・決着状態は retention 保護・被害は重複配信に限定)。重い分散合意機構(専用ロックサービス等)は導入しない。
- generation は情報「Lock」の属性として追加(新規 RDRA エンティティではない)。message_id 更新ロックは情報「Manifest」更新の実装機構であり、新規 RDRA 要素を発明していない。
- system 図の経路分離は spec(デーモンを起動する spec.md の方式別 note・状態遷移)と整合させた。方式B のみ STBY を経由し、方式A は外部クラスタが ACT を直接起動/停止する。

## 不変(再推論しない)

- 規模見積り・ティア構成(tier-daemon-worker / tier-ops-cli)・レイヤリング(runtime/usecase/domain/gateway)
- CTP-006 / CTP-007/008/009 / CTR-001〜004(本イベントは CTP-010 のみ精緻化)
- data_architecture の E-011 以外のエンティティ・E-001(前イベント 20260620_213604 の high_availability.* を維持)
- arch-decision-005 / 006(前イベントを維持)
