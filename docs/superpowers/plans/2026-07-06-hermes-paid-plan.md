# Hermes 高级计划计费(D)+ 订阅→容器升级链(配套②)实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 订阅生效(买 Starter/Pro)→ 同步通知 manager 热改容器资源 + 改 Instance.plan + 清免费窗;订阅过期 → 降级回 FREE + 设免费 2h 窗不碰运行容器;manager 三档 PlanType + docker update 热改 + scanner 三段(升补/降补/log 重建)+ storage 软配额挂字段。

**Architecture:** manager 先行(PlanType 三档 + 迁移 + update_container_resources + upgrade/downgrade 路由 + scanner 三段);new-api 后接(hermes.go 加 Upgrade/DowngradeHermesInstance 客户端 + CompleteSubscriptionOrder/ExpireDueSubscriptions 事务后同步触发);收尾集成测试。撤"免费模型",纯靠 TotalAmount(赠 Token)+ UpgradeGroup(user group)现成机制。

**Tech Stack:** savvy-manager(Python/FastAPI/SQLAlchemy/Docker SDK/alembic,pytest),new-api(Go/Gin/GORM,testify)。

## Global Constraints

- **分支**: `feat/hermes-paid-plan`(已在),每 Task 末 commit,跑通 PR 合 dev
- **不碰 hermes-workspace / hermes-agent / 退款链 / 本地模型 / storage 强制禁止 / "免费模型"**(spec 不碰清单)
- **JSON 包装**: new-api 后端任何 JSON 操作走 `common.Marshal/Unmarshal`,禁直用 `encoding/json`(new-api/CLAUDE.md 铁规;`json.RawMessage` 类型引用可)
- **DB 兼容**: manager alembic 迁移须 SQLite/MySQL/PG 兼容(用 `op.batch_alter_table` + `op.execute` 分方言)
- **protected**: 不动 `new-api`/`QuantumNous` 品牌引用
- **撤 PAID_RESIDENT 枚举**: 只留 FREE/STARTER/PRO 三档
- **scanner 节拍**: 沿 `scanner.py` 现有 BackgroundScheduler,1 分钟;storage 软配额 job 10 分钟
- **升补重试上限**: 3 次,超限 SYSLOG 告警停手(漏洞 1 修复)
- **manager 测试 mock_mode**: `monkeypatch.setattr(docker_manager.settings, "mock_mode", True)`,client fixture 已 override require_hmac 为 `{"user_id":"1"}`
- **new-api 测试 mock manager**: `httptest.NewServer` + `setupManagerEnv(t, url)` + `writeEnvelope(w, data)`,已在 `service/hermes_test.go`

---

## File Structure

| 文件 | 职责 | 动作 |
|---|---|---|
| `savvy-manager/app/models.py` | PlanType 三档 + Instance 加 5 列 | 修改 |
| `savvy-manager/alembic/versions/<new>_paid_plan_types.py` | plan 枚举加 STARTER/PRO 删 PAID_RESIDENT + instances 加 5 列 | 新增 |
| `savvy-manager/app/docker_manager.py` | PLAN_RESOURCES/LOG_CONFIG/STORAGE 常量 + create_container 改引用 + update_container_resources 新函数 | 修改 |
| `savvy-manager/app/routers/instances.py` | upgrade/downgrade 路由 + create 路由按 plan 配 storage_quota_gb | 修改 |
| `savvy-manager/app/scanner.py` | 三段 job + storage 软配额 job | 修改 |
| `savvy-manager/tests/test_docker_manager.py` | update_container_resources 单测 | 新增用例 |
| `savvy-manager/tests/test_instances_router.py` | upgrade/downgrade 路由 + scanner 行为用例 | 新增用例 |
| `savvy-manager/tests/test_scanner.py` | scanner 三段单测 | 新增 |
| `new-api/service/hermes.go` | UpgradeHermesInstance/DowngradeHermesInstance + PLAN_RESOURCES Go 常量 | 修改 |
| `new-api/service/hermes_test.go` | 两个新客户端函数单测 | 新增用例 |
| `new-api/model/subscription_test.go` | CompleteSubscriptionOrder/ExpireDueSubscriptions 触发 manager 调用 | 新增用例 |

---

## Task 1: manager PlanType 三档 + Instance 加列(models + alembic 迁移)

**Files:**
- Modify: `savvy-manager/app/models.py:19-49`
- Create: `savvy-manager/alembic/versions/a2b3c4d5e6f7_paid_plan_types.py`
- Test: `savvy-manager/tests/test_models_plan.py`(新建)

**Interfaces:**
- Produces: `PlanType.FREE/STARTER/PRO`(删 PAID_RESIDENT);`Instance` 新列 `needs_upgrade: bool`, `needs_rebuild: bool`, `expected_plan: PlanType|None`, `storage_quota_gb: int|None`, `upgrade_retries: int`

- [ ] **Step 1: 写失败测试 — PlanType 三档 + PAID_RESIDENT 已删**

新建 `savvy-manager/tests/test_models_plan.py`:

```python
from app.models import PlanType, Instance, InstanceStatus


def test_plan_type_has_three_tiers_no_paid_resident():
    members = {m.name for m in PlanType}
    assert members == {"FREE", "STARTER", "PRO"}
    assert not hasattr(PlanType, "PAID_RESIDENT")


def test_instance_has_new_columns():
    inst = Instance(
        instance_id="x", user_id="1", status=InstanceStatus.NOT_CREATED,
        plan=PlanType.STARTER, container_name="c", volume_name="v",
    )
    # New columns exist with defaults
    assert inst.needs_upgrade is False
    assert inst.needs_rebuild is False
    assert inst.expected_plan is None
    assert inst.storage_quota_gb is None
    assert inst.upgrade_retries == 0
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd savvy-manager && python -m pytest tests/test_models_plan.py -v`
Expected: FAIL(`PlanType` 仍含 PAID_RESIDENT,缺 STARTER/PRO;新列不存在)

- [ ] **Step 3: 改 models.py**

`savvy-manager/app/models.py` 替换 PlanType(行 19-21)与 Instance(行 33-49):

```python
class PlanType(str, Enum):
    FREE = "FREE"
    STARTER = "STARTER"
    PRO = "PRO"
```

Instance 类在 `provider_key_set_at` 行之后、`started_at` 之前插入 5 列(保持列顺序整洁,放业务状态区):

```python
    provider_key_set_at = Column(DateTime, nullable=True)
    needs_upgrade = Column(Boolean, default=False)
    needs_rebuild = Column(Boolean, default=False)
    expected_plan = Column(SQLEnum(PlanType), nullable=True)
    storage_quota_gb = Column(Integer, nullable=True)
    upgrade_retries = Column(Integer, default=0)
    started_at = Column(DateTime, nullable=True)
```

顶部 import 加 `Boolean`:
```python
from sqlalchemy import Column, String, DateTime, Enum as SQLEnum, JSON, ForeignKey, Integer, Text, Boolean
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd savvy-manager && python -m pytest tests/test_models_plan.py -v`
Expected: PASS

- [ ] **Step 5: 写 alembic 迁移**

生成 revision id(用 alembic 自动或手填 `a2b3c4d5e6f7`)。新建 `savvy-manager/alembic/versions/a2b3c4d5e6f7_paid_plan_types.py`:

