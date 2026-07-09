# Pro 用户控制台仍显示 FREE + 2h，且容器规格不显示

- 日期: 2026-07-09
- 受影响: Hermes 工作区控制台 (`/hermes`)、订阅→容器升级链
- 状态: 已修复（plan 对齐 + 规格回传）；1 个预存测试失败待单独修

---

## 问题症状

admin 账户实际付了 Pro 订阅，登录 Hermes 控制台：

1. **Plan 显示 FREE**（应显示 PRO）
2. **Remaining Time 显示“每次启动 2 小时”**（Pro 应 Unlimited）
3. **容器规格（CPU/RAM/存储）不显示**（该区块根本不存在）

同时未确认：用户升级容器是否支持升降配置。

---

## 根因

### 症状 1+2: 升级链有窗口漏洞，且无兜底

升级链路（已落地，非缺失）:

```
CompleteSubscriptionOrder  (new-api/model/subscription.go:618)
  └─ if upgradeGroup != "" && logUserId > 0
     └─ go NotifyManagerUpgrade(uid, group)        (service/hermes.go:549)
        └─ inst = GetHermesInstance(uid)
        └─ if inst.Status != "RUNNING" { return nil }   ← 漏洞在此
           └─ UpgradeHermesInstance(...)           → manager upgrade 路由
              └─ inst.plan = PRO; expires_at = None     (instances.py:450)
```

**根因窗口**: `NotifyManagerUpgrade` 只在容器 `RUNNING` 时调升级路由。常态是**先买套餐、容器还没建/已睡**（NOT_CREATED/SLEEPING）——此时订阅成功，`user.group` 确实改成 `pro`，但 manager 的 `inst.plan` 没人动。

**无兜底**: `scanner.check_needs_upgrade`（`scanner.py:42`）只扫 `needs_upgrade=True` 的实例。错过窗口的实例**从未被标过 `needs_upgrade`**（那条脏标只在 `update_container_resources` 失败时打），所以 scanner 永远不补救。

→ manager `inst.plan` 永久 FREE。之后启动，`start_instance` 第 134 行 `if inst.plan == FREE → expires_at = 2h` → 控制台显示 FREE + 2h。

### 症状 3: 规格从未回传前端

`PLAN_RESOURCES`（`docker_manager.py:11`）只在创建/热改容器时用。`InstanceResponse`（`users.py`）、Go 端 `HermesInstance`、前端 `types.ts` **都没有**规格字段 → 规格区在 UI 上根本不存在。

### 权威源确认

manager 的 `User.plan` 字段：`upsert_user` 建用户时写死 FREE，仅在 `create_instance:102` 读过一次，**从无更新路径** → 不可靠。唯一权威是 **new-api 的 `user.group`**（订阅生效后 `CompleteSubscriptionOrder` 改成 `pro`/`starter`），经 `service.GroupToPlanName` 映射成 `PRO`/`STARTER`/`FREE`。

---

## 修复思路

### 核心决策: 在 `start_instance` 对齐 (单一闭合点)

用户每次启动容器必经 `start_instance`，是把 manager `inst.plan` 与 new-api 权威 group 对齐的**唯一可靠单边闭合点**。无需新增 scanner job、无需广播、无需 new-api 主动轮询。

- new-api 启动时把当前用户权威 plan（`user.group` → `GroupToPlanName`）作为 `expected_plan` 传给 manager
- manager `start_instance` 在算 `expires_at` 之前：若 `expected_plan != inst.plan`，对齐 `inst.plan`、清 `needs_upgrade`/`expected_plan` 脏标、`expires_at=None`（升档免睡）
- 对齐后既有的 `if inst.plan == FREE → 2h` 自然按对齐后的 plan 算

### 规格回传（三端透传，静态按 plan）

后端 `PLAN_RESOURCES` 按 plan 映射出 cpu/mem/pids，加 `storage_quota_gb`，三端透传：

```
manager InstanceResponse (users.py)
  → new-api HermesInstance (service/hermes.go, snake)
  → controller toVO hermesInstanceVO (camelCase: cpuQuota/memLimit/pidsLimit/storageGb)
  → 前端 types.ts HermesInstance + index.tsx 规格格
```

前端按 plan 静态显示（2vCPU/2G 等），**不查 docker stats 真实用量**（动态监控超范围）。

