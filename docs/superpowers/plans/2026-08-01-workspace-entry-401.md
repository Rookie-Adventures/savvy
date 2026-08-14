# Workspace 入口 401 + token 续期 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修两个独立故障——SAVVY_PUBLIC_HOST 劫持 401（现象 C）+ token cookie 无续期（现象 A），让 workspace 首启不再撞 401、长开会话不死。

**Architecture:** 现象 C 纯 env 改值（零代码）。现象 A 在 nginx auth_request 续期位做：manager validate 端 token 临近过期时签新 token 放 header，nginx 抓 header 刷 cookie。续期在 nginx+manager 层，与新标签页/SPA 上下文无关。两个前端零改。

**Tech Stack:** Python/FastAPI（savvy-manager）、nginx、Docker Compose、pytest。

## Global Constraints

- spec: `docs/superpowers/specs/2026-08-01-workspace-entry-401-design.md`
- token 续期阈值固定 300 秒（5 min）；token 寿命保持 30 min 不变。
- `SAVVY_HMAC_SECRET` 在生成端与验证端同进程同 env，续期复用 `get_secret()`。
- json 用 stdlib 即可（token.py 已用 json/hmac/hashlib），无 wrapper 约束（那是 new-api 的）。
- 改 manager 代码需重建 savvy-manager 镜像才生效（本地 Docker 跑栈，见 memory project-local-dev-docker）。
- nginx 配置源是 `deploy/nginx.conf`，compose 挂载到容器 `/etc/nginx/conf.d/default.conf:ro`；改源 + `docker compose restart nginx` 即生效，无需重建。
- 不动 `new-api/web/default/src/features/hermes/`，不动 hermes-workspace 容器。

## File Structure

| 文件 | 责任 | 动作 |
|---|---|---|
| `savvy-manager/app/token.py` | token 签发/验证。加 `renew_access_token` 续期签发。 | Modify |
| `savvy-manager/app/routers/workspace.py` | nginx auth_request 调的 validate 端。加临过期续期 header，删 debug print。 | Modify |
| `savvy-manager/tests/test_hmac.py` | token 测试。加续期测试。 | Modify |
| `deploy/nginx.conf` | savvy-nginx 配置源。grab 续期 header 刷 cookie。 | Modify |
| `deploy/docker-compose.yml` + `docker-compose.yml` | savvy-manager env 段。改 SAVVY_PUBLIC_HOST。 | Modify |

---

## Task 1: token.py 加续期签发

**Files:**
- Modify: `savvy-manager/app/token.py`
- Test: `savvy-manager/tests/test_hmac.py`

**Interfaces:**
- Produces: `renew_access_token(instance_id: str, user_id: str, expires_in_minutes: int = 30) -> str`（返回新 token 字符串，不含 workspace_url）。复用现有 `generate_access_token` 取 `["token"]`，零重复 HMAC 逻辑。
- Consumes: 现有 `get_secret()`、`generate_access_token()`。

- [ ] **Step 1: Write the failing test**

追加到 `savvy-manager/tests/test_hmac.py` `TestAccessToken` 类内末尾：

```python
    def test_renew_access_token_returns_valid_new_token(self):
        from app.token import renew_access_token
        token = renew_access_token("inst-123", "user-456", expires_in_minutes=30)
        assert isinstance(token, str)
        assert "." in token  # payload.signature shape
        payload = verify_access_token(token)
        assert payload is not None
        assert payload["instance_id"] == "inst-123"
        assert payload["user_id"] == "user-456"
        # renewed token must have a fresh exp in the future
        import time
        assert payload["exp"] > int(time.time())

    def test_renewed_token_independent_of_old(self):
        from app.token import renew_access_token
        old = generate_access_token("inst-1", "u-1", expires_in_minutes=1)
        new = renew_access_token("inst-1", "u-1", expires_in_minutes=30)
        assert old["token"] != new  # different exp → different payload → different token
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd E:/savvy/savvy-manager && python -m pytest tests/test_hmac.py::TestAccessToken::test_renew_access_token_returns_valid_new_token -v`
Expected: FAIL — `ImportError: cannot import name 'renew_access_token'`

- [ ] **Step 3: Write minimal implementation**

在 `savvy-manager/app/token.py` 的 `generate_access_token` 函数之后、`verify_access_token` 之前加：

```python
def renew_access_token(
    instance_id: str,
    user_id: str,
    expires_in_minutes: int = 30,
) -> str:
    """Sign a fresh access token (sliding renewal). Reuses generate_access_token's
    signing path; returns just the token string, no workspace_url."""
    return generate_access_token(
        instance_id=instance_id,
        user_id=user_id,
        expires_in_minutes=expires_in_minutes,
    )["token"]
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd E:/savvy/savvy-manager && python -m pytest tests/test_hmac.py -v`
Expected: PASS（含新 2 个 + 原有 3 个）。

