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
import { api } from '@/lib/api'
import type {
  ApiResponse,
  HermesAccessToken,
  HermesInstance,
  HermesProviderState,
  StartHermesInstancePayload,
} from './types'

export async function getHermesInstance(): Promise<ApiResponse<HermesInstance>> {
  const res = await api.get('/api/hermes/instance')
  return res.data
}

export async function createHermesInstance(): Promise<
  ApiResponse<HermesInstance>
> {
  const res = await api.post('/api/hermes/instance')
  return res.data
}

export async function ensureHermesUser(): Promise<ApiResponse> {
  const res = await api.post('/api/hermes/user/ensure')
  return res.data
}

export async function startHermesInstance(
  instanceId: string,
  payload: StartHermesInstancePayload
): Promise<ApiResponse> {
  const res = await api.post(
    `/api/hermes/instance/${instanceId}/start`,
    payload
  )
  return res.data
}

export async function revokeHermesProviderKey(
  instanceId: string
): Promise<ApiResponse> {
  const res = await api.post(
    `/api/hermes/instance/${instanceId}/revoke-provider-key`
  )
  return res.data
}

export async function getHermesProviderState(
  instanceId: string
): Promise<ApiResponse<HermesProviderState>> {
  const res = await api.get(
    `/api/hermes/instance/${instanceId}/provider-state`
  )
  return res.data
}

export async function sleepHermesInstance(
  instanceId: string
): Promise<ApiResponse> {
  const res = await api.post(`/api/hermes/instance/${instanceId}/sleep`)
  return res.data
}

export async function getHermesAccessToken(
  instanceId: string
): Promise<ApiResponse<HermesAccessToken>> {
  const res = await api.post(`/api/hermes/instance/${instanceId}/access-token`)
  return res.data
}
