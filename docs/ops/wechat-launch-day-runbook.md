# 微信服务号上线日作战手册（2026-09-16）

> **用途**：经理额度耗尽前的完整交接文档。用户（老板）+ 接手智能体（同事）照此执行即可续接，无需历史上下文。
> **阅读顺序（接手智能体必读）**：本手册 → `docs/ops/wechat-launch-checklist.md`（勾选状态）→ `.superpowers/sdd/task-9-review.md` / `task-10-review.md`（已合并代码的审查结论与债台账）→ 需要改代码时再看 `task-9-brief.md` / `task-10-brief.md`。
> **边界红线**：不重置任何密钥（AppSecret/APIv3/商户私钥）；不动生产 DB 真实值；不解绑老 AppID（wx5757dd64f1823d38）关联；不改品牌字（QuantumNous/new-api/栗橙科技口径）；菜单里永不放 kfid 外链。

---

## 1. 今晚（09-15）状态快照

| 项 | 状态 |
|---|---|
| 代码 | Task 9（JSAPI 收银台）+ Task 10（微信身份）+ deeplink（?amount= 预填）已合并 dev、已 push origin、**已部署生产**（机B 镜像 2026-09-15 凌晨切换，两新表已建） |
| 本地 docker | 已验证：build 0 / 新表 / status 200 |
| 生产后台配置 | `WechatMpAppId`、`WechatAppSecret` **空**（JSAPI/微信登录休眠中，点了会友好报"未配置"）；Native 微信配置正常；支付宝全绿（09-15 验证单通过） |
| 服务号 | 认证**审核中**（预计 09-16 过）；法人绑定✓；简介合规✓；IP 白名单 120.77.11.137✓；菜单**未发布**（首发被拒：外链域名未进名单）；网页授权域名**未配** |
| 商户平台 | 新 AppID **未关联**；支付授权目录**未配** |
| 微信收款限制申诉 | 审理中（1-3 工作日），驳回原文发经理/接手者 |

---

## 2. 明日任务总表（按序执行，标注负责人）

### Step 0 认证确认〔用户〕
mp.weixin.qq.com → 设置与开发 → 微信认证 → 状态。
- **已通过** → 继续 Step 1。
- **仍审核中** → 先做 Step 3 里不依赖认证的两条自动回复，其余等回执；不要重复提交认证。

### Step 1 校验文件放置〔用户下载 → 智能体放置〕
1. 用户：mp → 设置与开发 → 账号设置 → **功能设置**（滚到底）→ 网页授权域名 → 点设置 → 下载校验文件（形如 `MP_verify_xxxxxxxx.txt`，内容一行字符串）。
2. 用户把**文件名+内容**发给智能体（对话或贴进 `tmp/mp_verify.txt` 注明文件名）。
3. 智能体：放**机A**（实例 `i-wz953hq55nljkz6jwm21`）nginx 站根：
   ```bash
   # 经 run_remote.ps1 执行（见 §6 命令 cookbook）
   printf '%s' '<文件内容>' > /var/www/savvy-agent/<文件名>.txt
   chmod 644 /var/www/savvy-agent/<文件名>.txt
   curl -s https://scheng.net/<文件名>.txt   # 自证：输出=文件内容
   ```
   路径依据：`deploy/nginx-scheng.conf` L41 `root /var/www/savvy-agent;`，`location /` 的 try_files 先命中精确文件。
4. 用户：回对话框填域名 `scheng.net`（**裸域：无 https://、无路径、无尾斜杠**）→ 保存。平台当场拉文件校验，过即生效（生效可能有 ≤5 分钟传播，菜单发布失败时先等 5 分钟重试一次）。
   > **✅ 09-15 已完成**：`MP_verify_BikG1GNzuH6axRKI.txt` 已放机A 并公网自证通过（网页授权域名 + 业务域名共用此文件）。
   > **坑（已踩）**：机A 线上 nginx 是"全量反代机B"版而非本仓库模板，且 regex `\.txt$` 会把 .txt 劫持到 workspace(401)——校验文件必须配 `location =` 精确匹配（已插入线上 443 server 段，备份在机A `/tmp/nginx-default.bak`）。**将来换新校验文件，记得同步加新 `location =` 条目**。

