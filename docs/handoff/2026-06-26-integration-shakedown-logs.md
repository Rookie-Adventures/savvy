# 2026-06-26 联调与网络编排实战日志及诊断交接文档

> **背景**：2026-06-26 晚间，用户与 Claude 进行了对 `new-api`（计费与主控制台）、`savvy-manager`（生命周期管理）、与 `Nginx` 动态反代理层全线的联合调试。
> **现状**：所有核心接口代码已经彻底通车（HMAC签名、JSON合规化、新GET验证接口、自定义响应包裹中间件等均已开发完成并打包）。目前，浏览器通过 Nginx 子请求发送鉴权已完美获得 `200 OK`，但在通过变量进行 `proxy_pass` 动态转发容器时，撞墙于 `502 Bad Gateway`。

---

## 1. 🌟 本轮联调大突破与已解决大坑记录

本轮调试我们攻克了多重深水区的底层技术瓶颈：

### 1.1 ✅ Nginx 直连宿主机大坑（问题已修复）
* **痛点**：Nginx 尝试将所有的 `/` 默认路由分发给宿主机的 `host.docker.internal:3000`。由于宿主机 3000 未监听而是在 Compose 内部网络里，导致全线 502。
* **修复**：修改了 `deploy/nginx.conf` 中的 upstream，将 `newapi` 显式解析目标设置为 Compose 服务域名 **`new-api:3000`**。

### 1.2 ✅ FastAPI 与 Go 契约不对齐：`manager returned failure` 悬案（问题已修复）
* **痛点**：Go 端（`new-api`）期望收到带有 `{success: true, data: ...}` 封装的响应。而 Python 裸跑直接返回了 `{user_id: 1, created: true}`。这导致 Go 解包器找不到 `success` 键默认为 `false` 并疯狂抛出 `manager returned failure` 错误。
* **修复**：在 `savvy-manager/app/main.py` 新写了一个全局 `envelope_response_middleware` 响应包裹中间件。自动拦截所有 `/internal/` 下响应，如果是 200 则自适应包装。

### 1.3 ✅ Uvicorn 底层崩溃：`Response content longer than Content-Length`（问题已修复）
* **痛点**：写完中间件后，Uvicorn 报错崩溃。起因是中间件重构了 Response 的 content 使得体积变大，但在复制 Headers（`response.headers`）时直接带入了原先裸报文极小的 `Content-Length`，导致长度不符。
* **修复**：在重新拼装 Response 时，对 headers 执行 `pop("content-length", None)` 移除，让 FastAPI 重新计算出 100% 精准的新长度。

### 1.4 ✅ Nginx 子请求变量作用域隔离导致的 401 漏斗（问题已修复）
* **痛点**：Nginx 在 `location /workspace/` 拦截到 Query Token 变量并放入 `$workspace_token`，但在 `auth_request /validate-token` 子请求中，由于变量作用域不穿透隔离，导致向 Manager 传递的 `X-Token` 请求头为空，报 `401 Unauthorized (Missing token)`。
* **修复**：重写了 `savvy-manager/app/routers/workspace.py`。加入**“URL 穿透抓取降级设计”**——当 `X-Token` 头为空时，主动截取 Nginx 自动携带的 `X-Original-URI` 原始完整 URL（`/workspace/1/?token=xxxx`）中的 Query 参数，百分之百抵御子请求变量失效。

### 1.5 ✅ Base64 `%3D%3D` URL 转义导致的 Invalid Token（问题已修复）
* **痛点**：穿透拿到 Token 后依然 401，日志显示 `Invalid or expired token`。起因是 URL 传递时 Base64 的尾部填充符 `==` 被转义为了 `%3D%3D`，而 Python 在 split Token 计算 HMAC 签名时未做解码，导致签名校验崩塌。
* **修复**：在 `savvy-manager/app/token.py` 校验入口处，强韧加入 `unquote(token)` 过滤逻辑，URL 解码完美复原。

### 1.6 ✅ Nginx 动态变量解析崩溃：`no resolver defined`（问题已修复）
* **痛点**：由于在 `proxy_pass` 中使用了变量 `$workspace_upstream`，Nginx 强制要求必须在 server 块内定义 DNS resolver 否则无法运行时域名解析。
* **修复**：在 `deploy/nginx.conf` 头部及 server 块加入了 Docker 的内置 DNS 服务器：**`resolver 127.0.0.11 valid=30s;`**。

---

## 2. 🔍 当前 502 网卡连通性卡点与诊断清单（明天第一步）

今天收尾时的 502 表明 **Nginx 在 200 OK 票据校验通过后，成功解析到了变量 `http://savvy-u1-w1:3000` 并试图去连接它，但连接被拒绝。**

### 2.1 极大嫌疑点：

由于本轮调试中，管理端的环境变量配置为：`SAVVY_MOCK_MODE=true`。
* **在这个模式下，Manager 并不会在宿主机上真正通过 Docker API 拉起 `savvy-u1-w1` 这个实体的 Docker 容器！**
* 仅仅是我们在数据库里伪造了一笔 “RUNNING” 状态的数据！
* **结果**：Nginx 去连接一个只存在于数据库幻觉里、实际物理上并没有启动的虚拟容器 `savvy-u1-w1:3000`，当然会无情抛出 `502 Bad Gateway`（连接被拒绝）！

---

## 3. 🛠️ 明天的实操打通三步走计划

### 第一步：验证是否是 MOCK 幻觉导致的 502
1. 检查是否有物理运行的容器：在宿主机运行 `docker ps`。
2. 看看有没有名字叫 `savvy-u1-w1` 且监听 `3000` 端口的真实容器存在。
3. 如果没有，说明完全符合嫌疑判定。因为 `MOCK_MODE=true` 下容器是假的，Nginx 连不上。

### 第二步：一键激活真实物理容器联动
若要在本地连通真实的统一工作空间：
1. **挂载 Docker 套接字**：修改 `docker-compose.yml` 中的 `savvy-manager` 服务，将宿主机的 Docker socket 挂载进去，使其具备控制权：
   ```yaml
   volumes:
     - /var/run/docker.sock:/var/run/docker.sock
   ```
2. **关闭 Mock Mode**：将 `SAVVY_MOCK_MODE` 更改为 **`false`**：
   ```yaml
   - SAVVY_MOCK_MODE=false
   ```
3. **保证本地或远程镜像就绪**：确保本地有 `hermes-unified:saas`（或根据 Dockerfile 预先 Build 好的 Workspace 镜像）。
4. **重启编排网络**：
   ```powershell
   docker compose down && docker compose up -d --build
   ```

### 第三步：如果仍有 502 怎么排查？
如果开启了真实容器，Nginx 依然 502，可用以下高保真命令 10 秒抓虫：
1. **检查容器网络**：确认启动的 `savvy-u1-w1` 工作空间容器是否被 Manager 自动归纳进了 Compose 内部创建的 `savvy_savvy-net` 桥接网络中。
2. **进入 Nginx 进行内部连通性测试（Ping）**：
   ```powershell
   docker compose exec nginx ping savvy-u1-w1
   ```
3. **手动模拟子请求鉴权并查看 headers 返回**：
   ```powershell
   docker compose exec nginx wget -S http://savvy-manager:8000/internal/workspace/validate -O- --header="X-Original-URI: /workspace/1/?token=xxxxx"
   ```

---

*本日志整理于 2026-06-26 晚间。所有核心代码已被完美保存并编译通过，项目已立于最终胜利的破晓前夕！*
