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
import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Switch } from '@/components/ui/switch'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import { CUSTOM_PAGE_SLUGS, CUSTOM_PAGE_LABELS, type CustomPageSlug } from '@/features/custom-pages/types'

type CustomPagesSectionProps = {
  defaultValues: Record<string, string>
}

const PAGE_KEYS: CustomPageSlug[] = [...CUSTOM_PAGE_SLUGS]

const OPTION_KEY_MAP: Record<CustomPageSlug, { content: string; enabled: string }> = {
  Product: { content: 'CustomPage.Product', enabled: 'CustomPage.Product.Enabled' },
  Faq: { content: 'CustomPage.Faq', enabled: 'CustomPage.Faq.Enabled' },
  Refund: { content: 'CustomPage.Refund', enabled: 'CustomPage.Refund.Enabled' },
  Contact: { content: 'CustomPage.Contact', enabled: 'CustomPage.Contact.Enabled' },
  OpenSource: { content: 'CustomPage.OpenSource', enabled: 'CustomPage.OpenSource.Enabled' },
}

export function CustomPagesSection(props: CustomPagesSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [pages, setPages] = useState<Record<CustomPageSlug, { content: string; enabled: boolean }>>(
    () => {
      const initial: Record<string, { content: string; enabled: boolean }> = {}
      for (const slug of PAGE_KEYS) {
        const keys = OPTION_KEY_MAP[slug]
        initial[slug] = {
          content: props.defaultValues[keys.content] || '',
          enabled: props.defaultValues[keys.enabled] === 'true',
        }
      }
      return initial as Record<CustomPageSlug, { content: string; enabled: boolean }>
    }
  )
  const [hasChanges, setHasChanges] = useState(false)

  useEffect(() => {
    const next: Record<string, { content: string; enabled: boolean }> = {}
    for (const slug of PAGE_KEYS) {
      const keys = OPTION_KEY_MAP[slug]
      next[slug] = {
        content: props.defaultValues[keys.content] || '',
        enabled: props.defaultValues[keys.enabled] === 'true',
      }
    }
    setPages(next as Record<CustomPageSlug, { content: string; enabled: boolean }>)
  }, [props.defaultValues])

  const handleContentChange = (slug: CustomPageSlug, value: string) => {
    setPages((prev) => ({ ...prev, [slug]: { ...prev[slug], content: value } }))
    setHasChanges(true)
  }

  const handleEnabledChange = (slug: CustomPageSlug, value: boolean) => {
    setPages((prev) => ({ ...prev, [slug]: { ...prev[slug], enabled: value } }))
    setHasChanges(true)
  }

  const handleSave = async () => {
    try {
      for (const slug of PAGE_KEYS) {
        const keys = OPTION_KEY_MAP[slug]
        await updateOption.mutateAsync({ key: keys.content, value: pages[slug].content })
        await updateOption.mutateAsync({ key: keys.enabled, value: String(pages[slug].enabled) })
      }
      setHasChanges(false)
      toast.success(t('Custom pages saved successfully'))
    } catch {
      toast.error(t('Failed to save custom pages'))
    }
  }

  return (
    <SettingsSection title={t('Custom Pages')}>
      <p className='text-muted-foreground text-sm'>
        {t('Configure content for custom pages. Content supports Markdown and HTML.')}
      </p>
      <div className='flex flex-col gap-6'>
        {PAGE_KEYS.map((slug) => (
          <div key={slug} className='flex flex-col gap-3 rounded-lg border p-4'>
            <div className='flex items-center justify-between'>
              <h4 className='text-sm font-medium'>{t(CUSTOM_PAGE_LABELS[slug])}</h4>
              <div className='flex items-center gap-2'>
                <span className='text-muted-foreground text-xs'>
                  {pages[slug].enabled ? t('Enabled') : t('Disabled')}
                </span>
                <Switch
                  checked={pages[slug].enabled}
                  onCheckedChange={(checked) => handleEnabledChange(slug, checked)}
                />
              </div>
            </div>
            <Textarea
              rows={8}
              value={pages[slug].content}
              onChange={(e) => handleContentChange(slug, e.target.value)}
              placeholder={t('Enter page content (supports Markdown/HTML)')}
            />
          </div>
        ))}
      </div>
      <div className='flex justify-end'>
        <Button onClick={handleSave} disabled={!hasChanges || updateOption.isPending}>
          {updateOption.isPending ? t('Saving...') : t('Save Settings')}
        </Button>
      </div>
    </SettingsSection>
  )
}
