import { describe, expect, it } from 'vitest'
import zh from '../locales/zh'
import en from '../locales/en'

describe('purchase page locale copy', () => {
  it('keeps purchase page descriptions focused on the user action', () => {
    const descriptions = [
      zh.purchase.description,
      en.purchase.description,
    ]

    for (const description of descriptions) {
      expect(description).not.toMatch(/内嵌|iframe|embedded/i)
      expect(description).toMatch(/\S/)
    }
  })
})
