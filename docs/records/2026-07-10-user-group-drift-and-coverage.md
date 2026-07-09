# Pro 用户控制台显 FREE（2回）、规格 storage 没跟升、新用户付费后启动报 models 空

- 日期: 2026-07-10
- 受影响: Hermes 控制台 plan/规格显示、订阅生效后 group 持久化、付费新用户启动工作区
- 状态: 已修（user.group 自愈对齐 + storage 动态映射 + 容量档位调整 + 渠道分组覆盖）；启动工作区 401 仍未定位（另案）
- 上游留痕: [2026-07-09-hermes-plan-drift-and-spec-display.md](./2026-07-09-hermes-plan-drift-and-spec-display.md)

---

## 症状（三独立症状，患者同构）

1. **admin 已付 Pro 但控制台仍显 FREE + 2h**（昨 7-09 留痕已修 plan 漂移，未收口）
2. **升 PRO 后 CPU/内存已升挡但存储仍显 10GB**（应 80GB，调挡后应 50GB）
3. **新注册用户买了订阅后启动工作区报** `new-api /v1/models returned no models for this key`，但渠道明明配了模型

---

## 根因

### 症状1: new-api `users.group` 漂回 default 无自愈（昨留痕 manager `inst.plan` 漂移的孪生缺陷）

权威契约: 订阅 active 未过期时,`users.group` 必须 = 该订阅 `upgrade_group`。实测 admin 满足: `user_subscriptions id=4` active、未过期、upgrade_group=pro,但 `users.group=default`。

写 `users.group` 的路径只有三条(全 in `model/subscription.go`):
- `CreateUserSubscriptionFromPlanTx` line 516-517 (激活写)
- `downgradeUserGroupForSubscriptionTx` line 467-468 (降级写)
- `ExpireDueSubscriptions` line 1078-1079 (过期汇总写)

三条**全有 activeSub 守卫**: 存在另一条 active 未过期升级订阅 → 不降。守卫正确,但它**只挡降、不治病**。一旦 group 历史漂回 baseline(default),守卫正确地**保持 default 永不上调**,即使存在显式生效的 pro 订阅。

→ `user.group` 无任何代码把它拉回 `upgrade_group`。漂移不自愈,与 7-09 留痕 line 39「scene-scanner 永远不补救 inst.plan」**完全同构** —— 7-09 漂在 manager `inst.plan`,本次漂在 new-api `user.group`。昨修的 start-instance 对齐因此收到 `expected_plan=FREE`(controller GetUserGroup 读到 default → GroupToPlanName → FREE → 传 FREE),把表面对齐代码也架空。

旁连效应链: `user.group=default` → `GroupToPlanName(default)=FREE` → controller 传 `expected_plan=FREE` → manager start 对齐分支不调(plan 已 FREE) → `inst.plan` 保 FREE → 前端显 FREE+2h。**root 在 new-api group 侧**,不是 manager 侧。

### 症状2: storage 展示走 DB 旧值,CPU/mem 走 plan 动态映射 —— 两路不同源

`InstanceResponse`(manager `users.py`) 出参:
- `cpu_quota/mem_limit/pids_limit` ← `_spec_for_plan(plan)` 按 plan 动态查 `PLAN_RESOURCES`(留痕7-09 加的 spec 透传)
- `storage_quota_gb` ← `inst.storage_quota_gb`(DB 建实例时按当时的 plan 写死,如 FREE=10)

升级对齐分支(`instances.py:148-152`,7-09 留痕那段)只改 `inst.plan` + 清脏标、**漏改 `inst.storage_quota_gb`**。而 scanner 那条同功能对齐路径(`scanner.py:100-103`)是**同步改了 storage**。→ 两处对齐点行为不对称 → plan 升 PRO、CPU/mem 因按 plan 动态映射正确显 4vCPU/8g,但 storage 仍取 DB 旧值 10。

### 症状3: 渠道只挂 default 组,付费用户升组后用不到模型

新注册用户 group=default,先买订阅 → group 升 `starter/pro` → 启动工作区用密钥调 `/v1/models` → new-api 按 token 的 user.group 分发渠道 → 渠道只在 `default` 组 → starter/pro 组**无任何渠道** → /v1/models 空 → manager `probe_default_model` 抛 no models → 前端报 `Failed to list models from provider`。

非代码 bug,是**渠道分组配置不全**: 既然按组驱动分发(记忆 `reference-deploy-access-and-domains` 铁律「user.group必须=channel.group否则/v1/models空」),渠道必须覆盖所有会出现的组(default/vip/svip/starter/pro 等),否则付费即失效(付费体验比免费更差)。

---

## 修复思路

### 症状1: 在 `GetUserGroup` 加 lazy 自愈对齐(对称于 7-09 的 start-instance 对齐)

`GetUserGroup`(`model/user.go:1017`) DB 读后调新函数 `reconcileUserGroupIfStale(userId, dbGroup)`(`model/subscription.go`): 仅当读出 group 是 baseline(""或"default"——漂移唯一可观测症状)才查一条 active 未过期且 `upgrade_group<>''` 订阅,有则 `Update("group", upgrade_group)` 写回 DB 并返回 upgrade_group。elevated group 全程短路(零热路径开销,99% 已付费用户读不受影响)、幂等(同值不写)。

闭合在 `GetUserGroup` 是因为读必经、controller 启动 hermes 调它;对齐后 `GroupToPlanName` → PRO → `expected_plan=PRO` → 7-09 的 manager start 对齐生效。一处修,new-api 与 manager 两侧同时对齐。

