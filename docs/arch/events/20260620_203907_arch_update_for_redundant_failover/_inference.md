# アーキテクチャ推論根拠サマリ(差分更新)

- event_id: 20260620_203907_arch_update_for_redundant_failover
- created_at: 2026-06-20T20:39:07
- trigger_event: rdra:20260620_171535_add_redundant_failover, nfr:20260620_194114_nfr_redundant_failover
- mode: 差分更新(冗長構成 active/standby 対応のみを再推論。規模・ティア構成・レイヤリングは不変)

## トリガーとなった RDRA/NFR 差分

### RDRA 差分(20260620_171535_add_redundant_failover)

| 種別 | 要素 | 変更 |
|------|------|------|
| バリエーション | 唯一性保証方式 | 追加。方式A(外部クラスタ委譲)/ 方式B(lease 自動奪取)の切替軸 |
| 外部システム | 外部クラスタ | 追加。方式A の Pacemaker/keepalived(fencing・VIP+serve 同一リソースグループ) |
| 情報 | Lock | lease レコード化(hostname/boot-id/acquired_at/renewed_at/ttl)。stale 判定を renewed_at + ttl へ |
| 情報 | 設定 | 唯一性保証方式・lease TTL・heartbeat 間隔・フォールバックポーリング間隔を追加 |
| 状態 | デーモン稼働状態 / Lock状態 | standby待機・昇格/降格遷移、lease 奪取遷移を追加 |
| 条件 | 二重起動防止 / AtomicWrite配置 / 二重配信防止 / message_id採番 | lease 化・split-brain 被害限定・fail-closed を追記 |

### NFR 差分(20260620_194114_nfr_redundant_failover)

| ID | メトリクス | 旧Lv → 新Lv | アーキ影響 |
|----|----------|------------|-----------|
| A.2.1.1 | サーバ内の冗長化 | 1 → 4 | CTP-006 を active/standby 完全冗長化(自動切替)へ全面改訂 |
| A.1.2.1 | サービス切替時間 | 1 → 4 | lease 失効検知〜standby 昇格で分オーダー自動切替(CTP-006) |
| A.4.1.2 | RTO | 2 → 4 | 自動フェイルオーバーで 10 分以内(CTP-006) |
| A.4.1.3 | RLO | 1 → 3 | standby が同一設定で昇格し平常時同等水準で復旧(CTP-006) |
| C.3.3.1 | 障害復旧方式 | 1 → 1(据置) | 復旧方式の説明を standby 自動昇格 + split-brain 被害限定へ(CTP-010) |

## 設計判断サマリ

### システムアーキテクチャ(変更分)

| 項目 | 推論結果 | confidence | 根拠 |
|------|---------|-----------|------|
| CTP-006 | 単一インスタンス → active/standby 冗長化(VIP + 共有 data_dir、二方式併用、同一バイナリ、重い合意なし) | user | REQ-018/SPEC-018-01, NFR A.2.1.1/A.1.2.1/A.4.1.2/A.4.1.3 |
| CTP-010 | split-brain 被害限定(AtomicWrite + fail-closed 冪等 + メッセージ境界 lease 確認 + Manifest read-merge-write、高々1メッセージ重複) | user | REQ-016/SPEC-016-01, NFR A.4.1.1/C.3.3.1 |
| CTR-004 | single-writer は lease 保持者(active)のみ。standby は書き込まない | user | 情報: Lock, NFR A.2.1.1 |
| technology_context.constraints | 単一インスタンス → active/standby(VIP + NFSv4 共有 data_dir、NTP 同期前提) | user | REQ-018 |

### データアーキテクチャ(変更分)

| エンティティ | 変更 | ストレージ | confidence | 根拠 |
|-------------|------|----------|-----------|------|
| E-011 Lock | lease レコード化(hostname/boot-id/renewed_at/ttl 追加)。stale 判定を renewed_at + ttl へ | file(共有 data_dir/NFSv4) | user | 情報: Lock |
| E-001 設定 | uniqueness_method/lease_ttl/heartbeat_interval/fallback_polling_interval 追加 | file | user | 情報: 設定, バリエーション: 唯一性保証方式 |

## 据置(意図的に変更しない)

- ティア構成(tier-daemon-worker / tier-ops-cli)・レイヤリング(runtime → usecase → domain / gateway)は不変。冗長化は同一バイナリの起動モード(active/standby)とロック方式の変更であり、レイヤ責務は変わらない
- 外部クラスタは「外部システム」であり新規ティアとして system_architecture.tiers には追加しない(RDRA では運用基盤の外部システムとして表現)。アーキ図(diagram_mermaid)と CTP-006/CTR-004 で連携を表現する
- E-012 メトリクス(インメモリ・cache)・その他エンティティの storage_mapping は不変

## confidence 内訳(本イベントで追加/変更した要素)

| セクション | high | medium | low | default | user | 合計 |
|-----------|:----:|:------:|:---:|:-------:|:----:|:----:|
| システムアーキテクチャ(CTP-006 改訂, CTP-010, CTR-004) | 0 | 0 | 0 | 0 | 3 | 3 |
| データアーキテクチャ(E-001, E-011 + storage_mapping) | 0 | 0 | 0 | 0 | 3 | 3 |
| 合計 | 0 | 0 | 0 | 0 | 6 | 6 |

## 要確認項目(confidence: low / 確認推奨)

- なし(全要素 confidence: user。REQ-016/REQ-018/SPEC-016-01/SPEC-018-01 のユーザー指定値)。
  ただし NFR 側 A.4.1.2 RTO(=Lv4, confidence medium)の最終値はユーザー確認推奨(NFR イベントの注記に準拠)。アーキ側は CTP-006 で「10分以内・自動フェイルオーバー」として反映済み。
