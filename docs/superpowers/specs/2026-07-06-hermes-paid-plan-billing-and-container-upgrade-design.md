# Hermes 高级计划计费(D)+ 订阅→容器升级链(配套②)设计

**日期**: 2026-07-06
**分支**: `feat/hermes-paid-plan`
**状态**: spec 待用户审
**前置**: A 子项目(hermes-startup-ux)已合 dev
**同期上线**: D 与 配套② 同一 spec / 同一 PR

## 起源

8 问拆线第 8 问(高级计划赠 Token/免费模型)+ 配套②(订阅生效→容器升级)。memory `project-hermes-startup-ux-and-followups` 钉死二者同期上线,因"付钱不给物"与"物钱不一致"是对称雷点,分开留已知账活在生产。

## 范围边界

**做**:
- new-api 三档套餐记录(Free/Starter/Pro)落 `SubscriptionPlan`
- 订阅生效(new-api)→ 同步通知 manager 升级容器资源 + 改 Instance.plan + 清免费窗
- 订阅过期(new-api)→ 同步通知 manager 降级(改 plan=FREE + 重设免费 2h 窗),不碰运行容器
- manager `PlanType` 扩三档 + `docker update` 热改入口 + upgrade/downgrade 路由
- manager scanner 三段:升级失败补偿 / 降级失败补偿 / log 重建闭合
- storage 软配额:挂字段 + 统计展示 + 超量软告警(不强制禁)

**不做**(显式排除):
- 退款链(留后续财务子项目)
- "免费模型"(会员专属免费调模型)— 评估为高难度改核心计费链路,撤掉。改用「赠 Token + user group」两现成机制覆盖
- 本地模型(全 API 转发,已确认)
- hermes-workspace 前端改动(memory lock)
- storage 强制禁止(无文件系统配额支撑,留后续)

## 用户决策记录(8 问)

| # | 决定 |
|---|---|
| Q1 | manager 三档 FREE/STARTER/PRO(扩 PlanType + alembic 迁移) |
| Q2 | 容器加资源用 `docker update` 热改 CPU/RAM/PIDs,log 留免费档,等下次 stop 后重建闭合 |
| Q3 | storage 软配额只挂字段 + scanner 统计展示 + 超量软告警,不强制禁 |
| Q4 | 升级:事务提交后同步调 manager,失败标 `needs_upgrade`,scanner 补偿(≤3 次)。不退款 |
| Q5 | 乙主动型:买完立刻查在跑 instance,有就加资源;没就盖"已付费"章(user.group 已升),下次开按付档起 |
| Q6 | 降级:订阅过期只改 plan=FREE + 重设免 2h expires_at,不碰运行容器,免费睡 scanner 顺势 stop,下次开按免费起 |
| Q7 | 同期交付升+降;撤"免费模型";D 纯靠「赠 Token(TotalAmount)+ user group」 |
| Q8 | 额度耗尽走主站钱包余额兜底(AllowWalletOverflow=true,admin-per-plan 开关) |

## 技术决策记录(自行决定,不再问用户)

