# Workspace 模型密钥注入与撤销设计

> 日期:2026-07-04
> 状态:设计完成,待评审
> 背景:Workspace 端口池路由已完成(workspace 占 41000-41099 端口根路径),进入工作区界面正常。但 agent 调模型的 B 层密钥(`OPENAI_API_KEY`/`OPENAI_BASE_URL`)从未注入,真发消息会失败。本设计补齐该层,并满足"首次启动必填我们的密钥 + 运行时可改自己的 + 一键撤销"的产品诉求。

## 1. 设计目标

1. **首次启动硬锁**:新用户第一次启动 workspace,必须填入用户在 new-api 控制台生成的 sk-xxx(下称"我们的密钥")。不填不让启动。
2. **运行时软控**:用户进工作区后,可在 workspace Settings 改成自己的 provider key/端点。我们加密保存,尊重用户意愿,但**不做硬性拦截**。
3. **一键撤销**:用户可在工作区控制台一键撤销所有 LLM provider 密钥。撤销只清密钥,**不动用户数据**(会话/记忆/文件/skills 原封不动)。撤销后调模型失败(401),用户回控制台重新填我们的密钥才能继续用 —— 等同首次启动流程。
4. **双层独立**:A 层(workspace↔agent 通信,固定 secret,部署方控制)与 B 层(agent→模型 provider,用户控制)职责分离,互不干扰。
5. **zero-fork**:不改 hermes-workspace / hermes-agent 源码,所有改动在 savvy-manager + new-api + docker 编排。

## 2. 架构与密钥分层

```
                      ┌──────────────────────────────────────────────┐
                      │ 容器 hermes-unified:saas (savvy-u{uid}-w1)    │
                      │                                              │
   浏览器 :410NN       │  workspace :3000  ──A层(固定secret)──> agent  │
        │             │                                       :8642  │
        └──token─────>│                                              │
                      │                       agent ──B层────────> │
                      │                          │                 │
                      └──────────────────────────┼─────────────────┘
                                                 │  跨网络(内网名/公网域名)
                                                 ▼
                      ┌──────────────────────────────────────────────┐
                      │ new-api 网关                                  │
                      │  /v1/chat/completions  ──> 上游 Anthropic 等 │
                      │  鉴权:用户 sk-xxx(扣 new-api 账户余额)      │
                      └──────────────────────────────────────────────┘
```

| 层 | 用途 | 来源 | 谁控制 |
|---|---|---|---|
| **A. `API_SERVER_KEY`/`HERMES_API_TOKEN`** | workspace ↔ agent 通信鉴权 | 部署级固定 secret | 部署方(我们) |
| **B. `OPENAI_API_KEY`/`OPENAI_BASE_URL`** | agent → new-api 模型调用 | 用户启动时填写、运行时可在 Settings 改 | 用户(初次必填 = new-api sk-xxx) |

new-api 作为 OpenAI 兼容网关:
- 注入 agent 的 `OPENAI_BASE_URL` 指向 new-api 的 `/v1`
- 注入 agent 的 `OPENAI_API_KEY` = 用户在 new-api 生成的 sk-xxx
- new-api 鉴权 sk-xxx → 扣用户余额 → 转发到上游 Anthropic/OpenAI

B 层密钥不进 savvy-manager DB 明文,加密存储(见 §3)。

## 3. 数据模型

savvy-manager 的 `instances` 表新增 3 列:

```python
provider_config_enc = Column(Text, nullable=True)        # 加密的 B 层配置快照
provider_config_alg = Column(String(32), nullable=True)  # 加密算法标识(fernet|aes-gcm,可扩展)
provider_key_set_at = Column(DateTime, nullable=True)    # 最近一次用户填入/改 key 的时间
```

`provider_config_enc` 加密前的明文 JSON 结构:

```json
{
  "base_url": "http://new-api:3000/v1",
  "api_key": "sk-xxx",
  "model": "claude-sonnet-4",
  "provider": "custom",
  "source": "ours"
}
```

