#!/usr/bin/env bash
# 机B 生产接入: ZeroClaw 智能体运行时 + savvy-mcp 工具层, 并把 new-api 对话后端从百炼切过来。
# 前提: deploy/.env 已有 AGENT_TOPUP_TOKEN 与 DEEPSEEK_API_KEY(手工一次性追加, 本脚本不写密钥)。
# 经 run_remote.ps1 执行: 输出重定向到文件再回读(云助手吞 stdout)。
#   bash deploy/ops/deploy_zeroclaw.sh > deploy/logs/deploy_zeroclaw.out 2>&1; tail -40 deploy/logs/deploy_zeroclaw.out
set -euo pipefail
cd /opt/savvy

echo "== 1. 代码同步 =="
git pull --ff-only

grep -q '^DEEPSEEK_API_KEY=' deploy/.env || { echo "FATAL: deploy/.env 缺 DEEPSEEK_API_KEY, 先手工追加(勿入库)"; exit 1; }

echo "== 2. 渲染 zeroclaw 配置与身份适配层 =="
DP_KEY=$(grep '^DEEPSEEK_API_KEY=' deploy/.env | cut -d= -f2-)
# 绝对路径: 脚本后半段会 cd 进 deploy/,相对 $ZDIR 会失效
ZDIR=/opt/savvy/deploy/data/zeroclaw
mkdir -p "$ZDIR/.zeroclaw/agents/topup/workspace"
sed "s|__DEEPSEEK_API_KEY__|$DP_KEY|g" deploy/zeroclaw/config.toml.tpl > "$ZDIR/.zeroclaw/config.toml"
chmod 600 "$ZDIR/.zeroclaw/config.toml"
cp deploy/zeroclaw/IDENTITY.md "$ZDIR/.zeroclaw/agents/topup/workspace/IDENTITY.md"
# distroless 容器以 uid 65534(nobody) 跑, bind mount 属主必须是它
chown -R 65534:65534 "$ZDIR"

echo "== 3. 起 savvy-mcp + zeroclaw =="
cd deploy
docker compose build savvy-mcp
docker compose up -d savvy-mcp zeroclaw
for i in $(seq 1 12); do
  [ "$(docker inspect -f '{{.State.Status}}' zeroclaw 2>/dev/null || echo none)" = running ] && break
  sleep 5
done
[ "$(docker inspect -f '{{.State.Status}}' zeroclaw)" = running ] || { echo "FATAL: zeroclaw 未常驻"; docker compose logs --tail 30 zeroclaw; exit 1; }

echo "== 4. 安装技能进 bundle(先清旧份保证与真源一致, 落 $ZDIR) =="
# docker exec 绕过 ENTRYPOINT, distroless 里必须给完整二进制路径; skills install 不覆盖已存在目标
rm -rf "$ZDIR/.zeroclaw/shared/skills/topup/savvy-quota-topup"
docker compose exec -T zeroclaw /usr/local/bin/zeroclaw skills install /zeroclaw-data/skills-src/savvy-quota-topup --bundle topup
docker compose exec -T zeroclaw /usr/local/bin/zeroclaw skills list || true
chown -R 65534:65534 "$ZDIR"
docker compose restart zeroclaw >/dev/null
# 只记录响应码做诊断:连接被拒=网关没起来,任何 HTTP 码=端口通了
echo "gateway / http_code: $(curl -s -o /dev/null -w '%{http_code}' --max-time 5 http://127.0.0.1:42617/ || echo refused)"

echo "== 5. 重建并重启 new-api(Go 侧已换 ZeroClaw WS 客户端) =="
docker compose build new-api
docker compose up -d new-api
sleep 15

echo "== 6. 对话后端参数落库(百炼键清除, ZeroClaw 键写入) =="
python3 - <<'PY'
import sqlite3
db = sqlite3.connect('/opt/savvy/deploy/data/new-api/one-api.db', timeout=20)
rows = [
    ('AgentZeroClawURL', 'ws://zeroclaw:42617'),
    # require_pairing=false 时网关忽略此值; 留非空以通过 Go 侧三键齐全闸门
    ('AgentZeroClawToken', 'intranet-only'),
    ('AgentZeroClawAgent', 'topup'),
]
for k, v in rows:
    db.execute("INSERT INTO options(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", (k, v))
db.execute("DELETE FROM options WHERE key LIKE 'AgentBailian%'")
db.commit()
print("options:", [r for r in db.execute("SELECT key,value FROM options WHERE key LIKE 'Agent%'")])
PY
docker compose restart new-api >/dev/null

echo "== 7. 端到端冒烟(游客一轮对话, DeepSeek 真调用) =="
sleep 10
curl -s -X POST http://127.0.0.1:3000/api/user/agent/chat \
  -H 'Content-Type: application/json' \
  -d '{"prompt":"你好，你能帮我做什么？"}' | head -c 600
echo
echo "== DONE =="
