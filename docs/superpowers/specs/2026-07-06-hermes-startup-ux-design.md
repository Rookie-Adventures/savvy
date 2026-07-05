# A 子项目 — Hermes 启动 UX 修复 + 免费时长 2h

## 元信息

- **日期**: 2026-07-06
- **分支**: `feat/hermes-startup-ux`(走 E 工程约定,跑通 PR 合 dev)
- **归属**: 用户提出的 8 问中的 #1、#2、#3 + 免费时长 3h→2h
- **不碰**: hermes-workspace 源码、new-api 后端(除非 A2 需暴露 1 字段)、savvy-manager 计费/订阅/支付、容器重置(B)、高级计划(D)、订阅→容器升级(配套②)
- **前置依赖**: 无,可立即开工

---

## 8 问 → 子项目映射(全项目坐标,防串档)

| # | 用户问题 | 子项目 |
|---|---|---|
| 1 | 免费显示"无限制",应启动后倒计时 | **A**(本设计) |
| 2 | 睡后再启仍弹密钥框,误导 | **A** |
| 3 | 唤醒无延时,容器没醒就能开工作区 | **A** |
| 4 | 一键重置容器 | B(独立 spec) |
| 5 | 关标签页同步睡眠 | 撤销 → 现 A 的"免费时长 2h"吸收此位 |
| 6 | 改动会不会动 workspace 源码 | 非功能约束。结论: A/B/D 皆**不碰** hermes-workspace |
| 7 | 专用分支开发跑通合 dev | E(工程约定,不单独 spec) |
| 8 | 高级计划含 Token/免费模型如何设计 | D(独立 spec,与配套②同期排期) |

**额外引出的第 5 条线(8 问外)**: 支付联动② = 订阅生效 → manager `docker update` 热改容器资源 + `Instance.plan FREE→PAID_RESIDENT` + 清 expires_at 免睡。**非 8 问中项,但在查死支付时发现"付钱不给物"雷点,必须与 D 配套上线**。编号留空,记为"D 配套②"。

## 子项目总清单

- **A** Free UX 修复 + 2h(本 spec)
- **B** 一键重置容器
- **D** 高级计划计费(赠 Token + 免费模型)
- **E** 工程流程约定(分支策略)
- **D 配套②** 订阅→容器升级链(查死支付时引出,非 8 问项)

---

## 现状(已读代码核实)

### A1 — 免费计时显示
- `savvy-manager/app/routers/instances.py:107-108`:`if inst.plan == PlanType.FREE: expires_at = now + timedelta(hours=3)`。FREE **有** expires_at,PAID_RESIDENT 为 None(无限制)。
- "免费显示无限制"是**前端显示 bug**: new-api 白标前端(`new-api/web/default/src/features/hermes/`)未把 FREE 的 expires_at 算成倒计时,或把 None 误判给 FREE。后端数据正确。
- 注: A 实现时定位 `features/hermes/` 下具体显示组件再修。

### A2 — 密钥弹窗只在首启弹
- `instances.py:69`:`is_first_start = inst.provider_config_enc is None`。首启硬锁 key;睡后快照在 → `is_first_start=False` → 后端**不再要求 key**(`instances.py:70-74` 仅 `is_first_start and not body.provider_api_key` 才抛 400)。
- 后端已正确。bug 在**前端每次启动都弹密钥框**。
- 前端是否已有 `has_provider_key`/`is_first_start` 状态可读,A 实现时定位;若无则 new-api 后端 `GetHermesInstance`/相关 handler 多暴露一个 `has_provider_key bool` 字段(一行)。

### A3 — 唤醒延时 + ready 修复
- `savvy-manager/app/docker_manager.py:156` `start_container()`:`container.start()` 立即返回 True,**零 ready 探测**。
- `instances.py:147`:`start_container` 返回后**立刻** `inst.status = InstanceStatus.RUNNING; db.commit()`。
- → 前端拿 RUNNING 即 `window.open(workspace)`(`features/hermes/index.tsx:121`),但 workspace node `server-entry.js` 进程要 3-6 秒才 listen `:3000`(`server-entry.js:10,243`)→ 用户看到空壳/连不上。
- 工作区开窗方式已确认: `window.open(url, '_blank', 'noopener,noreferrer')`,**整页新标签,非 iframe**。

