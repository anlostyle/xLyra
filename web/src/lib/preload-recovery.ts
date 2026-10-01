import { cacheBustedHref, clearFrontendCaches } from '@/lib/frontend-update'

export const PRELOAD_RECOVERY_KEY = 'xlyra:preload-recovery-at'
export const PRELOAD_RECOVERY_WINDOW_MS = 60_000

/** 同一窗口内只恢复一次，避免资源真的缺失时无限刷新 */
export function shouldRecoverFromPreloadError(storage: Pick<Storage, 'getItem' | 'setItem'> | null, now: number): boolean {
  if (!storage) {
    return false
  }
  try {
    const last = Number(storage.getItem(PRELOAD_RECOVERY_KEY))
    if (Number.isFinite(last) && last > 0 && now - last < PRELOAD_RECOVERY_WINDOW_MS) {
      return false
    }
    storage.setItem(PRELOAD_RECOVERY_KEY, String(now))
    return true
  } catch {
    return false
  }
}

async function resetFrontendRuntime() {
  try {
    if ('caches' in window) {
      await clearFrontendCaches(window.caches)
    }
    if ('serviceWorker' in navigator) {
      const registrations = await navigator.serviceWorker.getRegistrations()
      await Promise.all(registrations.map((registration) => registration.unregister()))
    }
  } catch {
    // 清理失败也继续刷新
  }
}

/** 懒加载页面脚本失败（旧缓存、网络抖动）时清掉缓存并刷新一次，而不是停在错误页 */
export function installPreloadErrorRecovery() {
  window.addEventListener('vite:preloadError', (event) => {
    let storage: Storage | null = null
    try {
      storage = window.sessionStorage
    } catch {
      storage = null
    }
    if (!shouldRecoverFromPreloadError(storage, Date.now())) {
      return
    }
    event.preventDefault()
    void resetFrontendRuntime().finally(() => {
      window.location.replace(cacheBustedHref(window.location.href, Date.now()))
    })
  })
}
