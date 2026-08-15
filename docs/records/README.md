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
- [2026-08-01-workspace-entry-401-and-token-renewal.md](./2026-08-01-workspace-entry-401-and-token-renewal.md) — workspace 撞401两根因：wslrelay在::1劫持localhost(改SAVVY_PUBLIC_HOST绕)+token 30min无续期(nginx auth_request滑动续期X-Renewed-Token→刷cookie)；前端跨上下文重签不可行；4commit已合e2e过
- [2026-08-02-workspace-404-mask-not-found.md](./2026-08-02-workspace-404-mask-not-found.md) — 401修通后落地404蒙版(SPA内部Page Not Found);改router.tsx basepath=BASE_URL派生+镜像重建+新token仍404;排除token/HTTP404/SSR首屏/route树;锁定浏览器0 script(SPA未hydrate)+可能SW(/sw.js)拦截/workspace无尾斜杠导航;**未解 → 已由 2026-08-15 结案**
- [2026-08-15-workspace-404-and-model-switch.md](./2026-08-15-workspace-404-and-model-switch.md) — 结案08-02悬案:404根因=vite base '/workspace/' + tanstackStart()没配basepath,Start启动无条件router.update({basepath:process.env.TSS_ROUTER_BASEPATH})覆盖成'/'→全失配落splat;修法=base改回'/'(端口池架构本就独占根路径,前缀是遗留);__HERMES_WORKSPACE_BASEPATH__在Start下是死代码(注入实测无效)。模型切换=composer只写本地store+chat-screen没读它,且/api/model-switch路由从未实现(SPA兜底200 text/html假装成功);更关键是上游chat/stream收下provider/model但runtime恒空永不采用(三探针实证)→只能写config.yaml;新增lib/set-default-model.ts(providerId沿用现值防脱离new-api网关+断言响应真JSON),4测过,new-api日志实证pro;顺手修models.ts按live-proxy活目录id收敛(hermes-agent不在网关里,选中写config"成功"但发消息才503),6→5正好等于new-api全集
