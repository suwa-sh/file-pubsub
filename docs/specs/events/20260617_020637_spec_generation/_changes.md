# 変更内容 (event 20260617_020637_spec_generation)

GitHub Issue #11「Producer push(put)型の受信モード追加」(REQ-012/013/014)の差分仕様化。
push 受信モード(source.type=inbox)を UC「ファイルを収集する(Collect)」へ追加する。pull 型の既存仕様は追加・拡張のみで破壊しない。

## 変更したファイル

| ファイル | 変更内容 |
|---------|---------|
| `ファイル配信業務/.../ファイルを収集する(Collect)/spec.md` | 概要・データフロー(mermaid)・処理フロー(sequence、pull/push の alt 分岐)・バリエーション一覧(取り込みトリガー方式・完了検知方式を追加、収集ソース種別に inbox)・分岐条件一覧・計算ルール(rename 判定・done マーカー判定を追加)・状態遷移・関連 RDRA モデル(外部システム「受信ディレクトリ」追加)・BDD(push 受信モード/即時/フォールバック/rename/marker/二重検知の各 Scenario 追加)を更新 |
| `ファイル配信業務/.../ファイルを収集する(Collect)/tier-daemon-worker.md` | 受信ディレクトリ取り込みハンドラ(fsnotify + フォールバックポーリング)を追加。runtime/usecase/domain/gateway の push 設計、エラーハンドリング(受信ディレクトリ未存在・イベント取りこぼし・マーカー削除)、共通コンポーネント参照(inbox コネクタ・完了検知方式)を追記。pull 記述は残置 |
| `ファイル配信業務/.../ファイルを収集する(Collect)/_model-summary.yaml` | usecase に InboxIngestCommand、domain に CompletionDetectionRule、gateway に InboxConnector を追加。object_storage に受信ディレクトリ `{source.directory}/{original_file_name}` と done マーカー `{source.directory}/{original_file_name}.done` を追加。processed/ の用途に push 対応を追記 |
| `_cross-cutting/datastore/object-storage-schema.yaml` | bucket「receiving-directories」(inbox 受信ディレクトリ + done マーカー)を追加。processed/ の purpose とライフサイクル、processed_json スキーマの source_file_identifier(done マーカー名)に push 対応を追記。config.yaml の purpose に inbox 設定キーを追記 |
| `_cross-cutting/ux-ui/ui-design.md` | 設定 YAML スキーマに source.type=inbox の例(directory 流用/completion/fallback_poll_interval。trigger キーは設けず常時ハイブリッド固定)を追加。記述ルールに push 受信モードの設定キー・検証を追記。push 受信モードのメトリクス契約への影響(既存 5 系列を流用、新規メトリクスを発明しない)・構造化ログ運用ルール(既存 event_type を流用)を追記 |
| `_cross-cutting/traceability-matrix.md` | 収集ソース属性 +5(収集モード/受信ディレクトリパス/取り込みトリガー方式/フォールバックポーリング間隔/完了検知方式)、バリエーション値 +6(inbox/トリガー2/完了検知3)、外部システム +1(受信ディレクトリ)を追加。網羅率を再計算 |
| `decisions/spec-decision-004.yaml` | source.type 独立値 inbox の選定(local 拡張しない) |
| `decisions/spec-decision-005.yaml` | 取り込みトリガーのハイブリッド方式(fsnotify + フォールバックポーリング、冪等性) |
| `decisions/spec-decision-006.yaml` | 完了検知方式の項目設計(stability/rename/marker、既定 stability、マーカー後始末) |

## 新規採番した ID

| ID | 種別/レイヤー | 指すもの |
|----|-------------|---------|
| LR-003 | runtime ルール | push 受信モードの取り込みトリガー = fsnotify イベント駆動 + フォールバックポーリングのハイブリッド(fallback_poll_interval) |
| LR-204 | domain ルール | 完了検知方式(安定判定 / rename / done マーカー)を収集モード非依存の共通完了判定ルールとして扱う |
| LR-205 | domain ルール | push 受信モードの二重検知(fsnotify + フォールバック)を処理済み管理 + message_id 採番で冪等取り込み |
| LR-304 | gateway ルール | inbox 収集コネクタの受信ディレクトリ列挙・取り込み(一時名書込 → rename)・回収時削除 |
| LR-305 | gateway ルール | done マーカー方式のマーカー(xxx.done)後始末(回収=削除 / 残す=処理済み管理で識別) |
| LP-302 | gateway パターン | inbox コネクタを収集コネクタ共通インターフェース(LP-301)の 1 実装として追加 |
| SP-012 | 仕様パターン | push 受信モード(inbox)= 受信ディレクトリへの put 取り込み、後段はソース種別非依存(REQ-012) |
| SP-013 | 仕様パターン | 取り込みトリガーのハイブリッド + 二重検知の冪等(REQ-013) |
| SP-014 | 仕様パターン | 完了検知方式の選択(安定判定 / rename / done マーカー)とマーカー後始末(REQ-014) |

## 網羅率(再計算後)

| カテゴリ | 全要素数 | カバー済み | 網羅率 |
|---------|:-------:|:--------:|:-----:|
| 情報の属性 | 63 | 63 | 100% |
| 条件 | 13 | 13 | 100% |
| バリエーションの値 | 24 | 24 | 100% |
| 状態遷移パス | 23 | 23 | 100% |
| 外部システム連携 | 6 | 6 | 100% |
| **合計** | **129** | **129** | **100%** |
