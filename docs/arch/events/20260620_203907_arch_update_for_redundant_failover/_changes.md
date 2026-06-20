# 変更サマリ

- event_id: 20260620_203907_arch_update_for_redundant_failover
- trigger_event: rdra:20260620_171535_add_redundant_failover, nfr:20260620_194114_nfr_redundant_failover
- created_at: 2026-06-20T20:39:07
- mode: 差分更新(冗長構成 active/standby 対応。REQ-016/REQ-018/SPEC-016-01/SPEC-018-01)

## 追加

- system_architecture/cross_tier_policies: CTP-010(split-brain 被害の限定。冪等 I/O + メッセージ境界 lease 確認 + Manifest read-merge-write で高々 1 メッセージ重複に限定。重い分散合意を持たない)
- system_architecture/cross_tier_rules: CTR-004(single-writer は lease 保持者(active)のみ。standby は書き込まない。pull/push 収集も lease 保持者だけが行う)
- data_architecture/entities/E-001 設定: uniqueness_method / lease_ttl / heartbeat_interval / fallback_polling_interval 属性を追加。E-011 Lock への 1:1 関連を追加
- data_architecture/entities/E-011 Lock: hostname / boot_id / renewed_at / ttl 属性を追加。E-001 設定への N:1 関連を追加
- decisions: arch-decision-005(唯一性保証の二方式併用)、arch-decision-006(split-brain 被害限定)

## 変更

- technology_context/constraints: 「単一インスタンス前提(HA なし)」→「active/standby 冗長構成(VIP + 共有 data_dir。lease 自動奪取 or 外部クラスタ委譲)」へ更新。共有 data_dir(NFSv4 推奨)・2 ノード active/standby・NTP 時刻同期前提を追記
- system_architecture/cross_tier_policies/CTP-006: 「単一インスタンス・非冗長(復旧は再起動 + Replay)」→「active/standby 冗長化(lease 自動奪取 / 外部クラスタ委譲)」へ全面改訂。VIP + 共有 data_dir、二方式併用(方式A 外部クラスタ fencing / 方式B lease 自動奪取)・同一バイナリ・重い分散合意なしを明記。confidence: user 維持(REQ-018 のユーザー判断)
- system_architecture/diagram_mermaid: VIP・active/standby・外部クラスタ(方式A)・共有 data_dir(NFSv4)・lease 保持者のみ収集を反映
- data_architecture/entities/E-011 Lock: model_type は resource_mutable のまま。PID 生存確認ベースのロックを lease レコード化(stale 判定を renewed_at + ttl 超過へ)
- data_architecture/storage_mapping/E-011: 共有 data_dir 上の lease ファイルである旨・renewed_at + ttl による stale 判定・マルチホスト対応へ reason を更新

## 削除

- なし

## confidence: user の上書きについて

- CTP-006 は既存 confidence: user。本変更は REQ-018/REQ-016 によるユーザー判断(差分更新の指示そのもの)であり user → user の更新として扱う(楽観的な自動上書きではない)。
- 新規 CTP-010 / CTR-004 / E-011・E-001 の追加属性も REQ-016/REQ-018 のユーザー指定値のため confidence: user とする。
