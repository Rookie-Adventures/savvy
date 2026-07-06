# Hermes 启动 UX 修复 + 免费时长 2h — 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修 Hermes workspace 控制台三个 UX bug(免费计时显示、密钥弹窗仅在首启、唤醒无 ready 延时导致空壳)+ 免费档时长 3h→2h 写死。

**Architecture:** A1 前端显示逻辑修(后端 remainingMinutes 链路先诊断钉死真因再修)。A2 前端密钥 Dialog 唤醒时不渲染密钥输入框。A3 manager `start_container` 加 ready 轮询 + 8s 缓冲;前端启动调用期间全 loading + 首启/唤醒差异化文案。A4 manager 一行时长 + PRD 同步。

**Tech Stack:** savvy-manager(Python/FastAPI/Docker SDK,pytest),new-api 白标前端(React 19/TypeScript,`web/default/src/features/hermes/`),new-api 后端(Go/Gin,hermes.go)。

## Global Constraints

- **不碰 hermes-workspace 源码**(`hermes-workspace/` 是 Nous 上游仓,改它造白标维护区)
- **不碰 new-api 后端计费/订阅/支付**(B/D 配套② 另线)
- **JSON 包装**: new-api 后端任何 JSON 操作走 `common.Marshal/Unmarshal`,禁直用 `encoding/json`(项目铁规,见 new-api/CLAUDE.md)
- **protected**: 不动 `new-api`/`QuantumNous` 品牌引用(new-api/CLAUDE.md 保护项)
- **DB 兼容**: new-api 后端若碰 DB 须 SQLite/MySQL/PG 三库兼容(本 plan 后端改动不在 DB 层,此条前置告警)
- **测试库**: manager 用 pytest(`savvy-manager/tests/`),前端 React 用现有测试惯例
- **分支**: `feat/hermes-startup-ux`(已在),每 Task 末 commit,跑通 PR 合 dev
- **免费时长写死 2h**,不可配(YAGNI)

---

## File Structure

| 文件 | 职责 | 动作 |
|---|---|---|
| `savvy-manager/app/docker_manager.py` | docker 容器生命周期,start_container 加 ready 探测 | 修改 |
| `savvy-manager/app/routers/instances.py:108` | 免费时长 hours=3→2 | 修改 1 行 |
| `savvy-manager/tests/test_docker_manager.py` | start_container ready 轮询单测 | 新增用例 |
| `savvy-manager/tests/test_instances_router.py` | start_instance 时长/态时序用例 | 新增用例 |
| `new-api/controller/hermes.go:32-47` | toVO remainingMinutes 链路(A1 诊断后修) | 修改 |
| `new-api/web/default/src/features/hermes/index.tsx` | 密钥 Dialog 唤醒分支(A2)+ 启动 loading/文案(A3)+ 倒计时显示对(A1 验) | 修改 |
| `docs/specs/hermes-saas-platform-prd.md:77` | "3 hours per start" → "2 hours per start" | 修改 |

---

## Task 1: A4 — 免费时长 3h→2h + PRD 同步(最小启动项)

**Files:**
- Modify: `savvy-manager/app/routers/instances.py:108`
- Modify: `docs/specs/hermes-saas-platform-prd.md:77`
- Test: `savvy-manager/tests/test_instances_router.py`

**Interfaces:**
- Produces: FREE 容器 start 后 `expires_at = started_at + 2h`(原 3h)

- [ ] **Step 1: 写失败测试 — FREE 新启动 expires_at = now+2h**

在 `savvy-manager/tests/test_instances_router.py` 加(沿用文件现有 fixture 风格;若文件用 `client`/`db` fixture 则用同名,否则按现有 monkeypatch 风格):

