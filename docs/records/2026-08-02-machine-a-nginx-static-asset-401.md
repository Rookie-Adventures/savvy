# 2026-08-02 机A nginx 静态资源路由劫持 → new-api 资源 401

## 症状

- scheng.net 首页备案 logo（gongxin.png / beian.png）不显示，浏览器控制台报 401。
- 字体文件 `/static/font/lora-latin-wght-normal.*.woff2` 加载 401。
- `logo.png`、`favicon.ico` 等根级静态资源 401。
- 本地开发环境一切正常，仅服务器（经机A反代）复现。
- 无痕模式 / Ctrl+Shift+R 强刷无效（非浏览器缓存）。

## 根因

机A nginx（`/etc/nginx/sites-enabled/default`）的 location 优先级设计缺陷：

```nginx
# 原配置（有 bug）
location / {
    proxy_pass http://172.24.96.233:3000;   # new-api
}
location ~* \.(?:png|webp|jpg|jpeg|gif|ico|svg|json|txt|woff2?)$ {
    proxy_pass http://172.24.96.233:41000;  # workspace-router（需 auth_request token）
}
```

nginx 匹配优先级：`=` > `^~` > `~`/`~*`（regex）> 普通前缀。

regex location 优先级**高于**普通前缀 `location /`，导致**所有**以 `.png`/`.woff2`/`.ico` 等结尾的请求——包括 new-api 自己的 `/static/font/*.woff2` 和 `/gongxin.png`——全被劫持到 workspace-router（41000）。

workspace-router 有 `auth_request /validate-token`（需 workspace_token cookie），new-api 的静态资源请求不带 token → **401 Unauthorized**。

### 为什么本地正常

本地 docker-compose 没有机A nginx 这层反代。浏览器直连 new-api:3000 或 workspace-router:41000，不存在路由劫持。

### 为什么无痕/强刷无效

401 是服务端路由错误，不是浏览器缓存。每次请求都走机A nginx → 都被 regex 劫持 → 都 401。

## 修复思路

利用 nginx 优先级规则，让 new-api 静态资源在 regex 之前被截走：

1. **`location ^~ /static/`**（preferential prefix，优先级高于 regex）→ new-api:3000
   - 覆盖所有 Vite 打包产物：JS/CSS/字体/图片
2. **`location = /gongxin.png`** 等精确匹配（最高优先级）→ new-api:3000
   - 覆盖 new-api `web/default/public/` 下的根级文件：
     gongxin.png, beian.png, logo.png, favicon.ico, pay-apple.png, pay-card.png, pay-google.png, waffo-logo-*.svg
3. **保留 regex** `\.(?:png|woff2?...)$` → workspace:41000
   - 仅匹配非 `/static/` 路径的剩余静态文件（workspace 自己的 public/ 资源）

优先级链：`= /gongxin.png` > `^~ /static/` > `~* \.png$` > `/ `

## 改动清单

| 位置 | 改动 | 方式 |
|---|---|---|
| 机A `/etc/nginx/sites-enabled/default` | +`location ^~ /static/` → 3000；+8 个精确匹配 → 3000；regex 保留给 workspace | aliyun RunCommand + base64 写入 |
| 机A nginx | `nginx -t && systemctl reload nginx` | 同上 |

**零代码改动**。纯机A nginx 配置层修复。

## 验证结果

```
curl -skI https://scheng.net/static/font/lora-latin-wght-normal.f60a385cfb.woff2 → 200 font/woff2
curl -skI https://scheng.net/logo.png       → 200 image/png
curl -skI https://scheng.net/favicon.ico    → 200 image/vnd.microsoft.icon
curl -skI https://scheng.net/gongxin.png    → 200 image/png
curl -skI https://scheng.net/beian.png      → 200 image/png
```

workspace-router 日志中 401 条目停止增长。

## 关联：hermes-unified:saas 镜像重建

同日发现 workspace 容器仍跑 7月21日旧镜像（5.69GB）。已执行：

```bash
cd /opt/savvy && docker build -f Dockerfile.unified -t hermes-unified:saas .
```

新镜像 `2026-08-02 05:22:26`（5.7GB）。savvy-manager scanner 的 `check_image_staleness`（每10分钟）
自动检测镜像 ID 不一致 → 标 `needs_rebuild=True` → `check_needs_rebuild`（每1分钟）在容器
SLEEPING 时自动 rm + create 换新版。RUNNING 容器不打断，等下次睡眠（FREE 2h 到期）自愈。
**无需手动停/删容器。**

## 已知限制

- 机A nginx 配置是手动维护的（不在 docker-compose 管理），每次改需 RunCommand 或 SSH。
  仓库 `deploy/nginx-scheng.conf` 是旧模板（还有 `BACKEND_HOST` 占位符），**与服务器实际配置不同步**。
- 如果 new-api 后续在 `public/` 新增根级静态文件，需手动在机A nginx 加精确匹配，
  否则又会被 regex 劫持到 workspace → 401。
  长期方案：把 regex 改成仅匹配 workspace 特有路径前缀（如 `/assets/`），
  或让 workspace 的静态资源全走 `/workspace/` 前缀。

## 调试小记（坑）

- 容器内没有 `strings` 命令（debian-slim 不带 binutils），`docker exec new-api strings /new-api`
  静默失败返回空。需从宿主机 `docker cp` 出来用 `grep -c` 检查二进制内容。
- PowerShell 对 `$host` 做变量替换（→ `System.Management.Automation.Internal.Host.InternalHost`），
  对 `\n` 不展开。用 sed 写 nginx 配置会被 PowerShell 搞坏。
  **解法**：Python 脚本 base64 编码整个配置文件，RunCommand 里 `echo '<b64>' | base64 -d > file`。
- 最初误判为浏览器缓存 / CDN 缓存 / Docker build cache 问题，花了大量时间排查。
  关键转折：`curl -skI https://scheng.net/gongxin.png` 从机A返回 401，从机B localhost:3000 返回 200
  → 锁定机A nginx 路由问题。
