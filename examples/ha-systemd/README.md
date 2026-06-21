# systemd 常駐 HA (active/standby 自動フェイルオーバー) サンプル

複数ホストで file-pubsub を常駐させ、active/standby 自動フェイルオーバーを組むための systemd サンプルです。唯一性保証の 2 方式 (同一バイナリ) を提供します。

| ファイル | 用途 |
|---|---|
| [`file-pubsub.service`](file-pubsub.service) | systemd unit (方式B / 方式A 共通。方式A ではクラスタ管理のため enable しない) |
| [`config-lease.yaml`](config-lease.yaml) | 方式B (lease 自動奪取、file-pubsub 単体) の設定例 |
| [`config-external-cluster.yaml`](config-external-cluster.yaml) | 方式A (外部クラスタ委譲、Pacemaker/keepalived) の設定例 |
| [`pacemaker-resource.sh`](pacemaker-resource.sh) | 方式A の Pacemaker リソース定義 (VIP + serve を同一グループ) |

## 共通の前提

- **全ホストで `data_dir` を同一の NFSv4 共有にマウント**する (例 `/mnt/file-pubsub/data`)。`file-pubsub.service` の `RequiresMountsFor` と `config.yaml` の `data_dir` を一致させる。
- **NTP による時刻同期**を有効にする (lease の有効期限判定が時刻依存)。
- **`lease_ttl` は NFS の属性キャッシュ (actimeo、既定最大 60s) より十分大きく**取る (推奨 90 以上。`lease_ttl <= 60` は警告ログを出す)。
- ファイル操作を行う `serve` は常に 1 つ (single-writer)。フェイルオーバー時の split-brain 被害は「高々 1 メッセージの重複配信 (破損・喪失なし)」に限定される。

---

## 方式B: lease 自動奪取 (file-pubsub 単体)

外部クラスタ不要。全ホストで同じ unit を enable すると、lease により唯一の active が選出され、active 障害時に別ホストが TTL 失効を検知して自動昇格します。**pull 型 (sftp/ftp/scp/local) は VIP 不要**(どのホストが active でも同じ収集元から引く)。

### セットアップ (各ホストで実施)

```bash
# 1. バイナリ配置
sudo install -m 0755 file-pubsub /usr/local/bin/file-pubsub

# 2. 設定配置 (全ホスト同一。data_dir は NFS マウント先)
sudo install -d /etc/file-pubsub
sudo install -m 0644 config-lease.yaml /etc/file-pubsub/config.yaml
# 認証情報は環境変数で渡す場合: /etc/file-pubsub/env に SFTP_PASSWORD=... 等

# 3. NFS 共有を data_dir にマウント (例。実際は /etc/fstab に記載)
#    sudo mount -t nfs4 nfs.example.internal:/export/file-pubsub /mnt/file-pubsub/data

# 4. 専用ユーザと unit 配置
sudo useradd --system --no-create-home file-pubsub || true
sudo install -m 0644 file-pubsub.service /etc/systemd/system/file-pubsub.service
sudo systemctl daemon-reload

# 5. 全ホストで enable + start (lease が active/standby を自動選出)
sudo systemctl enable --now file-pubsub.service
```

### 確認

```bash
# active のホストだけが startup / lease_active を出す。standby は lease_standby。
journalctl -u file-pubsub -n 20 --no-pager | grep -E "lease_active|lease_standby|startup"

# /healthz・/metrics は active のホストだけが応答する。
curl -fsS http://localhost:9090/healthz   # active: ok / standby: 接続拒否
```

### フェイルオーバー

active ホストを停止すると、`lease_ttl` 経過後に別ホストが昇格します。

```bash
# active ホストで
sudo systemctl stop file-pubsub      # graceful stop は lease を解放し、別ホストは TTL を待たず昇格
# 別ホストで
journalctl -u file-pubsub -f | grep -E "lease_active|promoted"
```

---

## 方式A: 外部クラスタ委譲 (Pacemaker / keepalived)

既に Pacemaker / keepalived で VIP を管理している環境向け。唯一性は**外部クラスタの fencing** が保証し、file-pubsub は常に active として強制起動します (standby polling しない)。**VIP と `serve` を同一リソースグループ**で束ね、同時に 1 ノードだけ起動させます。

### セットアップ

```bash
# 1. バイナリ・設定配置 (全ノード)。設定は external_cluster 版を使う。
sudo install -m 0755 file-pubsub /usr/local/bin/file-pubsub
sudo install -d /etc/file-pubsub
sudo install -m 0644 config-external-cluster.yaml /etc/file-pubsub/config.yaml

# 2. unit は配置するが enable しない (クラスタが起動・停止を制御する)
sudo install -m 0644 file-pubsub.service /etc/systemd/system/file-pubsub.service
sudo systemctl daemon-reload
sudo systemctl disable file-pubsub.service   # 重要: 自動起動させない

# 3. Pacemaker リソース定義 (どれか 1 ノードで一度だけ。値は環境に合わせる)
sudo bash pacemaker-resource.sh
```

### 確認

```bash
pcs status            # file-pubsub-ha グループ (vip + serve) がどのノードに居るか
sudo pcs resource move file-pubsub-ha <other-node>   # 手動フェイルオーバー検証
```

push 受信 (inbox) では Producer は VIP 宛に put します。VIP と serve が同一ノードに束ねられているため、「serve が居ないノードに VIP が付く」窓を避けられます。fsnotify は NFS で効かないため、`fallback_poll_interval` によるフォールバックポーリングで取り込みます。

---

## ローカルでまず試したい場合

NFS / クラスタを用意せず方式B のフェイルオーバー挙動だけ確認したいときは、共有ボリュームで 2 ノードを起動する [`examples/ha-docker-compose`](../ha-docker-compose) が手軽です。
