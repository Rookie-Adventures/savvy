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
import { createWeChatOABindToken } from '../../api'
import { getWeChatOATokenStatus } from '@/features/auth/api'
import { Dialog } from '@/components/dialog'

// ============================================================================
// WeChat bind dialog (profile): bind QR + 2s status polling
// ============================================================================

const POLL_INTERVAL_MS = 2000

interface WeChatBindDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSuccess: () => void
}

export function WeChatBindDialog({
  open,
  onOpenChange,
  onSuccess,
}: WeChatBindDialogProps) {
  const { t } = useTranslation()
  const [qrUrl, setQrUrl] = useState('')
  const [error, setError] = useState('')
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const stopPolling = useCallback(() => {
    if (timerRef.current) {
      clearInterval(timerRef.current)
      timerRef.current = null
    }
  }, [])

  const startPolling = useCallback(
    (token: string) => {
      stopPolling()
      timerRef.current = setInterval(async () => {
        try {
          const res = await getWeChatOATokenStatus(token)
          const status = res.data?.status
          if (status === 'completed') {
            stopPolling()
            toast.success(t('Binding successful'))
            onOpenChange(false)
            onSuccess()
          } else if (status === 'rejected') {
            stopPolling()
            setError(t('This WeChat account is already bound to another user'))
          } else if (status === 'expired') {
            stopPolling()
            setError(t('Link expired, please restart'))
          }
        } catch {
          // 轮询失败静默,下一轮重试
        }
      }, POLL_INTERVAL_MS)
    },
    [stopPolling, t, onOpenChange, onSuccess]
  )

  useEffect(() => {
    if (!open) {
      stopPolling()
      return
    }
    setQrUrl('')
    setError('')
    let cancelled = false
    createWeChatOABindToken().then((res) => {
      if (cancelled) return
      if (res.success && res.data?.token && res.data?.qr_url) {
        setQrUrl(res.data.qr_url)
        startPolling(res.data.token)
      } else {
        setError(res.message || t('Failed to start WeChat login'))
      }
    })
    return () => {
      cancelled = true
      stopPolling()
    }
  }, [open, startPolling, stopPolling, t])

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('WeChat Binding')}
      description={t('Scan the QR code with WeChat to bind your account')}
      contentClassName='max-w-sm'
      headerClassName='text-left'
      contentHeight='auto'
      bodyClassName='space-y-4'
    >
      {qrUrl && !error && (
        <div className='flex justify-center rounded-lg bg-white p-4'>
          <QRCodeSVG value={qrUrl} size={200} />
        </div>
      )}
      {error ? (
        <p className='text-destructive text-center text-sm'>{error}</p>
      ) : (
        <p className='text-muted-foreground text-center text-sm'>
          {t('Please complete authorization in WeChat')}
        </p>
      )}
    </Dialog>
  )
}
