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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Main } from '@/components/layout'
import {
  CardStaggerContainer,
  CardStaggerItem,
} from '@/components/page-transition'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import {
  createHermesInstance,
  ensureHermesUser,
  getHermesAccessToken,
  getHermesInstance,
  sleepHermesInstance,
  startHermesInstance,
} from './api'
import type { HermesInstance } from './types'

function InstanceStatus({ instance }: { instance: HermesInstance }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [openingWorkspace, setOpeningWorkspace] = useState(false)

  const invalidateInstance = () =>
    queryClient.invalidateQueries({ queryKey: ['hermes', 'instance'] })

  const startMutation = useMutation({
    mutationFn: () => startHermesInstance(instance.id),
    onSuccess: (res) => {
      if (res.success) {
        toast.success(t('Workspace started'))
        invalidateInstance()
      } else {
        toast.error(res.message || t('Failed to start workspace'))
      }
    },
    onError: () => toast.error(t('Failed to start workspace')),
  })

  const sleepMutation = useMutation({
    mutationFn: () => sleepHermesInstance(instance.id),
    onSuccess: (res) => {
      if (res.success) {
        toast.success(t('Workspace is sleeping'))
        invalidateInstance()
      } else {
        toast.error(res.message || t('Failed to sleep workspace'))
      }
    },
    onError: () => toast.error(t('Failed to sleep workspace')),
  })

  // Open Workspace: request a short-lived access token, then open the
  // manager-proxied workspace URL with the token as a query param.
  const handleOpenWorkspace = async () => {
    setOpeningWorkspace(true)
    try {
      const res = await getHermesAccessToken(instance.id)
      if (res.success && res.data) {
        const { workspaceUrl, token } = res.data
        const url = `${workspaceUrl}?token=${encodeURIComponent(token)}`
        window.open(url, '_blank', 'noopener,noreferrer')
      } else {
        toast.error(res.message || t('Failed to open workspace'))
      }
    } catch {
      toast.error(t('Failed to open workspace'))
    } finally {
      setOpeningWorkspace(false)
    }
  }

  const statusVariant = {
    running: 'default',
    sleeping: 'secondary',
    creating: 'outline',
    error: 'destructive',
  }[instance.status] as 'default' | 'secondary' | 'outline' | 'destructive'

  return (
    <Card>
      <CardHeader>
        <CardTitle className='flex items-center gap-2'>
          {t('Workspace Status')}
          <Badge variant={statusVariant}>{t(instance.status)}</Badge>
        </CardTitle>
      </CardHeader>
      <CardContent className='space-y-4'>
        <div className='grid grid-cols-2 gap-4 text-sm'>
          <div>
            <span className='text-muted-foreground'>{t('Plan')}</span>
            <p className='font-medium'>{instance.plan}</p>
          </div>
          <div>
            <span className='text-muted-foreground'>{t('Remaining Time')}</span>
            <p className='font-medium'>
              {instance.remainingMinutes !== undefined
                ? `${instance.remainingMinutes} ${t('minutes')}`
                : t('Unlimited')}
            </p>
          </div>
          {instance.lastError && (
            <div className='col-span-2'>
              <span className='text-muted-foreground'>{t('Last Error')}</span>
              <p className='text-destructive text-sm'>{instance.lastError}</p>
            </div>
          )}
        </div>

        <div className='flex gap-2'>
          {instance.status === 'sleeping' && (
            <Button
              size='sm'
              onClick={() => startMutation.mutate()}
              disabled={startMutation.isPending}
            >
              {startMutation.isPending ? t('Starting...') : t('Start')}
            </Button>
          )}
          {instance.status === 'running' && (
            <Button
              size='sm'
              variant='outline'
              onClick={() => sleepMutation.mutate()}
              disabled={sleepMutation.isPending}
            >
              {sleepMutation.isPending ? t('Sleeping...') : t('Sleep')}
            </Button>
          )}
          {instance.status === 'running' && (
            <Button
              size='sm'
              variant='secondary'
              onClick={handleOpenWorkspace}
              disabled={openingWorkspace}
            >
              {openingWorkspace ? t('Opening...') : t('Open Workspace')}
            </Button>
          )}
        </div>
      </CardContent>
    </Card>
  )
}

function HermesSkeleton() {
  return (
    <Card>
      <CardHeader>
        <Skeleton className='h-6 w-48' />
      </CardHeader>
      <CardContent className='space-y-4'>
        <div className='grid grid-cols-2 gap-4'>
          <Skeleton className='h-16' />
          <Skeleton className='h-16' />
        </div>
        <Skeleton className='h-8 w-32' />
      </CardContent>
    </Card>
  )
}

export function Hermes() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const { data, isLoading, error } = useQuery({
    queryKey: ['hermes', 'instance'],
    queryFn: getHermesInstance,
    refetchInterval: 10000,
  })

  // Ensure the current user exists in savvy-manager before showing the page.
  // Fire-and-forget; errors surface only if the user then tries an action.
  useEffect(() => {
    ensureHermesUser().catch(() => {
      // Swallow errors here; the user will see a clearer error on action.
    })
  }, [])

  const instance = data?.data

  const createMutation = useMutation({
    mutationFn: createHermesInstance,
    onSuccess: (res) => {
      if (res.success) {
        toast.success(t('Workspace created'))
        queryClient.invalidateQueries({ queryKey: ['hermes', 'instance'] })
      } else {
        toast.error(res.message || t('Failed to create workspace'))
      }
    },
    onError: () => toast.error(t('Failed to create workspace')),
  })

  return (
    <Main>
      <div className='min-h-0 flex-1 overflow-auto px-3 py-3 sm:px-4 sm:py-6'>
        <CardStaggerContainer className='mx-auto flex w-full max-w-3xl flex-col gap-4 sm:gap-6'>
          <CardStaggerItem>
            <div className='mb-4'>
              <h1 className='text-2xl font-bold'>{t('Hermes Workspace')}</h1>
              <p className='text-muted-foreground text-sm'>
                {t('Manage your cloud workspace')}
              </p>
            </div>
          </CardStaggerItem>

          <CardStaggerItem>
            {isLoading ? (
              <HermesSkeleton />
            ) : error ? (
              <Card>
                <CardContent className='py-8 text-center'>
                  <p className='text-muted-foreground'>
                    {t('Failed to load workspace status')}
                  </p>
                </CardContent>
              </Card>
            ) : instance && instance.id ? (
              <InstanceStatus instance={instance} />
            ) : (
              <Card>
                <CardContent className='py-8 text-center'>
                  <p className='text-muted-foreground mb-4'>
                    {t('No workspace found')}
                  </p>
                  <Button
                    onClick={() => createMutation.mutate()}
                    disabled={createMutation.isPending}
                  >
                    {createMutation.isPending
                      ? t('Creating...')
                      : t('Create Workspace')}
                  </Button>
                </CardContent>
              </Card>
            )}
          </CardStaggerItem>
        </CardStaggerContainer>
      </div>
    </Main>
  )
}
