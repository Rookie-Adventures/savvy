# Workspace 路由与端口池隔离方案

> **状态：已实施并跑通（2026-07-04 凌晨）**。真统一镜像 `hermes-unified:saas` 已构建（6.08GB）并部署，普通用户首启→填 provider key→发消息流式→sleep/wake 端到端通过。原 `workspace-stub/` 占位目录已移除（不可用于生产，见关键事实 1）。
> 原草案日期：2026-07-03。背景：workspace 与 agent 无法通信，打开即 404（4 处断裂，见下）。本文档保留作架构参考与运维验收手册。

## 问题根因（4 处断裂）

| # | 请求 | 当前行为 | 结果 |
|---|------|---------|------|
| A | `GET /workspace/?token=x` | nginx `proxy_pass $workspace_upstream`（变量）不 strip `/workspace/` 前缀 | 容器收 `/workspace/...`，但 workspace 只在根 `/` 提供服务 → 404 |
| B | 前端 `fetch('/api/...')`（232 处硬编码绝对路径） | 不带 `/workspace/` 前缀 → 命中 nginx `location /` → 转发 new-api | new-api 无此路由 → 404 |
| C | `favicon.ico` / `manifest.json` / `sw.js` | nginx 新加的 `return 404` 正则块直接拦截 | 静态资源 404 |
| D | nginx → workspace 容器:3000 | 统一镜像 s6 run 脚本未设 `HOST`；`server-entry.js` 默认 `HOST=127.0.0.1`，且非回环 host 需 `HERMES_PASSWORD` 或 `HERMES_ALLOW_INSECURE_REMOTE=1` 否则拒绝启动（安全机制 #122） | workspace server 仅容器内回环可达，nginx 跨容器连不上 → 502/连接拒绝 |

**根本矛盾**：workspace 是 TanStack Start 全栈应用（前端 + 232 处 `/api/...` server routes），设计为**独占根路径**。强行挂 `/workspace/` 子路径切断前端→server route 的同源调用。

## 关键架构事实（已验证）

1. **agent 不是独立容器**。`Dockerfile.unified` 用 s6-overlay 在同一容器跑：
   - hermes-agent gateway `:8642`
   - workspace server `:3000`
   - s6 run 脚本硬编码 `HERMES_API_URL=http://127.0.0.1:8642`（容器内回环）

   agent 通信在容器内完成，无跨容器网络问题。

2. **workspace 容器不映射宿主端口**，仅在 `savvy_savvy-net` 内网通过容器名访问。nginx 已能通过 `proxy_pass $workspace_upstream` + resolver `127.0.0.11` 动态解析。

3. **每用户单实例**：`instance_id = f"inst-{user_id}"`，`container_name = f"savvy-u{user_id}-w1"`（`savvy-manager/app/routers/users.py:92-93`）。

4. **new-api 前端消费 workspaceUrl**：`new-api/web/default/src/features/hermes/index.tsx:84` 构建 `${workspaceUrl}?token=...` 打开新标签。`workspace_url` 改为绝对 URL 后无缝兼容，new-api 零改动。

5. **savvy-manager config**：`env_prefix = "SAVVY_"`（`savvy-manager/app/config.py:21`），故 `SAVVY_PUBLIC_HOST` → `settings.public_host`。

## 解决方案：端口池隔离（token 驱动路由）

每个 workspace 分配一个专用 nginx 监听端口，workspace 占该端口的根路径。前端 232 处 `/api/...` 保持同源（端口），不再与 80 端口的 new-api 冲突。

**核心洞察**：路由已是 token 驱动（`auth_request` → `X-Workspace-Upstream` 头 → `proxy_pass $variable`）。端口池中每个端口功能相同，单一 server 块含 N 个 `listen` 指令即可服务所有 workspace。**无需 per-container 配置，无需 nginx reload**。

解决全部 4 个根因：
- A：无 `/workspace/` 前缀 → `proxy_pass` 到容器根 → ✓
- B：`/api/...` 同源（端口）请求 → 命中端口 server 块 → `auth_request` → 容器 → ✓
- C：根资源代理到容器根，删除正则 `return 404` 块 → ✓
- D：设 `HOST=0.0.0.0` + `HERMES_ALLOW_INSECURE_REMOTE=1`（后者已设）→ workspace server 绑定所有接口，nginx 可达 → ✓

agent 通信（容器内 :8642）不受影响。

---

## 实施步骤

### 步骤 0：镜像状态（已构建，无需重复）

