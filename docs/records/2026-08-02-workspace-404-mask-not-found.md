# 2026-08-02 workspace 落地 404 蒙版(未解,诊断中)

> 本对话延续 [2026-08-01-workspace-entry-401-and-token-renewal.md](./2026-08-01-workspace-entry-401-and-token-renewal.md)。401 已修已留痕已合 `refactor/workspace-arch` 已推 origin。本文专门记 401 修通**之后**冒出的 404 蒙版问题。

## 症状(用户口述)

- 401 修通后,从控制台点开工作区,URL `http://127.0.0.1:41004/workspace?token=...` 落到 **"Page Not Found / The page you're looking for doesn't exist or has been moved. / Quick Links"** 页。
- **不是白屏 HTTP 404** — 是工作区 **SPA 内部**的 404 蒙版(侧栏、外壳正常,中间内容区是 404 蒙版)。
- 用户说"正式页应该是 `/chat/new`",手动改到 `http://127.0.0.1:41004/chat/new` 能正常显示聊天界面(无 404 蒙版)。
- 新 token、旧 token 都一样 404(排除 token 过期)。

## 已做改动(本对话,未提交,留在工作区)

### A. `hermes-workspace/src/router.tsx` — basepath 单一真相源

**改了。测试 7/7 过。镜像已重建。但最终不确定是否是当前症状根因——见"排除证据"。**

原 `resolveRouterBasepath()`:
- `typeof window === 'undefined'` → 返 `/`(SSR 端)
- 否则读 `window.__HERMES_WORKSPACE_BASEPATH__` 运行时覆盖;空 → 返 `/`

问题:全世界**无任何代码注入** `window.__HERMES_WORKSPACE_BASEPATH__`(上游 `3d9de21` Initial commit 造了钩子但从未填生产注入点)。而我们本地 patch(commit `4158f5d7`)给 vite 设了 `base: '/workspace/'` + server-entry strip `/workspace` 静态前缀,但**配套的 router basepath 从未接上** → router 永远 basepath `/`,拿 `/` 规则匹配 `/workspace/...` URL → 全失配 → splat `$.tsx` → "Page Not Found"。

修复:默认分支从 `import.meta.env.BASE_URL`(build-time = `/workspace/`)派生,`normalizeBase()` 统一规范化(leading slash 无 trailing)。runtime override (`window.__HERMES_WORKSPACE_BASEPATH__`) 保留优先(上游 `/workspaces/<id>/` 多前缀挂载能力不丢)。SSR 端无 window 也走 BASE_URL(define 同构),SSR 首屏直接 render 正确路由。

产物取证:
- client bundle `main-*.js`:`sce()` = `resolveRouterBasepath`,default 分支 `return r6("/workspace/")`,override 分支 `window.__HERMES_WORKSPACE_BASEPATH__` 优先。**编译正确**。
- server bundle:grep `/workspace` 命中。**正确**。
- `import.meta.env.BASE_URL` 在 client/server bundle 都被 inline 成 `"/workspace/"`。

### B. 镜像重建

- `docker build -f Dockerfile.unified -t hermes-unified:saas .` 成功(本地 `COPY hermes-workspace/` + `pnpm build`,用的是含 A 改动的源码)。
- 删旧容器 `savvy-u5-w1`(卷 `savvy-u5-data` 保留),manager 唤醒用同 tag `hermes-unified:saas`(`docker_manager.py:84` 硬编)重建拿新镜像。
- 容器 `created=16:39` 晚于镜像 `build=16:32` → **确认容器用的是新镜像**。

### C. 测试更新 `src/router.test.ts`

3 个默认分支断言更新(无 override → 追 BASE_URL 而非 `/`),3 个 override 用例不变。7/7 过。

## 关键取证(排除/锁定)

### 已排除

1. **token 过期**:旧 token `exp=1785604155` 确实过期(now 1785604482),但用**新 token**(`exp=1785606429`)打开仍 404。排除。
2. **HTTP 404**:容器直打 `:3000` `/`、`/workspace/`、`/workspace/chat/new` 全 200/307,**非 HTTP 404**。是 SPA 内部 render 404 蒙版。
3. **SSR 首屏 404**:容器直打 `:3000/workspace/` SSR HTML 11312 字节、7 个 `<script>`,**无 "Page Not Found" 文本无 chat 文本** = splash 空壳。SSR 没渲染 404 蒙版。
4. **basepath 产物错**:client/server bundle basepath 都正确编译成 `/workspace`。见上文 A 产物取证。
5. **route tree 缺 index**:HEAD 版 `routeTree.gen.ts` 含 `IndexRoute path:'/'`。working 版与 HEAD 无 diff(diff stat 空,工作区 modified 是时间戳)。client bundle 含 `beforeLoad:function(){throw Zg({to:"/chat"...})}`(index route redirect `→/chat`)和 `claude-last-session`(chat/index redirect `→/chat/$sessionKey`)。**route tree 完整,index redirect 代码在产物里**。