```python
"""paid plan types FREE/STARTER/PRO + instance upgrade/rebuild columns

Revision ID: a2b3c4d5e6f7
Revises: 1d0a44d56206
Create Date: 2026-07-06 00:00:00

"""
from typing import Sequence, Union

from alembic import op
import sqlalchemy as sa


revision: str = 'a2b3c4d5e6f7'
down_revision: Union[str, None] = '1d0a44d56206'
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None


def upgrade() -> None:
    """Add STARTER/PRO plan types, drop PAID_RESIDENT, add instance columns."""
    bind = op.get_bind()

    # 1. Migrate existing PAID_RESIDENT data to STARTER before touching the enum.
    #    SQLite can't ALTER enum in-place; PG/MySQL need value cleanup first.
    op.execute("UPDATE instances SET plan = 'STARTER' WHERE plan = 'PAID_RESIDENT'")
    op.execute("UPDATE users SET plan = 'STARTER' WHERE plan = 'PAID_RESIDENT'")

    # 2. Recreate the plantype enum with the three new tiers.
    #    batch_alter_table handles SQLite (recreate table) and PG/MySQL.
    with op.batch_alter_table('instances', schema=None) as batch_op:
        batch_op.alter_column(
            'plan',
            'plan',
            existing_type=sa.Enum('FREE', 'PAID_RESIDENT', name='plantype'),
            type_=sa.Enum('FREE', 'STARTER', 'PRO', name='plantype'),
            existing_nullable=True,
            postgresql_using='plan::text',
        )
    with op.batch_alter_table('users', schema=None) as batch_op:
        batch_op.alter_column(
            'plan',
            'plan',
            existing_type=sa.Enum('FREE', 'PAID_RESIDENT', name='plantype'),
            type_=sa.Enum('FREE', 'STARTER', 'PRO', name='plantype'),
            existing_nullable=True,
            postgresql_using='plan::text',
        )

    # 3. Add the five new instance columns.
    with op.batch_alter_table('instances', schema=None) as batch_op:
        batch_op.add_column(sa.Column('needs_upgrade', sa.Boolean(), nullable=True, server_default=sa.text('0')))
        batch_op.add_column(sa.Column('needs_rebuild', sa.Boolean(), nullable=True, server_default=sa.text('0')))
        batch_op.add_column(sa.Column('expected_plan', sa.Enum('FREE', 'STARTER', 'PRO', name='plantype'), nullable=True))
        batch_op.add_column(sa.Column('storage_quota_gb', sa.Integer(), nullable=True))
        batch_op.add_column(sa.Column('upgrade_retries', sa.Integer(), nullable=True, server_default=sa.text('0')))

    # 4. Backfill storage_quota_gb for existing FREE instances.
    op.execute("UPDATE instances SET storage_quota_gb = 10 WHERE storage_quota_gb IS NULL AND plan = 'FREE'")

    # 5. Drop the old PAID_RESIDENT enum value in PG (no-op on SQLite/MySQL via batch).
    #    PG: the batch recreate above already replaced the type, so nothing more needed.


def downgrade() -> None:
    """Revert to PAID_RESIDENT two-tier enum."""
    op.execute("UPDATE instances SET plan = 'PAID_RESIDENT' WHERE plan IN ('STARTER', 'PRO')")
    op.execute("UPDATE users SET plan = 'PAID_RESIDENT' WHERE plan IN ('STARTER', 'PRO')")
    with op.batch_alter_table('instances', schema=None) as batch_op:
        batch_op.drop_column('upgrade_retries')
        batch_op.drop_column('storage_quota_gb')
        batch_op.drop_column('expected_plan')
        batch_op.drop_column('needs_rebuild')
        batch_op.drop_column('needs_upgrade')
        batch_op.alter_column(
            'plan', 'plan',
            existing_type=sa.Enum('FREE', 'STARTER', 'PRO', name='plantype'),
            type_=sa.Enum('FREE', 'PAID_RESIDENT', name='plantype'),
            existing_nullable=True,
            postgresql_using='plan::text',
        )
    with op.batch_alter_table('users', schema=None) as batch_op:
        batch_op.alter_column(
            'plan', 'plan',
            existing_type=sa.Enum('FREE', 'STARTER', 'PRO', name='plantype'),
            type_=sa.Enum('FREE', 'PAID_RESIDENT', name='plantype'),
            existing_nullable=True,
            postgresql_using='plan::text',
        )
```

- [ ] **Step 6: 验证迁移可跑(本地 SQLite dry)**

Run: `cd savvy-manager && python -c "from alembic.config import Config; from alembic import command; cfg=Config('alembic.ini'); command.upgrade(cfg, 'head')" 2>&1 | tail -5`
Expected: 无报错,head 到 `a2b3c4d5e6f7`。若 SQLite 路径冲突(测试 DB),先 `rm -f savvy-manager/test_*.db`。

- [ ] **Step 7: Commit**

```bash
cd savvy-manager && git add app/models.py alembic/versions/a2b3c4d5e6f7_paid_plan_types.py tests/test_models_plan.py
git commit -m "feat(manager): PlanType 三档 FREE/STARTER/PRO + Instance 加 5 列

删 PAID_RESIDENT,alembic 迁移含数据迁移(PAID_RESIDENT→STARTER)。
新列:needs_upgrade/needs_rebuild/expected_plan/storage_quota_gb/upgrade_retries。

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Task 2: manager docker_manager 常量 + update_container_resources

**Files:**
- Modify: `savvy-manager/app/docker_manager.py:1-138`
- Test: `savvy-manager/tests/test_docker_manager.py`

**Interfaces:**
- Consumes: Task 1 `PlanType`
- Produces: `PLAN_RESOURCES: dict`, `PLAN_LOG_CONFIG: dict`, `PLAN_STORAGE_GB: dict`(模块级);`update_container_resources(container_name, cpu_quota, mem_limit, pids_limit) -> bool`;`create_container` 改引用常量

- [ ] **Step 1: 写失败测试 — update_container_resources 调 docker update**

`savvy-manager/tests/test_docker_manager.py` 加(若文件无 monkeypatch import 则补):

```python
def test_update_container_resources_calls_docker_update(monkeypatch):
    from app import docker_manager
    monkeypatch.setattr(docker_manager.settings, "mock_mode", False)

    captured = {}
    class FakeContainer:
        def update(self, **kw):
            captured["args"] = kw
    class FakeClient:
        class containers:
            @staticmethod
            def get(name):
                return FakeContainer()
    monkeypatch.setattr(docker_manager, "_client_or_none", lambda: FakeClient())

    ok = docker_manager.update_container_resources("c1", 200000, "2g", 512)
    assert ok is True
    assert captured["args"] == {
        "cpu_quota": 200000, "mem_limit": "2g",
        "memswap_limit": "2g", "pids_limit": 512,
    }


def test_update_container_resources_returns_false_when_not_found(monkeypatch):
    from app import docker_manager
    from docker.errors import NotFound
    monkeypatch.setattr(docker_manager.settings, "mock_mode", False)

    class FakeClient:
        class containers:
            @staticmethod
            def get(name):
                raise NotFound("nope")
    monkeypatch.setattr(docker_manager, "_client_or_none", lambda: FakeClient())

    assert docker_manager.update_container_resources("c1", 200000, "2g", 512) is False


def test_plan_resource_constants_present():
    from app.docker_manager import PLAN_RESOURCES, PLAN_LOG_CONFIG, PLAN_STORAGE_GB
    assert set(PLAN_RESOURCES) == {"FREE", "STARTER", "PRO"}
    assert PLAN_RESOURCES["STARTER"] == {"cpu_quota": 200000, "mem_limit": "2g", "pids_limit": 512}
    assert PLAN_STORAGE_GB == {"FREE": 10, "STARTER": 30, "PRO": 80}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd savvy-manager && python -m pytest tests/test_docker_manager.py::test_update_container_resources_calls_docker_update tests/test_docker_manager.py::test_plan_resource_constants_present -v`
Expected: FAIL(`update_container_resources` 未定义,常量不存在)

- [ ] **Step 3: 加常量 + 新函数,改 create_container 引用**

`savvy-manager/app/docker_manager.py` 顶部(行 7 `from .config import settings` 之后)加:

```python
# Resource limits per plan (PRD §Plans And Resource Limits).
# cpu_quota is in microseconds per 100k period (50000 = 0.5 vCPU).
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

