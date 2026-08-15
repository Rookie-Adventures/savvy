# 2026-08-15 workspace 首屏 404 + 模型切换不生效（两个都已修并端到端验证）

> 404 这条**结案了 2026-08-02 那次没解开的悬案**（见 [2026-08-02-workspace-404-mask-not-found.md](./2026-08-02-workspace-404-mask-not-found.md)）。
> 上次留痕最后停在"同一份 bundle 编译 `/workspace`、运行 `/`，矛盾根因未定位到代码点，不再深追"。这次定位到了那个代码点。

## 症状

1. 从控制台点开工作区，落地就是 SPA 内部的 404 蒙版（侧栏正常，内容区 "Page Not Found"）。
2. 聊天里切换模型，界面标签变了，但实际调用的还是老模型（用户在 new-api 使用日志里看到两次测试都是 `deepseek-v4-flash`）。

---

## 问题 1：首屏 404

### 根因

三处不一致叠出来的：

1. `hermes-workspace/vite.config.ts` 设了 `base: '/workspace/'`（commit `4158f5d7` 引入，是端口池架构之前"共享域名 + 路径前缀"那套的遗留）。它让 SSR 把**所有**入口 307 到 `/workspace/*`。
2. `tanstackStart()` 调用时**没配 basepath**，所以构建期的 `TSS_ROUTER_BASEPATH` 是 undefined。
3. TanStack Start 启动时**无条件**执行 `router.update({ basepath: process.env.TSS_ROUTER_BASEPATH })`，把 `getRouter()` 里算好的 basepath 直接覆盖成 `/`。

于是 router 拿 `/` 的规则去匹配 `/workspace/*` 的 URL → 全部失配 → 落到 splat 路由 `src/routes/$.tsx` → 404 蒙版。

**这就是上次没找到的那个代码点。** 客户端 bundle 里长这样（编译产物实证）：

```js
const sce = () => EF({ routeTree: nce, context: {}, basepath: rce(), ... });
// …Start 的 bootstrap 随后：
e.update({ basepath: {}.TSS_ROUTER_BASEPATH, serializationAdapters: t })
```

`process.env` 被替换成字面量 `{}`，`{}.TSS_ROUTER_BASEPATH` === `undefined`。所以 `rce()`（= `resolveRouterBasepath`）**算什么都白算**。

### 顺带澄清两件事

- `router.tsx` 里那套 `window.__HERMES_WORKSPACE_BASEPATH__` 运行时覆盖机制，在 TanStack Start 下**是死代码**。实测用 nginx `sub_filter` 把这个全局真的注进去了（`injected: "/workspace"`），basepath 仍然是 `/`，因为 Start 在其后覆盖。所以上次留痕里"方向 2：在 server-entry 注入该全局"这条路走不通，已否掉。
- 上次留痕记的"手动改到 `/chat/new` 能正常进"这个绕法**已经失效**：现在每个入口都被 307 到 `/workspace/*`，`/chat` 也一样 404。

### 改动

`hermes-workspace/vite.config.ts`：`base: '/workspace/'` → `base: '/'`。

选删前缀而不是去配 `tanstackStart({router:{basepath}})`，理由是这个前缀在当前架构下**本来就是多余的**：`deploy/nginx.conf` 每个实例独占一个端口，注释原话"workspace 占该端口根路径""无前缀 strip 需求"。去掉前缀等于把不该存在的东西删掉，两边自然对齐，还顺带往上游收敛（上游 base 就是 `/`）。

`server-entry.js:147` 那段 strip `/workspace` 前缀的逻辑**故意保留**：滚动期间浏览器可能还揣着旧 HTML 去要 `/workspace/assets/*`，留着它能让这类请求落到 `/assets/` 分支返回真 404，浏览器好自恢复；删了会掉进 SSR 兜底拿到 200 text/html，变成黑屏。

### 验证

用控制台真实产出的 URL（`http://127.0.0.1:41000/?token=...`，manager 生成的就是根路径）在浏览器实测：

