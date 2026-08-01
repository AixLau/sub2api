import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const viewPath = resolve(dirname(fileURLToPath(import.meta.url)), '../PaymentView.vue')
const viewSource = readFileSync(viewPath, 'utf8')

describe('PaymentView palette', () => {
  it('keeps the recharge experience on its original blue theme', () => {
    expect(viewSource).toContain('--color-brand-400: 47 128 255;')
    expect(viewSource).toContain('--color-brand-500: 15 98 254;')
    expect(viewSource).toContain('--color-content-brand: 29 78 216;')
    expect(viewSource).toContain('linear-gradient(180deg, #f6f9ff 0%, #eef4fb 100%);')
    expect(viewSource).toContain(':global(.dark) .recharge-page-canvas')
  })
})