真统一镜像 `hermes-unified:saas` 已于 2026-07-04 凌晨构建并部署：

```bash
docker images hermes-unified:saas          # 确认存在（6.08GB）
docker history hermes-unified:saas | head -20
```

仅当 `Dockerfile.unified` 变更后才需重建：

```bash
docker build -f Dockerfile.unified -t hermes-unified:saas .
```

### 步骤 1：config.py — 端口池 + 公网 host

**文件**：`e:\savvy\savvy-manager\app\config.py`

`Settings` 类内添加（`env_prefix = "SAVVY_"` 已存在）：
```python
workspace_port_start: int = 41000
workspace_port_end: int = 41099
public_host: str = "localhost"
```

### 步骤 2：models.py — 添加 assigned_port 列

**文件**：`e:\savvy\savvy-manager\app\models.py`

`Instance` 类（约 line 33-45）添加字段：
```python
assigned_port = Column(Integer, nullable=True)
```
> MVP 用 `create_all`（无迁移系统），列添加后对旧行为 NULL，访问时回填（见步骤 5）。

### 步骤 3：users.py — 创建实例时分配端口

**文件**：`e:\savvy\savvy-manager\app\routers\users.py`

`create_instance` 函数（line 70-117），在 `container_name`/`volume_name` 设置后（line 94 之后）、`Instance(...)` 构造前（line 99 之前）插入端口分配逻辑：

```python
from ..config import settings

# 查询已占用端口
used_ports = {
    p[0] for p in db.query(Instance.assigned_port)
    .filter(Instance.assigned_port.isnot(None)).all()
    if p[0]
}
assigned_port = next(
    (p for p in range(settings.workspace_port_start, settings.workspace_port_end + 1)
     if p not in used_ports),
    None,
)
if assigned_port is None:
    raise HTTPException(status_code=503, detail="No available workspace ports")
```

`Instance(...)` 构造调用（line 99-106）添加 `assigned_port=assigned_port`：
```python
inst = Instance(
    instance_id=instance_id,
    user_id=user_id,
    status=InstanceStatus.SLEEPING,
    plan=plan,
    container_name=container_name,
    volume_name=volume_name,
    assigned_port=assigned_port,
)
```

`InstanceResponse`（line 17-26）添加字段 `assigned_port: int | None = None`，并在两处返回（line 58-67 get_instance、line 81-90 已有分支、line 110-117）填充 `assigned_port=inst.assigned_port`。

> 注意：`users.py` 顶部已 `from ..models import Instance, InstanceStatus, PlanType, User`，需补 `from ..config import settings`（或用 `from ..config import settings` 加在已有 import 区）。

### 步骤 4：token.py — 返回绝对 URL

**文件**：`e:\savvy\savvy-manager\app\token.py`

`generate_access_token` 函数签名改为接收 host/port：
```python
def generate_access_token(
    instance_id: str,
    user_id: str,
    expires_in_minutes: int = 30,
    workspace_host: str = "localhost",
    workspace_port: int = 41000,
) -> dict:
```

返回字典中 `workspace_url` 改为：
```python
"workspace_url": f"http://{workspace_host}:{workspace_port}/",
```
移除原 `f"/workspace/{user_id}/"` 构造。

### 步骤 5：instances.py — issue_access_token 传参 + 回填端口

**文件**：`e:\savvy\savvy-manager\app\routers\instances.py`

`issue_access_token`（line 128-145）修改：
```python
@router.post("/{instance_id}/access-token", response_model=AccessTokenResponse)
async def issue_access_token(instance_id: str, auth=Depends(require_hmac), db: Session = Depends(get_db)):
    inst = _get_instance(instance_id, auth["user_id"], db)

    if inst.status != InstanceStatus.RUNNING:
        raise HTTPException(status_code=409, detail="Instance is not running")

    # 回填：旧实例可能 assigned_port 为 NULL
    if not inst.assigned_port:
        from ..config import settings
        used_ports = {
            p[0] for p in db.query(Instance.assigned_port)
            .filter(Instance.assigned_port.isnot(None)).all() if p[0]
        }
        port = next(
            (p for p in range(settings.workspace_port_start, settings.workspace_port_end + 1)
             if p not in used_ports), None,
        )
        if port is None:
            raise HTTPException(status_code=503, detail="No available workspace ports")
        inst.assigned_port = port
        db.commit()

    from ..config import settings
    result = generate_access_token(
        instance_id=instance_id,
        user_id=auth["user_id"],
        expires_in_minutes=30,
        workspace_host=settings.public_host,
        workspace_port=inst.assigned_port,
    )

    return AccessTokenResponse(
        token=result["token"],
        expires_at=result["expires_at"],
        workspace_url=result["workspace_url"],
    )
```

