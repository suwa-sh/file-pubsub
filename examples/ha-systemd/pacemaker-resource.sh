#!/usr/bin/env bash
# 方式A (外部クラスタ委譲) の Pacemaker リソース定義サンプル (pcs)。
# VIP と file-pubsub serve を同一リソースグループで束ね、常に同一ノードで 1 つだけ
# 起動させる (fencing が唯一性を保証)。これにより「serve が居ないノードに VIP が付く」
# 窓を避ける (push 受信 inbox で重要)。
#
# 前提:
#   - Pacemaker / Corosync 構成済み、STONITH(fencing) 有効。NFSv4 共有 + NTP 同期。
#   - /etc/file-pubsub/config.yaml は config-external-cluster.yaml をコピー (全ノード同一)。
#   - file-pubsub.service は配置するが systemctl enable しない (クラスタが起動・停止を制御)。
#       systemctl disable file-pubsub.service
#
# 注意: 値 (VIP / NIC / 共有パス) は環境に合わせて変更すること。実行は一度だけ (どれか 1 ノード)。

set -euo pipefail

VIP="192.0.2.10"          # 収集/受信を束ねる仮想 IP
NETMASK="24"
NIC="eth0"

# VIP リソース。
pcs resource create file-pubsub-vip ocf:heartbeat:IPaddr2 \
  ip="${VIP}" cidr_netmask="${NETMASK}" nic="${NIC}" \
  op monitor interval=10s

# file-pubsub serve リソース (systemd unit をクラスタ管理する)。
pcs resource create file-pubsub-serve systemd:file-pubsub \
  op monitor interval=15s timeout=30s \
  op start timeout=60s \
  op stop timeout=300s

# VIP → serve の順で同一ノードに起動するリソースグループ。
# group 内は記載順に start / 逆順に stop され、同一ノードへ colocate される。
pcs resource group add file-pubsub-ha file-pubsub-vip file-pubsub-serve

# フェイルオーバーを優先する (リソース障害でノード移動)。
pcs resource meta file-pubsub-ha migration-threshold=1

echo "done. 'pcs status' で file-pubsub-ha グループの配置 (active ノード) を確認できます。"