```python
def test_free_start_sets_2h_expiry(client, db, monkeypatch):
    # 复用文件现有 create_instance / start_instance 调用样板
    # 断言 expires_at 距 started_at 约 2 小时(容差 ±60s 防 CI 抖动)
    inst = _create_free_instance(db)  # 文件现有 helper,无则照邻例造 NOT_CREATED FREE
    resp = client.post(f"/internal/instances/{inst.instance_id}/start",
                       json={"provider_api_key": "sk-test"},
                       headers=_hmac_headers())
    assert resp.status_code == 200
    db.refresh(inst)
    assert inst.expires_at is not None
    delta = inst.expires_at - inst.started_at
    assert 7100 <= delta.total_seconds() <= 7260  # 2h ± 抖动(2*3600=7200)
```

- [ ] **Step 2: 跑测试验失败**

Run: `cd savvy-manager && python -m pytest tests/test_instances_router.py::test_free_start_sets_2h_expiry -v`
Expected: FAIL — 当前 `timedelta(hours=3)` → delta≈10800s,assert 7100-7260 不成立

- [ ] **Step 3: 改 instances.py:108**

```python
# 原:        expires_at = now + timedelta(hours=3)
# 新:
        expires_at = now + timedelta(hours=2)
```

- [ ] **Step 4: 跑测试验通过**

Run: `cd savvy-manager && python -m pytest tests/test_instances_router.py::test_free_start_sets_2h_expiry -v`
Expected: PASS

- [ ] **Step 5: 同步 PRD**

`docs/specs/hermes-saas-platform-prd.md:77` 表内 "3 hours per start, then sleep" → "2 hours per start, then sleep"(Runtime 列,Free 行)。同步 liberal 上方第 88-94 行 "Free plan behavior" 描述里如有 "3 hours" 字样亦改 2(若第 91 行 `expires_at = started_at + 3h` 有则改 `+ 2h`)。

- [ ] **Step 6: 跑 manager 全测试防回归**

Run: `cd savvy-manager && python -m pytest tests -q`
Expected: 全绿(本改仅一字面常数,不应破其他)

- [ ] **Step 7: Commit**

```bash
git add savvy-manager/app/routers/instances.py savvy-manager/tests/test_instances_router.py docs/specs/hermes-saas-platform-prd.md
git commit -m "fix(manager): free session 3h→2h + PRD sync"
```

---

## Task 2: A3 — manager start_container ready 轮询 + 8s 缓冲

**Files:**
- Modify: `savvy-manager/app/docker_manager.py:156-172`(start_container 函数身)
- Test: `savvy-manager/tests/test_docker_manager.py`

**Interfaces:**
- Consumes: 现有 `_client_or_none()`、`settings.mock_mode`、`client.containers.get(name)`
- Produces: `start_container(name) -> bool` 语义变为"docker start + 等 status==running + 8s 缓冲后返 True";超时 30s 仍非 running 也返 True 不卡死

**关键设计**: 轮询 `container.reload()` 读 status;running 即停;最多 5 次 ×1s;再 `time.sleep(8)` 固定缓冲(workspace node server-entry listen :3000 要时间);超时返 True(不卡死,靠前端 loading 兜底)。mock_mode 直接 True(契约不变)。

- [ ] **Step 1: 写失败测试 — 轮询到 running 后加 8s 缓冲**

在 `savvy-manager/tests/test_docker_manager.py` 加(沿用文件现有 mock client 风格;若文件用 `FakeClient`/`MagicMock` 仿之,否则按下样板):