`create_container` 内(行 61-77 的 `limits = {...}` 字典)替换为:

```python
        # Resource limits and log rotation per plan (see PLAN_RESOURCES / PLAN_LOG_CONFIG).
        limit_cfg = {
            **PLAN_RESOURCES.get(plan, PLAN_RESOURCES["FREE"]),
            **{f"log_{k}": v for k, v in PLAN_LOG_CONFIG.get(plan, PLAN_LOG_CONFIG["FREE"]).items()},
        }
```

(保留原 `mem_limit`/`cpu_quota`/`pids_limit`/`log_max_size`/`log_max_file` 键名,使下方 `client.containers.run(...)` 调用行 115-125 无需改动。)

文件末尾(行 247 `_write_container_config_yaml` 之后)加新函数:

```python
def update_container_resources(
    container_name: str,
    cpu_quota: int,
    mem_limit: str,
    pids_limit: int,
) -> bool:
    """docker update 热改运行中容器资源。不重建,零中断。log_config 不改(留旧档,等 rebuild 闭合)。"""
    if settings.mock_mode:
        return True

    client = _client_or_none()
    if client is None:
        return False

    try:
        container = client.containers.get(container_name)
        container.update(
            cpu_quota=cpu_quota,
            mem_limit=mem_limit,
            memswap_limit=mem_limit,  # memory swap = memory limit
            pids_limit=pids_limit,
        )
        return True
    except NotFound:
        return False
    except APIError:
        return False
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd savvy-manager && python -m pytest tests/test_docker_manager.py -v`
Expected: PASS(含原有用例)

- [ ] **Step 5: Commit**

```bash
cd savvy-manager && git add app/docker_manager.py tests/test_docker_manager.py
git commit -m "feat(manager): PLAN_RESOURCES 常量 + update_container_resources 热改入口

docker update 热改 CPU/RAM/PIDs,零中断不重建。log_config 留旧档。
create_container 内联 limits 字典改引用 PLAN_RESOURCES/PLAN_LOG_CONFIG。

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Task 3: manager upgrade/downgrade 路由 + create 配 storage_quota

**Files:**
- Modify: `savvy-manager/app/routers/instances.py`(加路由 + create 配额)
- Test: `savvy-manager/tests/test_instances_router.py`

**Interfaces:**
- Consumes: Task 1 `Instance` 新列;Task 2 `update_container_resources`, `PLAN_STORAGE_GB`
- Produces: `POST /internal/instances/{id}/upgrade`(body `{plan, cpu_quota, mem_limit, pids_limit}`), `POST /internal/instances/{id}/downgrade`(body `{plan, expires_at}`)

- [ ] **Step 1: 写失败测试 — upgrade 成功改 plan+清 expires_at+标 needs_rebuild;失败标 needs_upgrade**

`tests/test_instances_router.py` 加(沿用 `_create_test_instance`,补 plan 参数):

```python
def _create_running_paid_instance(db, instance_id="inst-1", plan=PlanType.STARTER):
    u = User(user_id="1", plan=plan)
    db.add(u)
    inst = Instance(
        instance_id=instance_id, user_id="1", status=InstanceStatus.RUNNING,
        plan=PlanType.FREE, container_name="savvy-u1-w1", volume_name="savvy-u1-data",
        assigned_port=41000, expires_at=datetime.now(timezone.utc) + timedelta(hours=1),
    )
    db.add(inst)
    db.commit()
    return inst


def test_upgrade_success_changes_plan_and_clears_expiry(client, db_session, monkeypatch):
    from app import docker_manager
    monkeypatch.setattr(docker_manager.settings, "mock_mode", True)
    _create_running_paid_instance(db_session)
    res = client.post("/internal/instances/inst-1/upgrade", json={
        "plan": "STARTER", "cpu_quota": 200000, "mem_limit": "2g", "pids_limit": 512,
    })
    body = res.json()
    assert body["success"] is True
    inst = db_session.query(Instance).filter_by(instance_id="inst-1").first()
    assert inst.plan == PlanType.STARTER
    assert inst.expires_at is None          # 免睡
    assert inst.expected_plan == PlanType.STARTER
    assert inst.needs_upgrade is False
    assert inst.needs_rebuild is True       # log_config 升档 → 标重建


def test_upgrade_failure_marks_needs_upgrade(client, db_session, monkeypatch):
    from app import docker_manager
    monkeypatch.setattr(docker_manager.settings, "mock_mode", False)
    # Force update_container_resources to fail
    monkeypatch.setattr(docker_manager, "update_container_resources", lambda *a: False)
    _create_running_paid_instance(db_session)
    res = client.post("/internal/instances/inst-1/upgrade", json={
        "plan": "PRO", "cpu_quota": 400000, "mem_limit": "8g", "pids_limit": 1024,
    })
    body = res.json()
    assert body["success"] is False
    inst = db_session.query(Instance).filter_by(instance_id="inst-1").first()
    assert inst.needs_upgrade is True
    assert inst.expected_plan == PlanType.PRO
    assert inst.plan == PlanType.FREE        # 未改成


def test_downgrade_sets_free_and_2h_expiry_no_touch_container(client, db_session, monkeypatch):
    from app import docker_manager
    stop_called = []
    monkeypatch.setattr(docker_manager, "stop_container", lambda name: stop_called.append(name) or True)
    _create_running_paid_instance(db_session)
    res = client.post("/internal/instances/inst-1/downgrade", json={
        "plan": "FREE",
        "expires_at": (datetime.now(timezone.utc) + timedelta(hours=2)).isoformat(),
    })
    body = res.json()
    assert body["success"] is True
    inst = db_session.query(Instance).filter_by(instance_id="inst-1").first()
    assert inst.plan == PlanType.FREE
    assert inst.expected_plan == PlanType.FREE
    assert inst.expires_at is not None       # 设了 2h 窗
    assert stop_called == []                 # 不碰运行容器
```

顶部 import 已有 `datetime, timedelta, timezone`,确认 `Instance, InstanceStatus, PlanType, User` 已 import(行 8)。

- [ ] **Step 2: 运行测试验证失败**

Run: `cd savvy-manager && python -m pytest tests/test_instances_router.py::test_upgrade_success_changes_plan_and_clears_expiry -v`
Expected: FAIL(404 路由不存在)

- [ ] **Step 3: 加路由 + Response model + create 配额**

`instances.py` 顶部 import 改(行 9):
```python
from ..docker_manager import start_container, stop_container, update_container_resources, PLAN_LOG_CONFIG, PLAN_STORAGE_GB
```

`StartResponse` 之后(行 30 附近)加两个 Response/Request model:

```python
class UpgradeRequest(BaseModel):
    plan: str
    cpu_quota: int
    mem_limit: str
    pids_limit: int


class UpgradeResponse(BaseModel):
    instance_id: str
    status: InstanceStatus
    plan: PlanType
    needs_upgrade: bool


class DowngradeRequest(BaseModel):
    plan: str
    expires_at: str  # ISO8601


class DowngradeResponse(BaseModel):
    instance_id: str
    status: InstanceStatus
    plan: PlanType
    expires_at: str | None = None