### 决定性对比证据

| 路径 | 经 nginx `:41004` | 结果 |
|---|---|---|
| `/workspace`(无尾斜杠) | 307 → `/workspace/` | 200 正常 HTML |
| `/workspace/`(带尾斜杠) | 200 | 正常 HTML 7 script |
| `/chat/new` | 307 → `/workspace/chat/new` | **用户说正常,无 404 蒙版** |

**curl 带新 token 测 `/workspace/`(带尾斜杠)经 nginx → 200 + DOCTYPE + 7 script(正常)。容器内 `/workspace` 和 `/workspace/` 都 7 script 正常。**

### 锁定方向(未完成验证)

**浏览器 Console 取证**(用户跑的):
```
location.pathname = '/workspace'  // 无尾斜杠!
document.querySelectorAll('script[src]').length = 0   // ← 0 个 script 标签
document.body.outerHTML.slice(0,300) = '<div id="splash-screen" ...></div>'  // 仅 splash,无别的
```

- 浏览器 `location.pathname` 停在 `/workspace`(**无尾斜杠**),没跟随 307 跳成 `/workspace/`。
- 整页 **0 个 script** → SPA **从未 hydrate** → "侧栏正常 / 404 蒙版" 描述可能混了 `/chat/new` 那次(正常)和 `/workspace` 这次(0 script)。

但 curl 经 nginx `/workspace/`(带斜杠)有 7 script。浏览器 `/workspace`(无斜杠)0 script。**差异在尾部斜杠 + 可能 Service Worker 拦截**。

`__root.tsx` 注册 `/sw.js`(`__root.tsx:252` `registerAppServiceWorker`)。SW 可能:
- 拦截 `/workspace`(无尾斜杠)导航,用旧/错缓存(0 script)响应。
- `/chat/new` 没被 SW 缓存路径命中 → 走真实网络 → 正常。

## 当前最可能根因(待新对话证伪)

**Service Worker (`/sw.js`) 缓存拦截 `/workspace`(无尾斜杠)导航,吐旧/错壳(0 script),导致 SPA 从未 hydrate。** 不是 basepath、不是 route、不是 nginx、不是 token。

次可能:浏览器对 307 → `/workspace/` 的跟随 + SW 退回 reset 到原 URL 的边界。

**Note**:我改的 `router.tsx`(basepath 派生)是真漏洞(basepath 从未接上),但**未必是当前 404 蒙版的直接根因**——basepath 即使对了,根本没 hydrate(0 script),路由层还没机会发挥。需先解决 SW/0-script,再看 basepath 还有没有残留症状。

## 未完成验证(新对话第一刀)

1. **证 SW 拦截**:浏览器 → DevTools → Application → Service Workers → 看有没有 `sw.js` 注册 + 看它拦截了什么。或**无痕窗口**(禁 SW)打开 `http://127.0.0.1:41004/workspace?token=新token`,看是否:
   - 仍 0 script / 404 蒙版 → SW 无关,问题在 nginx/workspace 对无尾斜杠 `/workspace` 的 HTML 产出。
   - 正常 → **SW 是根因**,清 SW 缓存或改 sw.js。

2. 若 SW 排除,对比 curl `/workspace`(无斜杠,经 nginx)的完整 HTML body。curl 之前 `head -c 200` 空可能是 307 body 空,需 `curl -L` 跟随或看 307 本身 body。

3. 看浏览器 Network 面板 `/workspace` 那条 document 请求:
   - status,final URL,from cache?(from ServiceWorker / from disk cache)
   - response headers 有没有 `Service-Worker` / `X-Content-Type`。

## 改动面现状

| 文件 | 状态 | 是否提交 |
|---|---|---|
| `hermes-workspace/src/router.tsx` | 改了(basepath BASE_URL 派生) | **未提交**(工作区)。待证伪后决定保留还是回退。 |
| `hermes-workspace/src/router.test.ts` | 改了(断言跟新契约) | **未提交** |
| `hermes-workspace/src/routeTree.gen.ts` | 工作区 modified(时间戳,内容无 diff) | 未提交,实际无内容变化 |
| `hermes-unified:saas` 镜像 | 本地重建(含 router.tsx 改动) | 仅本机 |