| | 修复前 | 修复后 |
|---|---|---|
| 落地 pathname | `/workspace` | `/chat/201d05e9-…` |
| `__TSR_ROUTER__.basepath` | `/` | `/` |
| 命中路由 | `["__root__", "/$"]` | `["__root__", "/chat/$sessionKey"]` |
| 404 蒙版 | 有 | 无 |

（修复后 basepath 仍是 `/` 是**对的**：现在 URL 也在根路径，两边一致。）

---

## 问题 2：模型切换界面变了但实际没切

### 根因（两层，第二层是上游限制）

**第一层（我们的代码）**：composer 的 `handleModelSelect` 只把选择写进浏览器本地的 `session-model-store`，不发任何请求；而 `chat-screen.tsx` 组装请求时用的是 `_localModelOverride || gatewayModel`，**从没读过那个 store**（chat-screen 甚至 import 了 store 却没用——接线没接完）。所以界面读 store 显示新模型，请求发的是 session-status 返回的旧模型。

另外 `chat-screen.tsx` 的模型建议弹窗按钮 POST `/api/model-switch`——**这个路由从来没实现过**（git 全历史都没有）。请求落到 SPA catch-all 拿回 `200 text/html`，`res.ok` 为真 → 照常 dismiss 假装成功。

**第二层（上游 agent，决定性）**：把第一层接通后，`send-stream` 的请求体里确实带上了 model（实测 `model: "openai/inclusionai/ling-3.0-tiny:free"`），**但上游根本不采用**。对 gateway `:8642` 的 `/api/sessions/{id}/chat/stream` 做了三次探针：

| 传入 | `requested`（回显） | `runtime`（实际采用） |
|---|---|---|
| `NONEXISTENT-PROBE-MODEL-XYZ` | 收到 | **空** |
| `deepseek-v4-pro`（裸名） | 收到 | **空** |
| `provider:custom` + `deepseek-v4-pro`（与 config 完全一致） | 收到 | **空** |

结论：**该端点收下 provider/model 并原样回显，运行时一律不采用，永远走 config.yaml 的默认模型。** 前端传什么都没用。

唯一真生效的是写 `config.yaml`（`PATCH /api/hermes-config` + `{action:'set-default-model', providerId, modelId}`，settings 弹窗一直用的就是这条）。

### 改动

新增 `src/lib/set-default-model.ts`，两处调用方共用：

- `chat-composer.tsx` `handleModelSelect`：乐观更新标签 + 写 config，失败回滚标签并 toast 报错。
- `chat-screen.tsx` `handleSwitchModel`（建议弹窗）：同上，不再打那个不存在的 `/api/model-switch`。

两个关键设计点：

1. **`providerId` 沿用 config 现值，不按 UI 分组名改。** UI 里 `deepseek-v4-pro` 归在 DEEPSEEK 分组，但本部署所有模型都经 new-api 网关代理（`provider: custom`，`base_url: http://new-api:3000/v1`）。`applySetDefaultModel` 会把 `config.provider` 一起写掉，按分组名改会脱离网关直接把调用打挂。
2. **helper 里断言响应真是 JSON**，不只看 `res.ok`。路由缺失时 SPA 兜底返回 200 text/html，只看 `res.ok` 就会重演"假装成功"——这正是 `/api/model-switch` 骗了人这么久的原因。

产品取舍：用户拍板选"写全局默认"。代码原注释担心"会改掉所有渠道的全局默认"，但本部署一个用户独占一个容器，全局默认实际就是"这个用户自己的模型"，该顾虑在 SaaS 场景不成立。

### 验证

`src/lib/set-default-model.test.ts` 4 条全过，其中一条专门锁住"200 text/html 必须抛错"这个回归点。

端到端（用户发现问题时的同一个证据源，new-api 使用日志）：

