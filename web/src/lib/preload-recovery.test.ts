import { describe, expect, it } from 'vitest'
import { PRELOAD_RECOVERY_KEY, PRELOAD_RECOVERY_WINDOW_MS, shouldRecoverFromPreloadError } from '@/lib/preload-recovery'

function memoryStorage(initial: Record<string, string> = {}) {
  const data = new Map(Object.entries(initial))
  return {
    getItem: (key: string) => data.get(key) ?? null,
    setItem: (key: string, value: string) => void data.set(key, value),
  }
}

describe('shouldRecoverFromPreloadError', () => {
  it('recovers on the first failure and records the time', () => {
    const storage = memoryStorage()
    expect(shouldRecoverFromPreloadError(storage, 1_000_000)).toBe(true)
    expect(storage.getItem(PRELOAD_RECOVERY_KEY)).toBe('1000000')
  })

  it('does not loop within the recovery window', () => {
    const storage = memoryStorage({ [PRELOAD_RECOVERY_KEY]: '1000000' })
    expect(shouldRecoverFromPreloadError(storage, 1_000_000 + PRELOAD_RECOVERY_WINDOW_MS - 1)).toBe(false)
  })

  it('recovers again after the window has passed', () => {
    const storage = memoryStorage({ [PRELOAD_RECOVERY_KEY]: '1000000' })
    expect(shouldRecoverFromPreloadError(storage, 1_000_000 + PRELOAD_RECOVERY_WINDOW_MS)).toBe(true)
  })

  it('skips recovery when storage is unavailable', () => {
    expect(shouldRecoverFromPreloadError(null, 1)).toBe(false)
  })
})