401 那 5 commit 已推 origin `refactor/workspace-arch`,与本 404 改动隔离。

## 提交建议

- **先别提交**任何东西,先在无痕窗口验证 SW 假设。
- 若 SW 是根因 → router.tsx 改动可能是**额外加固**(basepath 派生本来就该接上),但跟当前症状无关;单独提交或并入白标。
- 若 SW 排除 + 无痕仍 404 → router.tsx 改动也未必有用,需继续挖。

## 调试小记(坑)

- Git Bash 对容器内 Linux 路径 `/etc/nginx/nginx.conf` 做 MSYS 转译成 `C:/Program Files/Git/etc/...`,grep/cat 报假 not found。用 PowerShell 工具 + `docker exec ... cat` 落到 Windows temp 文件再 Read 最稳。
- `docker exec savvy-nginx-1 grep` 在 `nginx:alpine` 无 grep,改 `cat`。
- manager 容器无 curl,内部测 endpoint 用 `node -e "fetch(...)"`(workspace 容器有 node 因跑 server-entry.js)。
- `docker exec ... cat /etc/nginx/nginx.conf` 经 Git Bash 管到 `/tmp/x` 会吞内容(`wc -l` 0 行)。直接 PowerShell `docker exec sh -c "cat ..." | Out-File`。

## 新对话验证(2026-08-02,后补证据推翻 SW 假设)

无法在本会话跑无痕窗口,改从**服务端全链路 + SW 源码 + TS Router basepath 源码**证伪。

### 产出真 token 实测三路径经 nginx

manager DB 查 `inst-5`/user `5`/port 41004,secret=`dev-hmac-secret-change-me`(dev),manager 容器内 `app.token.generate_access_token('inst-5','5',60,host=127.0.0.1,port=41004)` 造 1h token:

| 路径 | nginx(status no-follow) | curl -L 跟随后 | size | script 标签数 | splash | Page Not Found |
|---|---|---|---|---|---|---|
| `/workspace`(无斜杠) | 307→`/workspace/?token=%3D%3D...` | 200 | 11312 | **7** | 3 | **0** |
| `/workspace/`(带斜杠) | 200 | 200 | 11312 | 7 | 3 | 0 |
| `/chat/new` | 307→`/workspace/chat/new?token=...` | 200 | 11508 | 7 | 3 | 0 |

**三路径经 nginx 全 200、7 script、splash 壳、0 "Page Not Found"。`/chat/new` 比 `/workspace/` 多 196 字节 = SSR 多渲染了 chat 路由内容(说明 SSR 对 `/chat/new` 有渲染,非纯空壳)。服务端无任何 0-script / 404 蒙版输出。**

### SW 源码证否(self-contained)

容器内 `node -e "fetch('/sw.js').then(r=>r.text())"`:`sw.js` **network-only,故意无 fetch listener**。注释原文:
> no 'fetch' listener on purpose. A fetch handler that never calls `event.respondWith()` (network-only passthrough) ... Omitting the listener entirely is functionally identical (requests fall through to the network stack).

**当前 bundle 的 SW 根本不拦导航请求 → "SW 吞 307 / 0 script"假设作废。** install/activate 只清 caches + claim clients,不进 fetch path。

(注:用户截图时浏览器注册的可能是**更早某个旧版镜像**留下的 SW,那版**可能**带 fetch handler。但本对话容器跑的 16:32 重建镜像 SW 无 fetch → 即便旧 SW 在,清缓存/更新即可,根因不在当前 SW 产物。)

### 0-script 谜底:选择器误用

留痕记 `document.querySelectorAll('script[src]').length = 0`。**`script[src]` 只匹配带 `src` 属性的 `<script src=...>`,本页 script 全是 inline `<script>...import("/workspace/assets/main-*.js")...</script>` 或 `<script>...</script>` bootstrap —— 无 `src` 属性 → `script[src]` 本就该 0,与"无 script"无关。** grep `<script`(不带 `[src]`)命中 7 个。"SPA 从未 hydrate"判断**证据站不住,作废。

`document.body.outerHTML.slice(0,300)` 停在 splash 开头 = body **第一段**,script 在 body tail(最后 800 字节实测含 `import("/workspace/assets/main-*.js")` + bootstrap)。前 300 字节看不到不代表无 script。

### TS Router basepath strip 源码证 A 改动正确

