# 2026-09-08 企业微信客服入口配置生效但首页不显示 — 挂在死组件 Footer 上

## 症状

- 管理后台「系统设置 → 站点」新增了企业微信企业 ID / 微信客服链接两项，填好并保存。
- `docker compose build new-api` 重建、容器重启后，首页页脚看不到「在线客服」入口。
- 后台配置项本身正常，看不出报错，怀疑是构建没带上前端代码或配置没存住。

## 排查过程（逐层取证，不猜）

按数据流从后端往前端逐层验证，每层都要有实测证据：

| 层 | 取证方式 | 结果 |
| --- | --- | --- |
| 1. 配置落库 | `model/option.go` 的 `InitOptionMap` + `updateOptionMap` | 两处 `WeComCorpId` / `WeComKfUrl` 分支齐全 ✅ |
| 2. 接口下发 | `curl /api/status` | 已返回 `wecom_corp_id` / `wecom_kf_url` 且值正确 ✅ |
| 3. 镜像新鲜度 | 抓取运行时 bundle，查 `WeCom customer service URL`（本次新增的 i18n 标签） | 存在 → 镜像是最新的，**不是构建/缓存问题** ✅ |
| 4. 前端渲染 | LSP `findReferences` 查 `Footer` / `LegalLinks` 的引用 | **`Footer` 零引用** ❌ |

第 3 步很关键：它把「Docker 构建没带上代码」这个最像的嫌疑人排除掉了，避免在构建缓存上白费功夫。

## 根因

客服入口被加在 `footer.tsx` 的 `LegalLinks` 里，而 `LegalLinks` 只被同文件的 `Footer` 调用（314、371 行）。

但 `Footer` 组件**全站没有任何地方挂载**：

- `components/layout/index.ts` 导出了 `Header` / `Main` / `PageFooterPortal` 等，**没有导出 `Footer`**；
- 全仓库 `.tsx` 里搜不到 `<Footer`（`web/classic` 那个 `<Layout.Footer>` 是 antd 的，另一个主题的另一套组件）；
- 首页 `0ae91a2b74 feat(home): finesse redesign` 用 `TrustBlock` 顶掉了全局 `<Footer/>`（避免双页脚），而 `TrustBlock` 只复用了 `ProjectAttribution`，**没有复用 `LegalLinks`**。

所以链条是：客服入口 → `LegalLinks` → `Footer` → 无人渲染。配置一路正确下发到浏览器，但渲染点是死的。影响面不止首页——default 主题**任何页面**都不会出现这个入口，用户协议 / 隐私政策链接同理。

`footer.tsx:82` 的注释其实早就写明了意图（"Exported so the home page TrustBlock can compose the same legal row"），只是这根线一直没接上。

### 顺带发现的第二个 bug

同一个 commit 里给 items 类型加了 `external?: boolean` 并写了 `<a target="_blank">` 分支，但 push 客服项时**漏了 `external: true`**。即使渲染点接对了，绝对外链也会被 react-router 的 `<Link to="https://...">` 接管，点击变成站内路由 404。属于「修好第一个才会撞上第二个」的潜伏 bug。

## 改动

1. `features/home/components/sections/trust-block.tsx` — 按注释里既有的意图把 `LegalLinks` 接进首页页脚右列，与 `ProjectAttribution` 同处一个 flex 行（复刻 `Footer` 里的排布顺序）。
2. `components/layout/components/footer.tsx` — 客服项补 `external: true`；顺手把 `status as Record<string, unknown> | null` 的绕路强转简化为直接取值（`SystemStatus` 已有 `[key: string]: unknown` 索引签名）。
3. 同步更新 `TrustBlock` 的 ponytail 注释：原文写着"Contact + payment copy deliberately omitted (no-contact)"，现在客服入口是明确要展示的，注释留着会和代码矛盾。

后端一行没动——那部分本来就是对的。

## 验证

- `npm run typecheck`（`tsgo -b`）→ EXITCODE=0。
- `docker compose build new-api` → 成功；`docker compose up -d new-api` → `/api/status` 200。
- 浏览器实测首页页脚：「在线客服」出现在右列合规区、版权行上方；
  `<a href="https://work.weixin.qq.com/kfid/kfc9a0826599eedd605" target="_blank" rel="noopener noreferrer">` —— 绝对外链、新标签打开，未被降级成站内路由。
- 用户协议 / 隐私政策未显示，符合预期（后台未配置，`LegalLinks` 按项自隐）。

## 限制

- 只修了首页。default 主题其它公开页面（`PublicLayout`）本身不含任何页脚，那些页面依旧没有客服入口——这是既有状况，本次未扩大范围。
- 未处理小程序侧拉起客服会话，本次只覆盖官网展示。

