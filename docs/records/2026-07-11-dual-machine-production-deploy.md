# 双机生产部署实录

> **状态：已部署并验证通过（2026-07-11）**。端口池 HTTPS 方案落地，workspace 前端→gateway 全链路跑通。本文档记录服务器上**实际运行**的配置，非设计草案。
> 背景：workspace 打开后 "Connect Your Backend" 弹框（HTTP 404），根因是 `SAVVY_PUBLIC_HOST=/workspace` 导致 `fetch('/api/...')` 命中 new-api 而非 workspace。详见 [workspace-routing.md](./workspace-routing.md)。

## 机器清单

| 角色 | 实例 ID | 公网 IP | 内网 IP (VPC) | 规格 | 地域 |
|---|---|---|---|---|---|
| 机A (反代) | `i-wz953hq55nljkz6jwm21` | 8.135.58.63 | 172.24.96.232 | 2C/2G | cn-shenzhen |
| 机B (全栈) | `i-wz9b5nhr3idgu8fqvnvk` | 120.77.11.137 | 172.24.96.233 | 4C/8G | cn-shenzhen |

同 VPC，内网互通。**机B 不直接对外暴露 Web 端口**（3000/41000-41099 仅允许机A内网 IP 入站），所有外部流量经机A nginx 终结。

## 请求链路

```
浏览器
  ↓ HTTPS (443 / 41000-41099)
机A nginx (SSL 终结, 透明代理)
  ↓ HTTP (VPC 内网)
机B 端口
  ├── :3000 → new-api 容器 (Go + React 管理后台)
  ├── :8000 → savvy-manager 容器 (Python 容器编排)
  └── :41000-41099 → workspace-router 容器 (nginx, token 驱动路由)
                        ↓ auth_request → savvy-manager:8000/internal/workspace/validate
                        ↓ proxy_pass → savvy-u{N}-w1:3000 (workspace 容器, 根路径)
                                        ├── workspace server :3000
                                        └── hermes-agent gateway :8642 (容器内回环)
```

## 机A nginx 配置

**文件位置**：`/etc/nginx/sites-available/default` + `/etc/nginx/sites-available/workspace-ports`

### default（443 SSL + 80 重定向）

```nginx
server {
    listen 443 ssl default_server;
    server_name scheng.net www.scheng.net;
    ssl_certificate /etc/nginx/ssl/scheng.net.pem;
    ssl_certificate_key /etc/nginx/ssl/scheng.net.key;
    ssl_protocols TLSv1.2 TLSv1.3;
    client_max_body_size 256m;

    # 全部反代到机B new-api
    location / {
        proxy_pass http://172.24.96.233:3000;
        # ... 标准 proxy headers
    }

    # OpenAI 兼容 API (SSE)
    location /v1/ {
        proxy_pass http://172.24.96.233:3000;
        proxy_buffering off;
        proxy_read_timeout 600s;
    }

    # workspace public/ 静态资源 (图片/logo/manifest)
    location ~* \.(?:png|webp|jpg|jpeg|gif|ico|svg|json|txt|woff2?)$ {
        proxy_pass http://172.24.96.233:41000;
        expires 7d;
    }

    # workspace 页面（兼容旧路径，新方案走端口池）
    location /workspace {
        proxy_pass http://172.24.96.233:41000;
    }
}

server {
    listen 80 default_server;
    server_name scheng.net www.scheng.net;
    return 301 https://$host$request_uri;
}
```

### workspace-ports（41000-41099 HTTPS 端口池）

100 个 server block，结构完全相同，仅 listen 端口不同：

```nginx
server {
    listen 41000 ssl;                        # 41001, 41002, ... 41099
    server_name scheng.net www.scheng.net;

    ssl_certificate /etc/nginx/ssl/scheng.net.pem;
    ssl_certificate_key /etc/nginx/ssl/scheng.net.key;
    ssl_protocols TLSv1.2 TLSv1.3;

    client_max_body_size 100M;
    location / {
        proxy_pass http://172.24.96.233:41000;   # 对应端口到机B
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 600s;
        proxy_buffering off;
    }
}
```

**关键点**：机A **不做 auth_request**，只做透明 HTTPS→HTTP 代理。token 验证由机B workspace-router 处理。

**部署脚本**：`deploy/_deploy_https.py`（通过 paramiko SSH + SFTP 上传）

## 机B Docker 容器栈

**compose 文件**：`/opt/savvy/deploy/docker-compose.yml`

### 常驻容器

| 容器 | 镜像 | 端口映射 | 说明 |
|---|---|---|---|
| `new-api` | 本地构建 | 3000:3000 | Go+React 管理后台，API 网关 |
| `redis` | redis:7-alpine | 无(内网) | new-api 缓存/限流 |
| `savvy-manager` | 本地构建 | 8000:8000 | Python 容器编排，HMAC API |
| `workspace-router` | nginx:1.27-alpine | 41000-41099:41000-41099 | 端口池 token 路由 |
| `hermes-agent` | 本地构建 | 无 | 默认不启（profile: full） |

