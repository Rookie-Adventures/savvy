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
import { convertToWechatJsapiPay } from '../api'
import { parseWechatAgentOrder } from '../lib/agent-order'
import { readClaims, saveClaim } from '../lib/claim-storage'
import { isWeChatBrowser, retryWechatJsapiOauth } from '../lib/wechat-env'
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
  const [paying, setPaying] = useState(false)
  const [jsapiFailed, setJsapiFailed] = useState(false)
  const [showQr, setShowQr] = useState(false)
  const isHttp = codeUrl.startsWith('http')
  // 微信内置浏览器认不出 weixin:// 的扫码路径:它没法扫自己屏幕上的码,私有 scheme 也点不开
  const isWeChat = isWeChatBrowser()
  // 微信内的正解:把这张 Native 单换成 JSAPI 单，直接弹微信支付面板(付款人=当前微信用户)
  const canPayInWechat = !isHttp && isWeChat && Boolean(parsed?.outTradeNo)

  const payInWechat = async () => {
    if (!parsed?.outTradeNo) return
    setPaying(true)
    setJsapiFailed(false)
    try {
      const res = await convertToWechatJsapiPay(parsed.outTradeNo)
      if (res.message === 'success' && res.data?.pay_url) {
        window.location.href = res.data.pay_url
        return
      }
      if (res.message === 'wechat_oauth_required') {
        retryWechatJsapiOauth()
        return
      }
      setJsapiFailed(true)
    } catch {
      setJsapiFailed(true)
    } finally {
      setPaying(false)
    }
  }

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
      {canPayInWechat && (
        <div className='mt-3'>
          <Button className='w-full' disabled={paying} onClick={() => void payInWechat()}>
            {paying ? t('Preparing payment...') : t('Pay in WeChat')}
          </Button>
          {jsapiFailed && (
            <p className='text-muted-foreground mt-2 text-xs'>
              {t('Unable to start payment. Please try again later or contact support.')}
            </p>
          )}
        </div>
      )}
      {/* 微信内已有"微信里付款"这条正解，二维码就退成换设备时的次要入口：
          再摆一张大码、同时告诉用户"扫本机屏幕无效"是自相矛盾的。 */}
      {canPayInWechat && !showQr ? (
        <button
          type='button'
          className='text-muted-foreground mt-2 text-xs underline-offset-4 hover:underline'
          onClick={() => setShowQr(true)}
        >
          {t('Show QR code to pay on another device')}
        </button>
      ) : (
        <>
          <div className='mt-3 flex justify-center'>
            <div className='rounded-lg bg-white p-3'>
              <QRCodeSVG value={codeUrl} size={180} />
            </div>
          </div>
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
      {!isHttp && isWeChat && !canPayInWechat && (
        <p className='text-muted-foreground mt-2 text-xs leading-relaxed'>
          {t(
            'Copy this link and send it to any WeChat chat (e.g. File Transfer), then tap it to pay. Scanning on this phone will not work inside WeChat.'
          )}
        </p>
      )}
        </>
      )}
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
