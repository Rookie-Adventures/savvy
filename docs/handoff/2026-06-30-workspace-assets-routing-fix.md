# 2026-06-30 联调与网络编排：单页应用（SPA）子路径资产路由问题修复文档

## 1. 🌟 问题概述
在端到端联调测试中，浏览器通过 Nginx 子请求发送鉴权可完美获得 `200 OK`，但在访问工作空间 `/workspace/1` 时出现以下异常：
- **静态资源 `404 (Not Found)`**：无法加载 `/assets/main-xxxx.js`、`styles-xxxx.css` 等核心资源。
- **页面黑屏**：由于上述核心 JS/CSS 包加载失败，导致单页应用（SPA）无法运行，页面呈黑屏状态。
- **动态资源加载返回 HTML**：请求 `.js` 文件时，服务端返回了 HTML（单页应用的通用 Shell），导致浏览器抛出 `Uncaught (in promise) TypeError: Failed to fetch dynamically imported module` 错误。

---

## 2. 🔍 根本原因与诊断

### 原因一：Vite 静态资源绝对路径问题
默认情况下，Vite 编译产物的静态资源在 HTML 中都是 `/assets/main.js` 等绝对路径。
当浏览器请求 `/workspace/1`，由于其引用了 `/assets/...`，浏览器转而请求 `http://localhost/assets/...`。
Nginx 规则将 `/assets/` 拦截并直接转发至 Go 后端（`new-api`），由于 `new-api` 并不存放工作空间的静态文件，导致 `404`。

### 原因二：Nginx Token 状态保持缺失
在首屏访问 `/workspace/1/?token=xxx` 后，浏览器加载静态资产时不再携带 `?token=...` 查询参数。
Nginx 拦截这些属于 `/workspace/` 下的资产请求，但由于无法获取 Token，导致向 Manager 鉴权时报错 `401 Unauthorized`。

### 原因三：工作空间容器静态服务路径映射不一致
当 Vite 使用子路径编译（即 assets 在 `/workspace/assets/` 下）后，容器内 `server-entry.js` 无法识别带有 `/workspace` 路径的静态资源，从而无法映射到实际的磁盘文件（`/app/dist/client/assets/`），直接漏斗到 TanStack Start 的 Catch-All 服务端渲染（SSR）处理器，返回了 HTML Shell，而非 JavaScript 文件内容。

---

## 3. 🛠️ 解决方案与具体修复

### 步骤 1：配置 Vite 静态资源 Base 路径
修改 `hermes-workspace/vite.config.ts`，为 Vite 编译器配置 **`base: '/workspace/'`**。
这样在打包构建时，HTML 对资源的引用将自动变为 `/workspace/assets/...`。

### 步骤 2：添加 Nginx 状态保持机制
修改 `deploy/nginx.conf`，在 `location /workspace/` 段落中，增加 Token 存储机制：
当检测到 URL 查询参数中有 `token` 时，自动将其保存至浏览器的 HttpOnly Cookie 中：
```nginx
if ($arg_token != "") {
    add_header Set-Cookie "workspace_token=$arg_token; Path=/workspace/; HttpOnly; SameSite=Lax";
}
```
使后续所有资产请求（自动继承 `/workspace/` 路径）能够自动携带此 Cookie，过检 Nginx 拦截。

### 步骤 3：修复工作空间静态文件服务器
修改 `hermes-workspace/server-entry.js`，在 `tryServeStatic` 中，若检测到请求的 `pathname` 以 `/workspace` 开头，**强韧剥离前缀**，使其完美、无缝对应到容器内的物理磁盘路径 `/app/dist/client/assets/...`：
```javascript
let pathname = decodeURIComponent(url.pathname)
if (pathname.startsWith('/workspace')) {
  pathname = pathname.substring(10) || '/'
}
```

---

## 4. 📈 验证结果
重新构建 `hermes-unified:saas` 镜像，并移除重建旧容器，执行端到端访问：
- **静态资源响应**：`/workspace/assets/main-xxxx.js` 返回 **`200 OK`**。
- **文件类型正确**：`Content-Type` 识别为 **`application/javascript`**，成功回传 **2.4MB** 的核心运行代码。
- **测试通过**：首屏成功渲染，工作空间加载时由于网络编排机制彻底打通，黑屏与错位完全消失！