```python
import time as _time
from unittest.mock import MagicMock, patch

def test_start_container_old_start_with_mock_client():
    fake_container = MagicMock()
    # 模拟 docker start 后 status 从 pending 转 running
    statuses = iter(["pending", "pending", "running"])

    def fake_reload():
        fake_container.status = next(statuses)

    fake_container.reload.side_effect = fake_reload
    fake_container.start = MagicMock()

    fake_client = MagicMock()
    fake_client.containers.get.return_value = fake_container

    with patch("app.docker_manager._client_or_none", return_value=fake_client), \
         patch("app.docker_manager.settings") as fake_settings, \
         patch("app.docker_manager.time.sleep") as fake_sleep:  # 不真睡,加快测
        fake_settings.mock_mode = False
        from app.docker_manager import start_container
        result = start_container("ws-test")

    assert result is True
    fake_container.start.assert_called_once()
    # reload 至少调到看见 running
    assert fake_container.reload.call_count >= 2
    # 8s 固定缓冲被调用
    assert 8 in [c.args[0] for c in fake_sleep.call_args_list if c.args]


def test_start_container_timeout_returns_true():
    # status 永远 pending → 5 次轮询超时 → 仍 True(不卡死)
    fake_container = MagicMock()
    fake_container.status = "pending"
    fake_container.start = MagicMock()

    fake_client = MagicMock()
    fake_client.containers.get.return_value = fake_container

    with patch("app.docker_manager._client_or_none", return_value=fake_client), \
         patch("app.docker_manager.settings") as fake_settings, \
         patch("app.docker_manager.time.sleep"):
        fake_settings.mock_mode = False
        from app.docker_manager import start_container
        result = start_container("ws-test")

    assert result is True
    assert fake_container.reload.call_count == 5  # 最多 5 次


def test_start_container_mock_mode_short_circuits():
    with patch("app.docker_manager.settings") as fake_settings:
        fake_settings.mock_mode = True
        from app.docker_manager import start_container
        assert start_container("any") is True
```

- [ ] **Step 2: 跑测试验失败**

Run: `cd savvy-manager && python -m pytest tests/test_docker_manager.py::test_start_container_old_start_with_mock_client tests/test_docker_manager.py::test_start_container_timeout_returns_true -v`
Expected: FAIL — 现 `start_container` 不调 reload/sleep,断言不成立

- [ ] **Step 3: 改 start_container 实现**

`savvy-manager/app/docker_manager.py:156-172` 整函数替换为:

```python
def start_container(container_name: str) -> bool:
    if settings.mock_mode:
        return True

    client = _client_or_none()
    if client is None:
        return False

    try:
        container = client.containers.get(container_name)
        container.start()
    except NotFound:
        return False
    except APIError:
        return False

    # ready: poll container.status until running (max 5 x 1s), then a fixed
    # 8s buffer for the workspace node server-entry to bind :3000. Timeout is
    # not fatal — the frontend shows "Starting…" until status flips to RUNNING,
    # so a slow start degrades to a wait, not a broken workspace shell.
    for _ in range(5):
        try:
            container.reload()
            if container.status == "running":
                break
        except (NotFound, APIError):
            break
        time.sleep(1)

    time.sleep(8)
    return True
```

确认文件顶部已 `import time`(若无需加)。`NotFound`/`APIError` 已在文件 import(现实现用到了)。

- [ ] **Step 4: 跑测试验通过**

Run: `cd savvy-manager && python -m pytest tests/test_docker_manager.py -v`
Expected: PASS(三个新用例 + 现有用例不回归)

- [ ] **Step 5: 跑 manager 全测试防回归**

Run: `cd savvy-manager && python -m pytest tests -q`
Expected: 全绿

- [ ] **Step 6: Commit**

```bash
git add savvy-manager/app/docker_manager.py savvy-manager/tests/test_docker_manager.py
git commit -m "fix(manager): start_container ready probe + 8s buffer before RUNNING"
```

---

## Task 3: A2 — 前端密钥 Dialog 唤醒时不渲染密钥输入框

**Files:**
- Modify: `new-api/web/default/src/features/hermes/index.tsx:216-265`(Dialog body 区)
- Test: 手动验(React 组件测,文件现有测试结构)

**Interfaces:**
- Consumes: `instance.status`(`creating`/`sleeping`/`running`/`error`,lowercase)、`isFirstStart`(本组件内现 `status === 'creating'`,**保留此判** — 后端 `provider_config_enc` 已通过创建态正确反映,无需新字段)
- Produces: 唤醒(sleeping)时 Dialog **不显示**密钥 PasswordInput;首启(creating)时**显示**

**关键设计**: 现状(254-264)密钥输入框在 Dialog body 无条件渲染。改为主体条件渲染: `isFirstStart ? <密钥输入区> : <提示"无需填写,留空以使用平台密钥">`。Cancel/Start footer 不变;标题/描述已差异化(223/227)保留。