- [ ] **Step 5: Commit**

```bash
cd E:/savvy
git add savvy-manager/app/token.py savvy-manager/tests/test_hmac.py
git commit -m "feat(workspace): add renew_access_token for sliding cookie renewal"
```

---

## Task 2: workspace.py validate 端加续期 header + 删 debug print

**Files:**
- Modify: `savvy-manager/app/routers/workspace.py`

**Interfaces:**
- Consumes: `renew_access_token(instance_id, user_id, expires_in_minutes) -> str`（Task 1 产出）。
- Produces: validate 响应在临过期时多一个 header `X-Renewed-Token: <new>`；nginx 据此刷 cookie（Task 3）。
- 产出契约：仅当 `payload["exp"] - now < 300` 时设该 header，否则不设。

- [ ] **Step 1: Write the failing test**

新建 `savvy-manager/tests/test_workspace_validate.py`：

```python
import time
from unittest.mock import patch
from fastapi.testclient import TestClient
from app.main import app


def _make_token(instance_id="inst-test", user_id="u-1", ttl_minutes=30):
    from app.token import generate_access_token
    return generate_access_token(instance_id, user_id, expires_in_minutes=ttl_minutes)["token"]


def test_validate_emits_renewed_token_when_near_expiry(monkeypatch):
    """Token with <5min left must trigger X-Renewed-Token."""
    from app.token import generate_access_token
    # token expiring in 2 minutes (< 300s threshold)
    near_token = generate_access_token("inst-near", "u-near", expires_in_minutes=2)["token"]

    # stub Instance + DB so validate passes the instance checks
    class FakeInst:
        instance_id = "inst-near"
        user_id = "u-near"
        status = "running"
        container_name = "fake-ctr"

    class FakeQuery:
        def filter(self, *a):
            return self
        def first(self):
            return FakeInst()

    def fake_get_db():
        yield None

    monkeypatch.setattr("app.routers.workspace.get_db", fake_get_db)
    monkeypatch.setattr("app.routers.workspace.Instance", _FakeModel(FakeQuery()))

    client = TestClient(app)
    res = client.get("/internal/workspace/validate", headers={"X-Token": near_token})
    assert res.status_code == 200
    assert res.headers.get("x-renewed-token")
    # renewed token must verify
    from app.token import verify_access_token
    assert verify_access_token(res.headers["x-renewed-token"]) is not None


def test_validate_no_renewed_token_when_far_from_expiry(monkeypatch):
    """Token with >5min left must NOT emit X-Renewed-Token."""
    from app.token import generate_access_token
    fresh_token = generate_access_token("inst-fresh", "u-fresh", expires_in_minutes=30)["token"]

    class FakeInst:
        instance_id = "inst-fresh"
        user_id = "u-fresh"
        status = "running"
        container_name = "fake-ctr"

    class FakeQuery:
        def filter(self, *a):
            return self
        def first(self):
            return FakeInst()

    def fake_get_db():
        yield None

    monkeypatch.setattr("app.routers.workspace.get_db", fake_get_db)
    monkeypatch.setattr("app.routers.workspace.Instance", _FakeModel(FakeQuery()))

    client = TestClient(app)
    res = client.get("/internal/workspace/validate", headers={"X-Token": fresh_token})
    assert res.status_code == 200
    assert not res.headers.get("x-renewed-token")


class _FakeModel:
    """Stand-in for the Instance ORM class so db.query(Instance)... works without a DB."""
    def __init__(self, query):
        self._query = query
    def __call__(self, *a, **kw):
        return self  # not used; Instance is used as db.query(Instance) filter target
```

注：`db.query(Instance)` 在 validate 端用 `Instance.instance_id`/`Instance.user_id` 作 filter 列引用；FakeModel 仅需作为 `db.query(Instance)` 的参数被忽略掉。若 monkeypatch Instance 后 filter 调用报错，改用更简单的 monkeypatch：把整个 `_get_instance` 式逻辑 stub 掉——validate 端没调 `_get_instance`，是内联 `db.query(Instance)`，所以需 patch `Instance` 本身。若 FastAPI Depends 注入 get_db 不吃 monkeypatch，用 `app.dependency_overrides[get_db] = lambda: None` 替代。实施者按实际 API 风格调整 stub，**核心断言不变：near→有 header，fresh→无 header**。

- [ ] **Step 2: Run test to verify it fails**

Run: `cd E:/savvy/savvy-manager && python -m pytest tests/test_workspace_validate.py -v`
Expected: FAIL — validate 当前不返回 `X-Renewed-Token`。

- [ ] **Step 3: Write minimal implementation**

改 `savvy-manager/app/routers/workspace.py`：

