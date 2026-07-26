import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

/**
 * Merge Tailwind class lists with conflict resolution.
 * Used by Inspira UI-ported components under @/components/inspira.
 */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
