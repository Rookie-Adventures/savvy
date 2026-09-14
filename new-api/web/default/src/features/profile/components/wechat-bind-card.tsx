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
import { MessageCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { deleteWeChatOABinding, getWeChatOABinding } from '../api'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { StatusBadge } from '@/components/status-badge'
import { WeChatBindDialog } from './dialogs/wechat-bind-dialog'

// ============================================================================
// WeChat Binding Card Component (profile)
// ============================================================================

interface WeChatBindCardProps {
  loading: boolean
}

interface BindingState {
  bound: boolean
  openidMasked: string
}

export function WeChatBindCard({ loading: pageLoading }: WeChatBindCardProps) {
  const { t } = useTranslation()
  const [binding, setBinding] = useState<BindingState | null>(null)
  const [loading, setLoading] = useState(true)
  const [bindOpen, setBindOpen] = useState(false)

  const refetch = useCallback(async () => {
    setLoading(true)
    try {
      const res = await getWeChatOABinding()
      if (res.success && res.data) {
        setBinding({
          bound: res.data.bound,
          openidMasked: res.data.openid_masked,
        })
      }
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (!pageLoading) {
      void refetch()
    }
  }, [pageLoading, refetch])

  const handleUnbind = async () => {
    const res = await deleteWeChatOABinding()
    if (res.success) {
      toast.success(t('Unbound'))
      void refetch()
    } else {
      toast.error(res.message || t('Unbind failed'))
    }
  }

  return (
    <>
      <Card data-card-hover='false' className='gap-0 overflow-hidden py-0'>
        <CardHeader className='p-3 sm:p-5'>
          <CardTitle className='text-lg tracking-tight sm:text-xl'>
            {t('WeChat Binding')}
          </CardTitle>
          <CardDescription className='text-xs sm:text-sm'>
            {t('Bind your WeChat account for scan-code sign in')}
          </CardDescription>
        </CardHeader>
        <CardContent className='p-3 sm:p-5'>
          {pageLoading || loading ? (
            <Skeleton className='h-12 w-full' />
          ) : (
            <div className='flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between'>
              <div className='flex items-start gap-4'>
                <div className='bg-muted rounded-md p-2'>
                  <MessageCircle className='h-5 w-5' />
                </div>
                <div className='space-y-1'>
                  <div className='flex items-center gap-2'>
                    <p className='font-medium'>{t('WeChat Binding')}</p>
                    {binding?.bound ? (
                      <StatusBadge
                        label={t('Enabled')}
                        variant='success'
                        showDot
                        copyable={false}
                      />
                    ) : (
                      <StatusBadge
                        label={t('Disabled')}
                        variant='neutral'
                        showDot
                        copyable={false}
                      />
                    )}
                  </div>
                  <p className='text-muted-foreground text-sm'>
                    {binding?.bound
                      ? `${t('Bound WeChat')}: ${binding.openidMasked}`
                      : t('Not bound yet')}
                  </p>
                </div>
              </div>
              {binding?.bound ? (
                <Button
                  variant='destructive'
                  className='w-full sm:w-auto'
                  onClick={handleUnbind}
                >
                  {t('Unbind')}
                </Button>
              ) : (
                <Button
                  className='w-full sm:w-auto'
                  onClick={() => setBindOpen(true)}
                >
                  {t('Bind WeChat')}
                </Button>
              )}
            </div>
          )}
        </CardContent>
      </Card>

      <WeChatBindDialog
        open={bindOpen}
        onOpenChange={setBindOpen}
        onSuccess={refetch}
      />
    </>
  )
}