(a) 删除全部 `print(f"[DEBUG_VALIDATE]..."`（9 处）及 `print("[DEBUG_VALIDATE]...")`。

(b) 顶部 import 改：
```python
from ..token import verify_access_token, renew_access_token
import time
```

(c) `validate_workspace_token` 函数内，验签通过 + instance 校验通过后、return Response 之前，加续期判断。把现有 return 块改为：

```python
    # Sliding renewal: if token has <5min left, sign a fresh one and hand it
    # to nginx via header so it can refresh the workspace_token cookie.
    renewed_token = None
    remaining = payload.get("exp", 0) - int(time.time())
    if remaining < 300:
        renewed_token = renew_access_token(instance_id, user_id, expires_in_minutes=30)

    headers = {
        "X-User-Id": user_id,
        "X-Instance-Id": instance_id,
        "X-Workspace-Upstream": upstream_url,
    }
    if renewed_token:
        headers["X-Renewed-Token"] = renewed_token

    return Response(status_code=200, headers=headers)
```

（`upstream_url` 变量保留上方 `f"http://{inst.container_name}:3000"` 定义不变。）

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd E:/savvy/savvy-manager && python -m pytest tests/test_workspace_validate.py tests/test_hmac.py -v`
Expected: PASS（新 2 个 + Task 1 的 5 个）。

- [ ] **Step 5: Confirm debug prints gone**

Run: `grep -c "DEBUG_VALIDATE" E:/savvy/savvy-manager/app/routers/workspace.py`
Expected: `0`

- [ ] **Step 6: Commit**

```bash
cd E:/savvy
git add savvy-manager/app/routers/workspace.py savvy-manager/tests/test_workspace_validate.py
git commit -m "feat(workspace): emit X-Renewed-Token near expiry + drop debug prints"
```

---

## Task 3: savvy-nginx 抓续期 header 刷 cookie

**Files:**
- Modify: `deploy/nginx.conf`

**Interfaces:**
- Consumes: manager validate 响应 header `X-Renewed-Token`（Task 2 产出）。
- Produces: `X-Renewed-Token` 非空时发 `Set-Cookie workspace_token=<renewed>; Path=/; HttpOnly; SameSite=Lax`，浏览器续 cookie。

- [ ] **Step 1: Add renewal variable via auth_request_set**

在 `deploy/nginx.conf` 第 178 行（`auth_request_set $workspace_upstream ...` 之后）加一行：

```nginx
    auth_request_set $renew_token $upstream_http_x_renewed_token;
```

- [ ] **Step 2: Add renewal cookie map**

在第 34 行之后（现有 `$ws_set_cookie` map 块之后）加 map：空续期 token → 空（不发 cookie 即浏览器忽略）；有续期 → 发新 cookie。

```nginx
# Sliding renewal: when validate hands back a fresh token, refresh the cookie.
# Empty renew_token → empty Set-Cookie → browser ignores (same idiom as $ws_set_cookie).
map $renew_token $ws_renew_cookie {
    ""        "";
    default   "workspace_token=$renew_token; Path=/; HttpOnly; SameSite=Lax";
}
```

- [ ] **Step 3: Emit renewal cookie in location /**

在第 193 行（现有 `add_header Set-Cookie $ws_set_cookie always;` 之后）加第二行 add_header。nginx 允许同 location 多个 `add_header`，两条互不影响：

```nginx
        # Sliding renewal cookie (only set when validate returned X-Renewed-Token)
        add_header Set-Cookie $ws_renew_cookie always;
```

- [ ] **Step 4: Verify nginx config syntax**

Run: `docker exec savvy-nginx-1 nginx -t 2>&1 || (cd E:/savvy && docker compose restart nginx && docker exec savvy-nginx-1 nginx -t 2>&1)`
Expected: `syntax is ok` + `test is successful`。

（compose 挂载是 `:ro` 读源改无需重建，`restart nginx` 刷新容器读新源。）

- [ ] **Step 5: Commit**

```bash
cd E:/savvy
git add deploy/nginx.conf
git commit -m "feat(workspace): refresh workspace cookie via X-Renewed-Token from auth_request"
```

---

## Task 4: 改 SAVVY_PUBLIC_HOST（现象 C，配置层）

**Files:**
- Modify: `deploy/docker-compose.yml`
- Modify: `docker-compose.yml`

注：本机（开发自测）改 `docker-compose.yml` 的 savvy-manager env 段，值 `http://127.0.0.1`。服务器部署用 `deploy/docker-compose.yml`，值留对外域名占位由部署时填——本任务先改本机 compose 让现场可用。

- [ ] **Step 1: Find SAVVY_PUBLIC_HOST in local compose**

Run: `grep -n "SAVVY_PUBLIC_HOST" E:/savvy/docker-compose.yml E:/savvy/deploy/docker-compose.yml`
Expected: 两文件各一行（或仅其一）。

