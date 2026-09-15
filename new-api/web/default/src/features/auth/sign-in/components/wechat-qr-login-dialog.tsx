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
import { useCallback, useEffect, useRef, useState } from 'react'
import { QRCodeSVG } from 'qrcode.react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  claimWeChatOALogin,
  createWeChatOALoginToken,
  getWeChatOATokenStatus,
} from '@/features/auth/api'
import { useAuthRedirect } from '@/features/auth/hooks/use-auth-redirect'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/dialog'

// ============================================================================
// WeChat OA scan-login dialog (desktop): QR + 2s status polling + claim
// ============================================================================

type Phase = 'qr' | 'authorized' | 'created' | 'rejected' | 'expired'

const POLL_INTERVAL_MS = 2000
// ponytail: bind-existing 需要登录态,桌面未登录时先落 sessionStorage,密码登录成功后自动认领
const PENDING_BIND_TOKEN_KEY = 'wx_pending_bind_token'

interface WeChatQrLoginDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Ticket from the direct-login unbound redirect (?wx_token=): skip QR creation. */
  initialToken?: string
}

export function WeChatQrLoginDialog({
  open,
  onOpenChange,
  initialToken,
}: WeChatQrLoginDialogProps) {
  const { t } = useTranslation()
  const { handleLoginSuccess } = useAuthRedirect()
  const [token, setToken] = useState('')
  const [qrUrl, setQrUrl] = useState('')
  const [phase, setPhase] = useState<Phase>('qr')
  const [created, setCreated] = useState<{
    username: string
    initial_password: string
    uid?: number
  } | null>(null)
  const [busy, setBusy] = useState(false)
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const stopPolling = useCallback(() => {
    if (timerRef.current) {
      clearInterval(timerRef.current)
      timerRef.current = null
    }
  }, [])

  const startPolling = useCallback(
    (ticket: string) => {
      stopPolling()
      timerRef.current = setInterval(async () => {
        try {
          const res = await getWeChatOATokenStatus(ticket)
          const status = res.data?.status
          if (status === 'completed') {
            stopPolling()
            setBusy(true)
            const claim = await claimWeChatOALogin(ticket, 'login')
            setBusy(false)
            if (claim.success) {
              // uid 必须经 handleLoginSuccess 写入 localStorage,否则登录态接口全报 未提供 New-Api-User
              await handleLoginSuccess({ id: claim.data?.uid })
              toast.success(t('Welcome back!'))
            } else {
              toast.error(claim.message || t('Login failed'))
              setPhase('rejected')
            }
          } else if (status === 'authorized') {
            stopPolling()
            setPhase('authorized')
          } else if (status === 'rejected' || status === 'expired') {
            stopPolling()
            setPhase(status)
          }
        } catch {
          // 轮询失败静默,下一轮重试(网络抖动不应打断扫码)
        }
      }, POLL_INTERVAL_MS)
    },
    [stopPolling, t]
  )

  useEffect(() => {
    if (!open) {
      stopPolling()
      return
    }
    setPhase('qr')
    setCreated(null)
    if (initialToken) {
      setToken(initialToken)
      setQrUrl('')
      startPolling(initialToken)
      return
    }
    let cancelled = false
    createWeChatOALoginToken('login').then((res) => {
      if (cancelled) return
      if (res.success && res.data?.token && res.data?.url) {
        setToken(res.data.token)
        setQrUrl(res.data.url)
        startPolling(res.data.token)
      } else {
        toast.error(res.message || t('Failed to start WeChat login'))
        onOpenChange(false)
      }
    })
    return () => {
      cancelled = true
      stopPolling()
    }
  }, [open, initialToken, startPolling, stopPolling, t, onOpenChange])

  const handleCreate = async () => {
    setBusy(true)
    try {
      const res = await claimWeChatOALogin(token, 'create')
      if (res.success && res.data?.username) {
        setCreated({
          username: res.data.username,
          initial_password: res.data.initial_password ?? '',
          uid: res.data.uid,
        })
        setPhase('created')
      } else {
        toast.error(res.message || t('This WeChat account is already bound'))
      }
    } finally {
      setBusy(false)
    }
  }

  const handleBindExisting = () => {
    sessionStorage.setItem(PENDING_BIND_TOKEN_KEY, token)
    toast.info(t('Sign in with your existing account to bind this WeChat'))
    onOpenChange(false)
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('WeChat QR Login')}
      description={t('Scan the QR code with WeChat to sign in')}
      contentClassName='max-w-sm'
      headerClassName='text-left'
      contentHeight='auto'
      bodyClassName='space-y-4'
    >
      {phase === 'qr' && (
        <div className='space-y-4'>
          {qrUrl ? (
            <div className='flex justify-center rounded-lg bg-white p-4'>
              <QRCodeSVG value={qrUrl} size={200} />
            </div>
          ) : (
            <div className='flex h-[200px] items-center justify-center' />
          )}
          <p className='text-muted-foreground text-center text-sm'>
            {t('Please complete authorization in WeChat')}
          </p>
        </div>
      )}

      {phase === 'authorized' && (
        <div className='space-y-3'>
          <p className='text-muted-foreground text-center text-sm'>
            {t('Authorized. Choose an action')}
          </p>
          <Button className='w-full' onClick={handleCreate} disabled={busy}>
            {t('Create new account')}
          </Button>
          <Button
            variant='outline'
            className='w-full'
            onClick={handleBindExisting}
            disabled={busy}
          >
            {t('Bind to existing account')}
          </Button>
        </div>
      )}

      {phase === 'created' && created && (
        <div className='space-y-3'>
          <p className='text-sm'>
            {t('Account created')}:{' '}
            <code className='font-mono font-bold'>{created.username}</code>
          </p>
          <div className='bg-muted rounded-lg p-3'>
            <p className='text-xs'>
              {t('Initial password (shown only once):')}
            </p>
            <code className='font-mono text-sm'>
              {created.initial_password}
            </code>
          </div>
          <Button
            className='w-full'
            onClick={async () => {
              // 会话已在服务端落地,此处补 uid 并进钱包流程
              await handleLoginSuccess({ id: created.uid }, '/wallet')
            }}
          >
            {t('Continue')}
          </Button>
        </div>
      )}

      {phase === 'rejected' && (
        <p className='text-destructive text-center text-sm'>
          {t('This WeChat account is already bound to another user')}
        </p>
      )}

      {phase === 'expired' && (
        <p className='text-destructive text-center text-sm'>
          {t('Link expired, please restart')}
        </p>
      )}
    </Dialog>
  )
}