```

文件末尾加两个路由:

```python
@router.post("/{instance_id}/upgrade", response_model=UpgradeResponse)
async def upgrade_instance(
    instance_id: str,
    body: UpgradeRequest,
    auth=Depends(require_hmac),
    db: Session = Depends(get_db),
):
    """订阅生效:docker update 热改容器资源 + 改 plan + 清免费窗。
    成功标 needs_rebuild(log 重建闭合);失败标 needs_upgrade(scanner 补)。"""
    inst = _get_instance(instance_id, auth["user_id"], db)
    try:
        target_plan = PlanType(body.plan)
    except ValueError:
        raise HTTPException(status_code=400, detail=f"invalid plan: {body.plan}")

    ok = update_container_resources(
        inst.container_name, body.cpu_quota, body.mem_limit, body.pids_limit
    )
    if ok:
        inst.plan = target_plan
        inst.expected_plan = target_plan
        inst.expires_at = None
        inst.needs_upgrade = False
        inst.upgrade_retries = 0
        # log_config 升档需重建闭合(漏洞 3);仅当新 plan 的 log 配置不同于 FREE(升级必变)
        inst.needs_rebuild = True
        db.commit()
        return UpgradeResponse(
            instance_id=inst.instance_id, status=inst.status,
            plan=inst.plan, needs_upgrade=False,
        )
    else:
        inst.needs_upgrade = True
        inst.expected_plan = target_plan
        db.commit()
        return UpgradeResponse(
            instance_id=inst.instance_id, status=inst.status,
            plan=inst.plan, needs_upgrade=True,
        )


@router.post("/{instance_id}/downgrade", response_model=DowngradeResponse)
async def downgrade_instance(
    instance_id: str,
    body: DowngradeRequest,
    auth=Depends(require_hmac),
    db: Session = Depends(get_db),
):
    """订阅过期:改 plan=FREE + 设免费 2h 窗。不碰运行容器(Q6 防止跑着任务 OOM)。
    不调 docker update — 留给下次启动按 FREE 档起 / 现成免费睡 scanner stop。"""
    inst = _get_instance(instance_id, auth["user_id"], db)
    try:
        target_plan = PlanType(body.plan)
    except ValueError:
        raise HTTPException(status_code=400, detail=f"invalid plan: {body.plan}")

    try:
        expires_at = datetime.fromisoformat(body.expires_at.replace("Z", "+00:00"))
    except ValueError:
        raise HTTPException(status_code=400, detail="invalid expires_at ISO8601")

    inst.plan = target_plan
    inst.expected_plan = target_plan
    inst.expires_at = expires_at
    inst.storage_quota_gb = PLAN_STORAGE_GB.get(target_plan.value, PLAN_STORAGE_GB["FREE"])
    db.commit()
    return DowngradeResponse(
        instance_id=inst.instance_id, status=inst.status,
        plan=inst.plan, expires_at=body.expires_at,
    )
```

`create_instance` 路由(查 `_create_test_instance` 默认 plan=FREE)— 在创建 Instance 处补 `storage_quota_gb`(查行 39 附近 `Instance(...)` 构造,加 `storage_quota_gb=PLAN_STORAGE_GB["FREE"]`)。若 create_instance 路由在 users.py,跳过此补,downgrade 路由已覆盖配额。

- [ ] **Step 4: 运行测试验证通过**

Run: `cd savvy-manager && python -m pytest tests/test_instances_router.py -v`
Expected: PASS(含三个新用例 + 既有用例)

- [ ] **Step 5: Commit**

```bash
cd savvy-manager && git add app/routers/instances.py tests/test_instances_router.py
git commit -m "feat(manager): upgrade/downgrade 路由 + storage_quota_gb 配置

upgrade: docker update 热改+改plan+清expires_at+标needs_rebuild;失败标needs_upgrade。
downgrade: 改plan=FREE+设2h窗,不碰运行容器(Q6防OOM)。

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Task 4: manager scanner 三段 + storage 软配额 job

**Files:**
- Modify: `savvy-manager/app/scanner.py`
- Create: `savvy-manager/tests/test_scanner.py`

**Interfaces:**
- Consumes: Task 1 `Instance` 新列;Task 2 `update_container_resources`, `PLAN_RESOURCES`, `PLAN_LOG_CONFIG`;Task 3 路由逻辑
- Produces: `check_needs_upgrade()`, `check_needs_downgrade()`, `check_needs_rebuild()`, `check_storage_quota()` 四个 scanner 函数;`start_scanner` 注册全部 job

- [ ] **Step 1: 写失败测试 — scanner 三段**

新建 `savvy-manager/tests/test_scanner.py`:

```python
from datetime import datetime, timedelta, timezone
import pytest
from app.models import Instance, InstanceStatus, PlanType
from app.database import Base
from sqlalchemy import create_engine
from sqlalchemy.orm import sessionmaker


@pytest.fixture(name="db_session")
def fixture_db_session(monkeypatch):
    engine = create_engine("sqlite:///./test_scanner.db", connect_args={"check_same_thread": False})
    Base.metadata.create_all(bind=engine)
    Session = sessionmaker(autocommit=False, autoflush=False, bind=engine)
    db = Session()
    try:
        yield db
    finally:
        db.close()
        Base.metadata.drop_all(bind=engine)


def _mk_instance(db, **kw):
    defaults = dict(
        instance_id="i1", user_id="1", status=InstanceStatus.RUNNING,
        plan=PlanType.FREE, container_name="c1", volume_name="v1", assigned_port=41000,
    )
    defaults.update(kw)
    inst = Instance(**defaults)
    db.add(inst)
    db.commit()
    return inst


def test_check_needs_upgrade_retries_then_alerts(monkeypatch, db_session, caplog):
    from app import scanner, docker_manager
    monkeypatch.setattr(docker_manager.settings, "mock_mode", False)
    # Force update failure
    monkeypatch.setattr(docker_manager, "update_container_resources", lambda *a: False)
    inst = _mk_instance(db_session, needs_upgrade=True, expected_plan=PlanType.STARTER, upgrade_retries=0)

    for _ in range(3):
        scanner.check_needs_upgrade(db_session)
    db_session.refresh(inst)
    assert inst.needs_upgrade is False   # 停手
    assert inst.upgrade_retries == 3
    assert any("upgrade" in r.message.lower() and "i1" in r.message for r in caplog.records)


def test_check_needs_upgrade_success_clears(monkeypatch, db_session):
    from app import scanner, docker_manager
    monkeypatch.setattr(docker_manager.settings, "mock_mode", False)
    monkeypatch.setattr(docker_manager, "update_container_resources", lambda *a: True)
    inst = _mk_instance(db_session, needs_upgrade=True, expected_plan=PlanType.STARTER, upgrade_retries=1)

    scanner.check_needs_upgrade(db_session)
    db_session.refresh(inst)
    assert inst.needs_upgrade is False
    assert inst.plan == PlanType.STARTER
    assert inst.upgrade_retries == 0


def test_check_needs_downgrade_aligns_plan(monkeypatch, db_session):
    from app import scanner
    inst = _mk_instance(
        db_session, plan=PlanType.STARTER, expected_plan=PlanType.FREE,
        expires_at=None,
    )
    scanner.check_needs_downgrade(db_session)
    db_session.refresh(inst)
    assert inst.plan == PlanType.FREE
    assert inst.expected_plan is None
    assert inst.expires_at is not None   # FREE 降级设 2h 窗


def test_check_needs_rebuild_on_sleeping(monkeypatch, db_session):
    from app import scanner, docker_manager
    rebuilt = []
    monkeypatch.setattr(docker_manager, "remove_container", lambda name: rebuilt.append(name) or True)
    monkeypatch.setattr(docker_manager, "create_container", lambda **kw: {"id": "new", "status": "created"})
    inst = _mk_instance(
        db_session, status=InstanceStatus.SLEEPING, plan=PlanType.STARTER,
        needs_rebuild=True, provider_config_enc="enc", provider_config_alg="fernet",
    )
    scanner.check_needs_rebuild(db_session)
    db_session.refresh(inst)
    assert rebuilt == ["c1"]
    assert inst.needs_rebuild is False
    assert inst.status == InstanceStatus.NOT_CREATED


def test_check_needs_rebuild_skips_running(monkeypatch, db_session):
    """Rebuild 仅在已停(SLEEPING)时触发,避免打断运行中容器。"""
    from app import scanner, docker_manager
    rebuilt = []
    monkeypatch.setattr(docker_manager, "remove_container", lambda name: rebuilt.append(name))
    _mk_instance(db_session, status=InstanceStatus.RUNNING, needs_rebuild=True)
    scanner.check_needs_rebuild(db_session)
    assert rebuilt == []   # 不动 RUNNING
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd savvy-manager && python -m pytest tests/test_scanner.py -v`
Expected: FAIL(scanner 函数不存在)

