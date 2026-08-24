import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const viewPath = resolve(dirname(fileURLToPath(import.meta.url)), '../PaymentView.vue')
const viewSource = readFileSync(viewPath, 'utf8')

describe('PaymentView palette', () => {
  it('uses the shared blue-violet purchase canvas in light and dark modes', () => {
    expect(viewSource).toContain('--color-brand-400: 99 102 241;')
    expect(viewSource).toContain('--color-brand-500: 79 70 229;')
    expect(viewSource).toContain('--color-content-brand: 67 56 202;')
    expect(viewSource).toContain('linear-gradient(180deg, #f7f8ff 0%, #eef2ff 48%, #f8fafc 100%);')
    expect(viewSource).toContain(':global(.dark) .recharge-page-canvas')
  })
})