```
 2026-08-15 14:21:44+00 | deepseek-v4-pro     ← UI 里选了 pro 之后
 2026-08-15 14:12:31+00 | deepseek-v4-flash
 2026-08-15 14:12:11+00 | deepseek-v4-flash   ← 修复前，选什么都是 flash
```

UI 选 `deepseek-v4-pro` → `config.yaml` 的 `default` 变成 `deepseek-v4-pro` 且 `provider` 保持 `custom` → new-api 实际收到 `deepseek-v4-pro`。

---

---

## 顺手修的：模型选择器里混进了选不了的模型

修完切换后查证"上游有什么模型工作区会不会自动同步"，顺带挖出来的。

**自动同步是成立的**：`/api/models` 的 source 是 `models.json+hermes-agent+live-proxy`，其中 `live-proxy`（`fetchConfiguredLiveModels`）会从 config 的 `model.base_url` 拉活目录，也就是 new-api。实测 new-api 给该 key 暴露 5 个模型，工作区列表 6 个 —— 那 5 个全在。

**但多出来的第 6 个 `hermes-agent` 是选不了的**：它来自 `fetchClaudeModels()`（agent 自带目录），不在网关里。实测：

```
POST /v1/chat/completions {model: "hermes-agent"} → 503
{"code":"model_not_found","message":"No available channel for model hermes-agent under group default"}
```

而写 config.yaml 不校验模型存不存在，所以选中它切换那步照样"成功"、toast 不报错，**等发消息才 503** —— 又是一次"显示切了实际用不了"，只是失败点后移。

### 改动

`src/routes/api/models.ts`：拿到 live-proxy 活目录后按 id 收敛一次，只保留网关目录里有的。

三个约束：

1. **按 id 判、不按 `source` 标记判**。`deepseek-v4-flash` 是 config 的 default，被 unshift 到列表最前面时不带 `source: 'live-proxy'` 标记，但它确实在 new-api 里 —— 按标记过滤会把当前默认模型误杀。
2. **只在活目录非空时收敛**。拉取失败就退回原行为，免得把整个选择器清空。
3. **放在本地发现（ollama 等）合并之前**。那些是直连本机不经网关的，不能被网关目录裁掉（桌面版场景）。
   另外当前默认模型无条件保留，否则界面连自己正在用的模型都显示不出来。

### 验证

```
修复前 count=6  [deepseek-v4-flash, hermes-agent, cohere/north-mini-code:free,
                 deepseek-v4-pro, inclusionai/ling-3.0-tiny:free, openai/gpt-5.6-luna]
修复后 count=5  [deepseek-v4-flash, cohere/north-mini-code:free,
                 deepseek-v4-pro, inclusionai/ling-3.0-tiny:free, openai/gpt-5.6-luna]
```

正好等于 new-api 暴露的 5 个全集，默认模型保留，`providers` 里 `hermes` 分组随之消失。

> 注：模型可见性还受 new-api 用户组权限管着（`user.group` 要和 `channel.group` 对上，否则 `/v1/models` 空），不同套餐用户看到的列表本来就不同 —— 这是设计如此，不是 bug。

---

---

## 顺手修的 2：容器被外部删掉后实例状态永久卡 RUNNING

重建容器验证时撞出来的。`docker rm` 掉容器后想走 manager 唤醒，被拒：`Cannot start from status RUNNING`。

### 根因

`stop_container` 在容器不存在（`NotFound`）时返回 `False`，而**三个调用方清一色**是"返回 True 才改状态"：

| 调用方 | 位置 | 卡住的后果 |
|---|---|---|
| scanner 免费到期睡 | `scanner.py:33` | 每分钟扫到、每次 stop 失败、status 永远 RUNNING |
| sleep | `instances.py:380` | 500 "Failed to stop container"，状态不变 |
| stop | `instances.py:395` | 同上 |

所以容器只要以 manager 之外的途径消失（手动 `rm`/`prune`、宿主重启无 restart policy、OOM 清理），DB 就永久停在 RUNNING，自己爬不出来 —— 6 个定时任务里**没有任何一个**负责把 DB 状态和 docker 实际状态对账。

