# 変更サマリ

- event_id: 20260620_171535_add_redundant_failover
- 元USDM: 20260620_171535_add_redundant_failover
- 生成日時: 2026-06-20T17:36:17
- 出典: VIP + NFS 共有 data_dir による active/standby 自動フェイルオーバー対応(REQ-015〜REQ-018)

## 追加

- バリエーション: 唯一性保証方式（基盤運用管理。方式A=外部クラスタ委譲(Pacemaker/keepalived の fencing) / 方式B=lease自動奪取(TTL失効でstandbyが自動昇格)の切替軸。設定で切替・併用でき同一バイナリで両環境をカバー）(SPEC-015-02)
- 外部システム: 外部クラスタ（運用基盤。方式Aで唯一性を保証する Pacemaker/keepalived 等の抽象。fencing で serve リソースを起動/停止し VIP と serve を同一リソースグループで束ねる。file-pubsub は TTL 失効による自動奪取を行わず外部クラスタに委ねる）(SPEC-015-02, SPEC-017-02)
- 業務ポリシー(冗長構成方針): 単一インスタンス前提(HAなし)を改め、active/standby 冗長化(lease方式 / 外部クラスタ委譲)を前提とする方針。BUC.tsv にポリシー専用列がないため latest TSV へは行追加せず本サマリに記録し、アーキ(CTP-006 単一インスタンス前提)・NFR(可用性 A.2.1.1)の見直しは後段 dist-architecture / dist-quality-attributes の責務とする(REQ-018 priority=should、判断点に明記)(SPEC-018-01)

## 変更

- 情報: 設定 → 唯一性保証方式(方式A/方式B)・lease TTL・heartbeat間隔の属性を追加。バリエーション「唯一性保証方式」を関連付け。関連情報に Lock を追加。lease TTL は NFS 属性キャッシュ最大(actimeo既定60s)より十分大きく設定でき既定も大きく取る旨を追記(SPEC-015-02, SPEC-017-01, SPEC-017-02)
- 情報: Lock → PID生存確認ベースのロックを lease レコード化。属性に hostname / boot-id / acquired_at / renewed_at / ttl を追加。stale 判定を renewed_at + ttl 超過へ置換(マルチホスト対応)。バリエーション「唯一性保証方式」を関連付け、関連情報に 設定 を追加。single-writer を active/standby でも維持する旨を追記(SPEC-015-01)
- 状態: デーモン稼働状態 → standby待機 状態と昇格/降格遷移を追加(起動中→standby待機、standby待機→稼働中(active昇格)、稼働中→standby待機(active降格))(SPEC-015-03)
- 状態: Lock状態 → 「stale→デーモンを起動する→取得済」遷移を lease 奪取(renewed_at + ttl 超過判定、PID生存確認非依存。方式B自動奪取/方式A外部クラスタfencing後の取得)に拡張(SPEC-015-01, SPEC-015-03)
- 条件: 二重起動防止 → lease レコード化・standby待機・renewed_at + ttl による stale 判定・方式A/方式Bでの唯一性保証(single-writer)を追記。バリエーション「唯一性保証方式」を関連付け(SPEC-015-01, SPEC-015-03)
- 条件: AtomicWrite配置 → split-brain の窓で2つの serve が同一 data_dir を操作しても正式名は常に完全内容で途中状態を露出しない旨を追記(SPEC-016-01)
- 条件: 二重配信防止 → split-brain 時も Manifest 照合の fail-closed 冪等 + at-least-once 冪等再開で被害を高々1メッセージの重複配信に限定しデータ喪失しない旨を追記(SPEC-016-01)
- 条件: message_id採番 → 冪等照合(Manifest/処理済み)I/O 失敗時は fail-closed で安全側に倒し split-brain 時も既存メッセージ上書きを避ける旨を追記(SPEC-016-01)
- 外部システム: リモートファイル領域 → pull型(sftp/ftp/local)は active/standby どちらが引いても同一ソースで VIP 無関係、lease 保持者(active)だけが pull/archive し二重収集しない旨を追記(SPEC-017-01)
- 外部システム: 受信ディレクトリ → NFS では fsnotify が効かずフォールバックポーリング前提で、受信ディレクトリ取り込みは active な serve(方式Aでは VIP と同居)が行う旨を追記(SPEC-017-02)
- BUC: 配信基盤を運用するフロー / デーモンを起動する → active/standby 起動(active昇格 or standby待機)・唯一性保証方式の切替・外部クラスタ連携を説明に追記。関連にバリエーション「唯一性保証方式」とイベント「クラスタリソース制御(外部クラスタ)」を追加(SPEC-015-03)
- BUC: 配信基盤を運用するフロー / 冪等に処理を再開する → standby→active 昇格・split-brain 時も AtomicWrite + at-least-once 冪等再開 + fail-closed 冪等で被害を高々1メッセージに限定する説明に拡張。関連に条件「AtomicWrite配置」を追加(SPEC-016-01)

## 削除

- なし