### Step 2 商户平台两件事〔用户〕
pay.weixin.qq.com（管理员扫码）：
1. 产品中心 → AppID 账号管理 → **关联** `wxd0fbdccc072a49e2`（服务号管理员微信确认）。**严禁解绑/删除老 AppID wx5757dd64f1823d38**（Native 扫码付命根）。
2. 产品中心 → JSAPI 支付 → 支付授权目录 → 添加两条（**带尾斜杠、精确到目录**）：
   - `https://scheng.net/wallet/`
   - `https://scheng.net/subscriptions/`
   （旧文档里的 /console/ 是经典版路由，作废勿填。）

### Step 3 服务号内容配置〔用户〕
1. **自定义菜单**（认证后才可发布外链）→ 保存并发布：

   | 菜单 | 类型 | 内容 |
   |---|---|---|
   | AI服务（一级） | 跳转网页 | `https://scheng.net/` |
   | 充值套餐（一级，父） | 无（挂子菜单） | — |
   | └ 充值额度 | 跳转网页 | `https://scheng.net/wallet?amount=100` |
   | └ 300元服务包 | 跳转网页 | `https://scheng.net/wallet?amount=300` |
   | └ 订阅套餐 | 跳转网页 | `https://scheng.net/subscriptions` |
   | 联系客服（一级） | **发送消息→文字** | 「客服邮箱 support@scheng.net；或访问官网 scheng.net 点击右下角客服按钮。」 |

   名称限制：一级 ≤4 汉字/8 字母；子菜单 ≤8 汉字。发布报「链接内容不属于当前公众号」→ 回 Step 1 查域名是否保存成功/等 5 分钟；确认菜单里没有 kfid/非 scheng.net 外链。
2. **被关注自动回复**：「感谢关注栗橙科技官方服务号！我们提供 AI 智能体与云工作区等在线软件服务。官网：scheng.net，点击即可访问体验；问题咨询请邮件 support@scheng.net。」
3. **关键词自动回复**：关键词「客服」→ 回复带 `https://work.weixin.qq.com/kfid/kfc9a0826599eedd605` 的文本；关键词「100」「服务包」→ 回复带 `https://scheng.net/wallet?amount=100` 的文本（可选）。

### Step 4 后台填值 + 重启〔用户填 → 智能体重启〕
1. 用户：new-api 管理后台 → 系统设置 → 支付网关 → 微信配置段：
   - 「微信服务号 AppID」= `wxd0fbdccc072a49e2`
   - 「微信 AppSecret」= 凭据清单所存那串（**从 `e:\savvy-keys-inventory.md` 复制，勿手打**；密码框，留空=不更新）
   - 其余老字段（微信 App ID/商户号/序列号/公钥/私钥）**一个都不动**。
   保存。
2. 智能体：机B 重启（单例缓存必须重启才加载）：
   ```bash
   docker restart new-api && sleep 15 && curl -s -m 10 -o /dev/null -w '%{http_code}\n' http://localhost:3000/api/status   # 期望 200
   ```

### Step 5 验收〔用户操作 + 智能体盯日志〕
按序，全过=上线完成：
1. **微信内收银台**：微信打开服务号 → 菜单「充值额度」→ 金额已预填 100（deeplink 生效）→ 未登录则静默授权秒登（Task 10 direct）→ 选微信支付 → **收银台弹起**（非二维码）→ 付 ¥1（可改最小金额）→ 额度到账、日志 notify 200。
2. **桌面扫码登录**：PC 登录页「微信登录」→ QR → 手机扫 → ①已绑微信：桌面落会话；②未绑微信（换个没绑的微信号扫）：桌面亮[创建新账户]/[绑定已有账户]两按钮，各走一遍（创建拿一次性初始密码→登录→改密码）。
3. **个人中心绑定**：登录态 → 个人中心「微信绑定」卡 → 绑定→打码 openid 显示→解绑→再绑。
4. **回归**：PC 微信扫码（Native）充值一单、支付宝一单、密码登录、老用户会话——全部不受影响。
5. 任一失败 → §8 故障速查。

