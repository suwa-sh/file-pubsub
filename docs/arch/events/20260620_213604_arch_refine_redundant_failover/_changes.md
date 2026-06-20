# 変更サマリ

- event_id: 20260620_213604_arch_refine_redundant_failover
- trigger_event: spec:20260620_171535_add_redundant_failover(codex 再レビュー 2巡目)
- created_at: 2026-06-20T21:36:04
- mode: 差分更新(冗長構成 arch の codex 再レビュー残指摘 #9/#10。データモデルのみ。CTP/CTR/図/decisions は前イベント 20260620_203907 を維持)

## 変更

- data_architecture/entities/E-001 設定: フラットだった `uniqueness_method` / `lease_ttl` / `heartbeat_interval` を `high_availability.uniqueness_method` / `high_availability.lease_ttl` / `high_availability.heartbeat_interval`(high_availability.* ネスト表記)へリネーム。spec の設定キー(high_availability.*)・ConfigLoader 契約と整合。nullable を true(present 時のみ設定。ブロック省略時=単一インスタンス運用)に統一し、present 時の既定値(lease / 90 / lease_ttl/3)を description に明記。E-011 への 1:1 関連 description も high_availability 規定へ更新
- data_architecture/entities/E-011 Lock: 旧 PID 系フィールド `lock_holder_process_info`(nullable=false)を削除。lease 化(hostname/boot_id/acquired_at/renewed_at/ttl)に統一し、所有者は hostname + boot_id で識別。renewed_at description に heartbeat 所有者検証(自分自身に一致する場合のみ更新)を追記。単一インスタンス運用は lock ファイルの存在で二重起動を防ぐ点を明記(PID フィールドは持たない)
- data_architecture/storage_mapping/E-011: reason を「旧 PID フィールドを持たず所有者は hostname + boot_id」「単一インスタンスは lock ファイル存在で二重起動防止」へ更新
- diagram_mermaid(data_architecture ER): LOCK ブロックから `lock_holder_process_info` 行を削除

## 追加 / 削除

- 追加: なし(属性のリネームのみ)
- 削除: E-011 の `lock_holder_process_info` 属性

## confidence: user の上書きについて

- E-001・E-011 の属性は前イベントで confidence: user(REQ-016/REQ-018 のユーザー指定)。本変更は同一ユーザー判断の表記精緻化(high_availability.* ネスト・旧 PID 廃止)であり user → user の更新として扱う。RDRA の情報「設定」「Lock」に無い新規属性は追加していない(リネーム + 旧フィールド削除のみ)。
