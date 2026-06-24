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
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Main } from '@/components/layout'
import {
  CardStaggerContainer,
  CardStaggerItem,
} from '@/components/page-transition'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { getHermesInstance, startHermesInstance, sleepHermesInstance } from './api'
import type { HermesInstance } from './types'

function InstanceStatus({ instance }: { instance: HermesInstance }) {
  const { t } = useTranslation()

  const statusVariant = {
    running: 'success',
    sleeping: 'secondary',
    creating: 'warning',
    error: 'destructive',
  }[instance.status] as 'success' | 'secondary' | 'warning' | 'destructive'

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
            <Button size='sm' onClick={() => startHermesInstance(instance.id)}>
              {t('Start')}
            </Button>
          )}
          {instance.status === 'running' && (
            <Button
              size='sm'
              variant='outline'
              onClick={() => sleepHermesInstance(instance.id)}
            >
              {t('Sleep')}
            </Button>
          )}
          {instance.status === 'running' && (
            <Button size='sm' variant='secondary' asChild>
              <a href={instance.accessUrl} target='_blank' rel='noopener noreferrer'>
                {t('Open Workspace')}
              </a>
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
  const { data, isLoading, error } = useQuery({
    queryKey: ['hermes', 'instance'],
    queryFn: getHermesInstance,
    refetchInterval: 10000,
  })

  const instance = data?.data

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
            ) : instance ? (
              <InstanceStatus instance={instance} />
            ) : (
              <Card>
                <CardContent className='py-8 text-center'>
                  <p className='text-muted-foreground mb-4'>
                    {t('No workspace found')}
                  </p>
                  <Button>{t('Create Workspace')}</Button>
                </CardContent>
              </Card>
            )}
          </CardStaggerItem>
        </CardStaggerContainer>
      </div>
    </Main>
  )
}