`source` 字段两种值:
- `"ours"` — 首次启动我们注入的(默认端点 + 用户在 new-api 生成的 key)
- `"user"` — 用户进工作区后改的自己 key/端点

存这个字段是为了撤销 UI 区分显示("你当前用自己的 key"vs"你当前用我们的默认")。

**加密方案**:Fernet(基于 AES-128-CBC + HMAC,Python `cryptography` 库)。
- 密钥来源:`SAVVY_PROVIDER_ENC_KEY` env(32 字节 base64)。**不存 DB**。
- 没配置该 env 时:savvy-manager 启动失败并报错(fail-closed,显式拒绝明文回退)。
- 加密工具放 `savvy-manager/app/crypto.py` 新建文件。

**撤销标记**:不建专门列。撤销 = 把 `provider_config_enc`/`provider_key_set_at` 置 NULL,`source` 清空。容器内 config 同步清。

**用户表(users)**:不动。密钥绑实例级不绑用户级(符合"入库加密"决策)。

## 4. 密钥数据流(三条路径)

### 路径 A:首次启动(workspace 创建,必填)

```
new-api 前端(启动表单)
  │ POST /api/hermes/{instance_id}/start  body: { provider_api_key: "sk-...", provider_base_url?: "可选覆盖" }
  ▼
new-api controller/hermes.go
  │ 透传给 savvy-manager(已有 HMAC 鉴权通道)
  ▼
savvy-manager POST /internal/instances/{id}/start  (params 扩展)
  │ 1. 校验 key 格式(≥16 字符,sk- 前缀可选)
  │ 2. 用 SAVVY_PROVIDER_ENC_KEY 加密 → provider_config_enc
  │ 3. source="ours", base_url 默认 = settings.openai_base_url(env 切换 dev/prod)
  │ 4. 写 DB
  │ 5. 启动容器:docker_manager 传 env
  ▼
docker_manager.create_container  (A 层 env 不变;不传 B 层 env)
  │ 容器启动成功后:
  │ docker exec <容器名> sh -c "cat > /opt/data/config.yaml" 写入模板
  │   模板含 provider:custom + base_url + api_key + model(从 DB 解密后填入)
  │   用 base64 编码避免 shell escape 风险
  ▼
agent gateway 读 /opt/data/config.yaml → provider=custom 生效
  │
agent gateway :8642 ready
```

**注**:首启后 wake 不传 env、不重写 config.yaml(除非容器内文件不存在)。容器内 config.yaml 是唯一真相源,见 §7.1。

### 路径 B1:运行时改密钥(用户在 Settings 改,被动同步)

用户在 workspace Settings 改自己的 provider 配置时,workspace **只改容器内 `/opt/data/config.yaml`**(用 hermes 自带 vanilla 逻辑,我们不动)。savvy-manager DB 仍存旧快照。

**唤醒时对账**:每次 stop→wake 容器启动前(或在 start_container 调用前),savvy-manager:
1. `docker exec <容器名> cat /opt/data/config.yaml`(容器若 stop 不可读,跳过本周期)
2. 解析 yaml,跟 DB 加密快照解密后的内容比对(对比 `provider`/`base_url`/`api_key`/`model` 四字段)
3. 不一致 → 把容器新内容加密回写 DB,标 `source="user"`
4. 一致 → 不动作

优点:不改 hermes-workspace 源码,趁唤醒对账保持同步。用户聊天体验不受影响。

### 路径 C:撤销(只清密钥不动数据)

