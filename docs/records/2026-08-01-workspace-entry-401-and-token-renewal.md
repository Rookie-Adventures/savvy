# 2026-08-01 workspace 入口 401 + token 续期缺失

## 症状

客户访问 workspace(工作区)撞 401。表现被叠成一句:"新用户首启必撞 401,关标签重开才正常"。日志 343 条 `[DEBUG_VALIDATE]` 反复刷屏。

## 根因

两个独立故障叠加成单一症状描述:

### 现象 C — host 劫持(首启撞,主因)

- `savvy-manager` 用 `SAVVY_PUBLIC_HOST` 拼入口 URL。env 当前 `http://localhost`,经 `token.py:45` 产出 `http://localhost:41003/`。
- 浏览器解析 `localhost` 时 IPv6 优先,命中 `::1`。
- Windows `::1:41003` 被 `wslrelay.exe`(WSL2 端口转发)抢占监听,指向另一要求认证的服务 → 401 Authorization Required(`nginx/1.31.2` 口吻,**非** manager 的 "Invalid or expired token")。
- Docker 真实端口转发由 `com.docker.backend.exe` 绑 `::`(含 127.0.0.1),正常工作。
- 取证:`netstat` 见 PID 504(wslrelay)绑 `::1`、PID 12396(docker)绑 `::`;同 token 打 `127.0.0.1:41003` → 307 → `/workspace/` → 200;打 `localhost` → 401。
- 结论:入口 URL host 恒为 localhost,wslrelay 恒拦截 → **每个用戶每次首启都撞**。

### 现象 A — token cookie 无续期(长开会话死,次因)

- savvy-nginx `auth_request /validate-token` 每请求打 `GET /internal/workspace/validate`(`workspace.py`)→ `verify_access_token`(`token.py:74`)→ `payload["exp"] < time.time()` 过期即拒。
- token 寿命 30min。首启访问时 nginx `Set-Cookie workspace_token=<token>`,cookie 存同一个 30min token,过期全死。
- 前端 `handleOpenWorkspace`(`hermes/index.tsx:118`)用 `window.open(url,'_blank')` 开新标签页,新标签里是 workspace 自己的页面,非 new-api SPA。token 过期后 401 在新标签的浏览器导航层,new-api SPA fetch 拦截器在另一标签上下文够不着。
- 结论:**前端自动重签在新标签模式下不可行**(跨上下文硬约束,三次确认)。

### 两现象关系

C 主因(首启),A 次因(长开会话死)。"关标签重开自愈" = 关掉死会话标签 → 重新点 Open → 走 `issue_access_token` 拿新 30min token → 自愈。被叠成单一症状。

## 权威源 / 决策依据

- nginx auth_request 官方模式天然支持续期:`auth_request_set` 抓子请求响应 header,主请求据其 `add_header` 刷 cookie。续期在 nginx+manager 层,与浏览器标签上下文无关。
- 决策:不做前端自动重签(跨上下文不可行)、不做 iframe 嵌入(X-Frame-Options 风险 + 拆现有 window.open 手势)、不改 hermes-workspace 上游容器(受掣肘)、不拉长 token 寿命(治标)。nginx 续期是最佳实践 + 最 lazy(ponytail:复用现有 auth_request 链,前端零改)。

## 修复思路

### 现象 C(配置层,零代码)

仅改 env,不动代码。本机 `.env` 改 `SAVVY_PUBLIC_HOST=http://127.0.0.1`(绕 wslrelay 的 `::1`)。`token.py:45` 已正确处理带 scheme 的 host,无需改。

### 现象 A(nginx auth_request 续期)

- `token.py`:加 `renew_access_token(instance_id, user_id, expires_in_minutes=30) -> str`,DRY 复用 `generate_access_token` 取 `["token"]`,零重复 HMAC 逻辑。
- `workspace.py` validate 端:验签通过后算 `remaining = exp - now`;`if remaining < 300`(5min 阈值)调 `renew_access_token` 放响应 header `X-Renewed-Token`;header 仅临过期时设。**顺手删 9 处 `print(f"[DEBUG_VALIDATE]...")` 噪声**。
- `deploy/nginx.conf`:`auth_request_set $renew_token $upstream_http_x_renewed_token`;新 map `$renew_token → $ws_renew_cookie`(空→空,浏览器忽略,与现有 `$ws_set_cookie` 同套路);`location /` 加第 2 个 `add_header Set-Cookie $ws_renew_cookie always`。

