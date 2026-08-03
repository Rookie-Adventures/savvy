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
import { useEffect, useState } from 'react'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatLocalCurrencyAmount } from '@/lib/currency'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Checkbox } from '@/components/ui/checkbox'
import { Skeleton } from '@/components/ui/skeleton'
import { DEFAULT_DISCOUNT_RATE } from '../../constants'
import { formatCurrency, getPaymentIcon } from '../../lib'
import type { PaymentMethod } from '../../types'

interface PaymentConfirmDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onConfirm: () => void
  topupAmount: number
  paymentAmount: number
  paymentMethod: PaymentMethod | undefined
  calculating: boolean
  processing: boolean
  discountRate?: number
  usdExchangeRate?: number
}

export function PaymentConfirmDialog({
  open,
  onOpenChange,
  onConfirm,
  topupAmount,
  paymentAmount,
  paymentMethod,
  calculating,
  processing,
  discountRate = DEFAULT_DISCOUNT_RATE,
  usdExchangeRate = 1,
}: PaymentConfirmDialogProps) {
  const { t } = useTranslation()
  // 支付前同意闸门:每次打开弹窗重置,不跨单持久化
  const [agreed, setAgreed] = useState(false)
  useEffect(() => {
    if (open) setAgreed(false)
  }, [open])
  const hasDiscount = discountRate > 0 && discountRate < 1 && paymentAmount > 0
  const originalAmount = hasDiscount ? paymentAmount / discountRate : 0
  const discountAmount = hasDiscount ? originalAmount - paymentAmount : 0

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent className='max-sm:w-[calc(100vw-1.5rem)] sm:max-w-md'>
        <AlertDialogHeader>
          <AlertDialogTitle className='text-xl font-semibold'>
            {t('Confirm Payment')}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t('Review your payment details')}
          </AlertDialogDescription>
        </AlertDialogHeader>

        <div className='space-y-3 py-3 sm:space-y-4 sm:py-4'>
          <div className='flex items-center justify-between'>
            <span className='text-muted-foreground text-sm'>
              {t('Topup Amount')}
            </span>
            <span className='text-lg font-semibold'>
              {formatLocalCurrencyAmount(topupAmount * usdExchangeRate, {
                digitsLarge: 2,
                digitsSmall: 2,
                abbreviate: false,
              })}
            </span>
          </div>

          <div className='flex items-center justify-between'>
            <span className='text-muted-foreground text-sm'>
              {t('You Pay')}
            </span>
            {calculating ? (
              <Skeleton className='h-6 w-24' />
            ) : (
              <div className='flex items-baseline gap-2'>
                <span className='text-2xl font-semibold'>
                  {formatCurrency(paymentAmount)}
                </span>
                {hasDiscount && (
                  <span className='text-muted-foreground text-sm line-through'>
                    {formatCurrency(originalAmount)}
                  </span>
                )}
              </div>
            )}
          </div>

          {hasDiscount && !calculating && (
            <div className='bg-muted/50 rounded-lg p-3'>
              <div className='flex items-center justify-between text-sm'>
                <span className='text-muted-foreground'>{t('You save')}</span>
                <span className='font-semibold text-green-600'>
                  {formatCurrency(discountAmount)}
                </span>
              </div>
            </div>
          )}

          <div className='border-t pt-4'>
            <div className='flex items-center justify-between'>
              <span className='text-muted-foreground text-sm'>
                {t('Payment Method')}
              </span>
              <div className='flex items-center gap-2'>
                {getPaymentIcon(
                  paymentMethod?.type,
                  'h-4 w-4',
                  paymentMethod?.icon,
                  paymentMethod?.name
                )}
                <span className='font-medium'>{paymentMethod?.name}</span>
              </div>
            </div>
          </div>

          <label className='flex cursor-pointer items-start gap-2'>
            <Checkbox
              checked={agreed}
              onCheckedChange={(v) => setAgreed(v === true)}
              className='mt-0.5'
            />
            <span className='text-muted-foreground text-xs leading-relaxed'>
              {t('I have read and agree to the')}{' '}
              <a
                href='/user-agreement'
                target='_blank'
                rel='noopener noreferrer'
                className='text-primary underline-offset-4 hover:underline'
              >
                {t('User Agreement')}
              </a>{' '}
              {t('and')}{' '}
              <a
                href='/privacy-policy'
                target='_blank'
                rel='noopener noreferrer'
                className='text-primary underline-offset-4 hover:underline'
              >
                {t('Privacy Policy')}
              </a>
            </span>
          </label>
        </div>

        <AlertDialogFooter className='grid grid-cols-2 gap-2 sm:flex'>
          <AlertDialogCancel disabled={processing}>
            {t('Cancel')}
          </AlertDialogCancel>
          <AlertDialogAction
            onClick={onConfirm}
            disabled={processing || !agreed}
          >
            {processing && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
            {t('Confirm Payment')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