```
new-api 前端(工作区控制台)
  │ POST /api/hermes/{instance_id}/revoke-provider-key  (HMAC 鉴权)
  ▼
new-api → savvy-manager POST /internal/instances/{id}/revoke-provider-key
  │ 1. DB:provider_config_enc / provider_key_set_at / source 置空
  │ 2. 容器若在跑:docker exec 用 sed/yaml 库清 config.yaml 的 `model.provider`/
  │    `model.default`/`model.base_url`/`model.api_key` 字段(保留文件其他内容)
  │ 3. 容器不 stop,继续运行(A 层通信不受影响)
  │ 4. 用户数据 volume 完全不动
  ▼
前端展示"已撤销。下次启动需重新填密钥"
  │
  ▼ 容器内 agent 调模型失败(401/无凭证)
     用户必须回 new-api 控制台点"启动 workspace"重填 → 走路径 A
```

撤销后状态:

| 数据库 | 容器内 config.yaml | 容器状态 | 用户数据 |
|---|---|---|---|
| 加密快照清空 | provider 字段清空(API_SERVER_KEY 不动) | 不 stop,继续跑 | 不动 |

UI 保留(用户能看到工作区界面),但发消息调模型 401。重填 → agent 重载 config → 继续用。

## 5. 错误处理与风险

### 错误处理矩阵

| 失败点 | 现象 | 处理 |
|---|---|---|
| **首启未填密钥** | 用户跳过启动表单密钥字段 | new-api 前端硬校验,不让 POST。savvy-manager 二次校验,空则 400 |
| **首启密钥格式错** | 缺 `sk-` 前缀、长度 <16 | 同上,前端/savvy-manager 双层校验 |
| **`SAVVY_PROVIDER_ENC_KEY` 未配** | savvy-manager 启动加密失败 | 启动报错拒绝起(fail-closed)。**不接受明文回退** |
| **解密失败**(env key 轮换、DB 损坏) | 读旧快照失败 | 容器 wake 时跳过快照注入,落回"未配置"分支 → 用户重填(等同撤销) |
| **唤醒时 config 解析失败**(用户改错 config) | savvy-manager 反向对账读 yaml 失败 | 不回写,记 warn 日志;容器照常起,用户在工作区看错误并自修 |
| **撤销后调模型** | 401/无凭证 | **预期行为**,UI 保留可访问,引导用户回控制台重填 |
| **多实例并发** | 用户开两个 workspace | 每实例独立 `assigned_port` + 独立 `provider_config_enc`,无并发冲突 |

### 风险与节制

1. **明文短暂留内存**:savvy-manager 解密、传给 docker env 时,密钥短暂在 Python 进程内存。运行时不增风险(已加密在 DB),但强调:不入日志,也不入 docker inspect 历史(因 env 注入会暴露,见风险 2);savvy-manager 异常堆栈不打印 env 内容。

2. **docker inspect 泄漏**:`docker run -e OPENAI_API_KEY=...` 会让 key 出现在 `docker inspect` 输出 → 任何能访问 docker socket 的人可见。
   - **决策**:仍用 env 注入。任何能访问 docker socket 的人本就是部署信任边界内,与现有 A 层 `API_SERVER_KEY` 同等敏感等级。容器停后 env 不复存在(volume 数据仍保留,但 env 不在 volume)。简化实现,不改 Dockerfile.unified 的 run 脚本编排。

3. **撤销竞态**:用户撤销瞬间,workspace 内有 in-flight 调模型请求 → 已发出的请求继续走到上游成功,新请求开始 401。可接受,无需特殊处理。

4. **base_url 滥用**:首启只用我们默认端点,但用户进工作区可能改成任意 base_url(包括恶意端点收集他自己的 key)。这不影响我们(我们账户不牵涉),纯用户风险,符合"尊重意愿"。

5. **加密 key 轮换**:`SAVVY_PROVIDER_ENC_KEY` 轮换需全量解密→新 key 加密迁移脚本。首版不实现,文档里只标"轮换前请全量撤销"。

6. **new-api 知密钥状态**:撤销/改密钥状态只存 savvy-manager。new-api 前端展示"当前工作区是否用了我们的 key"需新 API `GET /internal/instances/{id}/provider-state`(HMAC 调用)。需加这个端点(见 §6)。

