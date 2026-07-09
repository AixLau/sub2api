export type LocaleMessages = Record<string, any>

export function mergeLocaleMessages<T extends LocaleMessages>(
  base: T,
  ...overrides: LocaleMessages[]
): T {
  const result: LocaleMessages = { ...base }

  for (const override of overrides) {
    mergeInto(result, override)
  }

  return result as T
}

function mergeInto(target: LocaleMessages, source: LocaleMessages): void {
  for (const [key, value] of Object.entries(source)) {
    if (isPlainObject(value) && isPlainObject(target[key])) {
      mergeInto(target[key] as LocaleMessages, value)
    } else {
      target[key] = value
    }
  }
}

function isPlainObject(value: unknown): value is LocaleMessages {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}