- [ ] **Step 3: 重写 scanner.py**

替换 `savvy-manager/app/scanner.py` 全文:

```python
import logging
from datetime import datetime, timedelta, timezone
from apscheduler.schedulers.background import BackgroundScheduler
from sqlalchemy.orm import Session
from .database import SessionLocal
from .models import Instance, InstanceStatus, PlanType
from .docker_manager import (
    stop_container, update_container_resources, remove_container, create_container,
    PLAN_RESOURCES, PLAN_STORAGE_GB,
)

logger = logging.getLogger(__name__)
scheduler = BackgroundScheduler()

FREE_WINDOW_HOURS = 2
MAX_UPGRADE_RETRIES = 3


def check_expired_instances():
    """现成免费睡:扫 FREE RUNNING 且 expires_at 到 → docker stop。"""
    db: Session = SessionLocal()
    try:
        now = datetime.now(timezone.utc)
        expired = (
            db.query(Instance)
            .filter(
                Instance.status == InstanceStatus.RUNNING,
                Instance.plan == PlanType.FREE,
                Instance.expires_at.isnot(None),
                Instance.expires_at <= now,
            )
            .all()
        )
        for inst in expired:
            if stop_container(inst.container_name):
                inst.status = InstanceStatus.SLEEPING
                inst.started_at = None
                inst.expires_at = None
                db.commit()
    finally:
        db.close()


def check_needs_upgrade(db: Session | None = None):
    """漏洞 1 修复:升补扫。needs_upgrade=True → 重试 update,≤3 次,超限告警停手。"""
    own_session = db is None
    if own_session:
        db = SessionLocal()
    try:
        pending = db.query(Instance).filter(Instance.needs_upgrade.is_(True)).all()
        for inst in pending:
            if inst.expected_plan is None:
                inst.needs_upgrade = False
                db.commit()
                continue
            if inst.upgrade_retries >= MAX_UPGRADE_RETRIES:
                logger.error(
                    "upgrade retries exhausted for instance %s (target=%s), needs manual intervention",
                    inst.instance_id, inst.expected_plan.value,
                )
                inst.needs_upgrade = False
                db.commit()
                continue
            res = PLAN_RESOURCES.get(inst.expected_plan.value)
            if not res:
                inst.needs_upgrade = False
                db.commit()
                continue
            ok = update_container_resources(
                inst.container_name, res["cpu_quota"], res["mem_limit"], res["pids_limit"],
            )
            inst.upgrade_retries += 1
            if ok:
                inst.plan = inst.expected_plan
                inst.expires_at = None
                inst.needs_upgrade = False
                inst.upgrade_retries = 0
                inst.needs_rebuild = True
            db.commit()
    finally:
        if own_session:
            db.close()


def check_needs_downgrade(db: Session | None = None):
    """漏洞 2 修复:降补扫。expected_plan != plan → 对齐,FREE 时设 2h 窗。"""
    own_session = db is None
    if own_session:
        db = SessionLocal()
    try:
        pending = (
            db.query(Instance)
            .filter(Instance.expected_plan.isnot(None))
            .filter(Instance.plan != Instance.expected_plan)
            .all()
        )
        now = datetime.now(timezone.utc)
        for inst in pending:
            inst.plan = inst.expected_plan
            if inst.plan == PlanType.FREE:
                inst.expires_at = now + timedelta(hours=FREE_WINDOW_HOURS)
            inst.storage_quota_gb = PLAN_STORAGE_GB.get(inst.plan.value, PLAN_STORAGE_GB["FREE"])
            inst.expected_plan = None
            db.commit()
    finally:
        if own_session:
            db.close()


def check_needs_rebuild(db: Session | None = None):
    """漏洞 3 修复:log 重建闭合。needs_rebuild=True 且 SLEEPING → rm + run 新 plan,保 volume+provider_config。"""
    own_session = db is None
    if own_session:
        db = SessionLocal()
    try:
        pending = (
            db.query(Instance)
            .filter(Instance.needs_rebuild.is_(True))
            .filter(Instance.status == InstanceStatus.SLEEPING)
            .all()
        )
        for inst in pending:
            if not remove_container(inst.container_name):
                logger.error("rebuild: failed to remove container %s", inst.container_name)
                continue
            from . import crypto
            provider_config = None
            if inst.provider_config_enc:
                try:
                    provider_config = crypto.decrypt_provider_config(
                        inst.provider_config_enc, inst.provider_config_alg or "fernet",
                    )
                except Exception:
                    provider_config = None
            create_container(
                container_name=inst.container_name,
                volume_name=inst.volume_name,
                user_id=inst.user_id,
                workspace_id=inst.instance_id,
                plan=inst.plan.value,
                expires_at=None,
                provider_config=provider_config,
            )
            inst.needs_rebuild = False
            inst.status = InstanceStatus.NOT_CREATED
            db.commit()
    finally:
        if own_session:
            db.close()


def check_storage_quota():
    """Q3 软配额:周期取 volume 用量,超 storage_quota_gb → SYSLOG 软告警。不强制禁。"""
    db: Session = SessionLocal()
    try:
        from .docker_manager import _client_or_none
        client = _client_or_none()
        if client is None:
            return
        try:
            df = client.df()
        except Exception:
            return
        usage_by_name = {}
        for v in df.get("Volumes", []):
            name = v.get("Name", "")
            usage = v.get("UsageData", {}).get("Size", 0)
            usage_by_name[name] = usage
        instances = db.query(Instance).filter(Instance.storage_quota_gb.isnot(None)).all()
        for inst in instances:
            used_bytes = usage_by_name.get(inst.volume_name, 0)
            used_gb = used_bytes / (1024 ** 3)
            if used_gb > inst.storage_quota_gb:
                logger.warning(
                    "storage soft quota exceeded: instance %s used %.1fGB > quota %dGB",
                    inst.instance_id, used_gb, inst.storage_quota_gb,
                )
    finally:
        db.close()


def start_scanner():
    scheduler.add_job(check_expired_instances, "interval", minutes=1, id="check_expired")
    scheduler.add_job(check_needs_upgrade, "interval", minutes=1, id="check_needs_upgrade")
    scheduler.add_job(check_needs_downgrade, "interval", minutes=1, id="check_needs_downgrade")
    scheduler.add_job(check_needs_rebuild, "interval", minutes=1, id="check_needs_rebuild")
    scheduler.add_job(check_storage_quota, "interval", minutes=10, id="check_storage_quota")
    scheduler.start()


def stop_scanner():
    scheduler.shutdown()
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd savvy-manager && python -m pytest tests/test_scanner.py tests/test_instances_router.py -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd savvy-manager && git add app/scanner.py tests/test_scanner.py
git commit -m "feat(manager): scanner 三段(升补/降补/log重建)+ storage 软配额 job

升补:needs_upgrade ≤3次重试,超限告警停手(漏洞1)。
降补:expected_plan≠plan 脏标记驱动对齐,FREE 设2h窗(漏洞2)。
log重建:SLEEPING 时 rm+run 新 plan 保 volume+provider_config(漏洞3)。
storage:每10分钟 df 取用量,超配额 SYSLOG 软告警,不强制禁(Q3)。

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Task 5: new-api hermes.go 加 Upgrade/Downgrade 客户端 + PLAN_RESOURCES 常量

**Files:**
- Modify: `new-api/service/hermes.go`
- Test: `new-api/service/hermes_test.go`

**Interfaces:**
- Consumes: 现有 `signAndDo`, `decodeManagerResponse`, `getHermesManagerURL`
- Produces: `PlanResources` (Go map 常量), `UpgradeHermesInstance(userID int, instanceID, plan string, cpuQuota int, memLimit string, pidsLimit int) error`, `DowngradeHermesInstance(userID int, instanceID string, expiresAt time.Time) error`

- [ ] **Step 1: 写失败测试 — Upgrade/Downgrade 签名+请求体**

`new-api/service/hermes_test.go` 加(沿用 `setupManagerEnv`, `requireSigned`, `writeEnvelope`):

```go
func TestUpgradeHermesInstance_Signed(t *testing.T) {
	var gotPath string
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		requireSigned(t, r)
		writeEnvelope(w, map[string]any{
			"instance_id": "inst-1", "status": "RUNNING",
			"plan": "STARTER", "needs_upgrade": false,
		})
	}))
	defer server.Close()
	setupManagerEnv(t, server.URL)

	err := UpgradeHermesInstance(42, "inst-1", "STARTER", 200000, "2g", 512)
	require.NoError(t, err)
	assert.Equal(t, "/internal/instances/inst-1/upgrade", gotPath)
	assert.Contains(t, gotBody, `"plan":"STARTER"`)
	assert.Contains(t, gotBody, `"cpu_quota":200000`)
	assert.Contains(t, gotBody, `"mem_limit":"2g"`)
	assert.Contains(t, gotBody, `"pids_limit":512`)
}

