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
  requestWechatPayment,
  isApiSuccess,
} from '../api'
import {
  isStripePayment,
  isWaffoPancakePayment,
  isAlipayPayment,
  isWechatPayment,
  submitPaymentForm,
} from '../lib'

// ============================================================================
// Payment Hook
// ============================================================================

export type PaymentResult =
  | { ok: true; wechatCodeUrl?: string }
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

        if (isWechat) {
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
            return { ok: true, wechatCodeUrl: response.data.code_url }
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
