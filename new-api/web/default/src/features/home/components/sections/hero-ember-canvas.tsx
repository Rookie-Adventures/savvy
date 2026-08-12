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
import { useEffect, useRef } from 'react'

// ponytail: reads the resolved --warning color via a hidden probe element.
// getComputedStyle().color does NOT reliably return rgb() — for an
// oklch()-declared CSS var, this browser serializes it as lab(...), which
// broke the naive regex-over-the-string approach (it read Lab L/a/b as if
// they were R/G/B and produced a purple tint by accident). Routing the
// value through a canvas 2D context's fillStyle setter/getter normalizes
// any CSS color string to rgb()/#hex, regardless of the color space it was
// declared in.
function readWarningRGB(): [number, number, number] {
  const fallback: [number, number, number] = [217, 119, 6]

  const probe = document.createElement('div')
  probe.className = 'text-warning'
  probe.style.position = 'absolute'
  probe.style.opacity = '0'
  probe.style.pointerEvents = 'none'
  document.body.appendChild(probe)
  const raw = getComputedStyle(probe).color
  document.body.removeChild(probe)

  const probeCanvas = document.createElement('canvas')
  const probeCtx = probeCanvas.getContext('2d')
  if (!probeCtx) return fallback

  probeCtx.fillStyle = raw
  const normalized = probeCtx.fillStyle

  const hexMatch = normalized.match(/^#([0-9a-f]{6})$/i)
  if (hexMatch) {
    const hex = hexMatch[1]
    return [
      parseInt(hex.slice(0, 2), 16),
      parseInt(hex.slice(2, 4), 16),
      parseInt(hex.slice(4, 6), 16),
    ]
  }

  const rgbMatch = normalized.match(/rgba?\((\d+),\s*(\d+),\s*(\d+)/)
  if (rgbMatch) {
    return [Number(rgbMatch[1]), Number(rgbMatch[2]), Number(rgbMatch[3])]
  }

  return fallback
}

interface Blob {
  x: number
  y: number
  r: number
  speedX: number
  speedY: number
  phase: number
  hueShift: number
}

export function HeroEmberCanvas() {
  const canvasRef = useRef<HTMLCanvasElement>(null)

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return

    const ctx = canvas.getContext('2d')
    if (!ctx) return

    const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches
    const [r, g, b] = readWarningRGB()

    let width = 0
    let height = 0
    const dpr = Math.min(devicePixelRatio || 1, 2)

    function resize() {
      if (!canvas) return
      width = canvas.clientWidth
      height = canvas.clientHeight
      canvas.width = width * dpr
      canvas.height = height * dpr
      ctx?.setTransform(dpr, 0, 0, dpr, 0, 0)
    }
    resize()
    addEventListener('resize', resize)

    const BLOBS: Blob[] = Array.from({ length: 5 }, (_, i) => ({
      x: Math.random(),
      y: Math.random() * 0.6,
      r: 0.28 + Math.random() * 0.22,
      speedX: 0.00006 * (i % 2 === 0 ? 1 : -1) * (0.6 + Math.random()),
      speedY: 0.00004 * (0.6 + Math.random()),
      phase: Math.random() * Math.PI * 2,
      hueShift: Math.random() * 20 - 10,
    }))

    let raf = 0
    let t = 0

    function drawFrame(time: number) {
      if (!ctx) return
      ctx.clearRect(0, 0, width, height)
      ctx.globalCompositeOperation = 'lighter'

      for (const blob of BLOBS) {
        const cx = (blob.x + Math.sin(time * blob.speedX + blob.phase) * 0.12) * width
        const cy = (blob.y + Math.cos(time * blob.speedY + blob.phase) * 0.08) * height
        const radius = blob.r * Math.max(width, height)

        const grad = ctx.createRadialGradient(cx, cy, 0, cx, cy, radius)
        grad.addColorStop(0, `rgba(${r + blob.hueShift}, ${g}, ${b}, 0.10)`)
        grad.addColorStop(0.5, `rgba(${r}, ${g}, ${b}, 0.045)`)
        grad.addColorStop(1, 'rgba(0,0,0,0)')

        ctx.fillStyle = grad
        ctx.fillRect(0, 0, width, height)
      }

      ctx.globalCompositeOperation = 'source-over'
    }

    if (reduce) {
      drawFrame(0)
    } else {
      const loop = (time: number) => {
        t = time
        drawFrame(t)
        raf = requestAnimationFrame(loop)
      }
      raf = requestAnimationFrame(loop)
    }

    return () => {
      removeEventListener('resize', resize)
      if (raf) cancelAnimationFrame(raf)
    }
  }, [])

  return (
    <canvas
      ref={canvasRef}
      aria-hidden
      className='pointer-events-none absolute inset-0 -z-20 h-full w-full'
    />
  )
}
