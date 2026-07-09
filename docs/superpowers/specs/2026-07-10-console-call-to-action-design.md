# 工作区控制台末尾 升档+引导区 设计

日期：2026-07-10
分支：dev
状态：已与用户逐节确认，待写实施计划

## 1. 背景与问题

Hermes Cloud Workspace 的工作区控制台（`hermes-workspace/src/screens/dashboard/dashboard-screen.tsx`）登录后是纯运维仪表盘：Header → OpsStrip → HeroMetrics → 图表行 → 会话/实例列表 + 侧轨卡片。现有底栏 `system-metrics-footer.tsx` 只有 CPU/RAM/Disk/Hermes 状态。

**问题**：当前的产品卖点和套餐分档在控制台里一条都没有。登录用户（多数是 FREE 档、还没付费）看不到「为什么要升级」「我这一档够不够」「第一次怎么用」。卖点散落在首页落地页，控制台零变现/卖点内容。

## 2. 目标

在控制台主内容流末尾加一个商业区块，让登录的 FREE 用户：
1. 知道**第一次怎么用**（首次唤醒专属工作区要先生成 API 密钥填进去——新手最高门槛，目前无说明）。
2. 知道**为什么用云端、不在自己电脑上搭**（升档安全感）。
3. 看到**三档对比和能升到哪**，FREE 用户一眼看到升一档多多少。

## 3. 不做（YAGNI / 口径红线）

- **不放备案尾块**。控制台非首页，信任尾块只留 scheng.net 首页一处。
- **不喊 API Gateway**。全程用「工作区」包装。
- **不说「不绑卡 / 无信用卡」**。国内用户，不套海外话术。改说「免费开箱即用」。
- **不出现任何「模型」维度的卖词**。禁止「真模型不偷换」「按上游原样转发」「不降级」「不配模型」「模型已接」等。整组模型轴的卖词全部删除/换词，避免让读者意识到「模型」是可被摆弄的产品维度。
- **不带价格**。价格是 new-api 的 `SubscriptionPlan` DB 表驱动、运营后台可变，控制台里照搬易被误解为「发价单」且易过期。三档卡只列存储 + 一句差异化卖点 + 升档 CTA。
- **不列技术参数**。不写 CPU quota / mem_limit / pids_limit，玩家不关心。
- **不进布局编辑模式的可见性开关**。这是商业区块不是可隐藏 widget，始终渲染。

## 4. 区块定位

- **区块名**（英文源码标识）：`ConsoleCallToAction`，中文渲染。
- **位置**：`dashboard-screen.tsx` 主内容行的**最末尾**——在会话列表 `SessionsIntelligenceCard` 与右侧轨卡片之后、`SystemMetricsFooter` 之前。
- **渲染方式**：页面主内容流的一部分，跟随滚动，**不悬浮、不固定**。用户把仪表盘滚到底才看到。
- **与运维底栏关系**：完全不碰 `system-metrics-footer.tsx`，底栏照旧 CPU/RAM/Hermes。新区块在它上方、在页面流里。

## 5. 区块内部顺序（从上到下）

1. **「第一次用？」引导 + 步骤条（3 步）** — 怎么用
2. **升档引导文（为什么云端 vs 本地）** — 给 FREE 的安全感
3. **三档卡 FREE / STARTER / PRO（含升档 CTA）** — 能升到哪

## 6. 组件 1：使用步骤条

新手门槛：首次启动要先有 API 密钥填进去才能唤醒专属工作区。放三档卡上方（先教怎么用，再说能升到哪）。

**引导**（步骤条上方一行）：「第一次用？三步唤醒你的专属工作区，免费开箱即用。」

**三步**：

| 步 | 标题 | 说明 | 热链 |
|---|---|---|---|
| ① | 生成 API 密钥 | 在令牌页创建一把属于你的密钥 | 指向 new-api `/token` 令牌页 |
| ② | 填入工作区设置 | 把密钥粘进工作区设置，绑定专属模型通道 | 指向工作区 Settings |
| ③ | 唤醒工作区 | 密钥就位后首次启动，云端工作区开箱即用 | — |

