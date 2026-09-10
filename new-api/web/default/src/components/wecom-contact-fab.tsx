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
import { QRCodeSVG } from 'qrcode.react'
import { useTranslation } from 'react-i18next'
import { useStatus } from '@/hooks/use-status'

// 全站右下角悬浮「微信客服」入口,占最角落(bottom-4),智能体机器人球
// 堆在它上方(见 widget.tsx)——这样聊天面板打开时紧贴自己的触发按钮,
// 中间不会夹着一个企微头像。
// 桌面 hover 在左侧浮出二维码卡(纯展示,pointer-events-none,扫完即走,
// 不需要把鼠标移进去,因此也不会出现 hover 间隙闪断);click 新标签打开
// 客服链接——微信内直接进会话,是移动端的主路径,hover 只是桌面增强。
// 二维码由 qrcode.react 拿后台配置的 kf url 现场生成,改链接自动跟随,
// 不放静态二维码图(静态图不跟配置走,改链接就过期)。
// 未配置 wecom_kf_url 时整体不渲染,与 LegalLinks 同样的自隐逻辑。
export function WeComContactFab(props: { agentPanelOpen?: boolean }) {
  const { t } = useTranslation()
  const { status } = useStatus()
  const kfUrl = status?.wecom_kf_url as string | undefined
  if (!kfUrl) return null

  return (
    <div className='fixed right-4 bottom-4 z-50'>
      <div className='group relative'>
        {/* hover 二维码卡:只看不点,pointer-events-none。聊天面板展开时改到
            面板左侧与它并排(fixed 到面板左缘、底边对齐面板底),不压在面板上;
            面板收起时贴在悬浮球左侧。底对齐而非垂直居中,避免伸出视口被切 */}
        <div
          className={`bg-background pointer-events-none fixed z-50 hidden flex-col items-center gap-2 rounded-xl border p-3 shadow-xl group-hover:flex ${
            props.agentPanelOpen
              ? 'right-[calc(5.5rem+min(420px,100vw-6.75rem))] bottom-4'
              : 'right-[4.75rem] bottom-4'
          }`}
        >
          <div className='rounded-lg bg-white p-2'>
            <QRCodeSVG value={kfUrl} size={140} />
          </div>
          <p className='text-muted-foreground text-xs whitespace-nowrap'>
            {t('Scan with WeChat to contact support')}
          </p>
        </div>

        <a
          href={kfUrl}
          target='_blank'
          rel='noopener noreferrer'
          aria-label={t('Customer Service')}
          className='bg-white flex size-12 items-center justify-center rounded-full shadow-lg transition-transform duration-200 hover:scale-105'
        >
          <img
            src='/wecom-avatar.png'
            alt=''
            aria-hidden='true'
            className='size-6'
          />
        </a>
      </div>
    </div>
  )
}