### 动态容器（savvy-manager 编排）

| 容器 | 命名规则 | 端口 | 说明 |
|---|---|---|---|
| `savvy-u{N}-w1` | `f"savvy-u{user_id}-w1"` | 无宿主映射 | workspace + gateway，内网 DNS 可达 |

### 关键环境变量

```yaml
# savvy-manager
SAVVY_DATABASE_URL: sqlite:////data/savvy-manager.db
SAVVY_PROVIDER_ENC_KEY: ${SAVVY_PROVIDER_ENC_KEY:?required}   # 32 字节 urlsafe base64
SAVVY_HMAC_SECRET: ${SAVVY_HMAC_SECRET:?required}             # 与 new-api 同值
SAVVY_MOCK_MODE: false                                         # 必须显式 false
SAVVY_WORKSPACE_NETWORK: savvy_savvy-net                      # Docker 网络名
SAVVY_PUBLIC_HOST: https://scheng.net                         # 带 scheme！
SAVVY_DEBUG: true

# new-api
SAVVY_HMAC_SECRET: ${SAVVY_HMAC_SECRET:?required}             # 与 savvy-manager 同值
HERMES_MANAGER_URL: http://savvy-manager:8000                 # 容器名寻址
SESSION_SECRET: ${SESSION_SECRET:?required}
```

### workspace-router nginx 配置

**文件**：`deploy/nginx.conf`（挂载到容器 `/etc/nginx/conf.d/default.conf`）

```nginx
# 端口池 server block (41000-41099)
server {
    listen 41000;  # ... listen 41001-41099

    resolver 127.0.0.11 valid=30s;

    # Token 兜底链: query arg → cookie → Referer header
    map $cookie_workspace_token $ws_token_from_cookie { "" ""; default $cookie_workspace_token; }
    map $arg_token $ws_token_from_arg { "" $ws_token_from_cookie; default $arg_token; }
    map $http_referer $ws_token_from_ref { default ""; ~*token=([^&\s]+) $1; }
    map $ws_token_from_arg $ws_token_with_ref { default $ws_token_from_arg; "" $ws_token_from_ref; }

    auth_request /validate-token;
    auth_request_set $workspace_upstream $upstream_http_x_workspace_upstream;

    location = /validate-token {
        internal;
        proxy_pass http://savvy_manager/internal/workspace/validate;
        proxy_set_header X-Token $ws_token_with_ref;
    }

    location / {
        add_header Set-Cookie $ws_set_cookie always;
        proxy_pass $workspace_upstream;   # 容器名:3000，由 validate 返回
    }
}
```

## 安全组规则

### 机A (`sg-wz91cel740d5i6hofe7l`)

| 方向 | 协议 | 端口 | 来源 | 说明 |
|---|---|---|---|---|
| 入站 | TCP | 443 | 0.0.0.0/0 | HTTPS 主入口 |
| 入站 | TCP | 41000-41099 | 0.0.0.0/0 | Workspace 端口池 |
| 入站 | TCP | 22 | 0.0.0.0/0 | SSH (建议限管理 IP) |
| 入站 | TCP | 3000 | 172.24.96.232/32 | (历史残留，可清理) |
| 入站 | TCP | 41000 | 172.24.96.232/32 | (被上面范围覆盖) |

### 机B

**不开公网 Web 端口**。仅允许机A内网 IP 访问 3000/41000-41099/8000。SSH 开放。

## 端口分配

每个 workspace 实例分配一个 `assigned_port`（41000-41099），存入 savvy-manager SQLite DB。

```python
# savvy-manager/app/routers/instances.py
port = next(p for p in range(settings.workspace_port_start, settings.workspace_port_end + 1)
            if p not in used_ports)
inst.assigned_port = port
```

访问 token 生成：

```python
# savvy-manager/app/token.py
"workspace_url": (
    f"{workspace_host}:{workspace_port}/"
    if workspace_host.startswith(("http://", "https://"))
    else f"https://{workspace_host}:{workspace_port}/"
)
# 当 SAVVY_PUBLIC_HOST=https://scheng.net → https://scheng.net:41000/
```

## 运维命令速查

