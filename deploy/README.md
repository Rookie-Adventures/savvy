# Savvy Agent 部署方案 (合规双机版)

> 备案站只反代,后端国内服务器,模型走已备案聚合商。零海外代理。

## 架构

```
┌─────────────────────────────────────────────────┐
│  机A: 备案站 2C2G                                 │
│  scheng.net (已备案:豫ICP2026026934/公安)         │
│  • Nginx + 静态首页 (finesse 8section)           │
│  • HTTPS证书 (ACME 自动续签)                     │
│  • 反代 /api/ + /v1/ → 机B                        │
└──────────┬──────────────────────────────────────┘
           │ (m机内网IP / 同机房VPC / SSH隧道)
           ▼
┌─────────────────────────────────────────────────┐
│  机B: 云服务器 (建议 4C8G+, 不备案, 不开公网Web)  │
│  docker compose up -d 启动整栈:                    │
│  • new-api (Go) ── SQLite (/data/one-api.db)      │
│  • redis (new-api 缓存/限流)                       │
│  • savvy-manager (容编, docker.sock)              │
│  • workspace-router (机B内部Nginx, 41000-41099池)  │
│  • hermes-agent (按需 profile full)              │
│  • (workspace 容器池由 savvy-manager 拉起, 2h自睡) │
│  • 海外模型 = 控制台加聚合商渠道 (合规, 不自建代理) │
└─────────────────────────────────────────────────┘
```

## 文件

| 文件 | 放哪 | 干嘛 |
|------|------|------|
| `docker-compose.yml` | 机B `/opt/savvy/deploy/` | 启后端整栈 |
| `.env.example` → `.env` | 同上 | 密钥 (不入仓库, .gitignore 已拦) |
| `nginx.conf` | 同上 (挂载入 workspace-router 容器) | **机B内部** workspace 41000-41099 端口池路由 (已有,勿改) |
| `nginx-scheng.conf` | 机A `/etc/nginx/sites-available/` | **机A** 公网入口反代 (我新写的) |
| `backup.sh` | 机B `/usr/local/bin/` | SQLite 定时备份 |
| `verify.sh` | 机B `/opt/savvy/deploy/` | 部署后健康自检 |
| `README.md` | 本文件 | 部署手册 |

### 两个 Nginx 别搞混
- **机A `nginx-scheng.conf`**: 公网入口, HTTPS, 静态首页, 反代机B后端 (初学者就这一个)
- **机B `nginx.conf`**: 容器内/workspace 端口池路由, 已有逻辑跑着, 挂载进 `workspace-router` 容器, 你不用碰

---

## 机A 部署步骤 (备案站)

```bash
# 1. 装 Nginx + certbot
sudo apt update && sudo apt install -y nginx certbot python3-certbot-nginx

# 2. 静态首页构建产物 (在开发机做)
cd /path/to/savvy/new-api/web/default && bun install && bun run build
# 产物在 dist/, 上传到机A:
scp -r dist/* root@机A:/var/www/savvy-agent/

# 3. 放 Nginx 配置 (先改 BACKEND_HOST 为机B内网IP)
sudo sed -i 's/BACKEND_HOST/<机B内网IP>/g' nginx-scheng.conf
sudo cp nginx-scheng.conf /etc/nginx/sites-available/scheng.net
sudo ln -s /etc/nginx/sites-available/scheng.net /etc/nginx/sites-enabled/
sudo rm -f /etc/nginx/sites-enabled/default

# 4. 先 HTTP 拿证书 (DNS 已解析到机A公网IP前提下)
sudo nginx -t && sudo systemctl reload nginx
sudo certbot --nginx -d scheng.net -d www.scheng.net
# certbot 自动改 ssl_certificate 路径 + 自动续签

# 5. 机A防火墙 (只开 22/80/443)
sudo ufw allow 22 && sudo ufw allow 80 && sudo ufw allow 443
sudo ufw --force enable

# 6. 备案尾块 (首页 footer 已在代码里, 无需额外动作)
# 确认主体名 + ICP号 + 公安号在首页底部可见

# 6. 验证
curl -I https://scheng.net
# 应返回 200, 首页静态 OK
```