### 步骤 6：docker_manager.py — HOST 环境兜底

**文件**：`e:\savvy\savvy-manager\app\docker_manager.py`

`create_container` 的 `environment`（line 85）改为：
```python
environment={
    "HERMES_ALLOW_INSECURE_REMOTE": "1",
    "HOST": "0.0.0.0",
    "PORT": "3000",
},
```

> **切勿**设 `HERMES_API_URL`。统一镜像 s6 脚本已设 `http://127.0.0.1:8642`（容器内回环），覆盖会破坏 agent 通信。
> 无需 `ports=` 参数（nginx 走内网 DNS，不映射宿主端口）。
> `HOST=0.0.0.0` 会触发 `server-entry.js` 的非回环检查，但 `HERMES_ALLOW_INSECURE_REMOTE=1`（已设）绕过。安全模型：容器在内网不映射宿主端口，外部访问由 nginx token 验证拦截。

### 步骤 7：nginx.conf — 端口池 server 块

**文件**：`e:\savvy\deploy\nginx.conf`

完整替换为：

```nginx
upstream newapi {
    server new-api:3000;
}

upstream savvy_manager {
    server savvy-manager:8000;
}

server {
    listen 80;
    server_name localhost;

    resolver 127.0.0.11 valid=30s;
    client_max_body_size 100M;

    # Savvy Manager API (internal only, called by new-api backend)
    location /manager/ {
        proxy_pass http://savvy_manager/;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # Everything else → new-api
    location / {
        proxy_pass http://newapi;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 300s;
    }
}

# Workspace server: 端口池，每端口功能相同，token 驱动路由
server {
    # 生成 41000-41099 的 listen 指令（共 100 行）
    listen 41000;
    listen 41001;
    listen 41002;
    # ... 继续到 ...
    listen 41099;

    resolver 127.0.0.11 valid=30s;
    client_max_body_size 100M;

    # Token: query 参数优先于 cookie
    set $workspace_token "";
    if ($cookie_workspace_token != "") {
        set $workspace_token $cookie_workspace_token;
    }
    if ($arg_token != "") {
        set $workspace_token $arg_token;
    }

    # Cookie path=/ 使后续 /api/* 请求携带 token
    if ($arg_token != "") {
        add_header Set-Cookie "workspace_token=$arg_token; Path=/; HttpOnly; SameSite=Lax";
    }

    # Token 验证（必须在同 server 内，auth_request 子请求在此解析）
    auth_request /validate-token;
    auth_request_set $user_id $upstream_http_x_user_id;
    auth_request_set $instance_id $upstream_http_x_instance_id;
    auth_request_set $workspace_upstream $upstream_http_x_workspace_upstream;

    location = /validate-token {
        internal;
        proxy_pass http://savvy_manager/internal/workspace/validate;
        proxy_pass_request_body off;
        proxy_set_header Content-Length "";
        proxy_set_header X-Original-URI $request_uri;
        proxy_set_header X-Original-Method $request_method;
        proxy_set_header X-Token $workspace_token;
    }

    # 根路径全部代理到 workspace 容器（无前缀 strip 需求）
    location / {
        proxy_pass $workspace_upstream;
        proxy_set_header X-User-Id $user_id;
        proxy_set_header X-Instance-Id $instance_id;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 300s;
    }
}
```

**关键变更**：
- 删除原 `location /workspace/` 块
- 删除静态资源正则 `return 404` 块
- 删除 80 端口 server 内的 `location = /validate-token`（移到 workspace server）
- 80 端口 server 只保留 `/manager/` 和 `/`（new-api）
- workspace server cookie `Path=/`（原 `/workspace/`）
- **listen 指令需完整生成 41000-41099 共 100 行**（实施者务必逐行写出，不可省略）

### 步骤 8：docker-compose — 暴露端口范围

**文件**：`e:\savvy\docker-compose.yml` 和 `e:\savvy\docker-compose.prod.yml`

nginx 服务 `ports` 添加端口范围：
```yaml
  nginx:
    image: nginx:alpine
    ports:
      - "80:80"
      - "41000-41099:41000-41099"
    volumes:
      - ./deploy/nginx.conf:/etc/nginx/conf.d/default.conf:ro
```