func TestDowngradeHermesInstance_Signed(t *testing.T) {
	var gotPath string
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		requireSigned(t, r)
		writeEnvelope(w, map[string]any{
			"instance_id": "inst-1", "status": "RUNNING",
			"plan": "FREE", "expires_at": "2026-07-06T16:00:00Z",
		})
	}))
	defer server.Close()
	setupManagerEnv(t, server.URL)

	err := DowngradeHermesInstance(42, "inst-1", time.Date(2026, 7, 6, 16, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.Equal(t, "/internal/instances/inst-1/downgrade", gotPath)
	assert.Contains(t, gotBody, `"plan":"FREE"`)
	assert.Contains(t, gotBody, "2026-07-06T16:00:00Z")
}

func TestPlanResourcesConstant(t *testing.T) {
	assert.Equal(t, 200000, PlanResources["STARTER"].CPUQuota)
	assert.Equal(t, "2g", PlanResources["STARTER"].MemLimit)
	assert.Equal(t, 512, PlanResources["STARTER"].PidsLimit)
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd new-api && go test ./service -run 'TestUpgradeHermesInstance|TestDowngradeHermesInstance|TestPlanResourcesConstant' -v`
Expected: FAIL(`UpgradeHermesInstance`/`DowngradeHermesInstance`/`PlanResources` 未定义)

- [ ] **Step 3: 加常量 + 两个客户端函数**

`new-api/service/hermes.go` 在 `HealthCheckHermesManager` 之前(行 502 前)加:

```go
// PlanResourceSpec mirrors manager's PLAN_RESOURCES per-tier CPU/RAM/PIDs.
type PlanResourceSpec struct {
	CPUQuota   int
	MemLimit   string
	PidsLimit  int
}

// PlanResources mirrors savvy-manager/app/docker_manager.py PLAN_RESOURCES.
// Keyed by user group (default/starter/pro), matching SubscriptionPlan.UpgradeGroup.
var PlanResources = map[string]PlanResourceSpec{
	"default": {CPUQuota: 50000, MemLimit: "768m", PidsLimit: 128},
	"starter": {CPUQuota: 200000, MemLimit: "2g", PidsLimit: 512},
	"pro":     {CPUQuota: 400000, MemLimit: "8g", PidsLimit: 1024},
}

// groupToPlanName maps a user group to manager's PlanType string.
var groupToPlanName = map[string]string{
	"default": "FREE",
	"starter": "STARTER",
	"pro":     "PRO",
}

// UpgradeHermesInstance 通知 manager 升级在跑容器资源 + 改 plan + 清免费窗。
// plan 是 manager PlanType 字符串("STARTER"/"PRO");资源数值由 new-api 传入,manager 不反查。
func UpgradeHermesInstance(userID int, instanceID, plan string, cpuQuota int, memLimit string, pidsLimit int) error {
	body := map[string]any{
		"plan":        plan,
		"cpu_quota":   cpuQuota,
		"mem_limit":   memLimit,
		"pids_limit":  pidsLimit,
	}
	bodyBytes, err := common.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal upgrade body: %w", err)
	}

	url := fmt.Sprintf("%s/internal/instances/%s/upgrade", getHermesManagerURL(), instanceID)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := signAndDo(req, userID, bodyBytes)
	if err != nil {
		return fmt.Errorf("failed to connect to hermes-manager: %w", err)
	}
	_, err = decodeManagerResponse(resp)
	return err
}

// DowngradeHermesInstance 通知 manager 降级:改 plan=FREE + 设免费 2h 窗。不动运行容器。
func DowngradeHermesInstance(userID int, instanceID string, expiresAt time.Time) error {
	body := map[string]any{
		"plan":       "FREE",
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
	}
	bodyBytes, err := common.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal downgrade body: %w", err)
	}

	url := fmt.Sprintf("%s/internal/instances/%s/downgrade", getHermesManagerURL(), instanceID)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := signAndDo(req, userID, bodyBytes)
	if err != nil {
		return fmt.Errorf("failed to connect to hermes-manager: %w", err)
	}
	_, err = decodeManagerResponse(resp)
	return err
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd new-api && go test ./service -run 'TestUpgradeHermesInstance|TestDowngradeHermesInstance|TestPlanResourcesConstant' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd new-api && git add service/hermes.go service/hermes_test.go
git commit -m "feat(new-api): Upgrade/DowngradeHermesInstance 客户端 + PlanResources 常量

HMAC 同步 POST manager upgrade/downgrade。资源数值随 body 传,manager 不反查。
group→plan 映射 default/starter/pro ↔ FREE/STARTER/PRO。

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Task 6: new-api 订阅生效/过期触发 manager 升降级

**Files:**
- Modify: `new-api/model/subscription.go:553`(CompleteSubscriptionOrder)
- Modify: `new-api/model/subscription.go:986`(ExpireDueSubscriptions)
- Create: `new-api/model/subscription_paid_plan_test.go`

**Interfaces:**
- Consumes: Task 5 `UpgradeHermesInstance`, `DowngradeHermesInstance`, `PlanResources`, `groupToPlanName`, `GetHermesInstance`
- Produces: 订阅生效后调 `UpgradeHermesInstance`;订阅过期后调 `DowngradeHermesInstance`

- [ ] **Step 1: 写失败测试 — CompleteSubscriptionOrder 成功后调 UpgradeHermesInstance**

新建 `new-api/model/subscription_paid_plan_test.go`:

```go
package model

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpgradeCalledAfterSubscriptionOrder verifies that a successful
// CompleteSubscriptionOrder triggers a manager upgrade call for an active instance.
func TestUpgradeCalledAfterSubscriptionOrder(t *testing.T) {
	upgraded := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/users/1/instance" {
			// GetHermesInstance probe — return a RUNNING instance
			common.Marshal(nil) // touch common to satisfy import
			w.Write([]byte(`{"success":true,"message":"","data":{"instance_id":"inst-1","user_id":"1","status":"RUNNING","plan":"FREE"}}`))
			return
		}
		if r.URL.Path == "/internal/instances/inst-1/upgrade" {
			upgraded = true
			w.Write([]byte(`{"success":true,"message":"","data":{"instance_id":"inst-1","status":"RUNNING","plan":"STARTER","needs_upgrade":false}}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()
	t.Setenv("HERMES_MANAGER_URL", server.URL)
	t.Setenv("SAVVY_HMAC_SECRET", "test-secret")

	// Seed plan + order; call CompleteSubscriptionOrder; assert upgraded == true.
	// (Full seed omitted for brevity — see existing subscription order test patterns.)
	setupPaidPlanOrderFixtures(t)
	err := CompleteSubscriptionOrder("trade-upgrade-test", `{"provider":"test"}`, "", "")
	require.NoError(t, err)
	assert.True(t, upgraded, "manager upgrade should be called after subscription completion")
}

