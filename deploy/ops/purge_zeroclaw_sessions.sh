#!/usr/bin/env bash
# 一次性运维: 清空 zeroclaw 落盘会话(中毒上下文处置)。2026-09-29 用一次,不并入 deploy_zeroclaw.sh。
# 为什么必须清:会话存 sessions.db 在卷上持久,浏览器 localStorage 的 session_id 跨重启续用同一会话,
# 旧 X402 回复里的 payapp 授权链接留在模型上下文里 → 模型不调工具、照抄链接形状自造 sid,
# 用户点开只得到微信一句"繁忙"(实测 01:40/01:41/01:48 三轮 tool_calls=0)。
# 改名保留 *.poisoned-<ts>.db 可回滚;下次对话自动新建会话。聊天历史是充值 widget 的一次性上下文,可弃。
#   bash deploy/ops/purge_zeroclaw_sessions.sh > deploy/logs/purge_sessions.out 2>&1; tail -20 deploy/logs/purge_sessions.out
set -euo pipefail
cd /opt/savvy/deploy

SDIR=/opt/savvy/deploy/data/zeroclaw/.zeroclaw/data/sessions
STAMP=$(date +%Y%m%d%H%M%S)

echo "== 停 zeroclaw =="
docker compose stop zeroclaw >/dev/null

echo "== 迁移会话库 =="
for db in sessions acp-sessions; do
  if [ -f "$SDIR/$db.db" ]; then
    mv "$SDIR/$db.db" "$SDIR/$db.poisoned-$STAMP.db"
    echo "moved $db.db -> $db.poisoned-$STAMP.db"
  fi
  rm -f "$SDIR/$db.db-wal" "$SDIR/$db.db-shm"
done
chown -R 65534:65534 /opt/savvy/deploy/data/zeroclaw

echo "== 起 zeroclaw =="
docker compose up -d zeroclaw >/dev/null
for i in $(seq 1 12); do
  [ "$(docker inspect -f '{{.State.Status}}' zeroclaw)" = running ] && break
  sleep 5
done
echo "state: $(docker inspect -f '{{.State.Status}}' zeroclaw)"
echo "gateway http_code: $(curl -s -o /dev/null -w '%{http_code}' --max-time 5 http://127.0.0.1:42617/ || echo refused)"
echo "== 目录留档 =="
ls -la "$SDIR" | tail -8
echo "== DONE =="
