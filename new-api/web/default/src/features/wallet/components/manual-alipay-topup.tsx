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
// ponytail: 临时人工收款入口,正式渠道接入后整个文件删除
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/dialog'
import { getPaymentIcon } from '../lib'
import alipayQrUrl from '@/assets/alipay-qr.png'

export function ManualAlipayTopup() {
  return (
    <Dialog
      title='支付宝付款'
      description='扫码支付后,请联系客服 support@scheng.net 提供付款凭证,人工核对后充值到账。'
      trigger={
        <Button
          variant='outline'
          className='min-h-14 w-full justify-start gap-2 rounded-lg px-3 py-2 text-left'
        >
          {getPaymentIcon('alipay', 'h-4 w-4')}
          <span className='truncate'>支付宝付款</span>
        </Button>
      }
    >
      <div className='flex flex-col items-center gap-3 pb-2'>
        <img
          src={alipayQrUrl}
          alt='支付宝收款二维码'
          className='w-64 max-w-full rounded-lg border object-contain'
        />
      </div>
    </Dialog>
  )
}
