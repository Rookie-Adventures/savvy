# 留痕目录 (records)

实际遇到的问题 → 根因 → 修复思路回顾。一问题一文档,作为历史留痕与排查蓝本。

## 与其它 docs 的区别

- `superpowers/plans/` — 实施计划(动手前)
- `specs/` — 设计规格
- `records/` — **事后回顾**:实际撞了什么、根因怎么挖到、怎么修、留了什么尾巴

## 目录

- [2026-07-09-hermes-plan-drift-and-spec-display.md](./2026-07-09-hermes-plan-drift-and-spec-display.md) — Pro 用户控制台仍显示 FREE+2h、容器规格不显示
- [2026-07-10-user-group-drift-and-coverage.md](./2026-07-10-user-group-drift-and-coverage.md) — Pro 用户组漂移无自愈、规格 storage 没跟升、付费新用户启动报 models 空
- [2026-07-11-dual-machine-production-deploy.md](./2026-07-11-dual-machine-production-deploy.md) — 双机生产部署实录：端口池 HTTPS、nginx 配置、安全组、运维命令速查
- [2026-07-18-antchain-evidence-logrus-and-key-deploy.md](./2026-07-18-antchain-evidence-logrus-and-key-deploy.md) — 蚂蚁链存证：SDK init 污染 logrus + 私钥无部署位 + 按 design.md 对账(握手失败仍装钩子/json→common/demo加回)；段1 TestShake 真握手已通，段2 真上链待办
- [2026-07-19-alipay-appeal-docs.md](./2026-07-19-alipay-appeal-docs.md) — 支付宝冻结/核查申诉官方要求实测留痕(Exa搜官方文档)：核查规则+商家安全服务小程序解限+准入门店照片卡点+链证deliverOrder对应物流凭证的SaaS等价物+两条申诉线区分+工具调用备忘
- [2026-07-20-antchain-stage2b-e2e-closed.md](./2026-07-20-antchain-stage2b-e2e-closed.md) — 蚂蚁链段2b deliverOrder发货第四步真链E2E通+生产20元沙箱真单链证四步上链直查验证（三getter有值）；11中文锁键+收款主体工商全称；trace成功日志已补未重编；尾巴=重编镜像/机B生效