### Step 6 收尾〔智能体〕
- 勾选 `docs/ops/wechat-launch-checklist.md` 对应项；
- 出问题按项目规矩留痕 `docs/records/YYYY-MM-DD-<问题>.md`；
- 汇报老板：上线完成 + 债台账现状（task-9/10-review.md 非阻塞观察清单）。

---

## 3. 配置值速查（唯一权威来源）

| 项 | 值 | 备注 |
|---|---|---|
| 服务号 AppID | `wxd0fbdccc072a49e2` | 原始 ID gh_5d09d0d431e5；JSAPI/身份全链路用它 |
| 服务号 AppSecret | 见 `e:\savvy-keys-inventory.md` | **本手册不写明文**；重置即废旧值，禁点 |
| Native 老 AppID | `wx5757dd64f1823d38` | 只服务 Native 扫码付，勿动勿解绑 |
| 商户号 | `1000524187` | 公钥模式（WechatPayPublicKeyId+PublicKey 已配） |
| 网页授权域名 | `scheng.net` | 裸域；mp 功能设置 |
| 支付授权目录 | `https://scheng.net/wallet/`、`https://scheng.net/subscriptions/` | 带尾斜杠 |
| 机A（备案站） | 实例 `i-wz953hq55nljkz6jwm21`，公网 8.135.58.63 | nginx 站根 `/var/www/savvy-agent` |
| 机B（整栈） | 实例 `i-wz9b5nhr3idgu8fqvnvk`，公网 120.77.11.137 | 仓库 /opt/savvy，compose 在 /opt/savvy/deploy |
| API IP 白名单 | 120.77.11.137 | 已配（开发者平台） |
| 后台新字段 | `WechatMpAppId`、`WechatAppSecret` | Task 9 新增，支付网关页 |
| 客服 kfid | `https://work.weixin.qq.com/kfid/kfc9a0826599eedd605` | 只进关键词回复，不进菜单 |

---

## 4. 服务包/ deeplink 用法（运营侧）

- **原理**：菜单/关键词里的链接带 `?amount=N` → 钱包页自动预填 N（整数、>0；低于 min_topup 自动回落下限，支付时校验不变）。代码已上线，**加新包=菜单加一条子菜单 URL，零代码**。
- **金额语义**：N=充值额度金额（元），账单显示「栗橙科技-服务包」；订阅套餐在 /subscriptions 选plan，账单「栗橙科技-<plan名>套餐」。
- **min_topup 当前值**：管理后台充值设置里看（改它影响所有渠道下限）。
- **改包价/加包**：只改菜单 URL；不要改代码。
- **iOS 注意**：微信内 iOS 用户虚拟商品购买合规口径沿用小程序决策（隐藏引导话术即可，H5 支付本身不受 IAP 约束因走微信收银台）。

---

## 5. 命令 cookbook（智能体执行用）

远程执行统一入口（本地 PowerShell）：
```powershell
powershell -ExecutionPolicy Bypass -File e:\savvy\run_remote.ps1 -File <本地脚本.sh> -Instance <实例ID> -Timeout <秒> -Wait <秒>
```
- 机B 重启：`ops_restart.sh`（仓库根，restart+health）或直写 `docker restart new-api`。
- 机B 部署新代码（仅当明天有代码变更）：
  ```bash
  cd /opt/savvy && git pull
  cd /opt/savvy/deploy
  bash -c 'docker compose -f /opt/savvy/deploy/docker-compose.yml -f /opt/savvy/deploy/docker-compose.override.yml build new-api'
  bash -c 'docker compose -f /opt/savvy/deploy/docker-compose.yml -f /opt/savvy/deploy/docker-compose.override.yml up -d --force-recreate new-api'
  ```
  **坑（已踩）**：assist shell 裸跑 `docker compose up` 报 "no configuration file provided"——必须显式 `-f` 双文件且包 `bash -c`（见经验库 3a2dec79）。