- [ ] **Step 1: 改 Dialog body 条件渲染**

`new-api/web/default/src/features/hermes/index.tsx` 第 254-264 行替换为:

```tsx
                <div className='space-y-2'>
                  {isFirstStart ? (
                    <>
                      <label className='text-sm font-medium'>
                        {t('Provider key (required on first start)')}
                      </label>
                      <PasswordInput
                        value={providerApikey}
                        onChange={(e) => setProviderApikey(e.target.value)}
                        placeholder='sk-...'
                        autoComplete='off'
                      />
                    </>
                  ) : (
                    <p className='text-sm text-muted-foreground'>
                      {t(
                        'Leave empty to keep your current key. Fill to switch keys.'
                      )}
                    </p>
                  )}
                </div>
```

i18n: 新键 `Leave empty to keep your current key. Fill to switch keys.` 需加进 `web/default/src/i18n/locales/{en,zh,...}.json`(zh-CN 译 "留空沿用当前密钥,填写则更换")。按文件 `web/default/AGENTS.md` 的 i18n 流程,跑 `bun run i18n:sync`(web/default 内)生成其他语言。

- [ ] **Step 2: i18n 同步**

Run: `cd new-api/web/default && bun run i18n:sync`
Expected: locales 文件补齐新键翻译;`_reports/` 是同步噪声,**勿提交**(见记忆 feedback-savvy-deploy-gotchas #4)

- [ ] **Step 3: 前端 build 验编译过**

Run: `cd new-api/web/default && bun run build`
Expected: 无 TS 报错(build 成功;`_reports/` 生成勿提交)

- [ ] **Step 4: 手动验(后续全栈验步统一在 Task 5 做;此步先确保 build)**

- [ ] **Step 5: Commit(不提交 `_reports/`)**

```bash
cd new-api/web/default && git add src/features/hermes/index.tsx src/i18n/locales
git commit -m "fix(hermes-ui): hide key input on wake dialog, only show on first start"
```

注意: 若 `_reports/` 被 `git status` 列为 modified,**跳过 `git add _reports/`**;它被 `.gitignore` 覆盖,正常不应出现。若出现是 gitignore 漏,加规则而非提交。

---

## Task 4: A3 — 前端启动 loading + 首启/唤醒差异化文案

**Files:**
- Modify: `new-api/web/default/src/features/hermes/index.tsx`(启动触发处 + loading)
- Test: 手动验(Task 5 统一)

**Interfaces:**
- Consumes: `startMutation.isPending`(已有,212/246 在用)、`isFirstStart`
- Produces: 启动期间按钮 disabled + loading 文案;首启与唤醒文案区分;首启提示"首次启动需配置环境,请耐心等待 10~30 秒"

**关键设计**: 现 Start 按钮(214)`startMutation.isPending ? 'Starting...' : 'Start'` 文案无首启/唤醒区分。Dialog footer 内 Start 按钮(247-249)同理。改: 期间文案 `isFirstStart ? t('Setting up environment…') : t('Waking up…')`;并在首启 Dialog 描述区(227-229 描述)加一句醒目提示。"10~30 秒"是首启整体估值(含 probe+build_snapshot+加密+docker run,非仅 ready 8s)。

- [ ] **Step 1: 改按钮 + 文案**

`new-api/web/default/src/features/hermes/index.tsx`:

第 209-215 行主 Start 按钮:
```tsx
              <Button
                size='sm'
                onClick={() => setStartOpen(true)}
                disabled={startMutation.isPending}
              >
                {startMutation.isPending
                  ? (isFirstStart ? t('Starting first setup…') : t('Waking up…'))
                  : t('Start')}
              </Button>
```

第 222-226 行 Dialog title:
```tsx
                title={
                  isFirstStart
                    ? t('First start requires an API key')
                    : t('Start workspace')
                }
```

第 227-229 行 Dialog description,在现描述前加首启醒目提示:
```tsx
                description={
                  isFirstStart
                    ? t(
                        'First-time setup takes 10–30 seconds to configure the environment. Please wait.'
                      ) +
                    '\n' +
                    t(
                      'You can generate one on the API Keys page and paste it here. We recommend the key you generated on this platform (billed to your account balance).'
                    )
                    : t(
                        'Waking up your workspace. This usually takes a few seconds.'
                      )
                }
```

第 242-250 行 footer Start 按钮:
```tsx
                    <Button
                      type='button'
                      onClick={handleStartSubmit}
                      disabled={startMutation.isPending}
                    >
                      {startMutation.isPending
                        ? (isFirstStart ? t('Setting up environment…') : t('Waking up…'))
                        : (isFirstStart ? t('Start setup') : t('Start'))}
                    </Button>
```

- [ ] **Step 2: i18n 同步新键**

新增键: `Starting first setup…`、`Waking up…`、`Start workspace`、`First-time setup takes 10–30 seconds to configure the environment. Please wait.`、`Waking up your workspace. This usually takes a few seconds.`、`Start setup`。zh-CN 译分别: "首次配置启动中…"、`"唤醒中…"`、`"启动工作区"`、`"首次配置需 10–30 秒初始化环境,请耐心等待。"`、`"正在唤醒您的工作区,通常几秒钟。"`、`"开始配置"`。

Run: `cd new-api/web/default && bun run i18n:sync`
Expected: locales 补齐;`_reports/` 噪声勿提交

- [ ] **Step 3: build 验**

Run: `cd new-api/web/default && bun run build`
Expected: TS 无错;成功

- [ ] **Step 4: Commit**

```bash
cd new-api/web/default && git add src/features/hermes/index.tsx src/i18n/locales
git commit -m "feat(hermes-ui): differentiated start/wake loading copy + first-setup notice"
```

---

## Task 5: A1 — 后端 remainingMinutes 链路诊断 + 修 + 前端倒计时验

**Files:**
- Modify: `new-api/controller/hermes.go:32-47`(toVO,诊断后定修法)
- Verify: `new-api/web/default/src/features/hermes/index.tsx:189-192`(前端显示,无需改代码,验后端修后正确)
- Test: `go test ./controller -run TestHermes`(若无现成测,加 table test)

**Interfaces:**
- Consumes: `service.HermesInstance{ExpiresAt,StartedAt,InstanceID,Status,Plan}`
- Produces: `hermesInstanceVO.RemainingMinutes` 对 FREE+有 expires_at **必设**(非 nil);PAID 或无 expires_at 保持 nil(前端 `!== undefined` 不成立 → "Unlimited")

**真因未钉死 — 三种可能,此 Task 先诊断**:
- 甲: manager 返 `expires_at` 字段名/格式与 `service.HermesInstance.ExpiresAt` json tag 不匹配 → 解析不进 → 跳过设置
- 乙: 已过期容器 `remainingMinutes` 返 nil → 跳过(此为正常,非 bug)
- 丙: `not_created`/首次态 expires_at 为空 → "Unlimited" 正常;bug 在 RUNNING 态前端缓存未刷新(此为前端轮询 bug,非后端)

- [ ] **Step 1: 写诊断测试 — 钉死真因(failing 阶)**

`new-api/controller/hermes_test.go`(若不存在则新建,沿用 `require`/`assert` per new-api/CLAUDE.md 测试规约):

```go
package controller

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToVOFreeRunningSetsRemainingMinutes(t *testing.T) {
	// 甲/丙 验: FREE + RUNNING + expires_at 2h 后 → RemainingMinutes 必非 nil
	future := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	inst := &service.HermesInstance{
		InstanceID: "i1",
		Status:     "RUNNING",
		Plan:       "FREE",
		ExpiresAt:  future,
		StartedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	vo := toVO(inst)
	require.NotNil(t, vo.RemainingMinutes, "FREE+RUNNING+有expires_at 必须 RemainingMinutes 非空")
	assert.Greater(t, *vo.RemainingMinutes, 100, "应≈120 分钟")
	assert.Equal(t, "running", vo.Status)
	assert.Equal(t, "FREE", vo.Plan)
}

func TestToVOPaidNoExpiryKeepsNilRemainingMinutes(t *testing.T) {
	// PAID_RESIDENT 无 expires_at → RemainingMinutes nil → 前端显示 "Unlimited"
	inst := &service.HermesInstance{
		InstanceID: "i2",
		Status:     "RUNNING",
		Plan:       "PAID_RESIDENT",
		ExpiresAt:  "",  // 付费无限制
	}
	vo := toVO(inst)
	assert.Nil(t, vo.RemainingMinutes, "PAID 无 expires_at 应 RemainingMinutes=nil → 前端 Unlimited")
}

func TestToVOExpiredFreeReturnsNilOrZero(t *testing.T) {
	// 乙: FREE 但已过期 → remainingMinutes 应返 nil(或 0)。当前实现:
	// remainingMinutes() 见 hermes.go:66+,需读它确认过期返 nil 还是负数
	// 此测先断言"不返负分钟" — 前端不应显示"-5 minutes"
	past := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)
	inst := &service.HermesInstance{
		InstanceID: "i3",
		Status:     "RUNNING",
		Plan:       "FREE",
		ExpiresAt:  past,
	}
	vo := toVO(inst)
	if vo.RemainingMinutes != nil {
		assert.GreaterOrEqual(t, *vo.RemainingMinutes, 0, "过期不应显示负分钟")
	}
}
```

- [ ] **Step 2: 跑测试观现状**

Run: `cd new-api && go test ./controller -run TestToVO -v`
Expected: 看 `TestToVOFreeRunningSetsRemainingMinutes` 是否 PASS。**若 PASS** → 后端 toVO 正确,真因转**丙(前端缓存/轮询)**,移步 Step 3 前端诊断。**若 FAIL** → 真因甲/乙,读 `hermes.go:38-42`+`66+` 修后端,进 Step 3A。

- [ ] **Step 3A(若后端 FAIL): 读 hermes.go:38-72 内容确认 remainingMinutes 实现**

读 `new-api/controller/hermes.go` 第 38-72 行。若 `remainingMinutes()` 对过期返 nil(乙,正确)且 FREE+未来 expires_at 应返非 nil → 那甲可能(manager 字段名)。验:`service.HermesInstance.ExpiresAt` json tag 与 manager `instance.py` 序列化字段名一致(`expires_at`)。若 manager 序列化用别名(如 `expires_at_iso`),改 `service.HermesInstance.ExpiresAt` 的 json tag 或 `toVO` 读字段对齐。具体改法依读后实情,保持"FREE+未来 expires_at → RemainingMinutes 非空"为契约不变量。

- [ ] **Step 3(若后端 PASS → 丙:前段缓存/刷新): 诊断前端轮询**

读 `new-api/web/default/src/features/hermes/` 的 polling/rerefresh 逻辑(grep `useQuery|refetch|setInterval|poll|invalidate|useEffect.*status`)。bugs 候选: 启动后未 refetch instance 状态 → 前端停留旧 VO(无 RemainingMinutes)→ 显示 Unlimited。修:start 成功 `mutate` 的 `onSuccess` 触发 `providerState`/instance query `invalidate` 或 `refetch`。

具体修法依读后实情,但不变量: `running` + FREE + `remainingMinutes` 已被后端正确返(Step 2 PASS 证明)→ 前端**必须**显示数字而非 Unlimited。若前端不轮询,RUNNING 后倒计时静态不滚动属可接受(scope 内不做秒级 ticker);但首次启动后从 `creating` 转 `running` 必须 refetch 拿到新 VO。

- [ ] **Step 4: 跑测试验通过**

Run: `cd new-api && go test ./controller -run TestToVO -v`
Expected: 三个 TestToVO 全 PASS(后端契约钉死)

- [ ] **Step 5: 前端 build 验(若 Step 3 改了前端)**

Run: `cd new-api/web/default && bun run build`
Expected: 成功

- [ ] **Step 6: Commit**

```bash
git add new-api/controller/hermes.go new-api/controller/hermes_test.go new-api/web/default/src/features/hermes/index.tsx  # 仅 add 实际改的
git commit -m "fix(hermes): remainingMinutes set for FREE running; refetch VO after start"
```

---

## Task 6: 全栈手动验

**Files:** 无(执行验)

- [ ] **Step 1: rebuild 后端+前端**

```bash
# 本地(已在 dev compose)
docker compose -f docker-compose.yml up -d --build new-api savvy-manager
```
Expected: 两镜像 rebuild 成功,容器 Up

- [ ] **Step 2: 验 A1 — 免费剩余时间显示**

UI 操作: 登录 → 开 hermes 工作区 → FREE 容器 start → 看 "Remaining Time" 显示数字(X minutes)非 "Unlimited"。付费(若有 PAID_RESIDENT 实例或 admin 造一个)→ 显示 "Unlimited"。
Expected: FREE 显数字、PAID 显 Unlimited

- [ ] **Step 3: 验 A2 — 密钥弹窗**

UI: FREE 容器首启 → 点 Start → Dialog **显示密钥输入框**; Sleep → 再启 → 点 Start → Dialog **不显示密钥输入框**(显示"留空沿用"提示)。
Expected: 首启显框、唤醒不显框

- [ ] **Step 4: 验 A3 — 唤醒 ready + 文案**

UI: 首启 → Dialog 描述见"首次配置需 10–30 秒";Start 按钮期间显示"首次配置启动中…/开始配置"。唤醒 → 描述"正在唤醒…几秒";按钮"唤醒中…"。停下后开 workspace 标签 → **不空壳**(workspace node 已 listen)。计时观察: docker start 到 RUNNING 标记约 8-12 秒(轮询+缓冲)。
Expected: 无空壳、文案对、首启慢于唤醒

- [ ] **Step 5: 验 A4 — 2h 时长**

新启动 FREE → manager DB 查 `expires_at - started_at ≈ 2h`:
```bash
docker exec newapi-db psql -U newapi -d new-api -c "..."  # 若 instance 在 newapi-db(否)
# instance 在 manager-db:
docker exec manager-db psql -U savvy -d savvy_manager -c "SELECT started_at, expires_at FROM instances WHERE status='SLEEPING' OR status='RUNNING' ORDER BY started_at DESC LIMIT 1;"
```
Expected: delta ≈ 7200s(±60s)

- [ ] **Step 6: PR**

```bash
git push -u origin feat/hermes-startup-ux
gh pr create --base dev --title "Hermes 启动 UX 修复 + 免费时长 2h (A 子项目)" --body-file <(cat <<'EOF'
本 PR 实现设计 docs/superpowers/specs/2026-07-06-hermes-startup-ux-design.md
- A1 免费计时显倒计时(后端 remainingMinutes 链路修 + 前端 refetch)
- A2 密钥弹窗仅首启渲染密钥框
- A3 manager ready 轮询+8s 缓冲、前端 loading/首启唤醒文案
- A4 免费时长 3h→2h + PRD 同步
不碰 hermes-workspace、不碰支付/订阅/重置/高级计划。

AI-assisted by Claude Code.
EOF
)
```
Expected: PR 开成,base=dev

---

## Self-Review

- **spec 覆盖**: A1(Task5)✓ A2(Task3)✓ A3(Task2 manager + Task4 前端)✓ A4(Task1)✓ PRD 同步(Task1 Step5)✓ 全栈验(Task6)✓
- **占位符**: A1 Task5 Step3A/3 含"诊断后定修法"分支 — 这是合法探测(技能允许先复现后修),有具体诊断路径 + 不变量契约,**非空 TODO**。其余 Task 全完整代码。
- **类型一致**: `isFirstStart`、`startMutation.isPending`、`instance.status` 跨 Task 一致;`RemainingMinutes *int`、`hermesInstanceVO` 字段名跨 Task 一致;`start_container -> bool` 签名不变。
- **i18n 警告**: Task3/4 新增 7 个 i18n 键,`bun run i18n:sync` 统一同步,`_reports/` 不提交(记忆 feedback-savvy-deploy-gotchas #4)。
