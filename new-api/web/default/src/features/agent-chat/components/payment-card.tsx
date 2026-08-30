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
import { useTranslation } from 'react-i18next'
import { ExternalLink } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'

type PaymentCardProps = {
  link: string
}

// 支付前同意闸门:每条链接挂载即重置,不跨消息持久化(对齐 wallet payment-confirm-dialog)
export function PaymentCard({ link }: PaymentCardProps) {
  const { t } = useTranslation()
  const [agreed, setAgreed] = useState(false)

  useEffect(() => {
    setAgreed(false)
  }, [link])

  return (
    <div className='bg-card my-2 rounded-lg border p-4'>
      <p className='text-sm font-medium'>
        {t('Alipay payment link generated')}
      </p>
      <label className='mt-3 flex cursor-pointer items-start gap-2'>
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
      <Button
        className='mt-3 w-full'
        disabled={!agreed}
        onClick={() => window.open(link, '_blank', 'noopener,noreferrer')}
      >
        <ExternalLink className='mr-2 h-4 w-4' />
        {t('Go to Pay')}
      </Button>
    </div>
  )
}
