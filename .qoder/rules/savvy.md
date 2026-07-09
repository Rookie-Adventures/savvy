---
trigger: always_on
---
# 项目规则

## 项目身份
- 本仓库是 savvy monorepo,四个子项目:`new-api`(Go+React 网关)/`hermes-agent`(Python 自进化AI)/`hermes-workspace`(TS/React 前端)/`savvy-manager`(Go/Python Docker编排)。

## 不可触碰的品牌与归属(最高优先级)
- 永不修改、删除、替换以下身份的引用/提及/品牌/归属:
  - `QuantumNous`(组织/作者身份)
  - `new-api`(项目名)
- 公开品牌:SaaS叫 **Savvy Agent**;容器产品叫 **Hermes Cloud Workspace**。
- 公司=**粟城科技网络工作室**;客服邮箱 `support@scheng.net`。

## 技术红线
### new-api 后端(Go)
- 数据库必须同时支持 SQLite + MySQL(>=5.7.8) + PostgreSQL(>=9.6),用 `model/main.go` 的 Dialect 适配。
- 所有 JSON 序列化/反序列化必须走 `common/json.go` 自定义包装,禁止直接用 `encoding/json`。
- 分层顺序: `router/` -> `controller/` -> `service/` -> `model/`(GORM)。
- `relay/` 放 AI provider 适配器与路由。

### savvy-manager
- 独立数据库,不与 new-api 共库。
- 与 new-api 通信走 HMAC 签名私有 API。

## 业务约定
- 免费 plan 容器每次跑 3 小时后自动 sleep(manager daemon 管),数据/卷保留。
- 部署:双机合规——机A备案站只反代静态,机B国内机跑整栈。客户测试账户入口走 scheng.net。

## 代码风格偏好
- 写代码要贴合周边现有风格(注释密度、命名、惯用法)。
- 多语言文案(i18n)是项目的一部分,新增字段记得同步 `en/ja/fr/ru/vi/zh`。
- 留痕问题放 `docs/records/`(一问题一 md),不是 plans/specs。

按你那工具支持的格式填进去就行。要不要我针对某个子项目再细化?

✻ Cooked for 25s

❯ 补充一下如果要修改代码的约束啥的 假设你现在是这个项目的经理，你会怎么交代你的手下嘛

经理交代手下的口吻,把约束分层说清楚:

# 改代码必读(项目经理交代)

## 0. 动手前(止杀阀)
1. 先问:这事非改不可吗?别人提的需求是"想要"还是"非要"。speculative 的就先放着,一句话说清"先不做"。
2. 再问:这功能代码库里已经有了没?helper/util/类型/惯用法就在隔壁几个文件里 → 复用,别重造。重写一个本就存在的东西是最常见的烂活。
3. 看清楚要动的代码:把真实流程从头到尾走一遍,每个相关文件都看,别之前别动手"——最短的错误改动 ≠ 最懒的改动,是第二个 bug。

## 1. 不可碰的(品牌与归属红线)
- 永不修改/删除/替换:`QuantumNous`(组织身份)、`new-api`(项目名)的所有引用、提及、品牌、归属。
- 公开品牌照旧:SaaS=Savvy Agent;容器产品=Hermes Cloud Workspace;support@scheng.net。

## 2. 技术红线(违反=回滚)
### new-api(Go)
- DB 必须同时跑通 SQLite + MySQL(≥5.7.8) + PostgreSQL(≥9.6)。写  Dialect 适配。
- JSON 一律走 `common/json.go` 包装,禁用 `encoding/json` 直接调。
- 分层:`router/ → controller/ → service/ → model/`(GORM)。别跨层
- provider 适配器放 `relay/`。
### savvy-manager
- 独立 DB,不与 new-api 共库;跨服务通信只走 HMAC 签名 API,别开裸

## 3. 怎么改(懒但正确)
- 找根因,别贴症状。ticket 说的症状只是入口 — 改之前把要动函数的  径上修一处,别在每个调用方各贴一个 guard。
- 最短能跑的 diff 优先。但前提是"看懂之后"。
- 别引入未要求的抽象:只有一个实现就别写接口,只有一个产品就别写工厂,永远不变的值就别做配置。
- 不写"以后用"的脚手架。以后的事以后再说。
- 删优于增。闷大于聪明——半夜 3 点被人叫起来 decode 的就是聪明代码。
- 故意的简化标一下:`// ponytail: 这里是 XXX,上限是 YYY,要升级走
- 两个 stdlib 方案一样短 → 取边界情况对的那个。

## 4. 代码风格
- 贴周边:看过再写,密度、命名、惯用法跟邻居文件对齐,别另起一套。
- i18n 是项目一部分,新增文案同步 en/ja/fr/ru/vi/zh。
- 问题留痕放 `docs/records/`(一问题一 md,含症状/根因/思路/改动/验证/限制/尾巴),不是 plans/specs。

## 5. 验证(非平凡逻辑必留一个可跑的检查)
- 有分支/循环/解析/钱或安全路径的逻辑 → 留一个最小的 assert 自检或一个 `test_*.py`/单测,改动破了就红。一行平凡的 trivial 不用测。
- trivial 调用方别全写单测套件,除非被要求。

## 6. 业务约定(别混淆)
- 容器:免费 plan 每次起跑 3 小时自动 sleep,卷保留。
- 部署:双机合规——机A 备案站只反代静态,机B 国内机跑整栈。入口 sch

## 7. 能砍的别省
- 解释 ≤ 3 行:跳过了啥、啥时候加。别写 essay、别做功能导游、别写
- 用户明确要的(报告/走查/分阶段说明)给全——那不算债。

## 8. 边界
- 绝不简化掉:信任边界的输入校验、防数据丢失的错误处理、安全措施、a11y 基本项、任何明确要求的东西。
- 用户坚持要全量版 → 建就是了,别再劝。