savvy-manager 服务 `environment` 添加：
```yaml
    environment:
      # ... 已有变量 ...
      SAVVY_PUBLIC_HOST: localhost   # dev；prod 改为真实域名
      SAVVY_WORKSPACE_PORT_START: 41000
      SAVVY_WORKSPACE_PORT_END: 41099
```

### 步骤 9：new-api — 零改动

无需修改。`GetHermesAccessToken`（`new-api/controller/hermes.go:206`）透传 `workspace_url`，前端 `index.tsx:84` 构建 `${workspaceUrl}?token=...` 打开新标签，新的绝对 URL 无缝兼容。

---

## 容器生命周期

- `assigned_port` 存 DB，sleep/wake（stop/start）不变。无需回收。
- 容器无宿主端口映射 → stop 时无需回收。
- 扫描器停过期容器 → 端口 server 返回 502/连接拒绝直到重启。无需 nginx 改动或 reload。
- `stop_container`/`start_container`/`remove_container` 零改动。

## 安全模型

- workspace 容器在 `savvy_savvy-net` 内网，**不映射宿主端口**，仅 nginx 可达。
- `HOST=0.0.0.0` + `HERMES_ALLOW_INSECURE_REMOTE=1` 仅让内网可达容器 3000。
- 外部访问 41000-41099 由 nginx `auth_request` token 验证拦截，未授权请求到不了容器。
- Token 30 分钟过期，HMAC-SHA256 签名，仅实例 owner 可生成。
- 浏览器不直连 savvy-manager。

---

## 验收步骤（实施已通过）

> **实测：2026-07-04 凌晨全部通过**。普通用户首启→填 provider key→发消息流式→sleep/wake→撤销重填，端到端通（见文末「模型密钥注入端到端验收」）。

### 前置
```bash
# 1. 统一镜像存在且非 stub
docker images hermes-unified:saas
docker history hermes-unified:saas | head -20

# 2. 重建并启动
docker compose -f docker-compose.yml up -d --build
```

### 端口监听检查
```bash
# nginx 监听 80 + 41000-41099
netstat -an | findstr "410"   # Windows
# 或容器内：
docker exec savvy-nginx-1 sh -c "netstat -tln | grep 410"
```
预期：41000-41099 全部 LISTEN。

### 数据库检查
```bash
# 创建实例后，DB 中 assigned_port 已设
docker exec savvy-manager-1 python -c "
from app.database import SessionLocal
from app.models import Instance
db = SessionLocal()
for i in db.query(Instance).all():
    print(i.instance_id, i.container_name, i.assigned_port, i.status)
"
```
预期：每个实例 `assigned_port` 在 41000-41099，非 NULL。

### 端到端功能验收
1. **登录 new-api** → 进入 Hermes 控制台 → 启动 workspace 实例。
2. **取 access token**：检查 API 响应 `workspaceUrl` = `http://<host>:410NN/`（绝对 URL，非 `/workspace/...`）。
3. **浏览器打开** `http://<host>:410NN/?token=...`：
   - workspace UI 正常加载，**无 404**。
   - DevTools Network：
     - `/api/sessions` → 200
     - `/api/connection-status` → 200
     - `/api/events` (SSE) → 200，持续流
     - `/favicon.ico`、`/manifest.json`、`/sw.js` → 200
     - 响应头含 `X-Instance-Id`
   - **无 new-api HTML 渗漏**（之前 404 的症状是返回 new-api 首页 HTML 导致 JS Syntax Error）。
4. **发聊天消息**：流式返回正常，agent 响应。`docker exec <container> sh -c "ss -tln | grep 8642"` 确认 agent 在容器内监听。
5. **Cookie 检查**：DevTools Application → Cookies → `workspace_token` 存在，`Path=/`。刷新页面后 `/api/*` 请求仍携带 cookie，validate 端点接受。

### 容器内 agent 验证
```bash
# agent 在容器内监听 8642（非独立容器）
docker exec savvy-u<user_id>-w1 sh -c "curl -s http://127.0.0.1:8642/health"
# workspace server 绑定 0.0.0.0:3000
docker exec savvy-u<user_id>-w1 sh -c "ss -tln | grep 3000"
```
预期：agent health OK；3000 绑定 `0.0.0.0`（非 `127.0.0.1`）。

