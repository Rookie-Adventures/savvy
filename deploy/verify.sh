#!/usr/bin/env bash
# 部署后健康自检 (跑在机B, 验整栈起来没)
# 用法: sudo ./verify.sh
set -u

PASS=0; FAIL=0
ok()   { echo "  ✓ $1"; PASS=$((PASS+1)); }
bad()  { echo "  ✗ $1"; FAIL=$((FAIL+1)); }

echo "=== Savvy 部署自检 ($(hostname)) ==="

# 容器状态
echo "[1/5] 容器运行:"
for c in new-api redis savvy-manager workspace-router; do
  if docker ps --format '{{.Names}}' | grep -q "^${c}$"; then ok "$c 运行中"
  else bad "$c 未运行"; fi
done

# new-api 健康
echo "[2/5] new-api 健康:"
if curl -fsS http://localhost:3000/api/status 2>/dev/null | grep -q '"success":true'; then
  ok "/api/status 返回 success:true"
else bad "/api/status 不通 (curl http://localhost:3000/api/status)"; fi

# SQLite 文件在
echo "[3/5] SQLite:"
DB=./data/new-api/one-api.db
if [ -f "$DB" ]; then ok "one-api.db 存在 ($(du -h "$DB" | cut -f1))"
else bad "one-api.db 不在 $DB (首次启动才生成, 重启容器一次)"; fi

# savvy-manager 响应
echo "[4/5] savvy-manager:"
if curl -fsS http://localhost:8000/ 2>/dev/null >/dev/null; then
  ok ":8000 可达"
else bad ":8000 不通"; fi

# workspace 路由器端口池
echo "[5/5] workspace-router:"
if curl -fsS http://localhost:41000/ 2>/dev/null >/dev/null || \
   ss -lnt | grep -q 41000; then
  ok "41000 端口监听"
else bad "41000 未监听"; fi

echo "---"
echo "通过 $PASS / 失败 $FAIL"
[ $FAIL -eq 0 ] && echo "✅ 部署看起来 OK, 下一步: 控制台初始化 + 配渠道" \
                || echo "❌ 有失败项, 查 docker compose logs <服务名>"
exit $FAIL