- 机A 放校验文件：实例换 `i-wz953hq55nljkz6jwm21`，命令见 Step 1.3。
- 生产配置长度核查（不打印明文）：`tmp/ops_wechat_cfg_len.sh`。
- 本地验证：`docker compose build new-api && docker compose up -d`（先确认 Docker Desktop 已启动）。

---

## 6. 故障速查（症状 → 第一动作）

| 症状 | 第一动作 |
|---|---|
| 菜单发布「链接内容不属于当前公众号」 | 域名没保存/未生效：回 Step 1 核对保存状态，等 5 分钟重试；确认无 kfid 外链 |
| 微信授权报 redirect_uri 错误（10003） | 网页授权域名填错格式（必须裸域 scheng.net）或未保存 |
| JSAPI 下单 APPID_MCHID_NOT_MATCH | 商户平台没关联新 AppID（Step 2.1） |
| 点微信登录/绑定报「微信登录未配置」 | 后台两字段没填或没重启（Step 4） |
| 支付成功不到账 | 看机B 日志 notify 状态码：400=验签败→对照 `docs/records/2026-09-12-wechat-pay-publickey-notify-400.md`（双端同模式铁律）；200 未到账=幂等/额度逻辑，查 top_ups 状态 |
| 支付宝 invalid-signature | 应用公私钥配对问题，见 09-14 排查记录；密钥工具重生成→平台换公钥→后台换私钥→重启 |
| 扫码后手机页「链接已失效」 | 票证 5 分钟过期/已消费，重新发起即可（设计行为） |
| 认证被驳 | 驳回原文发经理/接手者，按原文改材料重提（个体户主体+执照全称口径） |
| 收款限制申诉被驳 | 同上，原文交接 |

---

## 7. 日历与成本（别忘）

- **服务号年审**：约 2027-09 到期，300/年——设日历；过期=菜单外链/网页授权/支付全灭。
- **开放平台 300 一次性**：仅将来 unionid（服务号+小程序同绑）时才花；先试免费绑定路径。
- **AppSecret 轮换**：上线稳定一周后，开发者平台重置+后台同步+重启（该 secret 曾在对话明文出现）。
- **模板消息**（支付/到账通知）：债台账，需申请模板，不挡上线。
- **监控**：开发者平台接口监控加告警（授权/支付失败率）；机B 日志盯 wechat notify 非 200。

---

## 8. 相关文件索引

| 文件 | 内容 |
|---|---|
| `docs/ops/wechat-launch-checklist.md` | 配置勾选总表（A-F 组） |
| `docs/ops/wechat-launch-day-runbook.md` | 本手册 |
| `.superpowers/sdd/task-9-brief.md` / `task-9-review.md` | JSAPI 收银台任务书/审查（含债 6 条） |
| `.superpowers/sdd/task-10-brief.md` / `task-10-review.md` | 微信身份任务书/审查（含债 5 条） |
| `docs/records/wechat-jsapi-cashier.md` / `wechat-identity-phase1.md` | 交付报告（状态机图/时序图） |
| `docs/records/2026-09-12-wechat-pay-publickey-notify-400.md` | 验签事故根因（铁律出处） |
| `e:\savvy-keys-inventory.md` | 全部密钥明文（git 外，勿外传） |
| `tmp/ops_*.sh` + `run_remote.ps1` | 远程运维脚本集 |
| `deploy/nginx-scheng.conf` | 机A nginx（站根 /var/www/savvy-agent） |
