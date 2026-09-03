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
import { useTranslation } from 'react-i18next'
import { Bot, Trash2, X } from 'lucide-react'
import { Button } from '@/components/ui/button'

type ChatHeaderProps = {
  onClear: () => void
  // 仅 widget 传入(浮窗可关闭);独立页不传则不渲染关闭按钮
  onClose?: () => void
}

export function ChatHeader({ onClear, onClose }: ChatHeaderProps) {
  const { t } = useTranslation()
  return (
    <div className='flex items-center justify-between border-b px-3 py-2'>
      <div className='flex items-center gap-2'>
        <Bot className='text-primary h-4 w-4' />
        <span className='text-sm font-medium'>{t('AI assistant')}</span>
      </div>
      <div className='flex items-center gap-1'>
        <Button
          variant='ghost'
          size='icon'
          className='h-7 w-7'
          onClick={onClear}
          aria-label={t('Clear conversation')}
          title={t('Clear conversation')}
        >
          <Trash2 className='h-4 w-4' />
        </Button>
        {onClose && (
          <Button
            variant='ghost'
            size='icon'
            className='h-7 w-7'
            onClick={onClose}
            aria-label={t('Close')}
          >
            <X className='h-4 w-4' />
          </Button>
        )}
      </div>
    </div>
  )
}
