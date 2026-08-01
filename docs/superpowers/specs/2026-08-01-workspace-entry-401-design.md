#办公区入口 401 续期设计

日期: 2026-08-01
状态: 已批准

## 背景与根因

客户访问工作区 (workspace) 撞 401。调查发现两个独立故障叠加成"新用户首启必撞 401 + 关标签重开自愈"的症状描述。

### 现象 C：host 劫持 401（首启撞，主因）

- `savvy-manager` 用 `SAVVY_PUBLIC_HOST` 拼入口 URL。env 当前值 `http://localhost`，经 `token.py:45` 产出 `http://localhost:41003/`。
- 浏览器解析 `localhost` 时 IPv6 优先，命中 `::1`。
- Windows 上 `::1:41003` 被 `wslrelay.exe`（WSL2 端口转发，PID 504）抢占监听，它指向另一个要求认证的服务，凡从 `localhost` 进的请求一律 401 Authorization Required（nginx/1.31.2 口吻，非 manager 的 "Invalid or expired token"）。
- Docker 真实端口转发由 `com.docker.backend.exe`（PID 12396）绑在 `::`（含 IPv4 127.0.0.1）上，正常工作。
- 已证：同 token 打 `http://127.0.0.1:41003/` → 307 → `/workspace/?token=...` → 200。绕开 `::1` 即通。
- 这是"新用户首启必撞 401"的真凶：入口 URL host 恒为 `localhost`，wslrelay 恒拦截。

### 现象 A：token cookie 续期缺失（长开会话死，次因）

- savvy-nginx `auth_request /validate-token` 每请求打 manager `GET /internal/workspace/validate`（`workspace.py`）。
- `validate` 调 `token.py:verify_access_token`，其中 `payload["exp"] < time.time()` → 过期即 401。
- token 寿命 30min（`token.py:17 expires_in_minutes=30`）。首启访问时 nginx `Set-Cookie workspace_token=<token>; HttpOnly`，cookie 存的是这同一个 30min token。
- 30min 后 cookie 死，所有带 cookie 请求死亡。
- 前端 `handleOpenWorkspace`（`hermes/index.tsx:118`）用 `window.open(url, '_blank')` 开**新标签页**。新标签里是 workspace 自己的页面，不是 new-api SPA。token 过期后 401 发生在新标签的浏览器导航/请求层，new-api SPA 的 fetch 拦截器在另一标签上下文，够不着。
- 故"前端自动重签"在新标签模式下不可行。跨上下文硬约束已三次确认。

### 两个现象的关系

- C 是主因（首启撞），A 是次因（长开会话死）。"关标签重开自愈"= 关掉死会话标签后重新点 Open，走再签发_access_token 拿新 30min token → 自愈。被叠成单一症状描述。
- 两故障同生（入口 URL 生成 + token 生命周期），但根因独立、修复独立。

## 设计

### 节 1 — 修现象 C：host 劫持（零代码，配置层）

仅改 env 值，不动代码。

- 本机：`SAVVY_PUBLIC_HOST=http://127.0.0.1`
- 服务器（机A反代对外）：`SAVVY_PUBLIC_HOST=https://<对外域名>`

`token.py:45` 已正确处理带 scheme 的 host（`f"{workspace_host}:{workspace_port}/"`），无需改。

服务器对外的端口段映射（机A反代通配 41000-41099）是独立部署配置，不在本 spec 范围；本节只保证 env 值正确，端口段映射部署时一并配。

### 节 2 — 修现象 A：token cookie 续期（nginx auth_request 续期模式）

改动 manager + savvy-nginx，不动两个前端。

#### 2.1 token.py — 增续期函数

- `verify_access_token` 保持现有签名（返回 payload 或 None），验签成功时由调用方自己读 `payload["exp"]` 算剩余寿命（不改函数，保持调用方最小变动）。
- 新增 `renew_access_token(instance_id, user_id, expires_in_minutes=30) -> str`：复用现有 payload 形状 + `get_secret()` 重签，返回新 token 字符串。本质是 `generate_access_token` 去掉 `workspace_url` 返回的结构，仅回 token。
- `generate_access_token` 内部签 token 的那段可抽共用，避免重复（ ponytail：DRY，不重复 HMAC 逻辑）。但若抽公共函数增加一层间接得不偿失——直接让 `renew_access_token` 调 `generate_access_token` 取 `["token"]` 即可，零重复。

#### 2.2 workspace.py — validate 端在临过期时发新 token

- 验签通过后，算 `remaining = payload["exp"] - now`。
- `if remaining < 300`（5min 阈值）：调 `renew_access_token`，放响应 header `X-Renewed-Token: <new>`。
- 删除全部 7 处 `print(f"[DEBUG_VALIDATE]...")`（日志噪声元凶，上个对话 343 条）。
- 其余逻辑不变。

续期阈值 5min 与寿命保持 30min 的理由：5min 滑动窗口足够覆盖任何正常会话的请求间隔（交互中的人不会 5min 完全无请求），又不至于每请求都重签（token 易变攻击面 + 重签开销）。30min 短寿不变，长闲置自然过期符合免费试用安全语义——重建连接靠再签发_access_token 走全新 token。

#### 2.3 savvy-nginx default.conf — 抓续期 header 刷 cookie

- 加 `auth_request_set $renew_token $upstream_http_x_renewed_token;`
- 加 map 或在现有 `$ws_set_cookie` 逻辑旁：`$renew_token` 非空时发 `Set-Cookie workspace_token=$renew_token; Path=/; HttpOnly; SameSite=Lax` 续 cookie。
- 现有 `$ws_set_cookie`（仅 query 带 token 时发首启 cookie）保留不动；续期 cookie 是新增的、独立的 add_header，二者不互斥。

nginx auth_request 续期是天然续期位：续期发生在 nginx+manager 层，与新标签页/SPA 上下文无关，新标签页照常续期。

## 效果

- 现象 C：入口 URL 用可达 host，wslrelay 不再拦截。首启不再撞 401。
- 现象 A：cookie 寿命随活跃用户滑动续期，活跃会话永不过期；长闲置后自然过期，重连走再签发_access_token 拿新 token。前端零改动。

## 不做

- 前端自动重签（跨上下文不可行，三次确认）。
- token 寿命拉长（治标不用）。
- iframe 嵌入（X-Frame-Options 风险 + 拆现有 window.open 手势设计）。
- workspace 侧自重签（改上游 hermes-workspace 容器，受掣肘且脆弱）。

## 改动面汇总

| 文件 | 改动 |
|---|---|
| savvy-manager env `SAVVY_PUBLIC_HOST` | `http://localhost` → 本机 `127.0.0.1` / 服务器域名 |
| `savvy-manager/app/token.py` | +`renew_access_token()` (~5 行) |
| `savvy-manager/app/routers/workspace.py` | +临过期续期 header，-7 处 debug print |
| savvy-nginx `default.conf` | +`auth_request_set $renew_token` + 续期 Set-Cookie |

manager 改了需重建镜像。前端、hermes-workspace 零改。

## 验证

- 本机改 `SAVVY_PUBLIC_HOST=http://127.0.0.1`，重建 savvy-manager 镜像，重启。
- test2 点 Open Workspace → 入口 URL 应为 `http://127.0.0.1:41003/?token=...`，不再 401。
- 长开会话 >30min 后仍在用，观察 cookie 是否被 X-Renewed-Token 刷新（抓 nginx 响应 header 或 manager 日志）。
- 闲置 >30min 后无请求 → 自然过期 → 重新点 Open → 新 token 自愈。
- 日志无 [DEBUG_VALIDATE] 噪声刷屏。