## 机B 部署步骤 (后端)

```bash
# 1. 装 Docker
curl -fsSL https://get.docker.com | sh
sudo systemctl enable --now docker

# 2. 拉项目 (或 git clone)
mkdir -p /opt/savvy && cd /opt/savvy
# 把 deploy/ + new-api/ + savvy-manager/ + hermes-agent/ 传上来
# 或 git clone 整个 monorepo

# 3. 填密钥
cd deploy
cp .env.example .env
# 编辑 .env: 生成随机值
#   openssl rand -hex 32  → SESSION_SECRET
#   openssl rand -hex 16  → SAVVY_PROVIDER_ENC_KEY (确保 32 字符)
#   openssl rand -hex 24  → SAVVY_HMAC_SECRET
#   自定 Redis 密码

# ⚠️ .env 不入仓库 (deploy/.gitignore 已拦)。docker-compose.yml 全密钥走 ${VAR:?required}
#    从 .env 读, 缺任一变量 compose 启动 hard-fail (exit 非零, 拒绝静默起错配栈)。
#    必填: REDIS_PASSWORD / SESSION_SECRET / SAVVY_PROVIDER_ENC_KEY / SAVVY_HMAC_SECRET
#    其中 SAVVY_HMAC_SECRET 是 new-api ↔ savvy-manager 共用签名密钥, 两边读同一 .env 变量,
#    保证一致 (历史 bug: 不一致 → 全 /internal/* 401)。

# 4. 构建启动
docker compose up -d --build
docker compose ps   # 全部 healthy

# 5. 健康自检 (整栈起来没)
sudo chmod +x verify.sh && ./verify.sh
# 全过再继续, 有失败查 docker compose logs <服务名>

# 6. 首次进 new-api 控制台初始化
# 从本地 SSH 隧道访问机B:3000 (机B不开公网Web):
ssh -L 3000:localhost:3000 root@机B
# 浏览器开 http://localhost:3000 → 首次设管理员账号 + 改密码

# 7. 配模型渠道 (控制台 → 渠道)
#   国内已备案模型: 阿里通义/豆包/智谱GLM/DeepSeek/Kimi/百川 → 选对应类型填Key
#   海外模型(Claude/GPT): 加已备案聚合商渠道
#     类型=OpenAI, BaseURL=聚合商地址, Key=聚合商给的
#   价格/分组按你定价策略设

# 8. 装备份
sudo cp backup.sh /usr/local/bin/savvy-backup.sh
sudo chmod +x /usr/local/bin/savvy-backup.sh
sudo crontab -e
# 加行: 0 3 * * * /usr/local/bin/savvy-backup.sh >> /var/log/savvy-backup.log 2>&1
# 先手动跑一次验证:
sudo /usr/local/bin/savvy-backup.sh

# 9. 防火墙: 只开给机A + SSH, 不开公网Web (端口没绑回环, ufw 是唯一防线)
sudo ufw default deny incoming
sudo ufw allow from <机A内网IP> to any port 3000
sudo ufw allow from <机A内网IP> to any port 41000:41099
sudo ufw allow 22
sudo ufw enable
```

---

## 安全清单 (上线前过一遍)

