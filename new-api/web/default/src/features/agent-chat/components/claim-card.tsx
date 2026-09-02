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
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { useAuthStore } from '@/stores/auth-store'
import { claimAgentTopUp, getAgentTopUpStatus } from '../api'
import { markClaimDone } from '../lib/claim-storage'

type ClaimCardProps = {
  outTradeNo: string
  token: string
}

type Phase = 'waiting' | 'paid' | 'credited'

const POLL_INTERVAL_MS = 5000

// 认领卡片: 轮询订单状态 → 已支付且未登录给 [登录][注册] 跳转(sign-in 带 redirect 回跳,
// sign-up 无 redirect 参数,回跳由 widget 挂载时的 sessionStorage 恢复逻辑接力)
// → 已登录自动认领入账。后端认领幂等,卡片多实例并存不会双入账。
export function ClaimCard({ outTradeNo, token }: ClaimCardProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [phase, setPhase] = useState<Phase>('waiting')

  const tryClaim = useCallback(async () => {
    const res = await claimAgentTopUp(token)
    if (res.message === 'success') {
      markClaimDone(outTradeNo)
      setPhase('credited')
      toast.success(t('Top-up credited to your account'))
    }
  }, [token, outTradeNo, t])

  useEffect(() => {
    let stopped = false
    let timer: ReturnType<typeof setTimeout> | undefined
    const tick = async () => {
      try {
        const res = await getAgentTopUpStatus(token)
        if (stopped) return
        if (res.message === 'success' && res.data) {
          if (res.data.claimed) {
            markClaimDone(outTradeNo)
            setPhase('credited')
            return
          }
          if (res.data.status === 'success') {
            setPhase('paid')
            if (useAuthStore.getState().auth.user) await tryClaim()
            return
          }
        }
      } catch {
        // 网络抖动静默,继续轮询——钱已付时断链会让卡片永久卡在 waiting
      }
      if (!stopped) timer = setTimeout(() => void tick(), POLL_INTERVAL_MS)
    }
    void tick()
    return () => {
      stopped = true
      if (timer) clearTimeout(timer)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token])

  if (phase === 'credited')
    return (
      <div className='bg-card my-2 rounded-lg border p-4 text-sm'>
        {t('Top-up credited to your account')}
      </div>
    )
  if (phase === 'waiting')
    return (
      <div className='text-muted-foreground bg-card my-2 rounded-lg border p-4 text-sm'>
        {t('Waiting for payment confirmation...')}
      </div>
    )
  return (
    <div className='bg-card my-2 rounded-lg border p-4'>
      <p className='text-sm font-medium'>{t('Payment received')}</p>
      <p className='text-muted-foreground mt-1 text-xs'>
        {t('Sign in or register to claim your top-up')}
      </p>
      <div className='mt-3 flex gap-2'>
        <Button
          className='flex-1'
          onClick={() =>
            void navigate({
              to: '/sign-in',
              search: {
                redirect: window.location.pathname + window.location.search,
              },
            })
          }
        >
          {t('Sign In')}
        </Button>
        <Button
          variant='outline'
          className='flex-1'
          onClick={() => void navigate({ to: '/sign-up' })}
        >
          {t('Create account')}
        </Button>
      </div>
    </div>
  )
}
