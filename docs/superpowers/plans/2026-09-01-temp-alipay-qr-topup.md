# 临时支付宝二维码收款入口 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在钱包充值卡片加一个"支付宝付款"按钮，点开显示个人支付宝收款二维码（临时人工收款）。

**Architecture:** 新建自包含组件 `manual-alipay-topup.tsx`（按钮 + 弹窗 + 图片），在 `recharge-form-card.tsx` 只插入一行挂载。不碰后端、不碰现有支付逻辑、不做 i18n（文案硬编码中文）。回滚 = revert 一个 commit。

**Tech Stack:** React + TypeScript，rsbuild 构建，Base UI Dialog（经 `@/components/dialog` 高层封装），Rsbuild 原生支持 `import` 图片资源（`src/env.d.ts` 已引用 `@rsbuild/core/types`）。

## Global Constraints

- 分支：从 `dev` 切出 `temp/alipay-qr-topup`，禁止在其他分支直接改。
- 文案硬编码中文，不加 i18n 键。
- 二维码图片走 `src/assets/` + import 打包，禁止放 `public/`。
- 新文件必须带 QuantumNous AGPL 版权头（贴邻居文件格式）。
- 组件顶部标 `// ponytail: 临时人工收款入口，正式渠道接入后整个文件删除`。
- 纯展示组件，无分支/循环/解析逻辑 → 不写单测，以构建 + 手动预览为准。

---

### Task 1: 二维码图片 + 自包含组件 + 挂载 + 构建验证

**Files:**
- Create: `new-api/web/default/src/assets/alipay-qr.png`（复制自 `e:\savvy\tmp\alipay-qr.png`）
- Create: `new-api/web/default/src/features/wallet/components/manual-alipay-topup.tsx`
- Modify: `new-api/web/default/src/features/wallet/components/recharge-form-card.tsx`（import 1 行 + 挂载 3 行）

**Interfaces:**
- Consumes: `@/components/dialog` 的 `Dialog`（props: `title/description/trigger/children`）；`../lib` 的 `getPaymentIcon('alipay', 'h-4 w-4')`；`@/components/ui/button` 的 `Button`。
- Produces: `export function ManualAlipayTopup(): JSX.Element`，无 props。

- [ ] **Step 1: 从 dev 切临时分支**

```powershell
git -C e:\savvy checkout dev
git -C e:\savvy checkout -b temp/alipay-qr-topup
```

预期：当前分支变为 `temp/alipay-qr-topup`。

- [ ] **Step 2: 复制二维码图片**

```powershell
Copy-Item e:\savvy\tmp\alipay-qr.png e:\savvy\new-api\web\default\src\assets\alipay-qr.png
```

- [ ] **Step 3: 新建组件**

创建 `new-api/web/default/src/features/wallet/components/manual-alipay-topup.tsx`：

```tsx
/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
// ponytail: 临时人工收款入口,正式渠道接入后整个文件删除
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/dialog'
import { getPaymentIcon } from '../lib'
import alipayQrUrl from '@/assets/alipay-qr.png'

export function ManualAlipayTopup() {
  return (
    <Dialog
      title='支付宝付款'
      description='扫码支付后,请联系客服 support@scheng.net 提供付款凭证,人工核对后充值到账。'
      trigger={
        <Button
          variant='outline'
          className='min-h-14 w-full justify-start gap-2 rounded-lg px-3 py-2 text-left'
        >
          {getPaymentIcon('alipay', 'h-4 w-4')}
          <span className='truncate'>支付宝付款</span>
        </Button>
      }
    >
      <div className='flex flex-col items-center gap-3 pb-2'>
        <img
          src={alipayQrUrl}
          alt='支付宝收款二维码'
          className='w-64 max-w-full rounded-lg border object-contain'
        />
      </div>
    </Dialog>
  )
}
```

- [ ] **Step 4: 挂载到充值卡片**

在 `recharge-form-card.tsx` 顶部 `import { CreemProductsSection } from './creem-products-section'` 后加：

```tsx
import { ManualAlipayTopup } from './manual-alipay-topup'
```

在在线充值三元块结束处（`) : (`…`<Alert>` 那个分支的闭合 `)}`，即 `{/* Creem Products Section */}` 注释之前）插入：

```tsx
      {/* ponytail: 临时人工收款入口,正式渠道接入后连同组件一起删除 */}
      <div className='space-y-2.5 border-t pt-4 sm:space-y-3 sm:pt-6'>
        <ManualAlipayTopup />
      </div>
```

- [ ] **Step 5: 构建验证**

```powershell
cd e:\savvy\new-api\web\default
npm run build
```

预期：构建成功，无 TS/打包错误。

- [ ] **Step 6: 手动预览验证**

```powershell
npm run dev
```

打开钱包/充值页：按钮可见（不管后台有没有支付渠道）→ 点击弹出二维码弹窗 → 图片正常显示。验证通过后关闭。

- [ ] **Step 7: 提交**

```powershell
git add new-api/web/default/src/assets/alipay-qr.png new-api/web/default/src/features/wallet/components/manual-alipay-topup.tsx new-api/web/default/src/features/wallet/components/recharge-form-card.tsx
git commit -m "feat(web/wallet): 临时支付宝二维码人工收款入口"
```

（设计文档 `docs/superpowers/specs/2026-09-01-temp-alipay-qr-topup-design.md` 与本计划文件是否入库，由用户决定。）
