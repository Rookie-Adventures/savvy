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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ExternalLink } from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import { Button } from '@/components/ui/button'
import { parseWechatAgentOrder } from '../lib/agent-order'
import { readClaims, saveClaim } from '../lib/claim-storage'
import { ClaimCard } from './claim-card'

type WechatQrCardProps = {
  codeUrl: string
  context: string
}

// 微信 Native 单:code_url 是 weixin:// scheme,浏览器打不开,渲染成二维码让用户手机扫。
// claim_token 后端下单时已预发并随回复文本带回,本地落一份即挂认领卡,
// 不走支付宝 MCP 单那套 register 登记换 token 流程。
export function WechatQrCard({ codeUrl, context }: WechatQrCardProps) {
  const { t } = useTranslation()
  const [parsed] = useState(() => {
    const order = parseWechatAgentOrder(context)
    if (!order) return null
    const rec = readClaims().find(
      (r) => r.outTradeNo === order.outTradeNo && !r.done
    )
    if (!rec) {
      saveClaim({
        outTradeNo: order.outTradeNo,
        token: order.claimToken,
        done: false,
      })
    }
    return order
  })
  const [copied, setCopied] = useState(false)
  const isHttp = codeUrl.startsWith('http')

  const copyLink = () => {
    navigator.clipboard
      ?.writeText(codeUrl)
      .then(() => setCopied(true))
      .catch(() => {
        // clipboard API 不可用(非安全上下文/旧浏览器)时的 execCommand 兜底
        const ta = document.createElement('textarea')
        ta.value = codeUrl
        document.body.appendChild(ta)
        ta.select()
        document.execCommand('copy')
        ta.remove()
        setCopied(true)
      })
  }

  return (
    <div className='bg-card my-2 rounded-lg border p-4'>
      <p className='text-sm font-medium'>{t('Scan with WeChat to pay')}</p>
      <div className='mt-3 flex justify-center'>
        <div className='rounded-lg bg-white p-3'>
          <QRCodeSVG value={codeUrl} size={180} />
        </div>
      </div>
      {/* 三合一交付:桌面扫码 / 手机与微信内点链接 / 复制粘贴兜底,不判断宿主环境 */}
      <div className='mt-3 flex items-center gap-2'>
        <p className='text-muted-foreground min-w-0 flex-1 break-all text-[11px]'>
          {codeUrl}
        </p>
        {isHttp && (
          <Button
            size='sm'
            variant='outline'
            className='shrink-0'
            onClick={() =>
              window.open(codeUrl, '_blank', 'noopener,noreferrer')
            }
          >
            <ExternalLink className='mr-1 h-3.5 w-3.5' />
            {t('Open')}
          </Button>
        )}
        <Button
          size='sm'
          variant='outline'
          className='shrink-0'
          onClick={copyLink}
        >
          {copied ? t('Copied') : t('Copy')}
        </Button>
      </div>
      {parsed?.claimUrl && (
        <a
          href={parsed.claimUrl}
          target='_blank'
          rel='noopener noreferrer'
          className='text-primary mt-2 block text-center text-xs underline-offset-4 hover:underline'
        >
          {t('Open claim page after payment')}
        </a>
      )}
      {parsed && (
        <ClaimCard outTradeNo={parsed.outTradeNo} token={parsed.claimToken} />
      )}
    </div>
  )
}