对用户的后果：

1. 点开工作区 → nginx 看 status=RUNNING 放行 → proxy 到不存在的容器 → 502（nginx 的友好等待页只兜 403，兜不住 502）。*（这一环是代码推断，未实测）*
2. 想自救点启动 → `Cannot start from status RUNNING` 拒掉。***（这一环实测撞到）***

**用户没有任何自救路径，只能人工改库。**

### 改动

`docker_manager.py` `stop_container`：`NotFound` 从返回 `False` 改成返回 `True`。

语义上站得住：这个函数的契约是"确保容器不在运行"，容器都没了就是目标已达成，幂等。修在共享函数一处，三个调用方一起好 —— 比在每个调用方各加一个判断都小。

`APIError` **仍然返回 `False`**：那是真停不掉，故障信号不能跟着一起吞掉。这个区分是关键，不能图省事一律返 True。

修完后：容器意外消失 → 状态自动落回 SLEEPING → 用户点一下启动就走 create 重建。

### 验证

`tests/test_docker_manager.py` 追加 3 条，全过：NotFound 算成功、APIError 仍失败、正常 stop 成功。

---

## 限制与尾巴

- **模型切换现在是全局默认，不是每会话。** `session-model-store` 保留着做乐观 UI 更新，但"每会话不同模型"在当前上游下**做不到**。哪天上游让 `chat/stream` 真的采用 body 里的 model，才能恢复成每会话。
- `chat-screen.tsx` 发送时读 store 传 `model` 那行**保留着**（行为正确，上游暂不采用而已）。上游支持后即可自动生效。
- `src/lib/gateway-api.ts` 里的 `switchModel`（打不存在的 `/api/model-switch`）和 `setDefaultModel`（发 `{raw, reason}`，而 `LegacyPatchSchema` 只认 `{config, env}`，zod 会把未知键剥掉后返回 `{ok:true}`）**两个都是死导出且都是坏的**，本次没动（没有调用方）。要清理的话可以直接删，或改成转调 `setDefaultModelInConfig`。
- `chat-composer.tsx:352` 那个局部 `switchModel`（PATCH `/api/claude-proxy/api/config`，实测 404）同样是死代码，未清理。
- 容器里 `docker cp` 会留下旧 hash 的 chunk（cp 不删文件）。正式镜像重建后干净，不影响运行（新 HTML 只引新 hash）。
- 排查中临时给 `deploy/nginx.conf` 加过 `sub_filter` 注入实验，**已完整还原**，该文件最终无改动。

### 【待办·下个对话】`src/routes/api/__tests__/-models.test.ts` 2 条预先失败

**不是本次改动引入的** —— `git stash` 撤掉 `models.ts` 的改动重跑，照样这 2 条红，已证。文件来自上游 initial commit（`3d9de215fc`），我们从没改过。

失败的两条：

- `reads default model from CLAUDE_HOME config using YAML.parse`
- `reads nested model object syntax from config using YAML.parse`

现象：`json.models[0]` 是 `undefined`（列表为空），所以取 `.id` 炸掉。

**已排除的猜测**：原以为是 `models.ts:19` 的 `CLAUDE_HOME` 模块级常量在加载时求值、盖不住测试运行时改的 `process.env`。但测试里 `getHandler()` 做了 `vi.resetModules()` + 动态 `import('../models')`，模块会重新求值，**这个猜测不成立**。本机也没设 `HERMES_HOME`/`CLAUDE_HOME`，`.env` 里也没有。

**真实根因未定位**，下次从这里接着挖：为什么 mock 的 `fs.existsSync`/`readFileSync` 没让 `readClaudeDefaultModel()` 拿到 config。

优先级低：**纯测试问题，不影响任何线上行为**。但套件长期红着会有"狼来了"效应 —— 本次就差点误导（第一反应以为是自己改坏的，专门 stash 验证才排除），值得排期修掉。