- [ ] **Step 2: Change local compose value to http://127.0.0.1**

在 `docker-compose.yml` savvy-manager service 的 environment 段，将 `SAVVY_PUBLIC_HOST` 的值从 `http://localhost` 改为 `http://127.0.0.1`。

若 `docker-compose.yml` 没有 savvy-manager env 段（只有 `deploy/docker-compose.yml` 有），则改 `deploy/docker-compose.yml` 的本机用值——但 `deploy/docker-compose.yml` 是机A服务器配置可能反之。实施者按 grep 结果改**本机实际在跑的那个 compose**，值 `http://127.0.0.1`。

- [ ] **Step 3: Verify env reaches manager**

当前的 manager 容器 env 仍是 `http://localhost`。改 compose 后需重建/重启 manager 让新 env 生效：

Run: `cd E:/savvy && docker compose up -d savvy-manager`（或对应 compose 文件 + service 名）
然后验证：

Run: `docker exec savvy-manager sh -c 'echo $SAVVY_PUBLIC_HOST'`
Expected: `http://127.0.0.1`

- [ ] **Step 4: Commit**

```bash
cd E:/savvy
git add docker-compose.yml deploy/docker-compose.yml
git commit -m "fix(workspace): set SAVVY_PUBLIC_HOST to 127.0.0.1 to avoid wslrelay ::1 hijack"
```

---

## Task 5: 重建 savvy-manager 镜像 + 端到端验证

**Files:**
- 无新文件。复用 Task 1-4 改动。

- [ ] **Step 1: Rebuild savvy-manager image**

manager 改了 token.py + workspace.py，必须重建镜像（见 memory project-local-dev-docker）：

Run: `cd E:/savvy && docker compose build savvy-manager && docker compose up -d savvy-manager`

- [ ] **Step 2: Verify manager up + validate endpoint reachable**

Run: `docker ps --filter name=savvy-manager --format "{{.Status}}"`
Expected: Up。

- [ ] **Step 3: End-to-end: test2 Open Workspace no longer 401**

（需用户在 new-api web UI 操作，或实施者用 instance_id + access-token API 模拟）

a. 确认 test2 的 inst-4 RUNNING：
Run: `docker exec savvy-manager sh -c 'echo check via any db query or skip'`（实施者按现有 DB 访问方式确认）

b. 实施者拿新 access token + workspace_url：
通过 new-api `/api/hermes/instance/{id}/access-token` 或直接 manager issue 端点。验证返回的 `workspace_url` 现在是 `http://127.0.0.1:41003/`（不再是 `http://localhost:41003/`）。

c. 打该 URL：
```bash
TOKEN="<new token from b>"
curl -sL -o /dev/null -w "final=%{http_code} url=%{url_effective}\n" "http://127.0.0.1:41003/?token=$TOKEN"
```
Expected: `final=200 url=http://127.0.0.1:41003/workspace/?token=...`

- [ ] **Step 4: Verify sliding renewal end-to-end**

a. issue 一个 5min 寿命的短 token（若 access-token API 不支持自定义寿命，直接 manager 内 `generate_access_token(..., expires_in_minutes=3)` 取 token）。设其 cookie，等 >2min40s 但 <5min（remaining<300），打一个受 auth_request 保护的 workspace 路径，抓响应 header：

```bash
curl -sD - -o /dev/null -H "Cookie: workspace_token=<short token>" "http://127.0.0.1:41003/workspace/" | grep -i "set-cookie"
```
Expected: 出现 `Set-Cookie: workspace_token=<renewed>; Path=/; HttpOnly; SameSite=Lax`（新 token，非原短 token）。

b. 用新 cookie 再打一次，确认 200：
```bash
curl -s -o /dev/null -w "%{http_code}\n" -H "Cookie: workspace_token=<renewed>" "http://127.0.0.1:41003/workspace/"
```
Expected: `200`

- [ ] **Step 5: Verify logs clean of debug noise**

Run: `docker logs savvy-manager --tail 50 2>&1 | grep -c "DEBUG_VALIDATE"`
Expected: `0`（不再有 print 噪声）。

- [ ] **Step 6: Final commit if any drift**

无代码改动则跳过。若 e2e 发现微调，commit：

```bash
cd E:/savvy
git add -A
git commit -m "fix(workspace): e2e adjustments after renewal rollout"
```

---

## Rollout to server (本 plan 不含，部署时单独跑)

- 服务器：`deploy/docker-compose.yml` 的 `SAVVY_PUBLIC_HOST` 设对外域名 `https://<域名>`；机A反代通配 41000-41099 端口段映射到机B（见 memory project-deploy-architecture / reference-deploy-access-and-domains）。
- 重建服务器 manager 镜像 + restart nginx。
