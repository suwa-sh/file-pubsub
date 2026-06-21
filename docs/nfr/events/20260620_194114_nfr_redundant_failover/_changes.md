# 変更サマリ

- event_id: 20260620_194114_nfr_redundant_failover
- trigger_event: rdra:20260620_171535_add_redundant_failover
- created_at: 2026-06-20T19:41:14
- mode: 差分更新（可用性カテゴリの再評価。規模前提は不変のため既存システム前提を踏襲）
- source: 冗長構成(active/standby 自動フェイルオーバー)対応 (REQ-018/SPEC-018-01)

## 追加

- なし

## 変更

| ID | メトリクス | 旧Lv | 新Lv | confidence(旧→新) | 変更理由 |
|----|----------|------|------|------|---------|
| A.1.2.1 | サービス切替時間 | 1 | 4 | default → medium | 単一インスタンス(切替先なし)から active/standby 自動フェイルオーバーへ。lease 失効検知〜standby 昇格で分オーダー切替(ホットスタンバイ相当) |
| A.2.1.1 | サーバ内の冗長化 | 1 | 4 | user → user | REQ-018/SPEC-018-01 の主対象。冗長化なし(再起動で復旧)→ VIP + NFS 共有 data_dir の active/standby 完全冗長化(自動切替) |
| A.4.1.2 | RTO（目標復旧時間） | 2 | 4 | user → medium | 自動フェイルオーバーで RTO 主要因が lease 失効検知〜standby 昇格に。半日以内 → 10分以内 |
| A.4.1.3 | RLO（目標復旧レベル） | 1 | 3 | default → medium | standby は同一設定で昇格するため縮退運転ではなく平常時と同等水準で復旧 |
| C.3.3.1 | 障害復旧方式 | 1 | 1 | user → user | grade 据置。復旧方式の説明を「再起動 + Replay」から「standby 自動昇格による配信継続(split-brain でも被害は高々1メッセージ重複)」へ更新 |

## 削除

- なし

## 据置(意図的に変更しない)

- A.3.1.1 災害対策の範囲 / A.3.1.2 業務継続の要否: 今回の冗長化は VIP + NFS の同一サイト内 HA であり、遠隔地 DR(災害対策)ではないため据置(Lv0)
- A.4.1.1 RPO: 元ファイル DELETE は Archive 保存後に限定し Manifest で配送状態を追跡する設計は不変。データ損失なし(Lv4)を維持
- A.1.1.1 運用時間 / A.1.1.3 計画停止 / C.1.1.1 運用監視時間: 運用スケジュール・監視方針は不変

## 注記(confidence: user の上書きについて)

- A.2.1.1 と C.3.3.1 は既存 confidence: user。本変更は REQ-018/REQ-016 によるユーザー判断(差分更新の指示そのもの)であり、user → user の更新として扱う(楽観的な自動上書きではない)。
- A.4.1.2 RTO は既存 user。今回は自動フェイルオーバーを前提とした推論再評価のため confidence を medium に変更し、最終値はユーザー確認を推奨する(下記 _inference.md の要確認項目)。
