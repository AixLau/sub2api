import fs from 'node:fs'
import path from 'node:path'
import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

type LocaleTree = Record<string, unknown>

function flattenKeys(obj: LocaleTree, prefix = ''): Set<string> {
  const keys = new Set<string>()

  for (const [key, value] of Object.entries(obj)) {
    const fullKey = prefix ? `${prefix}.${key}` : key
    if (value && typeof value === 'object' && !Array.isArray(value)) {
      for (const childKey of flattenKeys(value as LocaleTree, fullKey)) {
        keys.add(childKey)
      }
    } else {
      keys.add(fullKey)
    }
  }

  return keys
}

function walkSourceFiles(dir: string): string[] {
  const entries = fs.readdirSync(dir, { withFileTypes: true })
  const files: string[] = []

  for (const entry of entries) {
    const filePath = path.join(dir, entry.name)
    if (entry.isDirectory()) {
      files.push(...walkSourceFiles(filePath))
    } else if (/\.(ts|vue)$/.test(entry.name) && !filePath.includes(`${path.sep}i18n${path.sep}locales${path.sep}`)) {
      files.push(filePath)
    }
  }

  return files
}

function findStaticLocaleKeyUses(): Map<string, string[]> {
  const srcDir = path.resolve(process.cwd(), 'src')
  const keyUses = new Map<string, string[]>()
  const staticKeyPattern = /(?:\b(?:t|\$t)|i18n\.global\.t)\(\s*(['"])([A-Za-z0-9_.-]+)\1\s*(?=[,)])/g

  for (const file of walkSourceFiles(srcDir)) {
    const text = fs.readFileSync(file, 'utf8')
    let match: RegExpExecArray | null
    while ((match = staticKeyPattern.exec(text))) {
      const key = match[2]
      const line = text.slice(0, match.index).split('\n').length
      const relativeLocation = `${path.relative(process.cwd(), file)}:${line}`
      const locations = keyUses.get(key) ?? []
      locations.push(relativeLocation)
      keyUses.set(key, locations)
    }
  }

  return keyUses
}

describe('used locale keys', () => {
  it.each([
    ['en', en],
    ['zh', zh]
  ])('%s locale contains every statically referenced key', (localeName, messages) => {
    const availableKeys = flattenKeys(messages as LocaleTree)
    const missingKeys = Array.from(findStaticLocaleKeyUses())
      .filter(([key]) => !availableKeys.has(key))
      .map(([key, locations]) => `${key} (${locations[0]})`)

    expect(missingKeys, `${localeName} missing locale keys`).toEqual([])
  })
})