| 项 | 决定 | 原因 |
|---|---|---|
| PAID_RESIDENT 枚举 | 删,只留 FREE/STARTER/PRO | PRD 无此名,语义裂口,迁移一次到位 |
| `expected_plan` 列 | 加(nullable) | scanner 本地脏标记驱动,不反查 new-api user group |
| docker SDK 调用 | `container.update(...)`(docker-py 原生) | 不用 subprocess |
| log 重建触发 | scanner 查 `needs_rebuild` → stop 后 rm + run,保 volume + provider_config_enc | 串入现成免费睡流程 |
| 支付 webhook 触发 | 四家(epay/creem/stripe/waffo_pancake)全加,都汇 `CompleteSubscriptionOrder` | 任意支付路径都触发升级 |
| 升/降信号 | new-api 事务提交后 HMAC 同步 POST,失败标 expected_plan,scanner 补 | 单实时触发 + 兜底 |
| user.group 取值 | `default` / `starter` / `pro` | 现成字段,admin 填 SubscriptionPlan.UpgradeGroup |
| storage 统计 | scanner 周期 `docker system df` 取 volume 用量,记 max,超 storage_quota_gb → SYSLOG 软告警 | 不实时,不伤性能 |
| Pro 价格/TotalAmount 具体值 | admin 后台填,spec 不硬编码;Pro enabled=false(Coming Soon) | 运营值非代码值 |
| 升补扫重试上限 | 3 次,超限 SYSLOG 告警停手 | 避免 quota-容器错配无限挂(漏洞 1 修复) |
| scanner 节拍 | 沿 `scanner.py` 现有 BackgroundScheduler,1 分钟 | 不加新 timer |
| scanner job 划分 | 三个独立 job:check_needs_upgrade / check_needs_downgrade / check_needs_rebuild + 既有 check_expired | 独立可关停 |

## 逻辑漏洞修复(设计评审产出)

**漏洞 1(严重)— quota 与容器档位错配**:升档 sync 失败期间,用户调主站 API 在 Pro 大 quota 池扣费,但容器仍 free 配置。若后续降级,user.group 回 free 但 quota 已从 Pro 池扣。**修复**:sync fail → `needs_upgrade=true`,manager scanner 限 3 次补成功,否则 SYSLOG 告警停手。错配时限 = 重试窗口(分钟级)。

**漏洞 2(中)— 降级双触发器**:原设计 new-api `ExpireDueSubscriptions` 主动调降级 + manager scanner 也治降级,两套叠加。**修复**:降级唯一权威触发 = new-api `ExpireDueSubscriptions`(事务提交后同步调 manager downgrade)。manager scanner 仅作**失败补救**,扫 `Instance.plan ≠ expected_plan` 触发。一个扫描,脏标记驱动。

**漏洞 3(中)— log 配额永不闭合**:Q2 log 留免费档,等"下次启动重建"。但睡醒用 `start_container`(不重建),log 永远低一档。**修复**:scanner 扫 RUNNING 且 `log_config ≠ 当前 plan 应有 log_config` → 标 `needs_rebuild`,下次 stop 后 rm + run 新 plan(保 volume + provider_config_enc)。

## 总体架构

### 升级链(订阅生效)

```
用户买 Starter/Pro
  ↓ 支付 webhook (epay/creem/stripe/waffo_pancake)
CompleteSubscriptionOrder (new-api, DB 事务内)
  ├ CreateUserSubscriptionFromPlanTx
  │   ├ 发 quota 到订阅池 (TotalAmount, 月重置 QuotaResetPeriod=monthly)
  │   └ 升 user.group = starter/pro
  └ 事务提交 ✓
  ↓ 事务外同步调 manager (HMAC)
UpgradeHermesManager(userID, plan=STARTER)
  ├ manager 查 user 在跑 instance (status=RUNNING)
  │   ├ 有 → POST /internal/instances/{id}/upgrade
  │   │       body: {plan: STARTER, cpu_quota: 200000, mem_limit: "2g", pids_limit: 512}
  │   │       manager: docker update 热改 + Instance.plan=STARTER + 清 expires_at(免睡)
  │   │              + Instance.expected_plan=STARTER
  │   │       失败 → Instance.needs_upgrade=true, scanner 限 3 次补
  │   └ 无 → 不调 (user.group 已升 = "已付费"章, instance 下次开按 STARTER 档起)
  └ 返回
```

### 降级链(订阅过期)