7. **多用户隔离**:每个 instance 独立密钥快照,不跨用户共享,符合 §3 实例级存储。

## 6. 组件改动清单

### savvy-manager(Python)

| 文件 | 改动 |
|---|---|
| `app/config.py` | 新增 `openai_base_url: str`(dev=`http://new-api:3000/v1`,prod=`https://<域名>/v1`)、`provider_default_model: str = "claude-sonnet-4"`、`provider_enc_key: str`(必填,缺则启动 fail) |
| `app/crypto.py` | **新建**:Fernet 加解密工具,封装 `encrypt_provider_config(dict)->str` / `decrypt_provider_config(str)->dict`。无 key 抛错 |
| `app/models.py` | `Instance` 加 3 列:`provider_config_enc`/`provider_config_alg`/`provider_key_set_at` |
| `app/routers/instances.py` | `issue_access_token` 不变;新增 `POST /{id}/revoke-provider-key`(HMAC 鉴权);`/{id}/start` 接收 `provider_api_key`/`provider_base_url` 参数 |
| `app/routers/users.py` | `create_instance` 不变;启动实例时若 `provider_config_enc` 为空 → 强制要求带 provider key(否则 400) |
| `app/docker_manager.py` | `create_container` environment 扩展 `OPENAI_API_KEY`/`OPENAI_BASE_URL`/`HERMES_MODEL`(**仅首启传**,唤醒不传);config.yaml 落地由 s6 run 跑 `hermes setup` 完成,不在 docker_manager 里 docker exec 写文件 |
| `app/`新文件 `provider_sync.py` | `reconcile_container_config(instance)` — 唤醒对账:读容器 config.yaml → 比对 → 加密回写 |
| `app/database.py` / main | 启动检查 `SAVVY_PROVIDER_ENC_KEY` 缺失 → fail-closed |
| `tests/test_crypto.py` | 新建,见 §7 |
| `tests/test_provider_config.py` | 新建 |
| `tests/test_instances_router.py` | 扩展 |
| `tests/test_docker_manager.py` | 扩展 |

### new-api(Go)

| 文件 | 改动 |
|---|---|
| `service/hermes.go` | `HermesInstance` 结构添加 provider-state 字段;`GetHermesProviderState(userID, instanceID)` HMAC 调 manager;新增 `RevokeHermesProviderKey`、`StartHermes` 透传 `provider_api_key` |
| `controller/hermes.go` | 新增 `StartHermes`(POST,接 `provider_api_key`/`provider_base_url` body)、`RevokeHermesProviderKey`、`GetHermesProviderState` handlers |
| `router/api-router.go` | 注册 `/api/hermes/:instance_id/start`、`/revoke-provider-key`、`/provider-state` 路由 |
| `service/hermes_test.go` | 扩展 |
| `controller/hermes_test.go` 或 `controller/hermes.go` 旁 | 新增对应测试 |

### new-api 前端(React 19)

| 文件 | 改动 |
|---|---|
| `web/default/src/features/hermes/index.tsx` | 启动 workspace 弹窗加 `providerApikey` 输入(必填,类型 password),提交带 `provider_api_key` |
| `web/default/src/features/hermes/api.ts` | `startInstance` 加参数;新增 `revokeProviderKey`、`getProviderState` API |
| 同区域新增"撤销密钥"按钮 + 状态展示("当前:我们的端点 / 你的自定义端点 / 未配置") | |

### docker 编排

| 文件 | 改动 |
|---|---|
| `Dockerfile.unified` | s6 `gateway` 服务的 run 脚本前置 `hermes setup` 写 config 条件触发:容器启动时检测 `OPENAI_BASE_URL` env,有则 setup 写 config,无则跳过(已有 config 不覆盖) |
| `docker-compose.yml` / `docker-compose.prod.yml` | manager env 加 `SAVVY_PROVIDER_ENC_KEY`(secret 占位)、`SAVVY_OPENAI_BASE_URL` |