- [ ] `.env` 已填随机密钥, 未入 git (`deploy/.gitignore` 加 `.env`); 缺任一 compose 启动 hard-fail
- [ ] docker-compose 密钥全 `${VAR:?required}` 从 .env 读 (无 CHANGE_ME 明文占位了)
- [ ] **SAVVY_HMAC_SECRET**: new-api 与 savvy-manager 两边读同一 .env 变量 (已统一)
- [ ] **SAVVY_MOCK_MODE=false** 在 compose 已显式设 (默认 true 会假编排)
- [ ] 机B防火墙: 3000/8000/41000-41099 仅机A能连, 不对公网 (现在端口不绑 127.0.0.1, ufw 是唯一防线!)
- [ ] 机B SSH: 改非22端口 + 密钥登录 + 禁root密码 (`PasswordAuthentication no`)
- [ ] 机A HTTPS 证书自动续签测试: `sudo certbot renew --dry-run`
- [ ] new-api 首次管理员密码已改, 非 `123456` 之类
- [ ] Redis 密码已设 (`REDIS_PASSWORD` 在 .env, new-api + redis 两端读同一值)
- [ ] 备份脚本手动跑过, `/var/backups/savvy` 有 .db.gz
- [ ] 异地备份 (rclone 到对象存储) 配置并验证一次
- [ ] 备案信息首页可见: 主体名(栗橙科技) + ICP号 + 公安备案号
- [ ] 海外模型仅经聚合商, 不自建海外代理节点

## 合规注意 (逐条, 别踩)

1. **国内用户访问的是 `scheng.net`(备案站)** —— 这一段快、不翻墙、合规
2. **调境外模型这一跳在机B服务器端** —— 合规责任在你, 不能自建海外代理
3. **境外模型(AI)必须在网信办完成大模型备案/登记** —— OpenAI/Claude 没有 → 你不能直接转售给国内公众
4. **正解:签国内已备案聚合商** —— 对方有跨境资质+大模型登记, 你是下游分销商
5. **数据跨境** —— 用户请求经聚合商出海, 聚合商担数据出境评估责任, 你签标准合同
6. **生成内容标识** —— new-api 给的响应要能挂"AI生成"水印, 首页明示模型来源(你卖点就是"按上游原样转发不偷换")
7. **公安备案** —— 30天内要在首页底部挂公安备案号+联网核查图标(你已有 豫公网安41010402003621)

## 后续扩展位 (现在不做, YAGNI)

| 场景 | 现在做法 | 扩容信号 | 扩容方案 |
|------|---------|---------|---------|
| SQLite撑不住 | 单文件 | 并发 >500 或写锁频繁 | `SQL_DSN` 改 MySQL, compose 加 mysql 服务, 零改代码 |
| 单机B扛不住 | 4C8G单点 | CPU常>80% | 拆机B2跑worker, Redis共享, SESSION_SECRET一致 |
| 备份不足 | 本地7天 | 想要异地+长时间 | backup.sh 填 `RCLONE_REMOTE` 推对象存储 |
| 海外模型要直连 | 经聚合商 | 流量大/有资质需求 | 公司申请大模型登记+数据出境评估 (重, 慎) |
| 高可用 | 单点 | 机B挂=瘫 | 机B2热备 + DB主从 (重, 你选了"先能跑", 日流水起来再说) |

## 机A 机B 分工速查

| 组件 | 机A(2C2G备案) | 机B(4C8G+) |
|------|---------------|------------|
| 静态首页 dist | ✅ | |
| HTTPS证书 | ✅ | |
| Nginx反代 | ✅ | |
| new-api后端 | | ✅ |
| SQLite文件 | | ✅ `/data/one-api.db` |
| Redis | | ✅ |
| savvy-manager | | ✅ |
| workspace-router (内部Nginx) | | ✅ 41000-41099端口池 |
| Docker容器池 | | ✅ (workspace 2h自睡) |
| 备份脚本 | | ✅ (cron 凌晨3点) |
| 公网Web入口 | ✅ scheng.net | ❌ 不开 |
| 备案 | ✅ 已办 | ❌ 不需要 |

---

**部署顺序**: 机A先(能开首页) → 机B启动 → 机A反代连通 → 控制台初始化 → 配渠道模型 → 装备份 → 上线。

部署完想让我接着接, 把机A/机B的IP和环境贴给我, 我帮你过配置 + 验证连通性。