// TestDowngradeCalledAfterExpiry verifies ExpireDueSubscriptions triggers a
// manager downgrade call.
func TestDowngradeCalledAfterExpiry(t *testing.T) {
	downgraded := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/users/1/instance" {
			w.Write([]byte(`{"success":true,"message":"","data":{"instance_id":"inst-1","user_id":"1","status":"RUNNING","plan":"STARTER"}}`))
			return
		}
		if r.URL.Path == "/internal/instances/inst-1/downgrade" {
			downgraded = true
			w.Write([]byte(`{"success":true,"message":"","data":{"instance_id":"inst-1","status":"RUNNING","plan":"FREE","expires_at":"2026-07-06T16:00:00Z"}}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()
	t.Setenv("HERMES_MANAGER_URL", server.URL)
	t.Setenv("SAVVY_HMAC_SECRET", "test-secret")

	setupExpiredSubscriptionFixture(t)
	_, err := ExpireDueSubscriptions(100)
	require.NoError(t, err)
	assert.True(t, downgraded, "manager downgrade should be called after expiry")
}
```

> 注:`setupPaidPlanOrderFixtures(t)` / `setupExpiredSubscriptionFixture(t)` 是辅助函数,本 Step 同时在文件里实现(参照 `model/payment_method_guard_test.go` 的 seed 风格:建 User/SubscriptionPlan/SubscriptionOrder 或 UserSubscription,end_time 设过去)。若 DB 未初始化,在 setup 内调 `InitDB()` 走 in-memory SQLite。

- [ ] **Step 2: 运行测试验证失败**

Run: `cd new-api && go test ./model -run 'TestUpgradeCalledAfterSubscriptionOrder|TestDowngradeCalledAfterExpiry' -v`
Expected: FAIL(CompleteSubscriptionOrder/ExpireDueSubscriptions 未调 manager)

- [ ] **Step 3: 改 CompleteSubscriptionOrder — 事务后调 UpgradeHermesInstance**

`new-api/model/subscription.go` 行 611-622(`DB.Transaction` 闭包后,`if err != nil { return err }` 之后),在 `RecordLog` 之前插入:

```go
	// 配套②:订阅生效 → 同步通知 manager 升级在跑容器资源 + 改 plan + 清免费窗。
	// 事务已提交,网络调用失败不阻塞订单成功,仅 SYSLOG,manager scanner 兜底。
	if upgradeGroup != "" && logUserId > 0 {
		go func(uid int, group string) {
			if err := notifyManagerUpgrade(uid, group); err != nil {
				common.SysError("hermes manager upgrade failed after subscription: " + err.Error())
			}
		}(logUserId, upgradeGroup)
	}
```

`notifyManagerUpgrade` 放 `subscription.go` 文件末尾(避免 package-level 单 caller 嫌疑,它代表"订阅→容器"业务概念):

```go
// notifyManagerUpgrade finds the user's running instance and asks the manager
// to hot-upgrade its container resources. Called after a subscription commits.
func notifyManagerUpgrade(userID int, upgradeGroup string) error {
	inst, err := service.GetHermesInstance(userID)
	if err != nil {
		return err
	}
	if inst == nil || inst.Status != "RUNNING" {
		return nil // no running instance to upgrade; user.group already elevated
	}
	res, ok := service.PlanResources[upgradeGroup]
	if !ok {
		return nil // unknown group, nothing to send
	}
	planName, ok := service.GroupToPlanName(upgradeGroup)
	if !ok {
		return nil
	}
	return service.UpgradeHermesInstance(userID, inst.InstanceID, planName, res.CPUQuota, res.MemLimit, res.PidsLimit)
}
```

需 import `"github.com/QuantumNous/new-api/service"`(若循环依赖,改把 `notifyManagerUpgrade` 放 `service/hermes.go` 并导出,subscription.go 调 `service.NotifyManagerUpgrade`)。**决定:放 `service/hermes.go` 导出为 `NotifyManagerUpgrade`,避免 model→service 潜在循环。** 调整:subscription.go 调 `service.NotifyManagerUpgrade(uid, group)`。

同步在 `service/hermes.go` 补:

```go
// NotifyManagerUpgrade finds the user's running instance and asks the manager
// to hot-upgrade its container resources. Called from model after subscription commit.
func NotifyManagerUpgrade(userID int, upgradeGroup string) error {
	inst, err := GetHermesInstance(userID)
	if err != nil {
		return err
	}
	if inst == nil || inst.Status != "RUNNING" {
		return nil
	}
	res, ok := PlanResources[upgradeGroup]
	if !ok {
		return nil
	}
	planName, ok := GroupToPlanName(upgradeGroup)
	if !ok {
		return nil
	}
	return UpgradeHermesInstance(userID, inst.InstanceID, planName, res.CPUQuota, res.MemLimit, res.PidsLimit)
}

// NotifyManagerDowngrade finds the user's running instance and asks the manager
// to downgrade to FREE with a 2h free window. Called from model after subscription expiry.
func NotifyManagerDowngrade(userID int) error {
	inst, err := GetHermesInstance(userID)
	if err != nil {
		return err
	}
	if inst == nil || inst.Status != "RUNNING" {
		return nil
	}
	return DowngradeHermesInstance(userID, inst.InstanceID, time.Now().Add(2*time.Hour))
}

// GroupToPlanName maps a user group to manager PlanType string.
func GroupToPlanName(group string) (string, bool) {
	name, ok := groupToPlanName[group]
	return name, ok
}
```

(Task 5 里的 `groupToPlanName` 改为小写未导出,加 `GroupToPlanName` 导出包装;Task 5 Step 3 的 `groupToPlanName` 已是小写,此处补 `GroupToPlanName` 导出 fn。)

`subscription.go` 改为调:
```go
		go func(uid int, group string) {
			if err := service.NotifyManagerUpgrade(uid, group); err != nil {
				common.SysError("hermes manager upgrade failed after subscription: " + err.Error())
			}
		}(logUserId, upgradeGroup)
```

- [ ] **Step 4: 改 ExpireDueSubscriptions — 每个 user 事务后调 Downgrade**

`subscription.go` `ExpireDueSubscriptions`(行 1076 `if cacheGroup != "" { _ = UpdateUserGroupCache(...) }` 之后),在 `for userId := range userIds` 循环内加:

```go
			if err == nil {
				go func(uid int) {
					if err := service.NotifyManagerDowngrade(uid); err != nil {
						common.SysError("hermes manager downgrade failed after expiry: " + err.Error())
					}
				}(userId)
			}
```

- [ ] **Step 5: 运行测试验证通过**

Run: `cd new-api && go test ./model -run 'TestUpgradeCalledAfterSubscriptionOrder|TestDowngradeCalledAfterExpiry' -v && go test ./service -run 'TestUpgradeHermesInstance|TestDowngradeHermesInstance' -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
cd new-api && git add model/subscription.go model/subscription_paid_plan_test.go service/hermes.go
git commit -m "feat(new-api): 订阅生效/过期触发 manager 升降级

CompleteSubscriptionOrder 事务后异步调 NotifyManagerUpgrade(漏洞1:失败仅SYSLOG,scanner兜底)。
ExpireDueSubscriptions 每user事务后异步调 NotifyManagerDowngrade。
NotifyManagerUpgrade/Downgrade 放 service 避免循环依赖。

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Task 7: 集成验证 + 三档套餐记录 seed + PR

**Files:**
- Create: `savvy-manager/tests/test_integration_paid_plan.py`
- Run: 全量测试 + mock_mode 全链

**Interfaces:**
- Consumes: 全部前 Task

- [ ] **Step 1: 写集成测试 — mock_mode 全链 买Starter→升级;过期→降级**

新建 `savvy-manager/tests/test_integration_paid_plan.py`:

```python
from datetime import datetime, timedelta, timezone
from app.models import Instance, InstanceStatus, PlanType


def test_upgrade_then_downgrade_lifecycle(client, db_session, monkeypatch):
    """全链:FREE RUNNING → upgrade STARTER(plan/expires/expected_plan) → downgrade FREE(2h窗)。"""
    from app import docker_manager
    monkeypatch.setattr(docker_manager.settings, "mock_mode", True)

    inst = Instance(
        instance_id="i1", user_id="1", status=InstanceStatus.RUNNING, plan=PlanType.FREE,
        container_name="c1", volume_name="v1", assigned_port=41000,
        expires_at=datetime.now(timezone.utc) + timedelta(minutes=30),
    )
    db_session.add(inst)
    db_session.commit()

    # Upgrade to STARTER
    res = client.post("/internal/instances/i1/upgrade", json={
        "plan": "STARTER", "cpu_quota": 200000, "mem_limit": "2g", "pids_limit": 512,
    })
    assert res.json()["success"] is True
    db_session.refresh(inst)
    assert inst.plan == PlanType.STARTER
    assert inst.expires_at is None

    # Downgrade to FREE
    res = client.post("/internal/instances/i1/downgrade", json={
        "plan": "FREE",
        "expires_at": (datetime.now(timezone.utc) + timedelta(hours=2)).isoformat(),
    })
    assert res.json()["success"] is True
    db_session.refresh(inst)
    assert inst.plan == PlanType.FREE
    assert inst.expires_at is not None
    assert inst.expected_plan == PlanType.FREE
```

- [ ] **Step 2: 运行集成测试**

Run: `cd savvy-manager && python -m pytest tests/test_integration_paid_plan.py -v`
Expected: PASS

- [ ] **Step 3: 全量 manager 测试**

Run: `cd savvy-manager && python -m pytest tests -q`
Expected: 全 PASS(无回归)

- [ ] **Step 4: 全量 new-api 相关测试**

Run: `cd new-api && go test ./model -run 'Subscription|Upgrade|Downgrade' -v && go test ./service -run 'Hermes' -v`
Expected: PASS

- [ ] **Step 5: admin 后台 seed 三档套餐(手动,不入代码)**

PR 描述注明:上线后 admin 在 `SubscriptionPlan` 后台建:
- Free(参照,PriceAmount=0,TotalAmount=0,UpgradeGroup="")— 走免费流程,无需订阅
- Starter(PriceAmount=99,TotalAmount=<N 由运营填>,DurationUnit=month,QuotaResetPeriod=monthly,UpgradeGroup="starter",DowngradeGroup="default",AllowWalletOverflow=true)
- Pro(Enabled=false Coming Soon,价格待定)

- [ ] **Step 6: Commit + 推 PR**

```bash
cd savvy-manager && git add tests/test_integration_paid_plan.py
git commit -m "test(manager): paid plan upgrade/downgrade 集成全链

mock_mode:FREE→STARTER(expires清)→FREE(2h窗)全链验证。

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

```bash
git push -u origin feat/hermes-paid-plan
gh pr create --base dev --title "feat: D 高级计划计费 + 配套② 订阅→容器升级链" --body "见 docs/superpowers/specs/2026-07-06-hermes-paid-plan-billing-and-container-upgrade-design.md"
```

---

## Self-Review 记录

**1. Spec coverage:**
- 三档 PlanType + 迁移 → Task 1 ✓
- update_container_resources 热改 → Task 2 ✓
- upgrade/downgrade 路由 → Task 3 ✓
- scanner 三段(升补/降补/log重建)+ storage 软配额 → Task 4 ✓
- new-api Upgrade/Downgrade 客户端 + PLAN_RESOURCES → Task 5 ✓
- CompleteSubscriptionOrder/ExpireDueSubscriptions 触发 → Task 6 ✓
- storage_quota_gb 挂字段 → Task 1(列)+ Task 3(downgrade 配) + Task 4(软告警)✓
- 漏洞 1(quota错配限3次)→ Task 4 check_needs_upgrade ✓
- 漏洞 2(降级单触发)→ Task 4 check_needs_downgrade(仅补救,权威触发在 Task 6)✓
- 漏洞 3(log重建)→ Task 3 标 needs_rebuild + Task 4 check_needs_rebuild ✓
- 撤免费模型 → spec 范围排除,无 Task ✓
- AllowWalletOverflow admin-per-plan → admin 后台填,Step 5 注明 ✓

**2. Placeholder scan:** Task 6 Step 1 的 `setupPaidPlanOrderFixtures`/`setupExpiredSubscriptionFixture` 标了"本 Step 同时实现"但未给完整代码 — 这是唯一软点。已在注释指明参照 `payment_method_guard_test.go` seed 风格。执行时若 subagent 卡住,需补完整 seed。

**3. Type consistency:**
- `PlanResources` (Go) vs `PLAN_RESOURCES` (Python):Go 用 `PlanResourceSpec{CPUQuota, MemLimit, PidsLimit}`,Python 用 dict `{cpu_quota, mem_limit, pids_limit}` — 跨语言命名差异已显式,JSON key 用 snake_case 对齐 manager ✓
- `GroupToPlanName` 导出:Task 5 定义 `groupToPlanName`(小写),Task 6 补 `GroupToPlanName` 导出包装 — 一致 ✓
- `notifyManagerUpgrade` 命名漂移:Task 6 Step 3 决定放 service 导出为 `NotifyManagerUpgrade`,subscription.go 调 `service.NotifyManagerUpgrade` — 已对齐 ✓