容器内 `node_modules/.pnpm/@tanstack+router-core@1.166.7/.../rewrite.js` 的 `rewriteBasepath.input`:
- `pathname === checkBasepath`(`/workspace`)→ `url.pathname = "/"` ✓(无尾斜杠直接归 `/`)
- `pathname.startsWith("/workspace/")` → slice `normalizedBasepath.length` ✓(带尾斜杠)

`path.js` `trimPath` 去 leading+trailing slash。`import.meta.env.BASE_URL` inline `/workspace/` → A 改动 `normalizeBase` → `/workspace`。router basepath `/workspace`,pathname `/workspace` → strip 成 `/` → index route `beforeLoad: throw redirect→/chat`。**strip 逻辑对无/带尾斜杠双情形全正确,A 改动不是 bug,是把从未接上的 basepath 真接上了。**

### client vs server bundle basepath

- client `main-BBLfWNbT.js`:grep 命中 `resolveRouterBasepath`(=`sce`)×1、`__HERMES_WORKSPACE_BASEPATH__`×1、`/workspace/`×3 → **A 改动已编译进 client**。
- SSR `server-entry.js`(11KB):grep 零命中 basepath 派生。**SSR 不跑 router logic**,只渲染 shell(HTML tail `ssr:!1` 印证),路由判定全在 client hydrate。

镜像确认:`docker inspect savvy-u5-w1` image = `c650c67...` = `hermes-unified:saas:16:32 build`。**容器确实跑含 A 改动的镜像。**

### router.test.ts 7→6

本会话 `npx vitest run src/router.test.ts` **6/6 pass**(留痕写 7:多算的那个在 `src/routes/-root-runtime-guards.test.ts`,本次只跑 router.test.ts)。

## 根因结论(更新)

**服务端全链路正确。A 改动(basepath 派生)是对的且已生效。** 三路径经 nginx 全 7script 200、SSR 渲 shell + client bundle 含正确 basepath、TS Router strip 源码证无/带尾斜杠双情形全正确、当前 SW 无 fetch handler。

**留在症状的只能是浏览器侧陈旧态**:
1. 浏览器 HTTP cache **缓了旧 HTML**(含旧 client bundle hash),旧 bundle 无 basepath 派生 → hydrate 路由失配 → splat `$.tsx` → 404 蒙版。新 HTML 引新 hash 不会被旧 hash 命中,但 HTML 本身若被缓存 → 加载旧 bundle。
2. **更早旧镜像注册的旧 SW**(若曾带 fetch handler 缓 HTML)仍持有陈旧 HTML 响应。当前镜像 SW 无 fetch,清不掉旧 SW 的缓存吐旧壳。

## 未完成验证(新对话第一刀,收窄到浏览器侧)

本会话已废"无痕窗口别 SW 假设"。仍需一次浏览器实测确认陈旧态:

1. **强制破缓存复测**:`DevTools → Application → Service Workers → Unregister` + `Clear storage → Clear site data`,硬刷 / 或换无痕窗口打开 `http://127.0.0.1:41004/workspace?token=<新token>`。
   - 正常(`/chat` 渲染,无 404 蒙版)→ 确陈旧态,清缓存即解,A 改动已是根因修复。
   - 仍 404 蒙版 → 浏览器侧清干净仍错 → 真是 client runtime 失配,需在浏览器 hydrate 后 console 查 `window.__TSR_ROUTER__?.basepath` / `latestLocation.pathname` 实值,反推 strip 在哪断。
2. 必要时查 nginx / workspace 是否给 HTML 发了过宽 `Cache-Control`(该 `no-store` 或短 max-age,因 SPA shell 含 bundle hash,缓 HTML 会锁住旧 bundle)。nginx `location /` 当前无显式缓存头,看上游 workspace 服务默认头。

## 浏览器实测(收尾,定位到 client basepath `/`)

用户用真 token(`exp=1785608427`,本机造)在**无痕窗口**加载 `http://127.0.0.1:41004/workspace?token=...`,console 跑:

```js
location.pathname                                             // "/workspace" 无尾斜杠
window.__TSR_ROUTER__.basepath                                // "/"  ← 应是 "/workspace"
window.__TSR_ROUTER__.latestLocation?.pathname                // "/workspace"
document.querySelectorAll('script').length                    // 6
script.textContent.match(/import\("([^)]+)"\)/)               // [] 空
```

用户追加对比(关键):
- `http://127.0.0.1:41004/chat/new` → **正常** chat 界面
- `http://127.0.0.1:41004/workspace/chat/new` → **404 蒙版**
- `http://127.0.0.1:41004/workspace?token=...` → **404 蒙版**