```
ExpireDueSubscriptions (new-api, DB 事务内)
  ├ 订阅 status=expired
  └ user.group 改回 PrevUserGroup (= default/free)
  ↓ 事务外同步调 manager (HMAC)
DowngradeHermesManager(userID, plan=FREE)
  ├ manager 查 user 在跑 instance
  │   ├ 有 → POST /internal/instances/{id}/downgrade
  │   │       body: {plan: FREE, expires_at: now+2h}
  │   │       manager: Instance.plan=FREE + Instance.expected_plan=FREE
  │   │              + expires_at=now+2h (不动运行容器, 给免费窗)
  │   │       失败 → scanner 补 (扫 Instance.plan ≠ expected_plan)
  │   └ 无 → 同样 (盖"已降"章, instance 下次开按 FREE 起)
  ↓ 之后 manager 现成免费睡 scanner 扫 expires_at → docker stop
  ↓ 用户下次点开始 → manager 按当前 plan=FREE 路径
```

## 数据模型变更

### new-api(零 schema 变更)

`SubscriptionPlan` 不加字段。admin 后台填三条记录:

```
Free    : PriceAmount=0,    TotalAmount=0,   UpgradeGroup=""        (参照,不走订阅机制)
Starter : PriceAmount=99,   TotalAmount=<N>, QuotaResetPeriod=monthly,
          UpgradeGroup="starter", DowngradeGroup="default", AllowWalletOverflow=true
Pro     : PriceAmount=<待定>, TotalAmount=<M>, 同结构, UpgradeGroup="pro",
          Enabled=false (Coming Soon)
```

`UserSubscription` 不加字段(PrevUserGroup/DowngradeGroup/EndTime 现成支持降级)。

### manager(schema 变更)

**改 `PlanType` 枚举**(`savvy-manager/app/models.py`):
```python
class PlanType(str, Enum):
    FREE = "FREE"
    STARTER = "STARTER"
    PRO = "PRO"
    # PAID_RESIDENT 删除
```

**`Instance` 加列**:
```python
needs_upgrade = Column(Boolean, default=False)            # 升级失败补扫标记
needs_rebuild = Column(Boolean, default=False)            # log 重建标记 (漏洞 3)
expected_plan = Column(SQLEnum(PlanType), nullable=True)  # 脏标记:系统下达目标 plan
storage_quota_gb = Column(Integer, nullable=True)         # Q3 软配额 (Free10/Start30/Pro80)
upgrade_retries = Column(Integer, default=0)              # 升补扫重试计数 (漏洞 1, ≤3)
```

**`User` 不加列** — manager 不存 user.group(new-api 权威)。

**alembic 迁移**:
1. `plan` 枚举加 STARTER、PRO;删除 PAID_RESIDENT(先清引用:现存 PAID_RESIDENT → STARTER)
2. `instances` 加 5 列(needs_upgrade / needs_rebuild / expected_plan / storage_quota_gb / upgrade_retries)
3. 现存 instance 按档位回填 storage_quota_gb(FREE→10)

## 模块级常量(manager 不入库)

`savvy-manager/app/docker_manager.py` 顶部:
```python
PLAN_RESOURCES = {
    "FREE":    {"cpu_quota": 50000,  "mem_limit": "768m", "pids_limit": 128},
    "STARTER": {"cpu_quota": 200000, "mem_limit": "2g",   "pids_limit": 512},
    "PRO":     {"cpu_quota": 400000, "mem_limit": "8g",   "pids_limit": 1024},
}
PLAN_LOG_CONFIG = {
    "FREE":    {"max-size": "10m", "max-file": "3"},
    "STARTER": {"max-size": "20m", "max-file": "5"},
    "PRO":     {"max-size": "50m", "max-file": "5"},
}
PLAN_STORAGE_GB = {"FREE": 10, "STARTER": 30, "PRO": 80}
```
原 `create_container` 内联 `limits` 字典(行 61-77)替换为引用 `PLAN_RESOURCES` / `PLAN_LOG_CONFIG`。

## 接口契约

### manager 新增路由(`savvy-manager/app/routers/instances.py`)

