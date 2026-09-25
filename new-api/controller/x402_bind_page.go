package controller

// 挂账认领页(服务端直出,不依赖前端构建)。
//
// 微信内打开 bind_url 后:
//   - 配了 X402MpOAuthURL(服务号网页授权) → 自动跳授权,回跳带 code,免登录认领;
//   - 未配 → 两个入口:已登录会话直接认领,或先站内登录再来。
// 认领完全复用 /api/x402/wechat/claim 与 /api/x402/claim,身份与入账口径一致。

import (
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

var x402BindPage = template.Must(template.New("x402bind").Parse(`<!doctype html>
<html lang="zh-CN"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>认领微信AI支付款项</title>
<style>
body{font-family:-apple-system,BlinkMacSystemFont,"Helvetica Neue",sans-serif;margin:0;padding:24px;background:#f7f8fa;color:#1f2329}
.card{max-width:420px;margin:0 auto;background:#fff;border-radius:12px;padding:24px;box-shadow:0 1px 3px rgba(0,0,0,.08)}
h1{font-size:18px;margin:0 0 12px}
p{font-size:14px;line-height:1.6;color:#4a5058;margin:0 0 12px}
.amount{font-size:28px;font-weight:600;margin:8px 0 16px}
.btn{display:block;width:100%;padding:12px;border:0;border-radius:8px;background:#07c160;color:#fff;font-size:15px;text-align:center;text-decoration:none;margin-bottom:10px;box-sizing:border-box}
.btn.secondary{background:#eef0f3;color:#1f2329}
#msg{font-size:14px;margin-top:8px;word-break:break-all}
#msg.ok{color:#07c160}#msg.err{color:#e34d59}
.muted{color:#8a9099;font-size:12px}
</style></head><body>
<div class="card">
  <h1>微信 AI 支付 · 款项认领</h1>
  <p>这笔款项已收到,认领后自动到账,无需重新支付。</p>
  <div class="amount">¥ {{.Money}}</div>
  {{if .Credited}}
  <div id="msg" class="ok">已到账,可直接关闭本页。</div>
  {{else if .AuthURL}}
  <p class="muted">若未自动跳转,点下方按钮用微信登录认领。</p>
  <a class="btn" href="{{.AuthURL}}">用微信登录并认领</a>
  {{else}}
  <a class="btn" href="javascript:void(0)" id="sessionBtn">已登录?立即认领</a>
  <a class="btn secondary" href="/login">先登录再认领</a>
  {{end}}
  <div id="msg"></div>
  <p class="muted">订单 {{.TradeNo}} · 凭证 15 分钟内有效</p>
</div>
{{if not .Credited}}
<div hidden id="claim" data-claim="{{.Claim}}" data-code="{{.Code}}"></div>
<script>
(function(){
  var box=document.getElementById('claim');
  var claim=box?box.getAttribute('data-claim'):'';
  var code=box?box.getAttribute('data-code'):'';
  var el=document.getElementById('msg');
  async function post(path){
    el.className='';el.textContent='处理中…';
    try{
      var r=await fetch(path,{credentials:'include'});
      var j=await r.json();
      el.className=j.success?'ok':'err';
      el.textContent=j.message||(j.success?'认领成功':'认领失败');
      if(j.success)setTimeout(function(){location.href='/';},1500);
    }catch(e){el.className='err';el.textContent='请求失败:'+e.message;}
  }
  var b=document.getElementById('sessionBtn');
  if(b)b.addEventListener('click',function(){post('/api/x402/claim?claim='+encodeURIComponent(claim));});
  if(code)post('/api/x402/wechat/claim?code='+encodeURIComponent(code)+'&claim='+encodeURIComponent(claim));
})();
</script>
{{end}}
</body></html>`))

// SkillBindPage — GET /api/x402/bind?claim=xxx[&code=微信授权码]
func SkillBindPage(c *gin.Context) {
	claim := strings.TrimSpace(c.Query("claim"))
	code := strings.TrimSpace(c.Query("code"))
	if claim == "" {
		c.String(http.StatusBadRequest, "缺少认领凭证")
		return
	}
	hold := model.ResolveX402HoldByClaim(claim)
	if hold == nil {
		c.String(http.StatusGone, "认领链接无效或已过期")
		return
	}
	self := url.Values{"claim": {claim}}
	selfURL := x402PageBase(c) + "/api/x402/bind?" + self.Encode()
	authURL := ""
	if tpl := strings.TrimSpace(operation_setting.X402MpOAuthURL); tpl != "" && code == "" {
		authURL = strings.ReplaceAll(tpl, "{redirect_uri}", url.QueryEscape(selfURL))
	}
	data := gin.H{
		"Money":    hold.Money,
		"TradeNo":  hold.TradeNo,
		"Credited": hold.Status == model.X402HoldStatusCredited,
		"AuthURL":  authURL,
		"Claim":    claim,
		"Code":     code,
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	if err := x402BindPage.Execute(c.Writer, data); err != nil {
		common.SysError("x402 bind page render failed: " + err.Error())
	}
}

func x402PageBase(c *gin.Context) string {
	if base := strings.TrimRight(system_setting.ServerAddress, "/"); base != "" {
		return base
	}
	return schemeOf(c) + "://" + c.Request.Host
}