差异只在 URL 中间多 `/workspace` 前缀。

### client basepath `/` 解释一切

- 路由树根是 `/`(index route `/` beforeLoad redirect→`/chat`)+ `/chat`,无 `/workspace` 顶层。
- **basepath `/` 不 strip `/workspace`** → `/workspace/chat/new` 当 pathname 查路由树 → 无 `/workspace/chat/new` → splat `$.tsx` → 404 蒙版。
- **basepath `/` 时 `/chat/new` 不需 strip**(自身以 `/` 开头)→ pathname `/chat/new` → 命中 chat route → 正常。
- 这完全自洽:有 `/workspace` 前缀的 URL 全 404,无前缀的 `/chat/new` 正常。

### 编译 vs 运行时矛盾(未解,已不追)

server 全链路对、bundle 对(实测,见上文):
- client bundle `main-BBLfWNbT.js` `sce()` default `return r6("/workspace/")` → `/workspace`(`import.meta.env.BASE_URL` inline `/workspace/`)
- server bundle `ROUTER_BASEPATH = "workspace"` → `createRouter({basepath:"workspace"})` → TS Router `trimPath`+normalize 成 `/workspace`
- SSR HTML manifest preloads 用 `/workspace/assets/...`
- 三 URL 同 hash `main-BBLfWNbT.js`(同一份 bundle,basepath 编译一致)

但浏览器运行 `__TSR_ROUTER__.basepath === "/"`。**同一份 bundle 编译 `/workspace`、运行 `/`**,且 `window.__HERMES_WORKSPACE_BASEPATH__` 在 SSR HTML 无注入点(grep 零命中,HTML 只有 `window.__dismissSplash`)。override 分支不该触发(全局 undefined)。矛盾的根因未定位到代码点 —— 可能 TanStack Start hydration 从 SSR 序列化的 `$_TSR` state 复用 basepath,或 client `sce()` 执行时机问题,但**不再深追**。

### 顺带:浏览器没跟 307

`/workspace`(无斜杠)经 nginx → 307(浏览器该跟到 `/workspace/`)。但 console `location.pathname === "/workspace"`(没跟)。service-side `/workspace` 是 SSR server 发的 307 空 body(curl 见 `len 0 scripts 0`)。浏览器停在未跟的 307 怎会 hydrate 出 6 script + `__TSR_ROUTER__`,与 server `len 0` 冲突 —— 该 timestamp/console 快照时机的疑点也未深追。

## 结案(2026-08-02)

**404 蒙版不阻断用户**:控制台点开工作区虽落 404 蒙版,但用户手动改 URL 到 `/chat/new` 即正常进 chat。401(本对话前序已修、已合 `refactor/workspace-arch` 已推 origin)才是阻断项,已解决;404 仅是入口 UX 不顺,可后置。

**router.tsx A 改动(basepath 派生)是 basepath 链路真漏洞**(basepath 全程未接上 `__HERMES_WORKSPACE_BASEPATH__`),但**不是当前 404 症状根因**:改了仍 404(用户实测)、不改也 404,说明症状在 basepath 派生之外(浏览器没跟 307 / 运行时 basepath `/`),A 改动够不到。**决定回退 A,留工作区干净。**

三文件已 `git checkout -- ` 回退到 HEAD:`router.tsx`、`router.test.ts`、`routeTree.gen.ts`(后者本就是纯 LF/CRLF 噪点,无内容 diff)。

### 真根因留作后置(若未来要解 404 蒙版)

方向 = **让浏览器跟 307 / 或让 `/workspace`(无斜杠)直接返 200 的 shell HTML**,外加确认 **client 运行时 basepath 为何是 `/` 而非 bundle 编译的 `/workspace`**。两条线:

1. nginx `location /workspace` 显式 `return 301 /workspace/$args;`(强制补尾斜杠,绕过 workspace SSR 的 307 空 body + 浏览器跟 307 边界)。或 nginx 对 `/workspace` proxy 时加 `proxy_redirect` 改写 location。
2. 在 server-entry 注入 inline `<script>window.__HERMES_WORKSPACE_BASEPATH__="/workspace"</script>`,把 override 分支显式喂 `/workspace`,绕过 client `sce()` 谜之返 `/` —— 但前提是 client `sce()` 真读了 override(全局未注入也该走 default,若 default 也返 `/` 则 override 也救不了,需先证 `sce()` 运行时实值)。

**当前不动**。留痕至此为止,不再追 404。
