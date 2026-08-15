import { afterEach, describe, expect, it, vi } from 'vitest'
import { setDefaultModelInConfig } from './set-default-model'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  })
}

/** SPA catch-all 的样子：200 但是 text/html，什么都没改。 */
function spaFallbackResponse() {
  return new Response('<!DOCTYPE html><html></html>', {
    status: 200,
    headers: { 'content-type': 'text/html; charset=utf-8' },
  })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('setDefaultModelInConfig', () => {
  it('沿用 config 现有 provider，只改 modelId', async () => {
    const calls: Array<{ url: string; init?: RequestInit }> = []
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: RequestInit) => {
        calls.push({ url, init })
        if (init?.method === 'PATCH') return jsonResponse({ ok: true })
        return jsonResponse({ activeProvider: 'custom' })
      }),
    )

    await setDefaultModelInConfig('deepseek-v4-pro')

    const patch = calls.find((c) => c.init?.method === 'PATCH')
    expect(patch).toBeDefined()
    expect(JSON.parse(String(patch?.init?.body))).toEqual({
      action: 'set-default-model',
      providerId: 'custom',
      modelId: 'deepseek-v4-pro',
    })
  })

  // 这条是本次 bug 的核心回归点：路由缺失时 SPA 兜底返回 200 text/html，
  // 只看 res.ok 会误判成功 —— 那正是 /api/model-switch 骗了人的原因。
  it('PATCH 拿到 200 text/html 时必须抛错，不能当成功', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (_url: string, init?: RequestInit) => {
        if (init?.method === 'PATCH') return spaFallbackResponse()
        return jsonResponse({ activeProvider: 'custom' })
      }),
    )

    await expect(setDefaultModelInConfig('deepseek-v4-pro')).rejects.toThrow(
      /text\/html/,
    )
  })

  it('服务端明确返回 ok:false 时抛错', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (_url: string, init?: RequestInit) => {
        if (init?.method === 'PATCH')
          return jsonResponse({ ok: false, error: 'config unavailable' })
        return jsonResponse({ activeProvider: 'custom' })
      }),
    )

    await expect(setDefaultModelInConfig('deepseek-v4-pro')).rejects.toThrow(
      'config unavailable',
    )
  })

  it('空模型名直接拒绝，不发请求', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    await expect(setDefaultModelInConfig('   ')).rejects.toThrow('modelId is empty')
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
