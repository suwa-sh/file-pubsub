# docker compose HA (active/standby 自動フェイルオーバー) デモ

2 ノード (`node-a` / `node-b`) を共有 data_dir で起動し、**方式B (lease 自動奪取)** による active/standby 自動フェイルオーバーを確認できる環境です。active を止めると、もう一方が lease の TTL 失効を検知して自動昇格します。

> **注意**: 共有 Docker ボリュームは「共有 data_dir」を模した簡易環境で、NFS そのものではありません。実運用では **NFSv4 共有 + NTP 時刻同期** を前提とし、`lease_ttl` を NFS の属性キャッシュ (actimeo、既定最大 60s) より十分大きく取ってください (本サンプルは即時確認のため `lease_ttl: 15` に短縮)。systemd での常駐構成は [`examples/ha-systemd`](../ha-systemd) を参照。

## 構成

| サービス | 役割 |
|---|---|
| `node-a` | file-pubsub ノード 1。`/data` は共有ボリューム、`/healthz`・`/metrics` はホスト `19090` |
| `node-b` | file-pubsub ノード 2。同じ共有ボリュームを参照、ホスト `19091` |
| `shared-data` (volume) | 両ノードが共有する `data_dir` (= 実運用の NFS 共有) |

両ノードは**同一 `config.yaml`** を読み、`high_availability.uniqueness_method: lease` で唯一の active を選出します。**`/healthz`・`/metrics` は active のときだけ応答**します (standby は HTTP サーバを起動しない) — これが「どちらが active か」の判定に使えます。

## 動作確認手順

前提: Docker / Docker Compose が利用可能なこと。

### 1. 起動

```bash
cd examples/ha-docker-compose
docker compose up -d --build
```

### 2. どちらが active かを確認

active のノードだけが `/healthz` に応答します。

```bash
curl -fsS http://localhost:19090/healthz && echo " <- node-a is active" || echo "node-a is standby"
curl -fsS http://localhost:19091/healthz && echo " <- node-b is active" || echo "node-b is standby"
```

ログでも確認できます (`lease_active` / `lease_standby`)。

```bash
docker compose logs node-a node-b | grep -E "lease_active|lease_standby"
```

### 3. 配信を確認 (active が収集・配信する)

```bash
echo "id,amount" > sources/local/invoice_001.csv
sleep 8   # polling_interval(5s) + stability_check(2s)
docker compose exec node-a ls /data/subscriptions/invoices/current/   # 共有なのでどちらのノードからも見える
```

`invoice_001.csv` が複製されていれば収集・配信成功です。

### 4. フェイルオーバーを確認

active ノード (ここでは node-a と仮定) を停止します。

```bash
docker compose stop node-a
```

`lease_ttl`(15s)経過後、node-b が lease を奪取して昇格します。

```bash
sleep 20
curl -fsS http://localhost:19091/healthz && echo " <- node-b promoted to active"
docker compose logs node-b | grep -E "lease_active|promoted"
```

昇格後に新しいファイルを置くと、node-b が収集・配信を継続します。

```bash
echo "id,amount" > sources/local/invoice_002.csv
sleep 8
docker compose exec node-b ls /data/subscriptions/invoices/current/   # invoice_002.csv も複製される
```

> 復帰: `docker compose start node-a` で node-a を戻すと、node-b が active を保持したまま node-a は standby になります (二重 active にはなりません)。

### 5. 後片付け

```bash
docker compose down -v   # -v で共有ボリュームも削除
```

## 仕組み (要点)

- **single-writer 維持**: ファイル操作 (収集 / Archive / Fan-out / Manifest 更新) を行うのは lease 保持者 (active) 1 ノードだけ。standby はスケジューラも HTTP サーバも起動しない。
- **自動昇格**: active は `heartbeat_interval` ごとに lease の `renewed_at` を更新。active 障害で更新が止まると `renewed_at + lease_ttl` を過ぎ、standby が stale と判定して奪取・昇格する。
- **split-brain 被害限定**: フェイルオーバーの瞬間に両ノードが一時的に重なっても、メッセージ境界 lease 確認 + Manifest の message_id ロック + 世代 CAS により被害は「高々 1 メッセージの重複配信 (破損・喪失なし)」に限定される。
- **方式A (外部クラスタ委譲)** を使う場合は `uniqueness_method: external_cluster` とし、VIP と serve を Pacemaker / keepalived の同一リソースグループで束ねます ([`examples/ha-systemd`](../ha-systemd) 参照)。