### 生命周期验收
1. **sleep 实例**（savvy-manager API 或等 3h 自动）→ 刷新 workspace 标签 → 502/连接拒绝。
2. **wake 实例** → 刷新 → 恢复正常。**无需 nginx reload**。
3. 多用户并发：创建第 2 个实例 → `assigned_port` 分配不同端口（41001）。两个 workspace 同时可用。

### nginx 配置语法
```bash
docker exec savvy-nginx-1 nginx -t
```
预期：`syntax is ok` + `test is successful`。

### Provider model 动态探测（2026-07-05 根本修复）

**背景**：原 `build_snapshot` 在用户不填 model 时回退到 `settings.provider_default_model = "claude-sonnet-4"`。但 new-api 那把 sk-key 只配了 deepseek channel → base chat 命中 `No available channel for model claude-sonnet-4` → 503 → agent 无回复。现象：workspace UI 正常加载，发"你好"无 AI 响应，manager log 显示 `POST /internal/instances/{id}/start 200` 但 chat 到不了上游。

**根因原则**：用户不选模型 — 由密钥决定。Manager 用这把 key 调 new-api `GET /v1/models`，取返回数组第一项作 `config.yaml model.default`。以后 new-api 增删 channel、换 model，manager 自动跟上，永不命中假频道。

**代码改动**：
- `savvy-manager/app/provider_config.py` `probe_default_model(api_key, base_url) -> str`：调 `/v1/models`，取 `data[0].id`。失败直接 raise（**无硬编码兜底** — 拒绝 ship 一个可能是假频道的猜测）。
- `savvy-manager/app/routers/instances.py` `start_instance`：`body.provider_model` 空且 `provider_api_key` 在 → 调 probe，失败抛 400 `"Failed to list models..."` 让用户知道钥匙/网络问题。
- `settings.provider_default_model` 现为死值（probe-or-fail 路径不触及），保留以兼容直接调 `build_snapshot` 的测试路径。

**部署前提**：源码改动必须 `docker compose -f <file> up -d --build savvy-manager` 重建镜像生效 — manager 跑的是镜像内 `/app/app/*`，不是宿主 bind-mount。recreate 不带 `--build` 则旧行为静默保留。

**验收**：reset 实例（DB `provider_config_enc=NULL, status=NOT_CREATED` + 删容器/卷）→ 前端首启带 key → 查 DB 快照 `snap['model']` == new-api `/v1/models` 首项；容器内 `config.yaml model.default` 同值。前往端发"你好" → agent 真回复。

### HMAC secret 跨 compose 文件对齐（2026-07-05 根本修复）

**背景**：现象类似 Model 不通 — 前端"创建工作区"报 `Invalid HMAC signature`，但错误出自 **new-api** log（`failed to get/create hermes instance: Invalid HMAC signature`），manager access log 无 401。

**根因**：`docker-compose.yml`（dev）`SAVVY_HMAC_SECRET=dev-hmac-secret-change-me`；`docker-compose.prod.yml`（prod）`SAVVY_HMAC_SECRET=change-me-in-production`。本机 new-api 由 dev compose 起，而一次 manager 重建错用 prod compose → 两边 secret 不一致 → new-api 签名被 manager `verify_hmac_signature` 拒 → 全部 `/internal/*` 401。

**同时暴露的 3 个 prod 隐 bug**：
1. prod new-api 完全没设 `SAVVY_HMAC_SECRET`（Go 读 `os.Getenv("SAVVY_HMAC_SECRET")`，不读 prod 设的 `SESSION_SECRET`）→ prod 部署必然 HMAC 挂。
2. prod new-api 没设 `HERMES_MANAGER_URL` → Go 默认 `http://localhost:8000`（容器内）→ 连不到 savvy-net 上的 `savvy-manager`。
3. dev/prod secret 静默漂移，无强制。

**根本修复**：
- 两 compose 文件 `SAVVY_HMAC_SECRET` 改读 `.env`：dev `${SAVVY_HMAC_SECRET:-dev-hmac-secret-change-me}`（无 .env 也能跑），prod `${SAVVY_HMAC_SECRET:?required}`（**hard-fail** — 无 .env 时 `compose config` 退出非零，拒绝静默起错配 manager）。
- prod new-api 补 `SAVVY_HMAC_SECRET` + `HERMES_MANAGER_URL=http://savvy-manager:8000`。
- `.env`（gitignored）新增 `SAVVY_HMAC_SECRET`；生成 `python -c "import secrets;print(secrets.token_urlsafe(32))"`。
- `SESSION_SECRET`（new-api gin session）与 HMAC 无关，不动。

