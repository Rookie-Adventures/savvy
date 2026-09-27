# ZeroClaw 生产配置模板。__DEEPSEEK_API_KEY__ 由 ops/deploy_zeroclaw.sh 从 deploy/.env 渲染,
# 真值只落在机B deploy/data/zeroclaw/.zeroclaw/config.toml(chmod 600),严禁入库。
schema_version = 3

[runtime]
# distroless 镜像没有 shell,但 agent 启动硬校验此路径存在;shell 工具不在白名单,永不执行
shell = "/usr/local/bin/zeroclaw"

[providers.models.deepseek.real]
uri = "https://api.deepseek.com/v1"
api_key = "__DEEPSEEK_API_KEY__"
model = "deepseek-chat"

# 对话入口别名 = new-api OptionMap 的 AgentZeroClawAgent 值
[agents.topup]
model_provider = "deepseek.real"
risk_profile = "topup"
mcp_bundles = ["topup"]
skill_bundles = ["topup"]

# 闭合白名单:模型只见这些 MCP 工具,http_request/shell/文件/浏览器全部不存在。
# weixinpay 侧不放 feedback/self_update/api_level:更新与诊断不是对话内能力,防模型自改插件
[risk_profiles.topup]
level = "full"
allowed_tools = [
  "savvy__create_wechat_topup",
  "savvy__query_topup_status",
  "savvy__get_balance",
  "savvy__get_usage",
  "savvy__list_topup_orders",
  "savvy__redeem_code",
  "savvy__apply_refund",
  "savvy__list_refunds",
  "savvy__invoke_skill",
  "weixinpay__weixinpay_register",
  "weixinpay__weixinpay_pay",
  "weixinpay__weixinpay_retry_pay",
]

[mcp]
enabled = true

[[mcp.servers]]
name = "savvy"
transport = "http"
url = "http://savvy-mcp:8000/mcp"

[[mcp.servers]]
name = "weixinpay"
transport = "http"
url = "http://weixinpay-mcp:8100/mcp"

[mcp_bundles.topup]
servers = ["savvy", "weixinpay"]

[skill_bundles.topup]

[gateway]
host = "0.0.0.0"
# 仅容器内网 + 宿主回环可达(见 compose 端口绑定),配对鉴权留作后续加固项
require_pairing = false
