"""Savvy Agent 额度管家 MCP server(Pay-tools 层)。

把 new-api 的 8 个充值/能力接口封装成类型化工具供 agent 运行时(ZeroClaw 等)调用。
鉴权边界:
- 游客单接口(topup create)的 X-Agent-Token 由本进程从环境变量注入,凭据不进模型上下文;
- 账户类接口的用户 API Key 按 SKILL v2.0.1 设计由用户明示提供、作为工具参数传入。
接口契约以 new-api 生产代码为准(router/api-router.go, controller/agent_*.go)。
"""
import os

import httpx
from mcp.server.fastmcp import FastMCP

BASE = os.environ.get("SAVVY_BASE_URL", "https://scheng.net").rstrip("/")
AGENT_TOKEN = os.environ.get("AGENT_TOPUP_TOKEN", "")

mcp = FastMCP("savvy", host="0.0.0.0", port=8000)


def _call(method: str, path: str, *, api_key: str = "", params=None, json=None):
    headers = {}
    if path.startswith("/api/user/agent/wechat/topup/create"):
        headers["X-Agent-Token"] = AGENT_TOKEN
    if api_key:
        headers["Authorization"] = f"Bearer {api_key}"
    with httpx.Client(timeout=30) as client:
        r = client.request(method, f"{BASE}{path}", headers=headers, params=params, json=json)
    try:
        body = r.json()
    except ValueError:
        body = r.text
    if r.status_code >= 400:
        # 服务端裁定原样转述:不吞状态码,让模型按 SKILL 降级/转述规则处理
        return {"ok": False, "status": r.status_code, "body": body}
    # new-api 部分鉴权/业务错误走 HTTP 200 + success:false,同样不得包装成成功
    if isinstance(body, dict) and body.get("success") is False:
        return {"ok": False, "status": r.status_code, "body": body}
    return {"ok": True, "status": r.status_code, "body": body}


@mcp.tool(description="创建微信充值订单(游客可发起)。金额由用户指定、服务端裁定;返回 code_url 扫码链接与 claim_token/claim_url 认领信息。")
def create_wechat_topup(amount_yuan: float) -> dict:
    return _call("POST", "/api/user/agent/wechat/topup/create", json={"amount_yuan": amount_yuan})


@mcp.tool(description="凭 claim_token 查询充值订单状态(pending/success 等)。用于用户问『刚才那笔到账没』。")
def query_topup_status(claim_token: str) -> dict:
    return _call("GET", "/api/user/agent/topup/status", params={"claim_token": claim_token})


@mcp.tool(description="查询账户余额与低余额提醒。需要用户自己的 Savvy API Key(sk-...)。")
def get_balance(api_key: str) -> dict:
    return _call("GET", "/api/user/agent/ability/balance", api_key=api_key)


@mcp.tool(description="查询近 N 天用量(默认 7 天,服务端窗口钳 1~30)。需要用户 API Key。")
def get_usage(api_key: str, days: int = 7) -> dict:
    return _call("GET", "/api/user/agent/ability/usage", api_key=api_key, params={"days": days})


@mcp.tool(description="分页查询本人充值订单记录。需要用户 API Key。")
def list_topup_orders(api_key: str, page: int = 1, page_size: int = 10) -> dict:
    return _call("GET", "/api/user/agent/ability/orders", api_key=api_key, params={"page": page, "page_size": page_size})


@mcp.tool(description="兑换优惠码(CDK)。写入类操作,须先向用户复述确认再调用。需要用户 API Key。")
def redeem_code(api_key: str, code: str) -> dict:
    return _call("POST", "/api/user/agent/ability/redeem", api_key=api_key, json={"code": code})


@mcp.tool(description="提交退款申请工单(不等于已退款;资金原路退回仍人工)。写入类操作,须先确认。需要用户 API Key。")
def apply_refund(api_key: str, out_trade_no: str, reason: str = "") -> dict:
    return _call("POST", "/api/user/agent/ability/refund/apply", api_key=api_key,
                 json={"out_trade_no": out_trade_no, "reason": reason})


@mcp.tool(description="查询本人退款工单进度。需要用户 API Key。")
def list_refunds(api_key: str, page: int = 1, page_size: int = 10) -> dict:
    return _call("GET", "/api/user/agent/ability/refund/list", api_key=api_key, params={"page": page, "page_size": page_size})


@mcp.tool(description="微信AI支付(X402)通道。第一步不带 payment_code 下单:status=402 属正常,响应里的 payment_code/out_trade_no 交给 weixinpay 支付工具;用户确认付款后带这两个值再调一次完成履约,返回入账结果。")
def invoke_skill(action: str, amount_yuan: float = 0, payment_code: str = "", out_trade_no: str = "") -> dict:
    headers = {}
    if payment_code:
        headers["WeixinPay-Required"] = payment_code
    if out_trade_no:
        headers["X-Out-Trade-No"] = out_trade_no
    body = {"action": action}
    if amount_yuan:
        body["amount_yuan"] = amount_yuan
    with httpx.Client(timeout=30) as client:
        r = client.post(f"{BASE}/api/skill/invoke", headers=headers, json=body)
    try:
        data = r.json()
    except ValueError:
        data = r.text
    # 402 是 X402 协议的"待支付"信号,不是失败;其余 >=400 原样转述
    return {
        "ok": r.status_code in (200, 402),
        "status": r.status_code,
        "payment_code": r.headers.get("WeixinPay-Required", ""),
        "out_trade_no": r.headers.get("X-Out-Trade-No", ""),
        "body": data,
    }


if __name__ == "__main__":
    mcp.run(transport="streamable-http")