**部署铁律**：重建 manager 必须用正在运行的 new-api 同一个 compose 文件。判定：`docker inspect <ctr> --format '{{index .Config.Labels "com.docker.compose.project.config_files"}}'`。文件不一致 → secret 不一致 → 静默 HMAC 401。

## 风险与备注

- **端口暴露**：41000-41099 公网可达，但每个端口需 token（auth_request）。可接受。若后续有泛域名 DNS，可改子域名 `ws-<uid>.host` 保持 80 端口，auth_request 机制不变。
- **池耗尽**：>100 并发 FREE workspace → 分配失败，返回 503。用 env 扩范围。每用户单实例 + 3h 自动 sleep，单机部署不太可能触达。
- **统一镜像构建重**：`Dockerfile.unified` 构建 workspace + 拉 `nousresearch/hermes-agent:latest`，部署前确保完成。
- **HERMES_API_URL 覆盖风险**：勿在 `docker_manager` 环境设 `HERMES_API_URL=http://hermes-agent:8642`（会破坏统一镜像回环）。
- **listen 指令必须完整**：nginx 不支持 `listen 41000-41099` 范围语法，必须逐行 `listen 410NN;`。实施者务必生成全部 100 行。

## 实施者检查清单

- [x] 步骤 0：统一镜像存在（非 stub）
- [x] 步骤 1：`config.py` 添加 3 个设置
- [x] 步骤 2：`models.py` Instance 添加 `assigned_port` 列
- [x] 步骤 3：`users.py` create_instance 分配端口 + InstanceResponse 字段
- [x] 步骤 4：`token.py` generate_access_token 接收 host/port 返回绝对 URL
- [x] 步骤 5：`instances.py` issue_access_token 传参 + 回填
- [x] 步骤 6：`docker_manager.py` environment 添加 HOST/PORT
- [x] 步骤 7：`nginx.conf` 端口池 server 块（100 个 listen）
- [x] 步骤 8：`docker-compose.yml` + `docker-compose.prod.yml` 端口范围 + env
- [x] 步骤 9：new-api 无改动（确认）
- [x] 验收：全部通过


---

### 模型密钥注入端到端验收

前置:
- `SAVVY_PROVIDER_ENC_KEY` 已在 .env 设置(32 字节 urlsafe base64)
- `docker compose up -d --build` 已重启 manager + 容器

1. 全新用户首启 workspace
   - 启动弹窗——不填 provider key,点启动 → 期望:400 "provider_api_key is required on first start"
   - 填入 new-api 生成的 sk-xxx → 启动成功
   - 进入工作区 → DevTools Network → `/api/sessions` 200
2. 发消息 → 流式返回(验证 B 层密钥真注入)
3. Settings → 改成自己的 Anthropic key → 仍能调模型
4. sleep → 唤醒 → 用新 key 仍能调用(验证唤醒对账回写 DB,source=user)
5. 工作区控制台点"撤销供应商密钥"
   - 状态显示:未配置
   - workspace UI 仍可访问 + 发消息 → 401/无凭证(预期)
   - docker exec savvy-u1-w1 cat /opt/data/config.yaml → provider/api_key 字段已空
   - volume 数据未变:`docker exec savvy-u1-w1 ls /workspace` 文件原样
6. 回控制台重填我们的 new-api sk → 恢复聊天
7. **Provider model 动态探测**（2026-07-05 新增）：reset 实例（DB enc=NULL + 删容器/卷）→ 首启带 key 不填 model → 查 `docker exec savvy-manager python -c "from app.database import SessionLocal;from app.models import Instance;from app import crypto;db=SessionLocal();i=db.query(Instance).filter(Instance.instance_id=='inst-1').first();s=crypto.decrypt_provider_config(i.provider_config_enc,i.provider_config_alg or 'fernet');print(s['model'])"` 应等于 new-api `/v1/models` 返回 `data[0].id`（非写死 `claude-sonnet-4`）→ 前端发"你好" agent 真回复。
8. **HMAC secret 对齐**：`docker inspect savvy-manager new-api --format '{{.Config.Env}}' | tr ' ' '\n' | grep SAVVY_HMAC_SECRET` 两侧值一致。重建 manager 用与 new-api 同一 compose 文件（`docker inspect new-api --format '{{index .Config.Labels "com.docker.compose.project.config_files"}}'`）。prod 无 `.env` 时 `docker compose -f docker-compose.prod.yml config` 应退出非零（hard-fail 生效）。
