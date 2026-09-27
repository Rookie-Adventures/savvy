#!/bin/sh
# 持久设备身份: machine-id 首次生成后落卷,之后每次启动原样恢复
set -e
mkdir -p /wxdata /var/lib/dbus
[ -s /wxdata/machine-id ] || cat /proc/sys/kernel/random/boot_id | tr -d '-' > /wxdata/machine-id
cp /wxdata/machine-id /etc/machine-id
cp /wxdata/machine-id /var/lib/dbus/machine-id
exec supergateway \
  --stdio "node /root/.hermes/plugins/weixinpay/dist/mcp-server.mjs" \
  --outputTransport streamableHttp \
  --port 8100