### A4 — 免费时长
- `instances.py:108`:`timedelta(hours=3)`。一行硬编码。
- PRD `docs/specs/hermes-saas-platform-prd.md:77` 同步:"3 hours per start" → "2 hours per start"。

---

## 设计

### A1 — 免费计时倒计时显示
**改**: `features/hermes/` 显示组件,plan=FREE + 有 expires_at → 显示剩余时间倒计时;plan=PAID_RESIDENT + expires_at=None → 显示"无限制"。
**动哪**: new-api 白标前端 1-2 文件。零后端、零 manager。
**风险**: 低。仅显示逻辑,不动数据。

### A2 — 密钥弹窗仅首启
**改**: 前端启动触发逻辑读 `is_first_start`/`has_provider_key`;仅 `true` 才弹密钥框,唤醒(快照已存在)直接调 start 不弹。
**动哪**: new-api 白标前端(密钥弹窗组件 + 启动触发处)。零后端(若现成字段)/ +1 行后端(若需暴露 `has_provider_key`)。零 manager。
**风险**: 低。

### A3 — 唤醒 ready 修复(路径 i + 前端 loading)
**manager 端**(`docker_manager.start_container`):
```
docker start → 轮询 container.reload(); status==running 则 break
  (≤5 次, 1s/次) → 再加 8 秒固定缓冲 → return True
超时(30s 仍非 running)也 return True(不卡死,前端 loading 兜底)
mock_mode 路径不变,直接 True
```
**前端端**(`features/hermes/index.tsx` 启动触发处):
- 启动调用期间全按钮禁用 + loading
- **首启文案**: "首次启动需配置环境,请耐心等待 10~30 秒"(首启实际含 probe_default_model + build_snapshot + 加密 + docker run 创建,比唤醒慢,故 10~30 秒安全估值)
- **唤醒文案**: "唤醒中…"
- 拿到 RUNNING(`status==RUNNING` 后端已确保 ready)→ `window.open`

**动哪**: manager `docker_manager.py` ~15 行 + new-api 白标前端启动触发处 + loading 组件。零 workspace、零 new-api 后端(除 A2 可能的 1 字段)。
**风险**: 中。改容器启动等待路径。测试:
- `test_docker_manager.py` 加 ready 轮询用例(mock client 返回 status 序列: `pending`→`pending`→`running` 验缓冲后才 True;`pending` 持续验证 30s 超时仍 True)
- `test_instances_router.py` 加"start 后 status 仅在 ready 后才 RUNNING"用例

### A4 — 免费时长 3h→2h(写死,不可配)
**改**: `instances.py:108` `timedelta(hours=3)` → `timedelta(hours=2)`。PRD:77 同步。
**只影响新启动**;已运行容器 `expires_at` 不变(符合"爱睡不睡"语义)。
**动哪**: manager 1 行 + PRD 1 行。
**风险**: 极低。

---

## 不碰清单(A 边界)

- hermes-workspace 源码 ✓
- new-api 后端(A2 最多 1 字段暴露)✓
- 支付/订阅/计费 ✓
- 容器重置(B)✓
- 高级计划计费(D)✓
- 订阅→容器升级(D 配套②)✓

---

## 测试策略

- manager 单测: `start_container` ready 轮询 + 超时(见 A3)
- manager 集成: `test_instances_router.py` 加 ready→RUNNING 时序用例 + A4 时长改 2h 用例(新启动 expires_at = now+2h)
- 前端: hand-check loading 态 + 首启/唤醒文案 render;A1 倒计时显示对(FREE 剩余 / PAID "无限制")
- 跑通全栈(本地 docker compose up --build new-api + savvy-manager)手动验: 三个 bug 各点一遍 + 时长验一次新启动

---

## 未定/留待实现时定位

- A1 具体显示组件文件路径(features/hermes/ 下,实现时 grep `expires_at|remaining|countdown|无限制|unlimited`)
- A2 前端是否已有 `has_provider_key` 状态字段(若无,后端加暴露)
- A3 manager 的 docker client 是否为 mock 注入友好(看现有 `test_docker_manager.py` 风格)

实现阶段定位,不影响 spec 完整性。