## 7. 关键实现决策

### 7.1 B 层密钥怎么注入容器 — config.yaml 直接写入(env 不用)

**关键事实**(已在 `hermes-agent/agent/agent/auxiliary_client.py:1949-1978` 验证):agent 的 `provider: "custom"` 解析顺序是 `resolve_runtime_provider(requested="custom")` → **优先读 config.yaml**,失败才 fallback 到 env `OPENAI_API_KEY`/`OPENAI_BASE_URL`。即 config.yaml 一旦存在并被解析,env 完全失效。

**`hermes setup` 是交互式 CLI**(见 `hermes-agent/cli.py:6206` 的 "Run 'hermes setup' to configure" 提示),不适合 s6 run 自动化无 stdin 场景。

**因此最简单可靠的方式**:savvy-manager 在容器启动后通过 `docker exec` 直接写一份完整的 `/opt/data/config.yaml`,完全跳过 env 与 setup。config.yaml 模板:

```yaml
model:
  provider: custom
  default: claude-sonnet-4     # 由 settings.provider_default_model 决定
  base_url: http://new-api:3000/v1   # dev 内网名 / prod 公网域名,由 settings.openai_base_url 决定
  api_key: <解密后的用户 sk-xxx>
  api_mode: chat_completions
```

设计:

- **首次创建容器**(`docker_manager.create_container`):照旧传 A 层 env(`API_SERVER_KEY` 等不变);**不传 B 层 env**。容器启动成功后,savvy-manager 调用 `docker exec <容器名> sh -c "cat > /opt/data/config.yaml"` 写入模板(用 base64 编码避免 shell escape 风险)。Agent 启动读 config,调模型走 custom+base_url。
- **wake(stop→start)**:容器内 config.yaml 已持久化(volume),s6 run 不动它,agent 直接用。savvy-manager 跑 §7.2 对账,不一致则加密回写 DB(`source=user`)。**不再传 env,不再写文件**(books up only)。
- **不需要改 Dockerfile.unified 的 s6 run 脚本** — 不依赖 `hermes setup`,s6 现有 gateway/dashboard 配置不变。

### 7.2 唤醒对账时机 — start_container 前,只读不改写容器

### 7.2 唤醒对账时机 — start_container 前,只读不改写容器

`savvy-manager/app/docker_manager.py` 的 `start_container` 被调时(用户 wake 或自动 wake),先调 `reconcile_container_config(instance)` 再 docker start。reconcile 流程:

1. 若 DB 有 `provider_config_enc` → 解密,作为"DB 记录的配置"
2. 若容器在 Running 状态:读容器内 `/opt/data/config.yaml`(docker exec cat)→ 解析 yaml
3. 不一致 → **容器内 yaml 为主**(因为 agent 也用 config),加密回写 DB,标 `source="user"`
4. 一致 → skip

容器 stop 状态下 docker exec 不可用 → 只在容器 Running 时对账。stop→start 周期中,容器内 config.yaml 已存在(volume 持久),s6 run 跳过 setup,agent 用 config,无需重新注入 env。

**撤销后 wake**(§7.3 描述):撤销已清容器 config.yaml provider 字段 + DB 快照 → wake 时容器内无 provider 配置 → s6 run 检测:`[ -f /opt/data/config.yaml ]` 仍 true(文件存在只是清了字段)→ 不跑 setup。需改成更精确的判断:s6 run 检测 config 中 `model.provider == custom` 才跳过 setup,否则重跑 setup(此时无 env → setup 进入空配置分支,agent 调模型失败,符合撤销语义)。

### 7.3 撤销如何清容器内 config

撤销时容器在跑 → docker exec 用 yaml 工具清 `model:`/`api_key:`/`base_url:` 三个顶层字段,保留其他(API_SERVER_KEY 在 env 不在 yaml,不受影响)。用户数据 volume 完全不动。

