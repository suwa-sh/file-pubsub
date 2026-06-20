# NFR グレード差分 — 冗長構成(active/standby 自動フェイルオーバー)対応

- event_id: 20260620_194114_nfr_redundant_failover
- trigger_event: rdra:20260620_171535_add_redundant_failover
- created_at: 2026-06-20T19:41:14
- mode: 差分更新（変更メトリクスのみ。全量スナップショットは `docs/nfr/latest/nfr-grade.md`）
- source: REQ-018/SPEC-018-01（可用性 A.2.1.1 の引き上げ）+ REQ-016/SPEC-016-01（split-brain 被害限定）

## 変更メトリクス一覧

| ID | メトリクス | 旧Lv | 新Lv | confidence | 確定レベルの内容 |
|----|----------|------|------|-----------|----------------|
| A.1.2.1 | サービス切替時間 | 1 | 4 | medium | 10分未満（ホットスタンバイ）= active/standby 自動フェイルオーバー（lease 失効検知〜standby 昇格） |
| A.2.1.1 | サーバ内の冗長化 | 1 | 4 | user | 完全冗長化（自動切替）= VIP + NFS 共有 data_dir 上の active/standby 自動フェイルオーバー（single-writer 維持） |
| A.4.1.2 | RTO（目標復旧時間） | 2 | 4 | medium | 10分以内（lease 失効検知〜standby 自動昇格による配信継続） |
| A.4.1.3 | RLO（目標復旧レベル） | 1 | 3 | medium | 平常時と同等（standby は同一 data_dir・同一設定で昇格するため全機能を継続） |
| C.3.3.1 | 障害復旧方式 | 1 | 1 | user | active/standby 自動フェイルオーバー（standby 自動昇格）による配信継続。split-brain でも被害は高々1メッセージ重複に限定 |

## 据置（意図的に変更しない）

- A.3.1.1 災害対策の範囲 / A.3.1.2 業務継続の要否: 同一サイト内 HA であり遠隔地 DR ではないため据置（Lv0）
- A.4.1.1 RPO: 元ファイル DELETE は Archive 保存後に限定し Manifest で配送追跡する設計は不変。データ損失なし（Lv4）を維持

## 推論根拠・要確認項目

詳細は同ディレクトリの `_inference.md` を参照。confidence: medium の A.1.2.1 / A.4.1.2 / A.4.1.3 はユーザー確認を推奨。