**POST /internal/instances/{instance_id}/upgrade**
- auth: require_hmac
- body: `{plan: "STARTER"|"PRO", cpu_quota: int, mem_limit: str, pids_limit: int}`
- 逻辑:
  1. 取 instance,校验属该 user
  2. `docker_manager.update_container_resources(container_name, cpu_quota, mem_limit, pids_limit)` 热改
  3. 成功 → `Instance.plan=plan`, `Instance.expected_plan=plan`, `Instance.expires_at=None`(免睡), `Instance.needs_upgrade=False`
  4. 失败 → `Instance.needs_upgrade=True`, `Instance.expected_plan=plan`, 返回 success=false
- res: `{success: bool, message: str}`

**POST /internal/instances/{instance_id}/downgrade**
- auth: require_hmac
- body: `{plan: "FREE", expires_at: ISO8601}`
- 逻辑:
  1. 取 instance
  2. `Instance.plan=FREE`, `Instance.expected_plan=FREE`, `Instance.expires_at=expires_at`(免费 2h 窗)
  3. 不动运行容器(Q6 锁定:不碰运行中容器避免 OOM)
  4. 失败 → 不改,返回 success=false(scanner 补)
- res: `{success: bool, message: str}`

### manager 新增函数(`docker_manager.py`)

```python
def update_container_resources(container_name, cpu_quota, mem_limit, pids_limit) -> bool:
    """docker update 热改运行中容器资源。不重建,零中断。log_config 不改(留免费档)。"""
    # mock_mode / client None 处理同 create_container
    container = client.containers.get(container_name)
    container.update(
        cpu_quota=cpu_quota,
        mem_limit=mem_limit,
        memswap_limit=mem_limit,
        pids_limit=pids_limit,
    )
    return True
```

### new-api 新增客户端函数(`service/hermes.go`)

```go
// UpgradeHermesInstance 通知 manager 升级在跑容器资源 + 改 plan + 清免费窗。
// 无在跑 instance 时 manager 返回 success=true(no-op)。
func UpgradeHermesInstance(userID int, instanceID, plan string, cpuQuota int, memLimit string, pidsLimit int) error

// DowngradeHermesInstance 通知 manager 降级:改 plan=FREE + 设免费 2h 窗。不动运行容器。
func DowngradeHermesInstance(userID int, instanceID string, expiresAt time.Time) error
```

### new-api 触发点改动

**`model/subscription.go::CompleteSubscriptionOrder`**(行 553):
- 事务提交成功后(行 614 `if err != nil` 之后),新增:
  - 若 `upgradeGroup != ""`(付费档),查该 user 在跑 instance(HMAC GET),有则同步调 `UpgradeHermesInstance`
  - 资源数值:new-api 侧持 `PLAN_RESOURCES` Go 常量副本(group→plan→cpu/mem/pids),随请求 body 传给 manager,manager 不反查 plan→资源
  - 失败不阻塞订单成功(订单已 commit),仅 SYSLOG 告警,scanner 兜底

**`model/subscription.go::ExpireDueSubscriptions`**(行 986):
- 每个过期 user 的事务提交后,新增:
  - 查该 user 在跑 instance,有则同步调 `DowngradeHermesInstance`(plan=FREE, expires_at=now+2h)
  - 失败不阻塞,scanner 兜底

## manager scanner 三段(`scanner.py`)

沿现有 `BackgroundScheduler`,1 分钟节拍。加三个 job:

**1. check_needs_upgrade**(升补,漏洞 1 修复)
```
扫 Instance.needs_upgrade=True
  ├ upgrade_retries < 3 → update_container_resources + 改 plan,成功清 needs_upgrade + upgrade_retries=0
  └ upgrade_retries >= 3 → SYSLOG 告警,清 needs_upgrade(停手,等人工)
每次失败 upgrade_retries += 1
```

**2. check_needs_downgrade**(降补,漏洞 2 修复)
```
扫 Instance.expected_plan IS NOT NULL AND Instance.plan != Instance.expected_plan
  → 改 Instance.plan=expected_plan
  → 若 expected_plan=FREE,设 expires_at=now+2h
  → 清 expected_plan(对齐)
```

