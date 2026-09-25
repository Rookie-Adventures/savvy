# 交接任务书：机B 部署 dev 分支（SkillPay / 智能体代触发充值）

> 给执行部署的 Agent：本文档自包含，按步骤执行即可，不需要额外上下文。
> 目标 commit：`930b995c10`（dev 分支 HEAD，含以下三个特性）
> - `feat(skillpay)`: new-api X402 付费问答接口（`/api/skill/invoke`）
> - `feat(skillpay)`: 微信 Agent 充值动作（服务包，金额自定）
> - `feat(agent-topup)`: **微信智能体代触发原生充值**（`/api/agent/wechat/topup/create`，本次主要目标）+ X-Agent-Token 鉴权

## 一、目标

把机B 上运行的 new-api 更新到 dev 分支 `930b995c10`，使新接口生效。**只重建 new-api 容器**，其余（redis / newapi-db / nginx / savvy-manager）一律不动。

## 二、前置信息（执行方需自备/确认）

| 项 | 值 |
|---|---|
| 部署根目录 | 机B `/opt/savvy` |
| compose 文件 | `/opt/savvy/deploy/docker-compose.yml` + `docker-compose.override.yml` |
| 服务名 | `new-api`（容器内监听 3000） |
| 数据库容器 | `newapi-db`（PostgreSQL，user `newapi`，db `new-api`） |
| 区域 | 阿里云 `cn-shenzhen` |
| 远程执行方式 | 优先 `deploy/ops/run_remote.ps1`（aliyun ecs RunCommand），需要 **实例 ID**（`i-` 开头）；或直接 SSH |
| 数据库备份目录 | `/opt/savvy/deploy/data/` |

## 三、执行脚本（已备好，不要重写）

`e:\savvy\deploy\ops\deploy_skillpay.sh`，本地已存在，流程：

1. 记录当前 commit 作为回滚点
2. **pg_dump 备份数据库**（必做）
3. `git fetch/checkout dev && git pull --ff-only origin dev`
4. `docker compose build new-api` → `up -d --force-recreate new-api`
5. sleep 20 → `curl http://localhost:3000/api/status`
6. 新路由存在性验证（见第五节验收标准）
7. 打印回滚命令

执行（Windows PowerShell）：

```powershell
powershell -File e:\savvy\deploy\ops\run_remote.ps1 `
  -File e:\savvy\deploy\ops\deploy_skillpay.sh `
  -Instance <机B实例ID>
```

实例 ID 未知时先问用户，不要猜、不要盲试。

## 四、可选：启用接口鉴权（建议）

若需防公网刷单，在**机B `/opt/savvy/deploy/.env`** 追加（值需与百炼平台环境变量一致）：

```bash
AGENT_TOPUP_TOKEN=<32位随机串>
```

加了才校验 `X-Agent-Token` 头；不加接口保持公开，不影响功能。

⚠️ **不要把任何密钥写进仓库或提交 git。**

## 五、验收标准（全部满足才算成功）

| 检查 | 期望 |
|---|---|
| `GET /api/status`（容器内 localhost:3000） | HTTP 200 |
| `docker ps` new-api | Up 状态 |
| `POST /api/agent/wechat/topup/create {"amount_yuan":0.001}` | HTTP 200 且 body 含 `充值金额需在 0.01~5000 元之间`（**证明路由存在且下限已放开到 1 分**；若提示 1~5000 说明还是旧构建，404=未部署成功，500=微信配置缺失） |
| `POST /api/agent/wechat/topup/create {"amount_yuan":0.01}` | HTTP 200 且 body 含 `"code_url":"weixin://wxpay/bizpayurl?pr=` 与 `claim_token`（**1 分极小单能真下单**） |
| `POST /api/skill/invoke {"action":"topup","amount_yuan":0.1}` | HTTP 503 `SKILLPAY_DISABLED`（未配 SKILLPAY_* 时的**正常**表现） |
| 最近 2 分钟日志 | 无 panic / 无 AutoMigrate 报错 |

外网验证（部署完成后）：

```bash
curl -s -X POST https://scheng.net/api/agent/wechat/topup/create \
  -H 'Content-Type: application/json' -d '{"amount_yuan":0.1}'
```
同样应返回金额范围错误（不是 404）。

## 六、回滚

```bash
cd /opt/savvy && git checkout <部署前记录的 OLD_REV> && cd deploy && \
docker compose -f docker-compose.yml -f docker-compose.override.yml build new-api && \
docker compose -f docker-compose.yml -f docker-compose.override.yml up -d --force-recreate new-api
```

数据库如需回滚：`docker exec -i newapi-db psql -U newapi new-api < /opt/savvy/deploy/data/newapi-backup-<时间戳>.sql`

## 七、禁止事项

- 不动 nginx（机A）、不动数据库/redis/manager 容器
- 不 force push、不修改 dev 分支历史
- 不提交任何密钥/私钥/token 到 git
- 不执行 `git checkout .` 之类会丢弃服务器本地改动的命令（先 `git status` 确认）
- 备份失败时**停止部署并报告**，不要硬着头皮继续

## 八、汇报格式

```
部署结果：成功/失败
机B 部署前 commit：xxxxx
部署后 commit：930b995c10
备份文件：/opt/savvy/deploy/data/newapi-backup-<ts>.sql（大小 xx MB）
健康检查：api/status HTTP 200
路由验证：create=HTTP 200(金额校验错误) / skill/invoke=HTTP 503
异常日志：（无 或 附上关键行）
是否启用 AGENT_TOPUP_TOKEN：是/否
```