```bash
# === 机B ===
# 查看所有容器
docker ps --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'

# savvy-manager 日志
docker logs -f savvy-manager --tail 50

# 重建 savvy-manager（改代码后）
cd /opt/savvy/deploy
docker compose -f docker-compose.yml up -d --build savvy-manager

# 查看实例端口分配
docker exec savvy-manager python3 -c '
from app.database import SessionLocal; from app.models import Instance
db = SessionLocal()
for i in db.query(Instance).all():
    print(f"{i.instance_id} port={i.assigned_port} status={i.status}")'

# workspace-router nginx 测试
docker exec workspace-router nginx -t

# === 机A ===
# nginx 重载
nginx -t && nginx -s reload

# 查看端口池监听
ss -tlnp | grep -cE '410[0-9][0-9]'

# 测试代理链
curl -sk -o /dev/null -w '%{http_code}' https://127.0.0.1:41000/
# 预期: 401 (需要 token)

# === 阿里云 CLI ===
# 远程执行命令
aliyun ecs RunCommand --RegionId cn-shenzhen --Type RunShellScript \
  --CommandContent $(echo '命令' | base64) \
  --InstanceId.1 i-wz953hq55nljkz6jwm21 --ContentEncoding Base64

# 查看安全组
aliyun ecs DescribeSecurityGroupAttribute --RegionId cn-shenzhen \
  --SecurityGroupId sg-wz91cel740d5i6hofe7l --Direction ingress
```

## 从零部署要点（一次性脚本提炼）

部署在其覆盖过程中使用了一组 `deploy/_*.py` 一次性 paramiko/aliyun-CLI 脚本探路，落地后已删除；以下保留其可复用片段，供未来从零重建机B时参考。

### 机B Docker 镜像源（终态）

```bash
# /etc/docker/daemon.json
cat > /etc/docker/daemon.json << 'EOF'
{
  "registry-mirrors": [
    "https://mirror.ccs.tencentyun.com",
    "https://docker.mirrors.tuna.tsinghua.edu.cn",
    "https://hub-mirror.c.163.com"
  ]
}
EOF
systemctl restart docker

# apt docker 源走清华（替代官方 download.docker.com）
# /etc/apt/sources.list.d/docker.list 替换为清华源后 apt update
```

### 机A 安全组开端口池

```bash
aliyun ecs AuthorizeSecurityGroup --RegionId cn-shenzhen \
  --SecurityGroupId sg-wz91cel740d5i6hofe7l \
  --IpProtocol tcp --PortRange 41000/41099 --SourceCidrIp 0.0.0.0/0 \
  --Description "workspace port pool"
```

### 修复 SAVVY_PUBLIC_HOST（机B）

改机B的 `docker-compose.yml`：`SAVVY_PUBLIC_HOST=https://scheng.net`（带 scheme），再：
```bash
cd /opt/savvy/deploy && docker compose up -d --build savvy-manager
# 验证
docker inspect savvy-manager --format '{{range .Config.Env}}{{println .}}{{end}}' | grep SAVVY_PUBLIC_HOST
```

### 机A HTTPS 端口池 nginx 配置生成

100 个同构 server block，仅 listen 端口不同。模板（`_deploy_https.py` 生成逻辑）：
```nginx
server {
    listen 41000 ssl;  # 遍历 41000..41099
    server_name scheng.net www.scheng.net;
    ssl_certificate /etc/nginx/ssl/scheng.net.pem;
    ssl_certificate_key /etc/nginx/ssl/scheng.net.key;
    ssl_protocols TLSv1.2 TLSv1.3;
    client_max_body_size 100M;
    location / {
        proxy_pass http://172.24.96.233:41000;   # 对应端口到机B
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 600s;
        proxy_buffering off;
    }
}
```
写入 `/etc/nginx/sites-available/workspace-ports` → symlink 到 enabled → `nginx -t && nginx -s reload`。
机A **不做 auth_request**，仅透明 HTTPS→HTTP 代理，token 验证由机B workspace-router 处理（见上"workspace-router nginx 配置"）。

> ⚠️ 一次性脚本曾**硬编码明文 root 密码**。重建勿复用该模式——走 SSH key 或临时环境变量。

## 端到端验收

```bash
# 1. 生成真实 token
docker exec savvy-manager python3 -c '
from app.config import settings; from app.token import generate_access_token
r = generate_access_token("inst-1", "1", 30, settings.public_host, 41000)
print(r["workspace_url"], r["token"])'

# 2. 浏览器打开 https://scheng.net:41000/?token=<TOKEN>
# 预期: workspace UI 加载，地址栏有 HTTPS 锁图标

# 3. DevTools Network 检查
# /api/auth-check → 200 {"authenticated":true,"authRequired":false}
# /api/ping → 200 {"ok":true,"status":200}
# /api/events (SSE) → 200 持续流
# /favicon.ico → 200

# 4. 发消息 → agent 流式回复
```

## 已知限制

1. **端口池上限 100**：超过 100 个并发 workspace 实例会分配失败（503）。当前单用户单实例 + 3h 自动 sleep，不太可能触达。
2. **非标准端口**：用户需访问 `scheng.net:41000`，部分企业防火墙可能拦截非标准端口。后续可考虑子域名方案 `ws.scheng.net`。
3. **SSL 证书共用**：端口池复用 `scheng.net` 证书。如果证书过期需所有端口同步更新（nginx reload 即可）。
4. **HTTP 被拒**：端口池仅监听 SSL，plain HTTP 请求会收到 400（`The plain HTTP request was sent to HTTPS port`）。