**formal 参数注入而非 service 内查**: 7-09 留痕已述(避免 service 层测试 init DB 炸)。本次 heal 放 model 层只调 DB,不碰 service 测试。

### 症状2: `_spec_for_plan` 也产 storage,展示层全按 plan 一致映射

`_spec_for_plan(plan)`(`users.py`) 额外查 `PLAN_STORAGE_GB[plan]` 把 `storage_quota_gb` 并入返回 dict。三处 InstanceResponse 拼装去掉显式 `storage_quota_gb=inst.storage_quota_gb`,改由 `**spec` 单独负责。→ 展示层 CPU/mem/storage 全按当前 `inst.plan` 动态一致性映射,DB 的 `inst.storage_quota_gb` 仅留给 scanner 软配额读用(scanner.py:169 那条按 DB 值告警的循环)。

附带修 start-instance 对齐分支补 `inst.storage_quota_gb = PLAN_STORAGE_GB[target]` —— 与 scanner.py:103 行为对齐(消除两闭合点不对称);虽然展示层已不再依赖该 DB 值,但 DB 真值同步对 scanner 软配额判定仍必要。

### 容量档位调整(用户决定)

`PLAN_STORAGE_GB` `docker_manager.py:21`: FREE 10→**5**、STARTER 30→**20**、PRO 80→**50**(新档存储配额)。CPU/mem 不变。

### 症状3: 渠道分组配置覆盖所有付费组(运维侧,非代码)

new-api 后台渠道编辑,把"分组"从仅 `default` 改为勾选所有会出现用户的组(default/vip/svip/starter/pro,多组按后台 UI 的多选/逗号语法)。一条渠道配置改完,三组(/五组)用户均能拉到 models → 启动工作区 probe 通过。

---

## 改动清单

| 层 | 文件 | 改动 |
|---|---|---|
| new-api | `model/subscription.go` | 加 `reconcileUserGroupIfStale(userId, dbGroup)`: baseline group 时查一条 active 升级订阅、写回 upgrade_group |
| new-api | `model/user.go` | `GetUserGroup` DB 读后调 reconcile,返回对齐后的 group(defer 回写 cache 用新值) |
| new-api | `model/subscription_group_reconcile_test.go` | 新增 3 测: 命中自愈(default→pro 写回 DB)/elevated 短路/真免费不升幻档 |
| manager | `app/routers/users.py` | `_spec_for_plan` 并入 `PLAN_STORAGE_GB[plan]` 产 storage;三处 InstanceResponse 去显式 `storage_quota_gb=inst...`,由 `**spec` 总负责 |
| manager | `app/routers/instances.py` | start 对齐分支补 `inst.storage_quota_gb = PLAN_STORAGE_GB[target]`(对称 scanner.py:103) |
| manager | `app/docker_manager.py` | `PLAN_STORAGE_GB` 容量调: 5/20/50 |
| manager | `tests/test_instances_router.py` | 升级对齐补 `storage_quota_gb==20` 断言;spec_fields 测改 50 |
| manager | `tests/test_docker_manager.py` | `PLAN_STORAGE_GB` 断言改 5/20/50 |
| 运维 | new-api 后台 | 渠道"分组"加 default/vip/svip/starter/pro 全覆盖(非代码) |

---

## 验证

- new-api `go build ./...` 过;`go test ./model/` 56 测全过(含 3 新 reconcile 测)
- manager `pytest tests/test_instances_router.py` 14 测全过;`test_docker_manager.py` 8 过 1 预存失败(pids 错契约,7-09 已 log(qaz) 无关本次)
- admin 真实验证: 启动工作区后前端 `4.00 vCPU · 8g`(PRO CPU/mem 挡已生效);刷新显存储(动态映射后追上 plan)
- 新用户付费后启动工作区: 后台改渠道分组覆盖全组后 → `/v1/models` 有模型、probe 通过、容器启动成功(用户确认"完美")

---

## 已知限制 / 尾巴

1. **启动工作区 401 未定位**: 用户两次反映前端启动工作区遇 401,但两端日志(new-api/manager/nginx)当时均无 401 access/error 记录,疑复现已过未新触发。后续若再现 → 抓 nginx access + new-api UserAuth 日志 + manager HMAC 校验日志。记忆 `feedback-savvy-deploy-gotchas` 提过 HMAC dev/prod drift 是强嫌疑。本次未碰,另案。
2. **DB `inst.storage_quota_gb` 与展示层解耦但未废弃**: scanner 软配额(scanner.py:169)仍读 DB 真值;展示层现走 plan 动态映射。两者数值在所有升级对齐路径(start + scanner)均已同步,故一致;若未来某路径只改 plan 不改 DB storage,展示与软配额会再分叉 —— start 已对齐、scanner 已对齐,两闭合点都改了,正常无分叉。
3. **reconcile 只升不降**: `reconcileUserGroupIfStale` 仅 baseline→upgrade 单向自愈,不处理 elevated 漂(不该降,留降级路径管)。elevated 短路是刻意的,避免读路径误降 pro 用户。
4. **渠道分组覆盖靠运维**: 代码侧 distribute 仍按 group 严格分发;若后续新增付费组(如 svip)忘了在渠道分组里加,会重蹈症状3。可考虑未来在订阅生效升组时校验"新组是否已有渠道覆盖"并告警 —— 本次不做,留尾巴。
5. **新用户 group 入 starter 谜**: `aaaa1`(id=3)注册即 starter 组,7-10 未深查注册写 group 处(`model/user.go:42` GORM default='default',不该 starter)。疑后台手动改或某注册选项带入,与本次修复主线无关,留观察。