续期在 nginx+manager 层,cookie 寿命随活跃用户滑动续期,活跃会话过期前自动刷新;长闲置自然过期 → 重连走 `issue_access_token` 全新 token。

## 改动清单

| 文件 | 改动 | commit |
|---|---|---|
| `savvy-manager/app/token.py` | +`renew_access_token` (~7 行) | 0572343 |
| `savvy-manager/app/routers/workspace.py` | +临过期续期 header, -9 处 debug print | 1c25a9f |
| `deploy/nginx.conf` | +`auth_request_set $renew_token` + map `$ws_renew_cookie` + `add_header Set-Cookie` | 62ca032 |
| `docs/superpowers/specs/2026-08-01-workspace-entry-401-design.md` | spec | 109f6f4 |
| `docs/superpowers/plans/2026-08-01-workspace-entry-401.md` | plan | 109f6f4 |
| `.env`(gitignored,不入库) | `SAVVY_PUBLIC_HOST` localhost→127.0.0.1 | 本地 |

manager 改了需重建镜像。nginx 改了 `restart nginx`(:ro 挂载,读源刷新)。前端、hermes-workspace 零改。

## 验证结果

- pytest:13 passed(2 新 validate + 5 token + 6 HMAC)。
- 现象 C:`http://127.0.0.1:41003/?token=...` → 307 → `/workspace/?token=...` → 200。
- 现象 A e2e:短 token(180s < 300 阈值)→ 带Cookie打 `/` → `Set-Cookie: workspace_token=<新token>`(新≠旧);新Cookie复打 `/workspace/` → 200;fresh 30min token → 无续期 header(不误续)。
- 日志:`DEBUG_VALIDATE` count 0;nginx debug 头清零。

## 已知限制(明确不碰)

- 续期阈值 300s / 寿命 30min 不变。长闲置(>30min 无请求)自然过期,靠重连自愈——符合安全语义。
- 不动 `new-api/web/default/src/features/hermes/`(前端),不动 hermes-workspace 上游容器。

## 遗留尾巴

- **服务器部署**(本机验证过,服务器待滚):
  - 机A `deploy/docker-compose.yml` 已是 `https://scheng.net`(对外域),但需确认机A nginx 反代**通配 41000-41099 端口段映射到机B**——独立部署配置,不在本 4 commit 范围。
  - 推新 manager 镜像到服务器并重建(`token.py`+`workspace.py` 改了)。
  - `deploy/nginx.conf` 同步到服务器 + `nginx -s reload`。
- 现象 C 在服务器侧若机A反代已是域名入口,服务器可能本不撞 401(localhost 劫持是 Windows WSL2 特有)。本地是 Windows,服务器机B是 Linux,无 wslrelay。但续期(A)服务器同样需要。

## 调试小记(坑)

- Git Bash 工具对 `nginx -t -c /path` 的 `-c` 参数做 MSYS 路径转换(`/etc/nginx/nginx.conf` → `C:/Program Files/Git/etc/nginx/...`),报假错。改用 PowerShell 工具绕过。
- Windows PowerShell 5.1 把原生命令 stderr 包成 ErrorRecord + `$?` 置假(`NativeCommandError`),即使命令 exit 0。`nginx -t` 说 "test is successful" 即成功,别被红字误导。
- manager 镜像无 curl,容器内测 endpoint 用 `httpx`(FastAPI 依赖)。nginx 坑内 curl 无 DNS resolver 直接打容器名会 exit 6;用 `docker exec savvy-manager python -c "..."` 最稳。
- nginx `auth_request_set` 抓的 header 名:nginx 把上游响应 header 名小写化、连字符转下划线,`X-Renewed-Token` → `$upstream_http_x_renewed_token` 抓。manager httpx 测确认返回 `x-renewed-token`。
