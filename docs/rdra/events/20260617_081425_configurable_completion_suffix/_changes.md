# 変更サマリ

- event_id: 20260617_081425_configurable_completion_suffix
- 元USDM: 20260617_081425_configurable_completion_suffix
- 生成日時: 2026-06-17T08:14:25
- 出典: 変更要望: push 受信モードの完了検知設定 completion を {mode, suffix} のネスト構造にし、rename/marker のサフィックスを設定可能にする(外部IF無変更での並行稼働・切替のため)

## 追加

- なし

## 変更

- 条件: 書き込み完了判定 → 完了検知方式の設定キーを completion(mode + suffix)に明確化。rename/doneマーカーのサフィックスを固定値(.tmp / .done)から Producer 規約に合わせ設定可能(既定 .tmp / .done)へ拡張
- バリエーション: 完了検知方式 → completion 設定が mode(stability/rename/marker)と suffix の指定を持つことを明記し、rename の一時拡張子・done マーカー拡張子が suffix で設定可能(.part/.ok 等)である旨を追記
- 情報: 設定 → 収集ソース定義の完了検知方式の指定形を completion: mode + suffix と明記
- 情報: 収集ソース → 完了検知方式の属性を completion(mode + suffix)に更新し、rename/marker のサフィックスが設定可能(既定 .tmp / .done)である旨を追記

## 削除

- なし