**布局**：横向，3 步左→右编号，步骤间细箭头/连接线。每步：圆圈编号 + 标题 + 一行小字。整体压在一张薄卡，不喧宾夺主。移动端纵向。

**密钥来源已定**：new-api 令牌页生成 → 回填工作区设置，与本地项目记忆 `project-provider-key-injection` 的 B 层供应商 key 注入是两套不同机制，本步骤指向令牌页那套。

## 7. 组件 2：升档引导文

步骤条与三档卡之间的薄引导，两三行，首页 `local-vs-cloud` 同源但压缩成短语。主旨给 FREE 看的安全感。

**主句**：「为什么用云端，不在自己电脑上搭 Hermes？」

**三条**（完全去掉模型维度，已按用户红线换词）：

| 条 | 文案 |
|---|---|
| 装好即用 | 不装运行时、不 clone、不配环境 |
| 云端常在 | 云端运行，无论你在哪里，你的任务和工作都不会中断 |
| 随用随醒 | 关掉不丢，数据安全，再开还在 |

## 8. 组件 3：三档卡

**布局**：并排三张，desktop 三列等宽；移动端纵向堆叠。左→右 FREE → STARTER → PRO 递进。

**每张卡**（从上到下）：
1. 档名（FREE / STARTER / PRO 英文档名 + 配色递进：FREE 灰、STARTER 蓝、PRO 主题强调色）
2. 存储容量大字（5 GB / 20 GB / 50 GB）
3. 一句差异化卖点
4. CTA 区

**差异化卖点**（中文，给 FREE 的升档理由）：

| 档 | 存储 | 一句卖点 |
|---|---|---|
| FREE | 5 GB | 免费开箱即用，每次启动 2 小时，数据安全 |
| STARTER | 20 GB | 挂机不掉线，长期挂着跑任务 |
| PRO | 50 GB | 多工作区并行，吃满规格跑重活 |

**CTA**：
- FREE 卡：放「当前方案」静态标记（你就在这档），不放升档按钮。
- STARTER / PRO 卡：各放升档按钮「升级到 STARTER / PRO」，指向 new-api 订阅/套餐页（具体路由实现时对）。

**口径守死三条**：不喊 API Gateway；不出现海外「不绑卡」话术；FREE 卡「2 小时」是死口径（不是 3h）。

## 9. 数据来源（实现时对线）

- **三档枚举**：`savvy-manager/app/models.py:19-22` `PlanType(FREE/STARTER/PRO)`，仅此三档，无 TEAM/ENTERPRISE。
- **规格映射**：`savvy-manager/app/docker_manager.py:11-21`，storage 5/20/50 GB（commit a69714935 改定）。
- **FREE 2h**：`savvy-manager/app/instances.py:164` `if inst.plan == PlanType.FREE` 设 2h 窗口。死口径。
- **卖点源材料**：`new-api/web/default/src/features/home/components/sections/` 落地稿 + 本地记忆 `feedback-home-trust-block.md` / `project-home-finesse-redesign.md`，已按本次红线换词。

## 10. 技术约束

- 新区块代码落 `hermes-workspace/src/screens/dashboard/components/` 下（与现有 dashboard 组件同目录），由 `dashboard-screen.tsx` 在最末尾引入渲染。
- 文案走该工作区现有 i18n（英文 key / zh.json value），不硬编码字符串。
- 不改 `system-metrics-footer.tsx`、不进 `use-dashboard-layout` 可见性表。

## 11. 验收

- 控制台滚到底，依次出现：步骤条 → 升档引导文 → 三档卡 → 维运维底栏。
- 全文 grep 无「不绑卡 / 信用卡 / API Gateway / 真模型 / 降级 / 不配模型 / 模型已接」。
- FREE 卡文案逐字含「2 小时」。
- 窄屏三档卡折行/堆叠不破版。
- 步骤②热链能跳到工作区 Settings；步骤①热链指向 new-api `/token`。

## 12. 后续尾巴（不在本 spec 范围）

- STARTER/PRO 升档按钮的目标路由（new-api 订阅/套餐页）要实现时确认具体路径。
- B 层供应商 key 注入（`project-provider-key-injection` 那套）与本步骤条的「令牌页密钥」是两套，UI 上是否要并存/区分，留待后续。
