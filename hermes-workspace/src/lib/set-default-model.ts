/**
 * 把默认模型写进 config.yaml。
 *
 * 为什么必须写 config：上游 agent 的 `/api/sessions/{id}/chat/stream` 会收下
 * body 里的 provider/model 并原样回显在 `requested` 里，但运行时一律不采用
 * （实测传乱名、传裸名、传 provider+model 配对三种情况，`runtime.model` 恒为
 * 空字符串），最终永远走 config.yaml 的默认模型。所以"切换模型"只有落到
 * config 才真生效，只在前端存选择的话就是"界面显示切了、实际没切"。
 */
export async function setDefaultModelInConfig(modelId: string): Promise<void> {
  const model = modelId.trim()
  if (!model) throw new Error('modelId is empty')

  const current = await fetchJson('/api/hermes-config')

  // GET 在 config 能力不可用时返回的是 200 + 降级包（ok:false、activeProvider
  // 为空串），HTTP 状态骗不出来。不拦住的话下面会 fallback 成 'custom' —— 那是
  // 猜的，不是读到的。猜错 provider 会让模型脱离 new-api 网关直接把调用打挂，
  // 所以这里宁可报错也不猜。
  if ((current as { ok?: boolean })?.ok === false) {
    const reason =
      (current as { message?: unknown })?.message ?? 'config capability unavailable'
    throw new Error(`无法读取当前配置：${String(reason)}`)
  }

  // providerId 沿用 config 现值，不按 UI 分组名改：本部署所有模型都经 new-api
  // 网关代理（provider=custom，base_url 指向 new-api），把 provider 改成
  // deepseek/openai 这类分组名会脱离网关，直接把调用打挂。
  const providerId =
    (current as Record<string, never>)?.activeProvider ||
    (current as { defaultModel?: { provider?: string } })?.defaultModel
      ?.provider ||
    'custom'

  const payload = await fetchJson('/api/hermes-config', {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ action: 'set-default-model', providerId, modelId: model }),
  })

  if ((payload as { ok?: boolean })?.ok === false) {
    const err = (payload as { error?: unknown })?.error
    throw new Error(String(err || 'config patch rejected'))
  }
}

/**
 * fetch + 断言拿到的真是 JSON。
 *
 * 只看 `res.ok` 不够：路由不存在时请求会落到 SPA 的 catch-all，拿回 200
 * text/html，`res.ok` 为真却什么都没发生 —— `/api/model-switch` 就是这么
 * 一直假装成功的（那个路由从来没实现过）。
 */
async function fetchJson(url: string, init?: RequestInit): Promise<unknown> {
  const res = await fetch(url, init)
  const contentType = res.headers.get('content-type') || ''
  if (!res.ok || !contentType.includes('application/json')) {
    throw new Error(
      `${init?.method || 'GET'} ${url} → HTTP ${res.status} (${contentType || 'no content-type'})`,
    )
  }
  return res.json()
}
