# アーキテクチャ推論根拠サマリ(差分更新)

- event_id: 20260620_213604_arch_refine_redundant_failover
- created_at: 2026-06-20T21:36:04
- trigger_event: spec:20260620_171535_add_redundant_failover(codex 再レビュー 2巡目)
- mode: 差分更新(データモデルの表記精緻化のみ。規模・ティア構成・レイヤリング・CTP/CTR・図/decisions は不変)

## トリガー

spec イベント 20260620_171535_add_redundant_failover への codex 再レビュー(2巡目)で、arch 側に 2 点の残指摘:

| 指摘 | 内容 | 対応 |
|------|------|------|
| #9 [major] | E-001 設定の uniqueness_method / lease_ttl / heartbeat_interval がフラットで、spec の設定キー(high_availability.*)・ConfigLoader の HighAvailability 型とネスト構造が不整合 | 3 属性を high_availability.* ネスト表記へリネーム。nullable=true(present 時のみ)へ統一し既定値を description に明記 |
| #10 [minor] | E-011 Lock に旧 PID 系 lock_holder_process_info が nullable=false で残存。lease 化と矛盾 | lock_holder_process_info を削除。所有者は hostname + boot_id で識別。単一インスタンス運用は lock ファイルの存在で二重起動を防ぐ(PID フィールドなし) |

## 推論判断

- arch の attributes はフラットな name/type/description/nullable/primary_key 構造(schema-arch-design.json)。ネストの表現は属性名のドット表記(high_availability.uniqueness_method 等)で行い、spec の YAML 設定キーと 1:1 に対応させた。新規エンティティ・新規概念は追加していない(RDRA 情報「設定」「Lock」の範囲内のリネーム + 旧フィールド削除)。
- nullable を true にしたのは high_availability ブロック省略時=単一インスタンス運用(これらのキーを持たない)を表現するため。present 時の既定(lease / 90 / lease_ttl/3)は description に記載。
- E-011 の lock_holder_process_info 削除は破壊的だが、lease レコード化(前イベント 20260620_203907 で hostname/boot_id/acquired_at/renewed_at/ttl を追加済み)により所有者識別は hostname + boot_id で完結する。単一インスタンス運用の二重起動防止は lock ファイルの存在(O_CREATE|O_EXCL)で担保するため PID フィールドは不要。

## 不変(再推論しない)

- 規模見積り・ティア構成(tier-daemon-worker / tier-ops-cli)・レイヤリング(runtime/usecase/domain/gateway)
- CTP-006 / CTP-010 / CTR-004(前イベント 20260620_203907 を維持)
- system_architecture の diagram_mermaid(VIP/active/standby/外部クラスタ)
- arch-decision-005 / 006(前イベントを維持)
