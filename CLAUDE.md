# CLAUDE.md

## このリポジトリの正体

**file-pubsub** — FTP GET/DELETE 型のレガシーファイル IF を Pub/Sub 風の配信モデル(Topic / Subscription / Archive / Fan-out / Manifest)へ変換する軽量ブリッジ。Go 1.26 のシングルバイナリ(常駐デーモン + 運用 CLI)。MIT ライセンスの OSS。

## 開発プロセス(必ずこの順で)

このリポジトリは **distillery(要件・仕様パイプライン)を正本とした仕様駆動開発**で運用する。コードを直接変更する前に、必ず仕様側から入る。

### 1. 要件・仕様の整理は distillery で行う

- 機能の追加・変更は、まず **distillery で要件を整理**する: `dist-requirements`(変更要望テキスト → USDM 分解 → RDRA 差分更新)
- 続けて **`dist-spec` で仕様化**する(UC 単位 spec + cross-cutting)
- 成果物の置き場(イベントソーシング。`events/` は不変、`latest/` が最新スナップショット):

| パス | 内容 |
|---|---|
| `docs/README.md` | 全成果物のナビゲーション(`generateReadme.js` で自動生成。手動編集しない) |
| `docs/usdm/latest/` | USDM 要求仕様(requirements.yaml / .md) |
| `docs/rdra/latest/` | RDRA モデル(アクター/情報/状態/条件/バリエーション/BUC の TSV) |
| `docs/nfr/latest/` | IPA 非機能要求グレード |
| `docs/arch/latest/` | アーキテクチャ設計(2 ティア × 4 層、依存ルール、ADR) |
| `docs/specs/latest/` | **実装の正本**。UC 別 spec.md / tier-*.md、cross-cutting(file レイアウト・メトリクス契約・CLI 規約) |

- 仕様に無いものを実装で発明しない。実装中に仕様の不足・矛盾を見つけたら、コードでごまかさず distillery の差分更新に戻す

### 2. 実装は distillery specs に従う

- `docs/specs/latest/` の該当 UC の spec.md / tier-*.md を読んでから着手する
- レイヤー構成と依存ルールは `docs/arch/latest/arch-design.yaml` に従う:
  `runtime → usecase → domain / gateway`(domain は I/O を持たない。gateway→domain のみ許可)
- ファイルレイアウト・Manifest スキーマは `docs/specs/latest/_cross-cutting/datastore/object-storage-schema.yaml` が正本
- CLI 出力・終了コード(0/1/2/3)・構造化ログのフィールドは `_cross-cutting/ux-ui/ui-design.md` が正本

### 3. ATDD: specs の受け入れ条件を先にテスト化する

- 各 UC spec.md の **BDD シナリオ(Given/When/Then)を受け入れテストとして先に書く**(`internal/e2e/` または該当パッケージのテスト)
- 受け入れテストが RED であることを確認してから実装に入る
- シナリオの具体値(topic=orders、message_id 形式等)は spec の記述をそのまま使う

### 4. TDD: ユニットレベルも specs を起点に RED → GREEN → REFACTOR

- domain のルール(状態遷移・採番・安定判定・冪等判定)は spec のトレーサビリティ表にある条件・状態遷移を 1 ケースずつテスト化してから実装する
- store/gateway は `t.TempDir()` でファイル実体を使ってテストする(モックで誤魔化さない)
- バグ修正は必ず再現テストを先に書く

### 5. qlty check 指摘ゼロをキープ

- 変更のたびにローカルで実行する:

```bash
qlty check --all --no-progress --no-formatters --fail-level medium   # CI と同じゲート。exit 0 を維持
qlty fmt --all                                                        # formatter
```

- **medium 以上(errcheck・security 系・hadolint 等)の指摘を残したまま commit しない**
- radarlint のコードスメル(low に triage 済み)は助言。新規コードでは複雑度 15 超・リテラル重複を作らないよう努める
- ツール構成は `.qlty/qlty.toml`(golangci-lint はバージョン固定。Go の major 更新時に built-with バージョンの整合を確認)

### 6. 作業単位ごとのレビュー(サブエージェント → Codex の二段)

**(a) サブエージェントレビュー**: 実装が一区切りしたら、生成した本人とは別のサブエージェントに、仕様(`docs/specs/latest/`)と突き合わせたレビューをさせる。観点: 仕様トレーサビリティ / クラッシュ耐性・冪等性 / テストの実効性。

**(b) Codex レビュー**: 作業単位(コミットのまとまり)ごとに `codex:rescue` で外部レビューを実施する(「レポートだけ、修正不要」と明示)。

**(c) 反証**: 指摘ごとに実体(コード・テスト実行・仕様)と照合して反証を試みる。誤検出・意図した設計判断・スコープ外は根拠つきで不採用にする。

**(d) 取り込み**: **反証しきれない指摘は必ず修正する**(回帰テスト追加 → 再テスト → qlty ゲート確認)。反証内訳(指摘数 / 不採用数と根拠 / 対応数)をコミットメッセージまたは PR に残す。

## 実装規約

- **コメントは日本語**で書く(仕様の制約・設計判断を示す最小限のもの。コード・識別子・エラーメッセージ・ログは英語)
- godoc コメントも日本語(`// FuncName は〜する。` の形式で対象名から始める)

## テスト規約

- **AAA パターン**: 各テスト本文を `// Arrange` `// Act` `// Assert` のコメントで 3 区画に分ける(準備・実行・検証を混ぜない)
- **テスト関数名**: `Test{テスト対象}_{XXXの場合}_{YYYであること}` 形式
  - テスト対象 = 関数・メソッド名(英語のまま)、条件と期待は日本語
  - 例: `TestNewMessageID_同名ファイルを再収集した場合_別のIDが採番されること`
  - `t.Run` のサブテスト名も「{XXXの場合}_{YYYであること}」に合わせる
- テーブル駆動テストは各ケース名を「XXXの場合_YYYであること」とし、本文の AAA 区画は維持する

## 検証コマンド

```bash
go test ./... -count=1            # ユニット + E2E (local source)
go test ./... -count=1 -race     # CI と同条件
go vet ./... && gofmt -l .
qlty check --all --fail-level medium

# 実機 E2E (sftp/ftp/local の 3 経路)
cd examples/docker-compose && docker compose up -d --build
echo "id,qty" > sources/local/invoices_test.csv
ls data/subscriptions/invoices/current   # 数十秒で複製される
docker compose down -v
```

## 横断的な注意点

- **データ整合の原則**: 元ファイルの削除は Archive 保存 + Manifest 記録の後のみ。配信は at-least-once(クラッシュ後再開で再配置があり得る)。Archive の retention 削除は決着済み(delivered/dlq)のみ
- **single-writer**: manifest を書くのは lock 保持者だけ(serve または replay)。status は読み取り専用
- **CI**(`.github/workflows/ci.yml`): test(race+coverage)/ qlty / goreleaser check / docker build / compose E2E。GitHub Actions は SHA ピン + 最小 permissions を維持する
- **リリース**: タグ `v*` push で goreleaser(Releases バイナリ)+ ghcr.io イメージ公開(`.github/workflows/release.yml`)
- 自動生成物(`docs/README.md`)は手動編集しない。再生成は `node <distillery>/skills/dist-pipeline/scripts/generateReadme.js`
