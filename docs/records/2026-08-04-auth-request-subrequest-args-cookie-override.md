# 2026-08-04 auth_request 子请求无 query args → 旧 cookie 顶掉新 token（401/进不去工作区）

## 症状

- 用户重新打开工作区撞 401（token 明明是新签发的、有效的）。
- 本地复现：同一浏览器开过 inst-19 后再开 inst-20，inst-20 一直卡在 403 等待页进不去；inst-19 正常。
- curl 不带 cookie 直接打 workspace 端口 → 307 正常；带上任意旧 `workspace_token` cookie 再打（URL 里带有效新 token）→ 401。
- 服务器（scheng.net）上同一用户重开工作区偶发 401，刷新/重开 token 后仍 401。

## 根因

**nginx 的 `auth_request` 验证子请求上下文里没有 query args，`$arg_token` 恒为空。**

旧 token 兜底链：

```nginx
map $arg_token $ws_token_from_arg {
    ""        $ws_token_from_cookie;   # ← 子请求里 $arg_token 恒空，永远走这条
    default   $arg_token;
}
```

后果链：

1. 验证子请求里 `$arg_token` 为空 → map 回退到 cookie。
2. `workspace_token` cookie **不分端口**（`127.0.0.1` / `scheng.net` 全端口共享）——开过任一工作区后，cookie 会发给所有 41000-41099 端口。
3. 用户再开**另一个**工作区（或旧 cookie 未过期时重新打开），URL 里的新 token 被旧 cookie 顶掉：
   - 旧 cookie 签名无效/结构坏 → 401；
   - 旧 cookie 属于别的实例 → 403 "Invalid instance"。
4. map 结果被变量缓存，主请求上下文里再取也是 cookie 值（诊断头实测）。

### 排查中踩过的坑（都记一下）

- nginx 1.31 的 `map` **值引用自己的源变量**（`default $arg_token`）实测求值异常（返回空），
  换正则捕获 `$1` 也救不回来——真正的问题是子请求无 args，不是自引用。
- 子请求上下文变量取值组合很怪：`$arg_token` 空，但 `$request_uri`、`$cookie_*`、`$http_referer` 完整。
- 403 等待页（error_page 403）会把 validate 的 403 和上游 403 都接住，日志里看到的 200 不代表真成功。

## 修复

### nginx（deploy/nginx.conf）

token 改从 `$request_uri` 正则提取（主/子请求里都是完整原始 URI）：

```nginx
map $request_uri $ws_token_from_uri {
    default "";
    ~*[?&]token=([^&\s]+) $1;
}
# 链：uri token > cookie > referer，逐级 map，值不自引用源变量
map $ws_token_from_uri $ws_token_l1 { "" $ws_cookie_raw; ~^(.+) $1; }
map $ws_token_l1 $ws_token_with_ref { "" $ws_token_from_ref; ~^(.+) $1; }
```

validate-token 子请求透传三个原始值给 manager：
`X-Token`（链结果）、`X-Token-Arg`、`X-Token-Cookie`。

### manager（savvy-manager/app/routers/workspace.py）

validate 端加 Python 侧优先级兜底（双保险）：
X-Token → X-Token-Cookie → X-Original-URI query → Referer query。

### 顺手修的

- 403 等待页加**重试上限**（ws_retry=10，约 30 秒）：超限显示"工作区可能尚未启动或已休眠，请回控制台点击启动"，不再无限傻刷。
- token.py 四个验签失败分支加 `[DIAG_401]` 日志（TEMP，定位完删）。

## 验证

curl 五连测（本地 41019，inst-20 有效 token）：

| 场景 | 结果 |
|---|---|
| 仅 URL token | 307 ✓ |
| URL 有效 token + 假 cookie | 307 ✓（修复前 401 ✗）|
| 仅有效 cookie | 307 ✓ |
| 仅假 cookie | 401 ✓ |
| 都没有 | 401 ✓ |

manager DIAG 确认 src=arg（URL token 优先）。

## 部署

- 本地验证通过后合并 dev，推 origin。
- 服务器：`git pull` → `docker compose up -d --build savvy-manager`（manager 代码变了）
  → workspace-router 只需 `nginx -s reload`（nginx.conf 是 :ro 挂载，文件更新即生效）。
- 机A 不用动（本次问题与机A无关）。

## 已知限制 / 尾巴

- cookie 跨端口共享的机制没变：同一浏览器同时开两个工作区，cookie 会被后开的覆盖。
  现在 URL token 优先所以不影响首次进入；但已打开的标签页靠 cookie 续命时，
  若 cookie 已被另一个工作区覆盖，刷新会走等待页/403 → 回控制台重开即可（自愈）。
  彻底方案：cookie 按端口隔离（cookie name 带端口号）——未做，观察必要性。
- TEMP DIAG 打印（`[DIAG_VALIDATE]`/`[DIAG_401]`/`[DIAG_403]`）定位完删除。
- 事件循环阻塞隐患未修：`start_instance`（async）里调阻塞的 `start_container`
  （time.sleep 最长 120s），启动窗口内整个 manager API 被冻结。建议后续用
  `asyncio.to_thread` 包掉 docker 阻塞调用。
