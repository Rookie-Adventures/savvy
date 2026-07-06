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
import { Dialog } from '@/components/dialog'
import { PasswordInput } from '@/components/password-input'
import {
  createHermesInstance,
  ensureHermesUser,
  getHermesAccessToken,
  getHermesInstance,
  getHermesProviderState,
  revokeHermesProviderKey,
  sleepHermesInstance,
  startHermesInstance,
} from './api'
import type { HermesInstance, StartHermesInstancePayload } from './types'

function InstanceStatus({ instance }: { instance: HermesInstance }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [openingWorkspace, setOpeningWorkspace] = useState(false)
  const [startOpen, setStartOpen] = useState(false)
  const [revokeOpen, setRevokeOpen] = useState(false)
  const [providerApikey, setProviderApikey] = useState('')

  const invalidateInstance = () =>
    queryClient.refetchQueries({ queryKey: ['hermes', 'instance'] })

  const invalidateProviderState = () =>
    queryClient.invalidateQueries({
      queryKey: ['hermes', 'providerState', instance.id],
    })

  const providerState = useQuery({
    queryKey: ['hermes', 'providerState', instance.id],
    queryFn: () => getHermesProviderState(instance.id),
    enabled: instance.status === 'running',
  })

  const startMutation = useMutation({
    mutationFn: (payload: StartHermesInstancePayload) =>
      startHermesInstance(instance.id, payload),
    onSuccess: (res) => {
      if (res.success) {
        toast.success(t('Workspace started'))
        setStartOpen(false)
        setProviderApikey('')
        invalidateInstance()
      } else {
        toast.error(res.message || t('Failed to start workspace'))
      }
    },
    onError: () => toast.error(t('Failed to start workspace')),
  })

  const revokeMutation = useMutation({
    mutationFn: () => revokeHermesProviderKey(instance.id),
    onSuccess: (res) => {
      if (res.success) {
        toast.success(t('Revoke provider key'))
        setRevokeOpen(false)
        invalidateProviderState()
        invalidateInstance()
      } else {
        toast.error(res.message || t('Revoke provider key'))
      }
    },
    onError: () => toast.error(t('Revoke provider key')),
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
  // A short settle delay covers the window between docker reporting 'running'
  // (status=RUNNING exposes the Open button) and the in-container HTTP server
  // finishing boot — opening too fast hits a not-yet-listening workspace
  // and the browser shows 401. 5s is enough in practice without feeling slow.
  const handleOpenWorkspace = async () => {
    setOpeningWorkspace(true)
    try {
      const res = await getHermesAccessToken(instance.id)
      if (res.success && res.data) {
        const { workspaceUrl, token } = res.data
        await new Promise((r) => setTimeout(r, 5000))
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

  // creating = NOT_CREATED yet (first start, key REQUIRED).
  // sleeping = wake (key OPTIONAL — empty key means backend uses DB snapshot).
  const canStart = instance.status === 'creating' || instance.status === 'sleeping'
  const isFirstStart = instance.status === 'creating'

  const handleStartSubmit = () => {
    if (isFirstStart && !providerApikey.trim()) {
      toast.error(t('First start requires an API key'))
      return
    }
    startMutation.mutate({ providerApiKey: providerApikey })
  }

  const providerStateNote =
    providerState.data?.data && instance.status === 'running'
      ? (() => {
          const { source } = providerState.data!.data!
          if (source === 'ours') {
            return t(
              "Currently using: this platform's key (billed to your balance)"
            )
          }
          if (source === 'user') {
            return t(
              'Currently using: your custom provider key (billed by your provider)'
            )
          }
          return t(
            'No provider key configured — chat will fail. Restart and provide a key.'
          )
        })()
      : null

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
              {instance.plan === 'FREE'
                ? t('2 hours per start')
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

        {providerStateNote && (
          <p className='text-muted-foreground text-xs'>{providerStateNote}</p>
        )}

        <div className='flex flex-wrap gap-2'>
          {canStart && (
            <>
              <Button
                size='sm'
                onClick={() => setStartOpen(true)}
                disabled={startMutation.isPending}
              >
                {startMutation.isPending
                  ? (isFirstStart ? t('Starting first setup…') : t('Waking up…'))
                  : t('Start')}
              </Button>
              <Dialog
                open={startOpen}
                onOpenChange={(open) => {
                  setStartOpen(open)
                  if (!open) setProviderApikey('')
                }}
                title={
                  isFirstStart
                    ? t('First start requires an API key')
                    : t('Start workspace')
                }
                description={
                  isFirstStart
                    ? t(
                        'First-time setup takes 10–30 seconds to configure the environment. Please wait.'
                      ) +
                    '\n' +
                    t(
                      'You can generate one on the API Keys page and paste it here. We recommend the key you generated on this platform (billed to your account balance).'
                    )
                    : t(
                        'Waking up your workspace. This usually takes a few seconds.'
                      )
                }
                contentClassName='sm:max-w-md'
                bodyClassName='space-y-4'
                footer={
                  <>
                    <Button
                      type='button'
                      variant='outline'
                      onClick={() => setStartOpen(false)}
                      disabled={startMutation.isPending}
                    >
                      {t('Cancel')}
                    </Button>
                    <Button
                      type='button'
                      onClick={handleStartSubmit}
                      disabled={startMutation.isPending}
                    >
                      {startMutation.isPending
                        ? (isFirstStart ? t('Setting up environment…') : t('Waking up…'))
                        : (isFirstStart ? t('Start setup') : t('Start'))}
                    </Button>
                  </>
                }
              >
                <div className='space-y-2'>
                  {isFirstStart ? (
                    <>
                      <label className='text-sm font-medium'>
                        {t('Provider key (required on first start)')}
                      </label>
                      <PasswordInput
                        value={providerApikey}
                        onChange={(e) => setProviderApikey(e.target.value)}
                        placeholder='sk-...'
                        autoComplete='off'
                      />
                    </>
                  ) : (
                    <p className='text-sm text-muted-foreground'>
                      {t(
                        'Leave empty to keep your current key. Fill to switch keys.'
                      )}
                    </p>
                  )}
                </div>
              </Dialog>
            </>
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
          {instance.status === 'running' && (
            <>
              <Button
                size='sm'
                variant='destructive'
                onClick={() => setRevokeOpen(true)}
              >
                {t('Revoke provider key')}
              </Button>
              <Dialog
                open={revokeOpen}
                onOpenChange={setRevokeOpen}
                title={t('Confirm revoke')}
                description={t(
                  'Revoke clears all LLM provider keys on this workspace. Your data is kept; chat will fail until you restart with a key.'
                )}
                contentClassName='sm:max-w-md'
                bodyClassName='space-y-4'
                footer={
                  <>
                    <Button
                      type='button'
                      variant='outline'
                      onClick={() => setRevokeOpen(false)}
                      disabled={revokeMutation.isPending}
                    >
                      {t('Cancel')}
                    </Button>
                    <Button
                      type='button'
                      variant='destructive'
                      onClick={() => revokeMutation.mutate()}
                      disabled={revokeMutation.isPending}
                    >
                      {t('Revoke provider key')}
                    </Button>
                  </>
                }
              >
                <p className='text-muted-foreground text-sm'>
                  {t(
                    'Your data (sessions, files, memory, skills) is preserved. Revoking only clears the key — sending messages will fail until you re-enter a key.'
                  )}
                </p>
              </Dialog>
            </>
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