### 形参注入 vs service 内部查询

初版在 `service.StartHermesInstance` 内调 `model.GetUserGroup`，但 service 层测试不 init DB → 既有 start/revoke 测试全部炸（`SQL logic error`）。改为 **controller 查 group 后把 `planName` 作为入参注入 service**，service 保持纯 → service 测试不碰 DB，可测性保留。

---

## 改动清单

| 层 | 文件 | 改动 |
|---|---|---|
| manager | `app/routers/instances.py` | `StartRequest` 加 `expected_plan`；`start_instance` 按 expected_plan 对齐 inst.plan + 清脏标 + 错开 2h |
| manager | `app/routers/users.py` | `InstanceResponse` 加 `cpu_quota`/`mem_limit`/`pids_limit`/`storage_quota_gb`；`get_instance`/`create_instance` 出参填；`_spec_for_plan` 辅助 |
| new-api | `service/hermes.go` | `StartHermesInstance` 加 `planName` 参数 → body `expected_plan`；`HermesInstance` struct 加规格四字段 |
| new-api | `controller/hermes.go` | `StartHermesInstance` 查 `model.GetUserGroup`+`GroupToPlanName` 传 planName；`hermesInstanceVO`/`toVO` 透传规格 camelCase |
| 前端 | `features/hermes/types.ts` | `HermesInstance` 加 `cpuQuota`/`memLimit`/`pidsLimit`/`storageGb` |
| 前端 | `features/hermes/index.tsx` | 规格显示格（vCPU·mem·Storage）；`Remaining Time` `plan?.toUpperCase()==='FREE'` 大小写健壮 |
| i18n | 6 locale en/zh/fr/ja/ru/vi | 加 `Resources` key |

---

## 验证

- manager `test_instances_router.py` 14 测全过（含 3 新: expected_plan STARTER 对齐/FREE 留 2h 窗/规格回传）
- new-api `service/hermes_test.go` 13 测全过（start 三处改签名 + expected_plan 断言）
- `go build ./...` 通过
- `bun run build` 通过
- i18n 6 locale JSON 合法

---

## 已知限制（本次不碰，已 log(qaz)）

1. **唤醒旧容器的 docker 资源热改**: `start_container` 是 stopped 容器 `docker start`，不重设 mem/cpu。Pro 用户唤醒一个 FREE 期起的容器，内存/CPU 仍 FREE；`inst.plan` 已对齐 PRO 但资源未升。需 `needs_rebuild` 路径（scanner.rm + create）重建，或用户手动删除重建才升资源。本次只修 plan 显示+免睡，资源热改留给 rebuild 闭合。
2. **规格静态显示**: 按 plan 映射的静态值，非容器 `docker stats` 真实用量。
3. **manager `User.plan` 不同步**: 不从 new-api 拉，权威在 new-api，manager `User.plan` 维持建库 FREE 语义不变。
4. **i18n `Resources` 等 fallback**: 非中英文 locale 见缺兜底到 key；本次已手动补 6 语言。

---

## 遗留尾巴: 1 个预存测试失败

`tests/test_docker_manager.py::test_update_container_resources_calls_docker_update` 失败，与本次无关（用 `git stash` 验证的思路因工作树保护未跑，代码已读取对比确认）:

- 生产代码 `docker_manager.py:268` 有意**不带 `pids_limit`** 调 `container.update`（`ponytail:` 注释: docker SDK `update_container()` 不支持热改 pids，pids 差异走 needs_rebuild 重建闭合）
- 测试 `test_docker_manager.py:153` 仍断言 `container.update` 收到 `pids_limit`

测试断言的是一个**错误契约**（docker 根本不支持）。一行删测试里的 `pids_limit` 即修，但不在本次范围，留待单独处理。

---

## 升降配置问答（原问题附带）

用户问“升级容器怎么会有升降配置吗”——**支持**:

- **升（订阅生效）**: `update_container_resources` 调 `docker update` 热改 CPU/RAM，零中断不重建（PIDs 不可热改，走 rebuild）
- **降（订阅过期）**: `downgrade` 路由不碰运行容器（Q6 防止跑着任务 OOM），改 `inst.plan=FREE` + 设 2h 窗，下次启动按 FREE 档起
- **本次修的**: 升级窗口漏掉时（容器非 RUNNING 时买订阅）的后期对齐，非升降机制本身
