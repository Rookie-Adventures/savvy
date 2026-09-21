# 临时支付宝二维码收款入口 设计文档

日期：2026-09-01
分支：`temp/alipay-qr-topup`（从 `dev` 切出）
性质：临时过渡方案，正式收款渠道接入后整体移除。

## 背景

当前没有可用收款渠道，钱包页充值卡片只显示"请联系管理员"。临时贴一张个人支付宝收款二维码，用户扫码付款后由人工核对到账充值。

## 范围

- 只改 `new-api/web/default` 前端（default 主题，线上在用）。
- 不碰后端、不碰支付下单/回调/轮询逻辑、不做 i18n（文案硬编码中文）、不做付款登记。

## 改动内容（1 个 commit）

1. **图片**：`e:\savvy\tmp\alipay-qr.png`（用户提供）→ 复制到
   `new-api/web/default/src/assets/alipay-qr.png`。
   通过 `import` 打包，避免走 `public/` 目录（历史上踩过 nginx 根路径静态资源被拦截的坑）。

2. **新组件** `new-api/web/default/src/features/wallet/components/manual-alipay-topup.tsx`，自包含：
   - "支付宝付款"按钮：复用现有 `Button`、`getPaymentIcon('alipay')`，样式对齐邻居支付按钮。
   - 点击弹出 Dialog（复用 `@/components/ui/dialog`）：显示收款二维码 + 说明文案（扫码付款后联系客服 `support@scheng.net` 凭付款记录人工充值）。
   - 文件顶部标注 `// ponytail: 临时人工收款入口，正式渠道接入后整个文件删除`。

3. **挂载点**：`recharge-form-card.tsx` 在线充值区块之后插入一行 `<ManualAlipayTopup />`。
   永远显示，不受后台支付渠道开关影响。

## 回滚方式

revert 该 commit（或删组件文件 + 删挂载一行）。改动不与任何现有支付逻辑耦合。

## 验证

- 前端构建通过。
- 本地预览钱包页：按钮可见 → 点击弹窗显示二维码。