## 后续（2026-09-09）：入口形态改为右下角悬浮球

上一节把 `LegalLinks` 接进页脚后，实测发现客服文字链孤零零占一行、切断了页脚 colophon，观感差。经与用户确认，入口形态整体改为**右下角悬浮球**，页脚不再承担客服入口：

- 新增 `components/wecom-contact-fab.tsx`（`WeComContactFab`），挂 `routes/__root.tsx` 的 `<AgentWidget />` 旁边；仅当 `status.wecom_kf_url` 有值才渲染。
- 交互：桌面 hover 在左侧浮出二维码卡（`qrcode.react` 的 `QRCodeSVG` 拿 kf url **现场生成**，`pointer-events-none` 纯展示，避免 hover 间隙闪断）；click 新标签打开客服链接（微信内直接进会话，是移动端主路径，hover 只是桌面增强）。
- 二维码不放静态图：静态图不跟后台配置走，改链接就过期；现场生成永远同步。
- 堆叠让位（最终顺序，经用户反馈调整）：客服球占最角落 `bottom-4`，机器人球堆其上 `bottom-20`（未配置客服时落回 `bottom-4`），聊天面板 `bottom-36`。这样面板打开时紧贴自己的触发按钮，中间不夹客服球（初版把客服球放机器人上方，面板一开就在面板与机器人之间夹一个企微头像，观感割裂）。各一个三元，不做跨组件共享状态。
- 二维码卡文案：「微信扫码联系企业客服」（六语同步）。
- 打磨（用户反馈）：两球统一用单一 `size-12`——机器人球原为 `size='icon'`(size-8) 叠加 `h-12 w-12`，跨冲突组合并结果不稳定导致两球大小不一；二维码卡从垂直居中改 `bottom-0` 底对齐——球贴最角落时居中会让卡片下半截伸出视口被切。实测两球均 48×48、卡片 bottom(873) ≤ innerHeight(889)。
- 打磨（用户反馈 2）：聊天面板展开时二维码卡原本压在面板上，改为展开在面板**左侧并排**（`fixed` 到面板左缘、底边对齐面板底）；面板收起时贴回悬浮球左侧。面板开关是瞬态 UI 状态，由 `__root` 持普通 useState 经 props 下发（`AgentWidget onOpenChange` → `WeComContactFab agentPanelOpen`），不上全局 store。实测面板展开时 card.right(1462) ≤ panel.left(1474) 不重叠。
- 打磨（用户反馈 3）：聊天面板本身从机器人球**上方**改到**左侧并排**（sm+ `right-[4.75rem] bottom-4`，宽 `min(420px,100vw-6.75rem)`；移动端窄屏仍堆上方免聊天窗压成细条）；二维码卡避让偏移随之改为 `right-[calc(5.5rem+min(420px,100vw-6.75rem))]`。实测 panel.right(1834) ≤ robot.left(1846)、panel.bottom(873) 贴底、card.right(1402) ≤ panel.left(1414)。
- 验证陷阱：本轮首次浏览器实测读到**旧缓存 bundle** 得出“面板仍在上方”的错误结论；抓运行时 bundle 确认新类名（`5.5rem`/`max-sm:right-4`）已在线、旧 `1.75rem` 已消失后，清缓存重测才全绿。UI 验证前必须先确认 bundle 新鲜度。
- 头像素材：用户提供的官方 `assets/logos/wecom-avatar.png` 复制到 `public/wecom-avatar.png`（public/ 原样服务、不进 bundle）。
- 页脚 `LegalLinks` 撤掉客服项及只为它存在的 `external` 分支，回归只渲染用户协议 / 隐私政策；仍由首页 `TrustBlock` 组合挂载。

验证：typecheck 0 错；docker 重建后浏览器实测 7 项全过（双球堆叠不重叠、hover 出 140×140 QR、`<a>` 带 target/rel、聊天面板不遮挡客服球、页脚已无「在线客服」、移开鼠标卡片消失）。

## 尾巴

- `Footer`（footer.tsx:191，约 190 行）是完整死代码，含 `customPageColumns` / `fallbackColumns` 等逻辑，建议单独一次清理删掉，别混在本次修复里。
- 项目里已有 `npm run knip`（死代码检测）。这类「功能加在没人挂载的组件上」的坑，跑一次 knip 就能提前暴露，值得进 CI 或至少进合并前检查。
- 若后续要让全站公开页都有客服入口，正解是把页脚挂回 `PublicLayout`，而不是在各页面各自拼一遍。