**3. check_needs_rebuild**(log 重建,漏洞 3 修复)
```
扫 Instance.needs_rebuild=True AND Instance.status=SLEEPING(已停)
  → docker rm + docker run 新 plan(保 volume + provider_config_enc)
  → 清 needs_rebuild, status=NOT_CREATED(等用户 start)
标 needs_rebuild 的时机:每次 upgrade 成功后,若新 plan 的 log_config ≠ 旧 → 标 needs_rebuild
```

**4. check_storage_quota**(Q3 软配额)
```
周期(决定:每 10 分钟,独立 job,不挤 1 分钟节拍):
  docker system df 取各 volume 用量
  超 Instance.storage_quota_gb → SYSLOG 软告警(不强制禁)
```

## 错误处理

| 场景 | 处理 |
|---|---|
| 升级 sync 调 manager 超时/失败 | 标 needs_upgrade,scanner 3 次补,超限告警停手 |
| 降级 sync 调 manager 失败 | 标 expected_plan,scanner 补(无限重试,因不碰运行容器安全) |
| docker update 失败(容器已删/Docker 宕) | needs_upgrade=true,scanner 补;容器已删则 instance 转 NOT_CREATED |
| rebuild 时 volume 损坏 | SYSLOG 告警,instance 转 ERROR,等人工 |
| new-api 事务提交后但 sync 调前崩溃 | 订阅已生效(user.group 已升),容器未升级;scanner 扫 expected_plan 缺失 → 不触发;需 new-api 侧补扫?决定:不加 new-api 补扫,YAGNI(崩溃罕见,人工补救可接受) |

## 测试

**manager(Python pytest)**:
- `update_container_resources` mock docker client,断言 update 参数
- upgrade 路由:成功改 plan+清 expires_at;失败标 needs_upgrade
- downgrade 路由:改 plan+设 expires_at,不动容器
- scanner 三段:needs_upgrade 重试上限;expected_plan 脏标记驱动;needs_rebuild 在 SLEEPING 触发

**new-api(Go testify)**:
- `CompleteSubscriptionOrder` 成功后调 `UpgradeHermesInstance`(mock manager client)
- `ExpireDueSubscriptions` 成功后调 `DowngradeHermesInstance`
- sync 调失败不阻塞订单成功

**集成**:
- mock_mode 全链:买 Starter → manager instance.plan=STARTER + expires_at=None
- 过期 → instance.plan=FREE + expires_at=now+2h,容器未停

## 风险

1. **new-api 上游同步冲突区**:memory 记 new-api 有白标冲突区。改 `CompleteSubscriptionOrder` / `ExpireDueSubscriptions` 是改上游核心函数,同步时要 patch 保留。缓解:改动放函数末尾(事务外),最小侵入。
2. **PAID_RESIDENT 删枚举的 PostgreSQL 迁移**:PG 删 enum 值需先清引用。迁移脚本先 UPDATE 再 ALTER TYPE。SQLite/MySQL 相对简单。
3. **HMAC 同步调 manager 的延迟**:支付 webhook 响应时间增加(manager docker update 耗时)。缓解:manager upgrade 路由 docker update 后立即返回,不等容器内部生效。
4. **Pro 档 Coming Soon**:spec 建 Pro 记录但 enabled=false。前端不展示购买入口。后续运营启用时改 enabled=true + 填价格。

## 不碰清单

- hermes-workspace 前端
- hermes-agent
- 退款链
- 本地模型
- storage 强制禁止
- "免费模型"(会员专属免费调)

## 链接

- memory: `project-hermes-startup-ux-and-followups`
- A 子项目 spec: `docs/superpowers/specs/2026-07-06-hermes-startup-ux-design.md`
- PRD: `docs/specs/hermes-saas-platform-prd.md`(Plans And Resource Limits 章节)