撤销时容器 stop → 只清 DB,下次 start 时容器内 config 仍是旧的,但 s6 run 会因 env 缺失(我们的 start_container 在撤销后会清空 env 注入)而 setup 一个空配置。具体:撤销后 docker_manager 知道 instance 的 `provider_config_enc` 为空,下次 start 时不传 `OPENAI_API_KEY`/`OPENAI_BASE_URL` env,s6 run 检测无 env 跳过 setup(保留旧 config.yaml 还是清掉?— **决策:旧 config.yaml 删除后容器内 agent 调模型自然失败,符合撤销语义**)。

## 8. 测试策略

按 new-api CLAUDE.md 的测试质量原则(保护真实行为/契约/不变式,不为覆盖率写测试)。

### savvy-manager(Python,pytest)

1. **`test_crypto.py`**(新建)— Fernet 加密往返:
   - 加密→解密还原
   - `SAVVY_PROVIDER_ENC_KEY` 缺失 → fail-closed 抛错
   - 旧 key 解密失败 → 不静默,抛待处理错误

2. **`test_provider_config.py`**(新建)— 三条路径核心不变式:
   - **A 首启**:必填 key 缺失 → 400;有效 → DB 加密快照写入 + `source="ours"`
   - **B1 对账**:wake 时容器 config.yaml 跟 DB 快照不一致 → 抄回 DB + `source="user"`
   - **C 撤销**:数据库快照清空 + 容器 config provider 字段清空 + **数据 volume 不动**(断言 volume 文件列表不变 + sessions 文件存在)

3. **`test_instances_router.py`**(扩展现有)— `/internal/instances/{id}/start` 接收 `provider_api_key` 参数 + `/revoke-provider-key` 端点:
   - HMAC 鉴权不变
   - 接收新参数路由到 docker_manager
   - 端口分配/分配逻辑不变(回归)

4. **`test_docker_manager.py`**(扩展)— env 注入扩展:
   - `OPENAI_API_KEY`/`OPENAI_BASE_URL`/`HERMES_MODEL` 正确传入
   - 不泄漏到日志(断言 `caplog.text` 不含 key 内容)

### new-api(Go,testify)

5. **`hermes_test.go`**(扩展现有)— controller/service 层:
   - `provider_api_key` 字段从前端 → controller → service → 透传到 manager(字段不丢、不串改)
   - HMAC 签名涵盖新 body 字段
   - `GetHermesProviderState` 端点返回正确状态(ours/user/none)

### 不写的测试(避免反模式)

- 不测 docker inspect 输出(跟实现细节强绑定)
- 不测 Fernet 内部(轮子已测过)
- 不测 yaml 解析异常(hermes-agent 自己负责)
- 不测 new-api 前端 React 表单校验(已有前端测试体系,不重复)

### 端到端验收(手测,文档记录)

1. 全新用户首启 workspace,必填 sk-xxx,启动后 workspace `/api/sessions` 200
2. 进工作区发消息 → 流式返回(验证 B 层密钥生效)
3. Settings 页改自己的 Anthropic key → 仍能调模型
4. sleep→wake → 用新 key 仍能调用(验证 B1 对账)
5. 点撤销 → workspace UI 保留,发消息 → 401
6. 回控制台重填我们的 sk → 恢复

## 9. 涉及的代码位置参考

