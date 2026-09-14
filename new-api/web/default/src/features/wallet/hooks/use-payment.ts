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
import { useState, useCallback } from 'react'
import i18next from 'i18next'
import { toast } from 'sonner'
import {
  calculateAmount,
  calculateStripeAmount,
  calculateWaffoPancakeAmount,
  requestPayment,
  requestStripePayment,
  requestAlipayPayment,
  requestAlipayQRPayment,
  requestWechatPayment,
  requestWechatJsapiPayment,
  startWechatJsapiOauth,
  isApiSuccess,
} from '../api'
import {
  isStripePayment,
  isWaffoPancakePayment,
  isAlipayPayment,
  isAlipayQRPayment,
  isWechatPayment,
  isWechatInAppBrowser,
  invokeWeixinPay,
  submitPaymentForm,
} from '../lib'

// ============================================================================
// Payment Hook
// ============================================================================

export type PaymentResult =
  | { ok: true; qrCodeUrl?: string }
  | { ok: false }

export function usePayment() {
  const [amount, setAmount] = useState<number>(0)
  const [calculating, setCalculating] = useState(false)
  const [processing, setProcessing] = useState(false)

  // Calculate payment amount
  const calculatePaymentAmount = useCallback(
    async (topupAmount: number, paymentType: string) => {
      try {
        setCalculating(true)

        const isStripe = isStripePayment(paymentType)
        const isPancake = isWaffoPancakePayment(paymentType)
        // Alipay/WeChat use the same amount calc as epay (local currency / USD).
        const response = isStripe
          ? await calculateStripeAmount({ amount: topupAmount })
          : isPancake
            ? await calculateWaffoPancakeAmount({ amount: topupAmount })
            : await calculateAmount({ amount: topupAmount })

        if (isApiSuccess(response) && response.data) {
          const calculatedAmount = parseFloat(response.data)
          setAmount(calculatedAmount)
          return calculatedAmount
        }

        // Don't show error for calculation, just set to 0
        setAmount(0)
        return 0
      } catch (_error) {
        setAmount(0)
        return 0
      } finally {
        setCalculating(false)
      }
    },
    []
  )

  // Process payment
  const processPayment = useCallback(
    async (topupAmount: number, paymentType: string): Promise<PaymentResult> => {
      try {
        setProcessing(true)

        const isStripe = isStripePayment(paymentType)
        const isAlipay = isAlipayPayment(paymentType)
        const isAlipayQR = isAlipayQRPayment(paymentType)
        const isWechat = isWechatPayment(paymentType)
        const amount = Math.floor(topupAmount)

        if (isAlipay) {
          const response = await requestAlipayPayment({
            amount,
            payment_method: 'alipay',
          })
          if (!isApiSuccess(response)) {
            toast.error(
              response.message || i18next.t('Payment request failed')
            )
            return { ok: false }
          }
          if (response.data?.pay_link) {
            toast.success(i18next.t('Redirecting to payment page...'))
            // In-tab redirect — Alipay cashier expects the browser to land on
            // its page; window.open across an await loses user-gesture context.
            window.location.href = response.data.pay_link as string
            return { ok: true }
          }
          return { ok: false }
        }

        if (isAlipayQR) {
          const response = await requestAlipayQRPayment({
            amount,
            payment_method: 'alipay',
          })
          if (!isApiSuccess(response)) {
            toast.error(
              response.message || i18next.t('Payment request failed')
            )
            return { ok: false }
          }
          if (response.data?.code_url) {
            // Caller renders the QR; no redirect.
            return { ok: true, qrCodeUrl: response.data.code_url }
          }
          return { ok: false }
        }

        if (isWechat) {
          // 微信内浏览器走 JSAPI 调起,其余走扫码(Native)——ponytail: 双 AppID 决策,openid 按 AppID 隔离
          if (isWechatInAppBrowser()) {
            const res = await requestWechatJsapiPayment({ amount, payment_method: 'wechat' })
            if (!isApiSuccess(res)) {
              toast.error(res.message || i18next.t('Payment request failed'))
              return { ok: false }
            }
            if (res.message === 'wechat_oauth_required') {
              // 未授权:跳静默授权,微信带 code 回回调后写 session openid,前端回到钱包页再付
              const authRes = await startWechatJsapiOauth()
              if (isApiSuccess(authRes) && authRes.data?.authorize_url) {
                window.location.href = authRes.data.authorize_url
                return { ok: false } // 跳转中,不视为失败
              }
              toast.error(i18next.t('WeChat Oauth') + ': ' + i18next.t('Payment request failed'))
              return { ok: false }
            }
            if (res.data?.appId) {
              invokeWeixinPay(
                {
                  appId: res.data.appId,
                  timeStamp: res.data.timeStamp,
                  nonceStr: res.data.nonceStr,
                  package: res.data.package,
                  signType: res.data.signType,
                  paySign: res.data.paySign,
                },
                () => {
                  // ponytail: usePayment 无 onPurchaseSuccess 回调(见 use-payment 签名),
                  // 支付成功后刷新页面以同步余额(notify 已在后端落库)。
                  window.location.reload()
                }
              )
              return { ok: true }
            }
            return { ok: false }
          }
          const response = await requestWechatPayment({
            amount,
            payment_method: 'wechat',
          })
          if (!isApiSuccess(response)) {
            toast.error(
              response.message || i18next.t('Payment request failed')
            )
            return { ok: false }
          }
          if (response.data?.code_url) {
            // Caller renders the QR; no redirect.
            return { ok: true, qrCodeUrl: response.data.code_url }
          }
          return { ok: false }
        }

        const response = isStripe
          ? await requestStripePayment({
              amount,
              payment_method: 'stripe',
            })
          : await requestPayment({
              amount,
              payment_method: paymentType,
            })

        if (!isApiSuccess(response)) {
          toast.error(response.message || i18next.t('Payment request failed'))
          return { ok: false }
        }

        // Handle Stripe payment
        if (isStripe && response.data?.pay_link) {
          window.open(response.data.pay_link as string, '_blank')
          toast.success(i18next.t('Redirecting to payment page...'))
          return { ok: true }
        }

        // Handle non-Stripe payment
        if (!isStripe && response.data) {
          const url = (response as unknown as { url?: string }).url
          if (url) {
            submitPaymentForm(url, response.data)
            toast.success(i18next.t('Redirecting to payment page...'))
            return { ok: true }
          }
        }

        return { ok: false }
      } catch (_error) {
        toast.error(i18next.t('Payment request failed'))
        return { ok: false }
      } finally {
        setProcessing(false)
      }
    },
    []
  )

  return {
    amount,
    calculating,
    processing,
    calculatePaymentAmount,
    processPayment,
    setAmount,
  }
}
