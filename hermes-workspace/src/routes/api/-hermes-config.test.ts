import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@tanstack/react-router', () => ({
  createFileRoute: (_path: string) => (opts: any) => opts,
}))

vi.mock('../../server/auth-middleware', () => ({
  isAuthenticated: () => true,
}))

// 用可变状态 + 顶层 vi.mock，别用 doMock/doUnmock：doUnmock 是把模块还原成
// 真实实现（而不是恢复这个顶层 mock），真实 getCapabilities() 返回未探测的
// 初始值 config:false，会让它后面的用例走降级分支拿到空 providers 而误红。
const gateway = vi.hoisted(() => ({
  capabilities: { config: true } as { config: boolean },
  forceReprobeGateway: vi.fn(),
}))

vi.mock('../../server/gateway-capabilities', () => ({
  ensureGatewayProbed: vi.fn(),
  forceReprobeGateway: gateway.forceReprobeGateway,
  getCapabilities: () => gateway.capabilities,
}))

vi.mock('../../server/local-provider-discovery', () => ({
  ensureDiscovery: vi.fn(),
  getDiscoveryStatus: () => [],
  getDiscoveredModels: () => [],
}))

let tmpHome = ''
const originalEnv: Record<string, string | undefined> = {}

function setEnv(key: string, value: string | undefined) {
  if (!(key in originalEnv)) originalEnv[key] = process.env[key]
  if (value === undefined) delete process.env[key]
  else process.env[key] = value
}

beforeEach(() => {
  tmpHome = fs.mkdtempSync(path.join(os.tmpdir(), 'hermes-config-route-'))
  setEnv('HERMES_HOME', tmpHome)
  setEnv('CLAUDE_HOME', undefined)
  gateway.capabilities = { config: true }
  gateway.forceReprobeGateway.mockReset()
  vi.resetModules()
})

afterEach(() => {
  for (const [key, value] of Object.entries(originalEnv)) {
    if (value === undefined) delete process.env[key]
    else process.env[key] = value
  }
  for (const key of Object.keys(originalEnv)) delete originalEnv[key]
  fs.rmSync(tmpHome, { recursive: true, force: true })
})

async function loadHandlers(modulePath: string) {
  const mod = await import(modulePath)
  return (mod as any).Route.server.handlers
}

describe('canonical /api/hermes-config route', () => {
  it('GET returns normalized provider state with paths and active provider', async () => {
    fs.writeFileSync(
      path.join(tmpHome, 'config.yaml'),
      'provider: openrouter\nmodel: auto\n',
      'utf-8',
    )
    fs.writeFileSync(
      path.join(tmpHome, '.env'),
      'OPENROUTER_API_KEY=sk-test-1234\n',
      'utf-8',
    )

    const handlers = await loadHandlers('./hermes-config')
    const res = await handlers.GET({
      request: new Request('http://localhost/api/hermes-config'),
    })
    const body = await res.json()

    expect(body.ok).toBe(true)
    expect(body.activeProvider).toBe('openrouter')
    expect(body.activeModel).toBe('auto')
    expect(body.paths.hermesHome).toBe(tmpHome)
    const openrouter = body.providers.find((p: any) => p.id === 'openrouter')
    expect(openrouter.configured).toBe(true)
    expect(openrouter.isDefault).toBe(true)
  })

  it('PATCH dispatches set-default-model and returns the action message', async () => {
    const handlers = await loadHandlers('./hermes-config')
    const res = await handlers.PATCH({
      request: new Request('http://localhost/api/hermes-config', {
        method: 'PATCH',
        body: JSON.stringify({
          action: 'set-default-model',
          providerId: 'openrouter',
          modelId: 'auto',
        }),
      }),
    })
    const body = await res.json()

    expect(body).toMatchObject({ ok: true, message: 'Default model updated.' })
    expect(
      fs.readFileSync(path.join(tmpHome, 'config.yaml'), 'utf-8'),
    ).toMatch(/provider: openrouter/)
  })

  it('PATCH legacy { config } body deep-merges and preserves siblings', async () => {
    fs.writeFileSync(
      path.join(tmpHome, 'config.yaml'),
      'memory:\n  user_profile_enabled: true\n',
      'utf-8',
    )

    const handlers = await loadHandlers('./hermes-config')
    await handlers.PATCH({
      request: new Request('http://localhost/api/hermes-config', {
        method: 'PATCH',
        body: JSON.stringify({ config: { memory: { memory_enabled: true } } }),
      }),
    })

    const onDisk = fs.readFileSync(path.join(tmpHome, 'config.yaml'), 'utf-8')
    expect(onDisk).toContain('memory_enabled: true')
    expect(onDisk).toContain('user_profile_enabled: true')
  })

  it('PATCH rejects malformed action bodies with 400', async () => {
    const handlers = await loadHandlers('./hermes-config')
    const res = await handlers.PATCH({
      request: new Request('http://localhost/api/hermes-config', {
        method: 'PATCH',
        body: JSON.stringify({ action: 'set-default-model' }),
      }),
    })
    expect(res.status).toBe(400)
  })

  it('PATCH returns 503 when the gateway capability is unavailable', async () => {
    // 重探之后仍然 false —— 这才是真的不可用，该拒。
    gateway.capabilities = { config: false }
    const handlers = await loadHandlers('./hermes-config')
    const res = await handlers.PATCH({
      request: new Request('http://localhost/api/hermes-config', {
        method: 'PATCH',
        body: JSON.stringify({ action: 'set-api-key', envKey: 'X', value: 'y' }),
      }),
    })
    expect(res.status).toBe(503)
    expect(gateway.forceReprobeGateway).toHaveBeenCalledTimes(1)
  })

  // 缓存里的 config=false 可能只是 dashboard 抖了一下留下的陈旧值（探测超时
  // 仅 3s，失败结果却按 120s 缓存），不该让用户干等两分钟才能切模型。
  it('PATCH re-probes once before giving up, so a stale false does not block the write', async () => {
    gateway.capabilities = { config: false }
    gateway.forceReprobeGateway.mockImplementation(async () => {
      gateway.capabilities = { config: true }
    })
    const handlers = await loadHandlers('./hermes-config')
    const res = await handlers.PATCH({
      request: new Request('http://localhost/api/hermes-config', {
        method: 'PATCH',
        body: JSON.stringify({
          action: 'set-default-model',
          providerId: 'custom',
          modelId: 'deepseek-v4-flash',
        }),
      }),
    })
    expect(gateway.forceReprobeGateway).toHaveBeenCalledTimes(1)
    expect(res.status).toBe(200)
    expect(fs.readFileSync(path.join(tmpHome, 'config.yaml'), 'utf-8')).toMatch(
      /deepseek-v4-flash/,
    )
  })
})

describe('legacy /api/claude-config alias', () => {
  it('GET aliases provider.maskedCredentials to provider.maskedKeys for the legacy /settings page', async () => {
    fs.writeFileSync(
      path.join(tmpHome, '.env'),
      'OPENROUTER_API_KEY=sk-test-1234\n',
      'utf-8',
    )

    const handlers = await loadHandlers('./claude-config')
    const res = await handlers.GET({
      request: new Request('http://localhost/api/claude-config'),
    })
    const body = await res.json()
    const openrouter = body.providers.find((p: any) => p.id === 'openrouter')

    expect(openrouter.maskedKeys).toEqual(openrouter.maskedCredentials)
    expect(openrouter.maskedKeys.OPENROUTER_API_KEY).toBeTruthy()
  })
})