- `savvy-manager/app/docker_manager.py:81-99` — 现有 env 注入位置(API_SERVER_KEY 模式参考)
- `savvy-manager/app/routers/instances.py:42` — `/{instance_id}/start` 端点
- `savvy-manager/app/routers/users.py:73` — `create_instance`
- `savvy-manager/app/config.py:17` — Settings 类扩展位置
- `new-api/controller/hermes.go:206` — `GetHermesAccessToken` 参考
- `new-api/service/hermes.go:55-69` — HMAC 鉴权 + service URL 工具
- `new-api/web/default/src/features/hermes/index.tsx:84` — `workspaceUrl` 拼装位置
- `Dockerfile.unified` — s6 gateway run 脚本位置
- `hermes-agent/agent/auxiliary_client.py:1965-1966` — agent 读 `OPENAI_API_KEY`/`OPENAI_BASE_URL` 证据
- `hermes-agent/cli-config.yaml.example:14` — 默认 `model.default: "anthropic/claude-opus-4.6"`
- `hermes-workspace/docs/docker.md` — `HERMES_API_TOKEN=API_SERVER_KEY` 关系
- `hermes-workspace/README.md:234-243` — provider key env 注入范例(README 原文证实运行时读 env)

## 10. 未纳入本设计的事项

- OAuth 登录(Gmail/GitHub)— 独立项目
- Stripe/Creem 支付 webhook — 独立项目
- 端口池耗尽时的扩容策略 — 已在 workspace-routing.md 覆盖,本设计不涉及
- `SAVVY_PROVIDER_ENC_KEY` 轮换迁移 — 首版标 "轮换前请全量撤销",迁移脚本留作后续
- API_SERVER_KEY 从硬编码改为 env/secret — 独立改进(与本设计正交)

## 11. 控制台使用说明与提醒文案(new-api 前端)

new-api 工作区控制台需在关键交互点补充说明与提醒,**降低用户误操作 + 突出商业诉求(默认走我们的端点)**。所有文案走 i18n(`t('English key')`),中英双语。

### 11.1 启动 workspace 弹窗(密钥输入处)

输入框下方提示文案要点:
- **为什么需要**:"为了让你能调用模型,首次启动需填入一个 API 密钥。我们推荐使用你在本平台生成的密钥(扣本平台账户余额,模型按本平台价格计费)。"
- **从哪取**:"在 [API Keys 页面] 生成一个密钥,粘贴到这里。" 含跳转链接到 `new-api` 令牌管理页
- **可不可填自己的**:"如果你有自己的 Anthropic/OpenAI 等供应商密钥,启动后也可在工作区 Settings 中改用 — 但费用将由供应商直接向你收取,不走本平台账户。"
- **安全承诺**:"密钥加密存储于服务器,可在工作区控制台随时一键撤销。撤销只清密钥,你的会话/文件/记忆数据不受影响。"

### 11.2 工作区卡片 / 状态展示(已启动后)

工作区状态旁需展示当前密钥来源(`GetHermesProviderState` 返回的 `source`):
- `ours` → "当前使用:本平台密钥(账户计费)"
- `user` → "当前使用:你自定义的供应商密钥(费用由供应商收取)"
- `none`(已撤销)→ "未配置密钥,发消息会失败。请重新启动并填入密钥。"

### 11.3 "一键撤销密钥"按钮处

按钮旁/确认弹窗:
- **动作说明**:"撤销将清空当前 workspace 的所有 LLM 供应商密钥(本平台密钥 + 你自定义的密钥)。"
- **数据保留**:"撤销不会删除你的会话历史、文件、记忆、skills — 这些数据原封不动保留在容器里。"
- **后果提醒**:"撤销后,workspace 内发消息将失败(返回 401),直到你重新填入密钥为止。"
- **二次确认**:撤销动作需二次确认弹窗,避免误点

### 11.4 通用提醒(可选,放页脚或帮助页)

- 密钥轮换:用户提示"建议定期在 new-api 令牌页轮换密钥,旧密钥自动失效后需重新启动 workspace"
- 安全建议:"如担心密钥泄漏,可随时点撤销清空。"

### 实施约束

- 所有文案进 `web/default/src/i18n/locales/{en,zh}.json`,键为英文源串
- 启动弹窗密钥字段 `providerApikey` 类型 `password`,带可切换明文查看
- 撤销按钮颜色醒目(red/danger),二次确认不可跳过

