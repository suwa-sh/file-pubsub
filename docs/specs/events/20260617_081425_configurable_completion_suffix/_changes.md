# 変更内容 (event 20260617_081425_configurable_completion_suffix)

push 受信モードの完了検知設定 `completion` を、単一文字列から `{ mode, suffix }` のネスト構造へ変更し、
rename/marker のサフィックスを Producer 規約に合わせ設定可能にする差分仕様化 (REQ-014 / SPEC-014-03)。
UC「ファイルを収集する(Collect)」のみが影響。pull 型・後段(Archive / Fan-out / Manifest)は不変。
これは push 受信モード追加 (20260617_020637_spec_generation) に対する差分。

## 変更したファイル

| ファイル | 変更内容 |
|---------|---------|
| `ファイル配信業務/.../ファイルを収集する(Collect)/spec.md` | バリエーション/分岐条件/計算ルール(rename 判定・done マーカー判定)に `completion.suffix` を追記。BDD の Given を `completion=X` → `completion.mode=X` に修正。suffix 可変を検証する Scenario を追加(rename=`.part` / marker=`.ok`) |
| `ファイル配信業務/.../ファイルを収集する(Collect)/tier-daemon-worker.md` | 完了検知の処理フロー・ビジネスルールを `completion.mode` + `completion.suffix`(既定 .tmp/.done、設定可能)に更新。BDD Given を `completion.mode=X` に修正 |
| `ファイル配信業務/.../ファイルを収集する(Collect)/_model-summary.yaml` | object_storage の done マーカーパターンを `{source.directory}/{original_file_name}.done` 固定 → `{...}{source.completion.suffix}`(既定 .done、設定可能)に変更。config.yaml の purpose を `source.completion(mode + suffix)` に更新 |
| `_cross-cutting/ux-ui/ui-design.md` | 設定 YAML スキーマの `completion` を `{ mode, suffix }` のネスト構造に変更(mode: stability/rename/marker、suffix: rename/marker のサフィックス・既定 .tmp/.done・設定可能)。記述ルール・push 受信モード設定キー表を更新 |
| `_cross-cutting/datastore/object-storage-schema.yaml` | bucket「receiving-directories」のマーカーパターンを `{original_file_name}{source.completion.suffix}`(既定 .done、設定可能)に変更。purpose を `source.completion.mode` 表記に更新 |
| `decisions/spec-decision-006.yaml` | negative に「サフィックスは completion.suffix で設定可能(spec-decision-007 参照)、既定値(.tmp/.done)を README で明示」を追記 |
| `decisions/spec-decision-007.yaml` | (新規) completion を {mode, suffix} のネスト構造にし rename/marker のサフィックスを設定可能にする判断記録。代替案(.tmp/.done 固定 / 平坦 2 キー)と不採用理由を含む |

## 新規採番した ID

| ID | 種別 | 指すもの |
|----|------|---------|
| SPEC-014-03 | USDM 仕様 | completion を { mode, suffix } のネスト構造にし、rename/marker のサフィックスを設定可能にする(既定 .tmp/.done) |

## トレーサビリティへの影響

RDRA 要素の追加・削除はなし(条件「書き込み完了判定」・バリエーション「完了検知方式」・情報「設定」「収集ソース」の
説明文を更新したのみ)。要素数(条件 13 / バリエーション値 24 / 外部システム 6 等)は不変のため、
`_cross-cutting/traceability-matrix.md` の網羅率(100%)は維持され、本差分では再生成不要。
